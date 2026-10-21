package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/presets"
	"github.com/naoray/anvil/internal/procs"
	"github.com/naoray/anvil/internal/scaffold"
)

const removeFixtureYAML = `resources:
  ports:
    - name: web
processes:
  - name: web
    command: exec sleep 60
    stop_timeout: 2s
`

func ownedDatabaseRecord(name string) config.OwnedDatabase {
	return config.OwnedDatabase{Name: name, Engine: "mysql", Role: config.DbRoleApplication,
		Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}}
}

// newRemoveFixture prepares feature-a and feature-b through the binary so
// each owns an identity, a port reservation and a runtime directory.
func newRemoveFixture(t *testing.T) *agentFixture {
	t.Helper()
	f := newAgentFixture(t, removeFixtureYAML, nil)
	f.env = subprocessEnvFrom(append(f.env, "ANVIL_PORT_RANGE=43200-43299"))
	for _, branch := range []string{"feature/a", "feature/b"} {
		_, stderr, code := f.runJSON(f.repo, "ensure", branch)
		require.Equal(t, 0, code, stderr)
	}
	return f
}

// removalSnapshot captures everything a dry run must leave untouched.
type removalSnapshot struct {
	worktree  map[string]string
	runtime   map[string]string
	worktrees string
	branches  string
}

func takeRemovalSnapshot(t *testing.T, f *agentFixture, wt string) removalSnapshot {
	t.Helper()
	return removalSnapshot{
		worktree:  snapshotTree(t, wt),
		runtime:   snapshotTree(t, f.runtimeRoot()),
		worktrees: gitRun(t, f.repo, "worktree", "list", "--porcelain"),
		branches:  gitRun(t, f.repo, "branch", "--list"),
	}
}

func TestRemoveDeletedSiblingDoesNotBlockRemoval(t *testing.T) {
	f := newRemoveFixture(t)
	a, b := f.worktreePath("feature-a"), f.worktreePath("feature-b")
	require.NoError(t, config.WriteLocalState(a, config.LocalState{Databases: []config.OwnedDatabase{ownedDatabaseRecord("demo_a")}}))
	require.NoError(t, os.RemoveAll(b))

	stdout, stderr, code := f.run(f.repo, "remove", "feature-a", "--dry-run", "--force")
	require.Equal(t, 0, code, stdout+stderr)
	require.Contains(t, stdout+stderr, "git worktree prune")
	require.Contains(t, stdout+stderr, "git worktree repair")
	require.Contains(t, stdout+stderr, "⚠ registered worktree "+b, "printed as a warning")
	require.NotContains(t, stdout+stderr, "WARNING: registered worktree")

	envelope, stderr, code := f.runJSON(f.repo, "remove", "feature-a", "--dry-run")
	require.Equal(t, 0, code, stderr)
	require.Contains(t, strings.Join(stringList(t, envelope["warnings"]), "\n"), "git worktree prune")
	require.Contains(t, strings.Join(stringList(t, envelope["warnings"]), "\n"), b)

	stdout, stderr, code = f.run(f.repo, "remove", "feature-a", "--force")
	require.Equal(t, 0, code, stdout+stderr)
	require.Contains(t, stdout+stderr, "git worktree prune")
	require.NoDirExists(t, a)
}

// Git never prunes a locked entry, so a locked worktree on unmounted media is
// expected back with its borrow record: removal must not drop its database.
func TestRemoveFailsClosedForMissingLockedSibling(t *testing.T) {
	f := newRemoveFixture(t)
	a, b := f.worktreePath("feature-a"), f.worktreePath("feature-b")
	require.NoError(t, config.WriteLocalState(a, config.LocalState{Databases: []config.OwnedDatabase{ownedDatabaseRecord("demo_a")}}))
	gitRun(t, f.repo, "worktree", "lock", "--reason", "usb", b)
	require.NoError(t, os.RemoveAll(b))

	stdout, stderr, code := f.run(f.repo, "remove", "feature-a", "--force")
	require.NotEqual(t, 0, code)
	require.Contains(t, stdout+stderr, "registered worktree "+b+" is locked (usb) but absent; reconnect it, or unlock and prune it")
	require.DirExists(t, a)

	envelope, _, code := f.runJSON(f.repo, "remove", "feature-a", "--dry-run")
	require.Equal(t, 7, code)
	require.Equal(t, "cleanup_blocked", field(t, envelope, "error", "code"))
	require.DirExists(t, a)
}

// A removal that deletes the worktree but then fails must still report it.
func TestRemoveLifecycleReportsRemovedOnLaterFailure(t *testing.T) {
	f := newRemoveFixture(t)
	a := f.worktreePath("feature-a")
	root := f.runtimeRoot()
	t.Setenv("ANVIL_RUNTIME_ROOT", root)
	pc := &ProjectContext{GitDir: filepath.Join(f.repo, ".git"), Config: &config.Config{}}
	deps := removeLifecycleDependencies{
		resolvePreset:   func(*ProjectContext, string, string) presets.ResolvedPreset { return presets.ResolvedPreset{} },
		scaffoldManager: func(*ProjectContext) *scaffold.ScaffoldManager { return scaffold.NewScaffoldManager() },
		removeWorktree: func(gitDir, path string, force bool) error {
			if err := git.RemoveWorktree(gitDir, path, force); err != nil {
				return err
			}
			// Port release after removal now fails.
			return os.WriteFile(filepath.Join(root, "ports.json"), []byte("{not json"), 0o600)
		},
		printInfo: func(string) {},
	}
	report := &removeReport{data: &removeData{}}
	err := runRemoveLifecycle(pc, git.Worktree{Path: a, Branch: "feature/a"}, removeLifecycleOptions{Quiet: true, Report: report}, scaffold.CleanupOptions{}, nil, deps)
	require.ErrorContains(t, err, "worktree removed, but runtime cleanup failed")
	require.NoDirExists(t, a)
	require.True(t, report.data.Removed)
}

func TestRemoveFailsClosedForUnreadableOrMalformedSibling(t *testing.T) {
	for name, corrupt := range map[string]func(t *testing.T, path string){
		// A directory in place of .anvil.local is unreadable even for root.
		"unreadable": func(t *testing.T, path string) {
			require.NoError(t, os.Remove(path))
			require.NoError(t, os.Mkdir(path, 0o755))
		},
		"malformed": func(t *testing.T, path string) {
			require.NoError(t, os.WriteFile(path, []byte("bad: ["), 0o600))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newRemoveFixture(t)
			a, b := f.worktreePath("feature-a"), f.worktreePath("feature-b")
			require.NoError(t, config.WriteLocalState(a, config.LocalState{Databases: []config.OwnedDatabase{ownedDatabaseRecord("demo_a")}}))
			corrupt(t, filepath.Join(b, config.LocalStateFile))

			stdout, stderr, code := f.run(f.repo, "remove", "feature-a", "--force")
			require.NotEqual(t, 0, code)
			require.Contains(t, stdout+stderr, "cannot inspect sibling")
			require.DirExists(t, a)

			envelope, _, code := f.runJSON(f.repo, "remove", "feature-a", "--force")
			require.Equal(t, 7, code)
			require.Equal(t, "cleanup_blocked", field(t, envelope, "error", "code"))
			require.DirExists(t, a)
		})
	}
}

func TestRemoveDryRunIsNonInteractiveAndMutatesNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("managed processes require macOS or Linux")
	}
	f := newRemoveFixture(t)
	a := f.worktreePath("feature-a")
	require.NoError(t, config.WriteLocalState(a, config.LocalState{Databases: []config.OwnedDatabase{ownedDatabaseRecord("demo_a")}}))
	envelope, stderr, code := f.runJSON(f.repo, "start", "feature-a")
	require.Equal(t, 0, code, stderr)
	pid := int(field(t, envelope, "data", "processes", 0, "pid").(float64))
	t.Cleanup(func() {
		if _, _, code := f.run(f.repo, "stop", "feature-a"); code != 0 {
			t.Logf("stopping fixture process %d failed", pid)
		}
	})
	before := takeRemovalSnapshot(t, f, a)

	// Ordinary dry run: CI=true, stdin is not a TTY, no --force.
	stdout, stderr, code := f.run(f.repo, "remove", "feature-a", "--dry-run", "--delete-branch")
	require.Equal(t, 0, code, stdout+stderr)
	combined := stdout + stderr
	require.NotContains(t, combined, "requires confirmation")
	require.Contains(t, combined, "Would stop managed process web")
	require.Contains(t, combined, "Would remove feature/a")
	require.Contains(t, combined, "[DRY RUN] Would delete branch")
	require.Equal(t, before, takeRemovalSnapshot(t, f, a), "ordinary dry run must not mutate anything")

	// JSON dry run: exactly one envelope describing the plan.
	envelope, stderr, code = f.runJSON(f.repo, "remove", "feature-a", "--dry-run", "--delete-branch")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, envelope["ok"])
	require.Equal(t, "remove", envelope["command"])
	require.Equal(t, true, envelope["mutating"])
	require.Equal(t, true, field(t, envelope, "data", "dry_run"))
	require.Equal(t, false, field(t, envelope, "data", "removed"))
	require.Equal(t, "feature/a", field(t, envelope, "data", "worktree", "branch"))
	require.Equal(t, a, field(t, envelope, "data", "worktree", "path"))
	require.Equal(t, "web", field(t, envelope, "data", "stop_processes", 0, "name"))
	require.Equal(t, float64(pid), field(t, envelope, "data", "stop_processes", 0, "pid"))
	require.Equal(t, "web", field(t, envelope, "data", "release_ports", 0, "name"))
	require.NotEmpty(t, field(t, envelope, "data", "remove_runtime_dir"))
	require.Equal(t, "untouched", field(t, envelope, "data", "databases", "action"))
	require.Equal(t, "demo_a", field(t, envelope, "data", "databases", "names", 0))
	require.Equal(t, "feature/a", field(t, envelope, "data", "branch", "name"))
	require.Equal(t, true, field(t, envelope, "data", "branch", "delete"))
	require.Equal(t, false, field(t, envelope, "data", "branch", "deleted"))
	require.Equal(t, before, takeRemovalSnapshot(t, f, a), "JSON dry run must not mutate anything")

	_, err := procs.StartIdentity(pid)
	require.NoError(t, err, "dry runs never stop processes")
}

// databases.suffix appears only when legacy suffix-based cleanup applies: a
// db_suffix, no exact records and a db.destroy step that would act.
func TestRemoveJSONReportsSuffixOnlyForLegacyCleanup(t *testing.T) {
	for name, tc := range map[string]struct {
		yaml    string
		records bool
		keepDB  bool
		suffix  any
	}{
		"no db.destroy step":    {yaml: "", suffix: nil},
		"exact records":         {yaml: "cleanup:\n  steps:\n    - name: db.destroy\n", records: true, suffix: nil},
		"keep-db":               {yaml: "cleanup:\n  steps:\n    - name: db.destroy\n", keepDB: true, suffix: nil},
		"legacy suffix cleanup": {yaml: "cleanup:\n  steps:\n    - name: db.destroy\n", suffix: "legacy_sfx"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newAgentFixture(t, tc.yaml, nil)
			wt := f.worktreePath("feature-x")
			require.NoError(t, os.MkdirAll(filepath.Dir(wt), 0o755))
			gitRun(t, f.repo, "worktree", "add", "-b", "feature/x", wt)
			state := config.LocalState{DbSuffix: "legacy_sfx"}
			if tc.records {
				state.Databases = []config.OwnedDatabase{ownedDatabaseRecord("demo_x")}
			}
			require.NoError(t, config.WriteLocalState(wt, state))
			args := []string{"remove", "feature-x", "--dry-run"}
			if tc.keepDB {
				args = append(args, "--keep-db")
			}
			envelope, stderr, code := f.runJSON(f.repo, args...)
			require.Equal(t, 0, code, stderr)
			databases := field(t, envelope, "data", "databases").(map[string]any)
			require.Equal(t, tc.suffix, databases["suffix"])
		})
	}
}

func TestRemoveJSONNeverPrompts(t *testing.T) {
	f := newRemoveFixture(t)
	a := f.worktreePath("feature-a")

	envelope, _, code := f.runJSON(f.repo, "remove", "feature-a")
	require.Equal(t, 2, code)
	require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
	require.Contains(t, field(t, envelope, "error", "message"), "--force")
	require.DirExists(t, a)

	envelope, _, code = f.runJSON(f.repo, "remove", "--dry-run")
	require.Equal(t, 2, code)
	require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))

	envelope, _, code = f.runJSON(f.repo, "remove", "no-such-worktree", "--dry-run")
	require.Equal(t, 3, code)
	require.Equal(t, "worktree_not_found", field(t, envelope, "error", "code"))

	envelope, _, code = f.runJSON(f.repo, "remove", "feature-a", "--force", "--delete-branch")
	require.Equal(t, 0, code)
	require.Equal(t, false, field(t, envelope, "data", "dry_run"))
	require.Equal(t, true, field(t, envelope, "data", "removed"))
	require.Equal(t, true, field(t, envelope, "data", "branch", "deleted"))
	require.NoDirExists(t, a)
	require.Empty(t, gitRun(t, f.repo, "branch", "--list", "feature/a"))
}

func TestPruneDryRunNeitherPromptsNorFetches(t *testing.T) {
	f := newAgentFixture(t, "", nil)
	// origin is unreachable: a fetch would fail the project.
	gitRun(t, f.repo, "remote", "add", "origin", filepath.Join(t.TempDir(), "unreachable.git"))
	gitRun(t, f.repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	wt := f.worktreePath("feature-merged")
	require.NoError(t, os.MkdirAll(filepath.Dir(wt), 0o755))
	gitRun(t, f.repo, "worktree", "add", "-b", "feature/merged", wt)
	before := snapshotTree(t, wt)
	worktrees := gitRun(t, f.repo, "worktree", "list", "--porcelain")

	stdout, stderr, code := f.run(f.repo, "prune", "--dry-run")
	combined := stdout + stderr
	require.Equal(t, 0, code, combined)
	require.Contains(t, combined, "feature/merged is merged")
	require.Contains(t, combined, "[DRY RUN] Would remove 1 worktree(s)")
	require.Contains(t, combined, "against the current origin/main without fetching")
	require.NotContains(t, combined, "git fetch")
	require.NoFileExists(t, filepath.Join(f.repo, ".git", "FETCH_HEAD"))
	require.Equal(t, before, snapshotTree(t, wt))
	require.Equal(t, worktrees, gitRun(t, f.repo, "worktree", "list", "--porcelain"))
	require.Contains(t, gitRun(t, f.repo, "branch", "--list"), "feature/merged")
}

func TestPruneDryRunRequiresRemoteTrackingRef(t *testing.T) {
	f := newAgentFixture(t, "", nil)
	wt := f.worktreePath("feature-x")
	require.NoError(t, os.MkdirAll(filepath.Dir(wt), 0o755))
	gitRun(t, f.repo, "worktree", "add", "-b", "feature/x", wt)

	stdout, stderr, code := f.run(f.repo, "prune", "--dry-run")
	require.NotEqual(t, 0, code)
	require.Contains(t, stdout+stderr, "no origin/main ref; run git fetch (or prune without --dry-run) first")
	require.DirExists(t, wt)
}

func TestUnlinkCleanDryRunNeverPrompts(t *testing.T) {
	f := newRemoveFixture(t)
	a := f.worktreePath("feature-a")
	before := takeRemovalSnapshot(t, f, a)

	stdout, stderr, code := f.run(f.repo, "unlink", "demo", "--clean", "--dry-run")
	require.Equal(t, 0, code, stdout+stderr)
	require.Contains(t, stdout+stderr, "[DRY RUN] Would unlink project after worktree cleanup")
	require.Equal(t, before, takeRemovalSnapshot(t, f, a))
	require.DirExists(t, f.worktreePath("feature-b"))
	stdout, stderr, code = f.run(f.repo, "context", "feature-a", "--json")
	require.Equal(t, 0, code, stdout+stderr, "project stays linked")
}

func stringList(t *testing.T, value any) []string {
	t.Helper()
	items, ok := value.([]any)
	require.True(t, ok, "expected array, got %T", value)
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.(string)
	}
	return out
}
