package checks

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/workenv"
	"github.com/naoray/anvil/internal/wtstate"
)

// crossFixture is one repository with several linked worktrees sharing a
// runtime root, as Anvil lays them out.
type crossFixture struct {
	t       *testing.T
	repo    string
	base    string
	root    string
	counter string
	common  string
	shared  string
}

type crossWorktree struct {
	name    string
	path    string
	id      string
	runtime string
	role    string
	port    int
	// declared overrides or adds declared (project) variables.
	declared map[string]string
	policy   ReusePolicy
}

func newCrossFixture(t *testing.T) *crossFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("check commands use sh")
	}
	repo := canonicalTemp(t)
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "t@e.st"}, {"config", "user.name", "T"}} {
		crossGit(t, repo, args...)
	}
	require.NoError(t, os.WriteFile(filepath.Join(repo, "src.txt"), []byte("v1"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".anvil.local\nlocal/\n"), 0o644))
	crossGit(t, repo, "add", ".")
	crossGit(t, repo, "commit", "-m", "init")
	shared := canonicalTemp(t)
	require.NoError(t, os.WriteFile(filepath.Join(shared, "SHA256SUMS"), []byte("model-v1"), 0o644))
	f := &crossFixture{t: t, repo: repo, base: canonicalTemp(t), root: canonicalTemp(t), shared: shared,
		counter: filepath.Join(canonicalTemp(t), "runs.count"), common: filepath.Join(repo, ".git")}
	return f
}

func canonicalTemp(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func crossGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func (f *crossFixture) add(name, role string, port int) *crossWorktree {
	f.t.Helper()
	path := filepath.Join(f.base, name)
	crossGit(f.t, f.repo, "worktree", "add", "--detach", path, "HEAD")
	identity, _, err := wtstate.EnsureIdentity(path)
	require.NoError(f.t, err)
	if role != "" {
		_, err := wtstate.UpdateMetadata(path, wtstate.MetadataUpdate{Role: role}, time.Now())
		require.NoError(f.t, err)
	}
	w := &crossWorktree{name: name, path: path, id: identity.ID, runtime: filepath.Join(f.root, "worktrees", identity.ID), role: role, port: port}
	require.NoError(f.t, os.MkdirAll(w.runtime, 0o700))
	return w
}

// env mirrors workenv.Build: worktree-specific Anvil values, resources,
// databases and declared project variables derived from them.
func (f *crossFixture) env(w *crossWorktree) *workenv.Environment {
	env := &workenv.Environment{}
	set := func(source, name, value string) {
		env.Set(workenv.Variable{Name: name, Value: value, Source: source, Secret: workenv.IsSecretName(name, nil)})
	}
	set(workenv.SourceAnvil, "ANVIL", "1")
	set(workenv.SourceAnvil, "ANVIL_WORKTREE_ID", w.id)
	set(workenv.SourceAnvil, "ANVIL_WORKTREE_PATH", w.path)
	set(workenv.SourceAnvil, "ANVIL_WORKTREE_NAME", w.name)
	set(workenv.SourceAnvil, "ANVIL_BRANCH", "")
	set(workenv.SourceAnvil, "ANVIL_PROJECT", "demo")
	set(workenv.SourceAnvil, "ANVIL_INSTANCE", workenv.InstanceSlug("demo", w.name, w.id))
	database := "demo_" + strings.TrimPrefix(w.id, "wt_")
	set(workenv.SourceDatabase, "ANVIL_DB_DATABASE", database)
	set(workenv.SourceResource, "ANVIL_RUNTIME_DIR", w.runtime)
	set(workenv.SourceResource, "ANVIL_ARTIFACTS_DIR", filepath.Join(w.runtime, "artifacts"))
	set(workenv.SourceResource, "ANVIL_PORT_WEB", strconv.Itoa(w.port))
	set(workenv.SourceResource, "PORT", strconv.Itoa(w.port))
	declared := map[string]string{
		"APP_URL":     "http://127.0.0.1:" + strconv.Itoa(w.port) + "/",
		"CACHE":       filepath.Join(w.runtime, "cache"),
		"ROOT":        w.path,
		"DB_DATABASE": database,
		"DSN":         "mysql://root@127.0.0.1/" + database,
		"COUNT":       f.counter,
		"API_TOKEN":   "secret-" + w.id,
	}
	for key, value := range w.declared {
		declared[key] = value
	}
	for _, key := range sortedNames(declared) {
		set(workenv.SourceProject, key, declared[key])
	}
	return env
}

func sortedNames(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (f *crossFixture) request(w *crossWorktree, check config.CheckConfig, misses *[]string) RunRequest {
	return RunRequest{
		Check: check, Worktree: WorktreeRef{ID: w.id, Path: w.path}, RuntimeDir: w.runtime, RuntimeRoot: f.root,
		Env: f.env(w), Reuse: true, CommonDir: f.common,
		Producer:    Producer{WorktreeID: w.id, Path: w.path, Role: w.role, LeaseOwner: "agent-" + w.name},
		ReusePolicy: w.policy,
		Resolve: func(config.CheckCommand) (CommandTarget, error) {
			return CommandTarget{Dir: w.path, Env: f.env(w)}, nil
		},
		Resources: []SharedResource{{Name: "models", Path: f.shared, Identity: config.SharedIdentity{File: "SHA256SUMS"}}},
		ReuseMiss: func(reason string) {
			if misses != nil {
				*misses = append(*misses, reason)
			}
		},
	}
}

func (f *crossFixture) run(w *crossWorktree, check config.CheckConfig) (*Result, []string) {
	f.t.Helper()
	var misses []string
	result, err := Run(context.Background(), f.request(w, check, &misses))
	require.NoError(f.t, err)
	require.Equal(f.t, StatusPassed, result.Status, "%+v", result)
	return result, misses
}

func (f *crossFixture) runs() int {
	content, err := os.ReadFile(f.counter)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(f.t, err)
	return strings.Count(string(content), "run\n")
}

func reusableCheck() config.CheckConfig {
	return config.CheckConfig{Name: "fast", Reusable: true, Commands: cmds(
		`echo run >> "$COUNT"`,
		`[ -z "$DIRTY_ME" ] || echo dirtied >> src.txt`,
		`mkdir -p "$ANVIL_ARTIFACTS_DIR" && echo report > "$ANVIL_ARTIFACTS_DIR/report.txt"`,
	), Artifacts: []string{"${ANVIL_ARTIFACTS_DIR}/report.txt"}}
}

func TestCrossWorktreeReuseIdentifiesProducer(t *testing.T) {
	f := newCrossFixture(t)
	author := f.add("author", wtstate.RoleImplementation, 41001)
	review := f.add("review", wtstate.RoleReview, 41002)
	check := reusableCheck()

	produced, _ := f.run(author, check)
	require.False(t, produced.Reused)
	require.Nil(t, produced.ReusedFrom)
	require.Equal(t, &Producer{WorktreeID: author.id, Path: author.path, Role: wtstate.RoleImplementation, LeaseOwner: "agent-author"}, produced.Producer)
	require.NotEmpty(t, produced.Identity.ContentDigest)
	require.NotEmpty(t, produced.Identity.Git.Tree)
	require.Equal(t, 1, f.runs())

	reused, misses := f.run(review, check)
	require.Empty(t, misses)
	require.True(t, reused.Reused)
	require.Equal(t, 1, f.runs(), "nothing executed in the review worktree")
	require.Equal(t, &ReusedFrom{WorktreeID: author.id, Path: author.path, Role: wtstate.RoleImplementation, LeaseOwner: "agent-author", RunID: produced.RunID, Check: "fast"}, reused.ReusedFrom)
	require.Equal(t, review.id, reused.Worktree.ID, "the consumer record carries the consumer's own identity")
	require.Empty(t, reused.Commands)
	require.NotEqual(t, produced.Identity.Digest, reused.Identity.Digest, "worktree-specific values differ")
	require.Equal(t, produced.Identity.ContentDigest, reused.Identity.ContentDigest, "content identity normalizes them")

	latest, err := Store{RuntimeDir: review.runtime}.Latest("fast")
	require.NoError(t, err)
	require.Equal(t, reused.RunID, latest.RunID)
	current, err := ComputeIdentity(context.Background(), IdentityRequest{WorktreePath: review.path, RuntimeDir: review.runtime, Check: check, Env: f.env(review),
		Resources: f.request(review, check, nil).Resources})
	require.NoError(t, err)
	assessment := Assess(latest, current)
	require.True(t, assessment.PassingAndCurrent, "%v", assessment.Reasons)
	require.Equal(t, reused.ReusedFrom, assessment.ReusedFrom)

	again, misses := f.run(review, check)
	require.Empty(t, misses)
	require.True(t, again.Reused)
	require.Equal(t, reused.ReusedFrom, again.ReusedFrom, "the consumer record keeps its provenance")
	require.Equal(t, 1, f.runs())

	self, misses := f.run(author, check)
	require.Empty(t, misses)
	require.True(t, self.Reused)
	require.Equal(t, author.id, self.ReusedFrom.WorktreeID, "same-worktree reuse names the worktree itself")
	require.Equal(t, produced.RunID, self.ReusedFrom.RunID)
}

func TestCrossWorktreeReuseIdentityChanges(t *testing.T) {
	cases := []struct {
		name string
		// change prepares the consumer (and may change the check it runs).
		change func(t *testing.T, f *crossFixture, consumer *crossWorktree, check *config.CheckConfig)
		reused bool
	}{
		{name: "unchanged", reused: true, change: func(*testing.T, *crossFixture, *crossWorktree, *config.CheckConfig) {}},
		{name: "ambient inherited variable", reused: true, change: func(t *testing.T, _ *crossFixture, _ *crossWorktree, _ *config.CheckConfig) {
			t.Setenv("CLAUDE_SESSION_ID", "consumer-session")
		}},
		{name: "toolchain-affecting inherited variable", change: func(t *testing.T, _ *crossFixture, _ *crossWorktree, _ *config.CheckConfig) {
			t.Setenv("GOFLAGS", "-mod=mod")
		}},
		{name: "definition", change: func(_ *testing.T, _ *crossFixture, _ *crossWorktree, check *config.CheckConfig) {
			check.Description = "changed"
		}},
		{name: "inputs", change: func(t *testing.T, _ *crossFixture, consumer *crossWorktree, _ *config.CheckConfig) {
			require.NoError(t, os.WriteFile(filepath.Join(consumer.path, "local", "fixture.txt"), []byte("other"), 0o644))
		}},
		{name: "declared environment", change: func(_ *testing.T, _ *crossFixture, consumer *crossWorktree, _ *config.CheckConfig) {
			consumer.declared = map[string]string{"MODE": "other"}
		}},
		{name: "identity_env variable", change: func(t *testing.T, _ *crossFixture, _ *crossWorktree, _ *config.CheckConfig) {
			t.Setenv("FEATURE_FLAG", "off")
		}},
		{name: "toolchain", change: func(t *testing.T, _ *crossFixture, consumer *crossWorktree, _ *config.CheckConfig) {
			require.NoError(t, os.WriteFile(filepath.Join(consumer.path, "local", "tool.version"), []byte("tool 2"), 0o644))
		}},
		{name: "shared resource", change: func(t *testing.T, f *crossFixture, _ *crossWorktree, _ *config.CheckConfig) {
			require.NoError(t, os.WriteFile(filepath.Join(f.shared, "SHA256SUMS"), []byte("model-v2"), 0o644))
		}},
		{name: "tree", change: func(t *testing.T, _ *crossFixture, consumer *crossWorktree, _ *config.CheckConfig) {
			require.NoError(t, os.WriteFile(filepath.Join(consumer.path, "src.txt"), []byte("v2"), 0o644))
			crossGit(t, consumer.path, "commit", "-qam", "v2")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLAUDE_SESSION_ID", "producer-session")
			t.Setenv("FEATURE_FLAG", "on")
			t.Setenv("GOFLAGS", "-mod=readonly")
			f := newCrossFixture(t)
			author := f.add("author", wtstate.RoleImplementation, 41001)
			review := f.add("review", wtstate.RoleReview, 41002)
			for _, w := range []*crossWorktree{author, review} {
				require.NoError(t, os.MkdirAll(filepath.Join(w.path, "local"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(w.path, "local", "fixture.txt"), []byte("same"), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(w.path, "local", "tool.version"), []byte("tool 1"), 0o644))
				w.declared = map[string]string{"MODE": "fast"}
			}
			check := reusableCheck()
			check.Inputs = []string{"local/fixture.txt"}
			check.Toolchain = []string{"cat local/tool.version"}
			check.Resources = []string{"models"}
			check.IdentityEnv = []string{"FEATURE_FLAG"}
			f.run(author, check)

			consumerCheck := check
			tc.change(t, f, review, &consumerCheck)
			result, misses := f.run(review, consumerCheck)
			require.Equal(t, tc.reused, result.Reused, "misses: %v", misses)
			if tc.reused {
				require.Equal(t, 1, f.runs())
				require.Equal(t, author.id, result.ReusedFrom.WorktreeID)
			} else {
				require.Equal(t, 2, f.runs())
				require.Len(t, misses, 1)
				require.Nil(t, result.ReusedFrom)
			}
		})
	}
}

func TestCrossWorktreeReuseTrustPolicies(t *testing.T) {
	cases := []struct {
		name     string
		role     string
		policy   ReusePolicy
		reused   bool
		reasonIn string
	}{
		{name: "any", role: wtstate.RoleReview, policy: ReusePolicy{Trust: TrustAny}, reused: true},
		{name: "default is any", role: wtstate.RoleReview, reused: true},
		{name: "same-worktree", role: wtstate.RoleReview, policy: ReusePolicy{Trust: TrustSameWorktree}, reasonIn: "same-worktree"},
		{name: "same-role refuses another role", role: wtstate.RoleReview, policy: ReusePolicy{Trust: TrustSameRole}, reasonIn: "role"},
		{name: "same-role accepts the same role", role: wtstate.RoleImplementation, policy: ReusePolicy{Trust: TrustSameRole}, reused: true},
		{name: "exclude implementation", role: wtstate.RoleReview, policy: ReusePolicy{Trust: TrustAny, ExcludeRoles: []string{wtstate.RoleImplementation}}, reasonIn: "excluded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCrossFixture(t)
			author := f.add("author", wtstate.RoleImplementation, 41001)
			consumer := f.add("consumer", tc.role, 41002)
			consumer.policy = tc.policy
			check := reusableCheck()
			f.run(author, check)
			result, misses := f.run(consumer, check)
			require.Equal(t, tc.reused, result.Reused, "misses: %v", misses)
			if !tc.reused {
				require.Len(t, misses, 1)
				require.Contains(t, misses[0], tc.reasonIn)
			}
		})
	}

	t.Run("a stored consumer record obeys a later restriction", func(t *testing.T) {
		f := newCrossFixture(t)
		author := f.add("author", wtstate.RoleImplementation, 41001)
		review := f.add("review", wtstate.RoleReview, 41002)
		check := reusableCheck()
		f.run(author, check)
		result, _ := f.run(review, check)
		require.True(t, result.Reused)
		review.policy = ReusePolicy{ExcludeRoles: []string{wtstate.RoleImplementation}}
		result, misses := f.run(review, check)
		require.False(t, result.Reused)
		require.Contains(t, strings.Join(misses, "\n"), "excluded")
		require.Equal(t, 2, f.runs())
	})
}

func TestCrossWorktreeReuseRefusesUnsafeProducers(t *testing.T) {
	cases := []struct {
		name     string
		before   func(t *testing.T, f *crossFixture, producer *crossWorktree)
		after    func(t *testing.T, f *crossFixture, producer, consumer *crossWorktree)
		reasonIn string
	}{
		{name: "dirty producer", before: func(t *testing.T, _ *crossFixture, producer *crossWorktree) {
			require.NoError(t, os.WriteFile(filepath.Join(producer.path, "wip.txt"), []byte("wip"), 0o644))
		}, after: func(t *testing.T, _ *crossFixture, producer, _ *crossWorktree) {
			require.NoError(t, os.Remove(filepath.Join(producer.path, "wip.txt")))
		}, reasonIn: "no producer recorded at this content identity"},
		{name: "producer dirtied by its own run", before: func(t *testing.T, _ *crossFixture, _ *crossWorktree) {
			t.Setenv("DIRTY_ME", "1") // inherited and unlisted: not part of the content identity
		}, after: func(t *testing.T, _ *crossFixture, producer, _ *crossWorktree) {
			require.NoError(t, os.Unsetenv("DIRTY_ME"))
			crossGit(t, producer.path, "checkout", "--", "src.txt")
		}, reasonIn: "no producer recorded at this content identity"},
		{name: "dirty consumer", after: func(t *testing.T, _ *crossFixture, _, consumer *crossWorktree) {
			require.NoError(t, os.WriteFile(filepath.Join(consumer.path, "wip.txt"), []byte("wip"), 0o644))
		}, reasonIn: "uncommitted changes"},
		{name: "missing artifact", after: func(t *testing.T, _ *crossFixture, producer, _ *crossWorktree) {
			require.NoError(t, os.Remove(filepath.Join(producer.runtime, "artifacts", "report.txt")))
		}, reasonIn: "missing"},
		{name: "changed artifact", after: func(t *testing.T, _ *crossFixture, producer, _ *crossWorktree) {
			require.NoError(t, os.WriteFile(filepath.Join(producer.runtime, "artifacts", "report.txt"), []byte("tampered"), 0o644))
		}, reasonIn: "changed"},
		{name: "unregistered producer", after: func(t *testing.T, f *crossFixture, producer, _ *crossWorktree) {
			crossGit(t, f.repo, "worktree", "remove", "--force", producer.path)
		}, reasonIn: "registered"},
		{name: "producer identity changed", after: func(t *testing.T, _ *crossFixture, producer, _ *crossWorktree) {
			require.NoError(t, os.Remove(filepath.Join(producer.path, ".anvil.local")))
			_, _, err := wtstate.EnsureIdentity(producer.path)
			require.NoError(t, err)
		}, reasonIn: "identity"},
		{name: "index entry outside the producer's runtime directory", after: func(t *testing.T, f *crossFixture, producer, _ *crossWorktree) {
			entries, err := filepath.Glob(filepath.Join(f.root, "reuse", "*", "fast", "*", producer.id+".json"))
			require.NoError(t, err)
			require.Len(t, entries, 1)
			content, err := os.ReadFile(entries[0])
			require.NoError(t, err)
			var entry map[string]any
			require.NoError(t, json.Unmarshal(content, &entry))
			stray := filepath.Join(f.root, "stray", "result.json")
			resultContent, err := os.ReadFile(entry["result"].(string))
			require.NoError(t, err)
			require.NoError(t, os.MkdirAll(filepath.Dir(stray), 0o700))
			require.NoError(t, os.WriteFile(stray, resultContent, 0o600))
			entry["result"] = stray
			content, err = json.Marshal(entry)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(entries[0], content, 0o600))
		}, reasonIn: "outside"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCrossFixture(t)
			author := f.add("author", wtstate.RoleImplementation, 41001)
			review := f.add("review", wtstate.RoleReview, 41002)
			check := reusableCheck()
			if tc.before != nil {
				tc.before(t, f, author)
			}
			f.run(author, check)
			if tc.after != nil {
				tc.after(t, f, author, review)
			}
			result, misses := f.run(review, check)
			require.False(t, result.Reused)
			require.Len(t, misses, 1)
			require.Contains(t, misses[0], tc.reasonIn)
			require.Equal(t, 2, f.runs())
		})
	}
}

func TestContentEnvironmentNormalization(t *testing.T) {
	f := newCrossFixture(t)
	a := f.add("a", wtstate.RoleImplementation, 41001)
	b := f.add("b", wtstate.RoleReview, 41002)
	check := reusableCheck()
	identity := func(w *crossWorktree) Identity {
		t.Helper()
		id, err := ComputeIdentity(context.Background(), IdentityRequest{WorktreePath: w.path, RuntimeDir: w.runtime, Check: check, Env: f.env(w)})
		require.NoError(t, err)
		return id
	}
	require.NotEqual(t, identity(a).EnvDigest, identity(b).EnvDigest, "the full environment digest stays exact")
	require.Equal(t, identity(a).ContentEnvDigest, identity(b).ContentEnvDigest)

	// A declared value that merely looks like another worktree's path is not
	// normalized: only this worktree's own values are.
	b.declared = map[string]string{"ROOT": a.path}
	require.NotEqual(t, identity(a).ContentEnvDigest, identity(b).ContentEnvDigest)
	b.declared = map[string]string{"ROOT": b.path + "-old"}
	require.NotEqual(t, identity(a).ContentEnvDigest, identity(b).ContentEnvDigest, "paths are replaced only at path boundaries")
}

func TestCrossWorktreeReuseNormalizesToolchainPaths(t *testing.T) {
	f := newCrossFixture(t)
	author := f.add("author", wtstate.RoleImplementation, 41001)
	review := f.add("review", wtstate.RoleReview, 41002)
	check := reusableCheck()
	check.Toolchain = []string{"pwd", `echo "$ANVIL_RUNTIME_DIR/bin/tool"`}
	produced, _ := f.run(author, check)
	result, misses := f.run(review, check)
	require.True(t, result.Reused, "misses: %v", misses)
	require.NotEqual(t, produced.Identity.ToolchainDigest, result.Identity.ToolchainDigest, "the exact identity keeps raw probe output")
	require.Equal(t, produced.Identity.ContentToolchainDigest, result.Identity.ContentToolchainDigest)
}

func TestReuseIndexForgetsVanishedResults(t *testing.T) {
	entries := func(f *crossFixture, id string) []string {
		t.Helper()
		found, err := filepath.Glob(filepath.Join(f.root, "reuse", "*", "fast", "*", id+".json"))
		require.NoError(t, err)
		return found
	}
	t.Run("lookup removes an entry whose result is gone", func(t *testing.T) {
		f := newCrossFixture(t)
		author := f.add("author", wtstate.RoleImplementation, 41001)
		review := f.add("review", wtstate.RoleReview, 41002)
		produced, _ := f.run(author, reusableCheck())
		require.Len(t, entries(f, author.id), 1)
		require.NoError(t, os.RemoveAll(produced.RunDir))
		result, misses := f.run(review, reusableCheck())
		require.False(t, result.Reused)
		require.Contains(t, strings.Join(misses, "\n"), "no longer exists")
		require.Empty(t, entries(f, author.id), "the dangling entry was removed")
	})
	t.Run("releasing a worktree removes its entries", func(t *testing.T) {
		f := newCrossFixture(t)
		author := f.add("author", wtstate.RoleImplementation, 41001)
		other := f.add("other", wtstate.RoleImplementation, 41003)
		f.run(author, reusableCheck())
		check := reusableCheck()
		check.Description = "other definition"
		f.run(other, check)
		require.NoError(t, ForgetWorktree(f.root, author.id))
		require.Empty(t, entries(f, author.id))
		require.Len(t, entries(f, other.id), 1, "other worktrees' entries stay")
		dirs, err := filepath.Glob(filepath.Join(f.root, "reuse", "*", "fast", "[0-9a-f]*"))
		require.NoError(t, err)
		require.Len(t, dirs, 1, "empty content directories are removed")
	})
}
