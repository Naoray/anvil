package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/lockfile"
)

const crossReuseFixtureYAML = `env:
  APP_URL: http://127.0.0.1:${ANVIL_PORT_WEB}/
  APP_ROOT: ${ANVIL_WORKTREE_PATH}
  APP_CACHE: ${ANVIL_RUNTIME_DIR}/cache
  MODE: ${CHECK_MODE}
resources:
  ports:
    - name: web
checks:
  fast:
    reusable: true
    commands:
      - echo run >> "$RUN_LOG"
`

func TestCheckReuseAcrossWorktreesAtTheSameContent(t *testing.T) {
	skipWithoutCheckExecution(t)
	runLog := filepath.Join(t.TempDir(), "runs.log")
	f := newAgentFixture(t, crossReuseFixtureYAML, nil)
	f = withEnv(f, "RUN_LOG="+runLog, "CHECK_MODE=fast", "CLAUDE_SESSION_ID=author-session")
	runs := func() int {
		content, err := os.ReadFile(runLog)
		if os.IsNotExist(err) {
			return 0
		}
		require.NoError(t, err)
		return strings.Count(string(content), "run\n")
	}

	envelope, stderr, code := f.runJSON(f.repo, "ensure", "author", "--role", "implementation")
	require.Equal(t, 0, code, stderr)
	author := field(t, envelope, "data", "worktree", "path").(string)
	authorID := field(t, envelope, "data", "worktree", "id").(string)
	_, stderr, code = f.runJSON(f.repo, "lease", "acquire", "author", "--owner", "claude-author")
	require.Equal(t, 0, code, stderr)
	commit := gitRun(t, author, "rev-parse", "HEAD")

	envelope, stderr, code = f.runJSON(author, "check", "fast")
	require.Equal(t, 0, code, stderr)
	produced := field(t, envelope, "data", "results", 0).(map[string]any)
	producedRun := produced["run_id"].(string)
	require.Equal(t, authorID, field(t, produced, "producer", "worktree_id"))
	require.Equal(t, "implementation", field(t, produced, "producer", "role"))
	require.Equal(t, "claude-author", field(t, produced, "producer", "lease_owner"))
	require.Equal(t, true, field(t, produced, "assessment", "passing_and_current"))
	require.Nil(t, field(t, produced, "assessment", "reused_from"))
	require.Equal(t, 1, runs())

	// A fresh review worktree of the same commit, with a different agent
	// session, port, path, ID and runtime directory, reuses the author's pass.
	reviewer := withEnv(f, "CLAUDE_SESSION_ID=review-session")
	envelope, stderr, code = reviewer.runJSON(f.repo, "ensure", "--review", commit)
	require.Equal(t, 0, code, stderr)
	review := field(t, envelope, "data", "worktree", "path").(string)
	reviewID := field(t, envelope, "data", "worktree", "id").(string)

	envelope, stderr, code = reviewer.runJSON(review, "check", "fast", "--reuse")
	require.Equal(t, 0, code, stderr)
	reused := field(t, envelope, "data", "results", 0).(map[string]any)
	require.Equal(t, true, reused["reused"], "warnings: %v", envelope["warnings"])
	require.Equal(t, 1, runs(), "nothing ran in the review worktree")
	from := field(t, reused, "assessment", "reused_from").(map[string]any)
	require.Equal(t, authorID, from["worktree_id"])
	require.Equal(t, author, from["path"])
	require.Equal(t, "implementation", from["role"])
	require.Equal(t, "claude-author", from["lease_owner"])
	require.Equal(t, producedRun, from["run_id"])
	require.Equal(t, "fast", from["check"])
	require.Equal(t, true, field(t, reused, "assessment", "passing_and_current"))
	require.Equal(t, reviewID, field(t, reused, "worktree", "id"), "the consumer record carries the reviewer's identity")
	require.Equal(t, reviewID, field(t, reused, "producer", "worktree_id"))

	envelope, stderr, code = reviewer.runJSON(review, "check", "--list")
	require.Equal(t, 0, code, stderr)
	entry := checkEntry(t, envelope, "fast")
	require.Equal(t, true, field(t, entry, "assessment", "passing_and_current"))
	require.Equal(t, authorID, field(t, entry, "assessment", "reused_from", "worktree_id"))
	envelope, stderr, code = reviewer.runJSON(review, "status")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, field(t, envelope, "data", "checks", 0, "passing_and_current"))
	require.Equal(t, authorID, field(t, envelope, "data", "checks", 0, "reused_from", "worktree_id"))

	// Reviewers can refuse author-produced results.
	other := withEnv(f, "CLAUDE_SESSION_ID=strict-session")
	envelope, stderr, code = other.runJSON(f.repo, "ensure", "--review", commit)
	require.Equal(t, 0, code, stderr)
	strict := field(t, envelope, "data", "worktree", "path").(string)
	envelope, stderr, code = other.runJSON(strict, "check", "fast", "--reuse", "--reuse-exclude-role", "implementation")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, false, field(t, envelope, "data", "results", 0, "reused"))
	require.Contains(t, strings.Join(stringsOf(envelope["warnings"]), "\n"), "excluded")
	require.Equal(t, 2, runs())

	// A declared environment change is part of the content identity.
	changed := withEnv(f, "CHECK_MODE=slow")
	envelope, stderr, code = changed.runJSON(f.repo, "ensure", "--review", commit)
	require.Equal(t, 0, code, stderr)
	declared := field(t, envelope, "data", "worktree", "path").(string)
	envelope, stderr, code = changed.runJSON(declared, "check", "fast", "--reuse")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, false, field(t, envelope, "data", "results", 0, "reused"))
	require.Equal(t, 3, runs())

	// A changed check definition (a new commit) is never served the old pass.
	require.NoError(t, os.WriteFile(filepath.Join(f.repo, "anvil.yaml"), []byte(strings.Replace(crossReuseFixtureYAML,
		`echo run >> "$RUN_LOG"`, `echo run >> "$RUN_LOG" # changed`, 1)), 0o644))
	gitRun(t, f.repo, "commit", "-qam", "change check")
	envelope, stderr, code = f.runJSON(f.repo, "ensure", "--review", "HEAD")
	require.Equal(t, 0, code, stderr)
	next := field(t, envelope, "data", "worktree", "path").(string)
	envelope, stderr, code = f.runJSON(next, "check", "fast", "--reuse")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, false, field(t, envelope, "data", "results", 0, "reused"))
	require.Equal(t, 4, runs())
}

func TestCheckReuseTrustFlagValidation(t *testing.T) {
	f := newAgentFixture(t, crossReuseFixtureYAML, nil)
	envelope, _, code := f.runJSON(f.repo, "check", "fast", "--reuse", "--reuse-trust", "everyone")
	require.Equal(t, 2, code)
	require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
}

func TestCheckReuseRolePolicyFromConfig(t *testing.T) {
	skipWithoutCheckExecution(t)
	runLog := filepath.Join(t.TempDir(), "runs.log")
	yaml := crossReuseFixtureYAML + `roles:
  review:
    reuse_exclude_roles: [implementation]
`
	f := withEnv(newAgentFixture(t, yaml, nil), "RUN_LOG="+runLog, "CHECK_MODE=fast")
	envelope, stderr, code := f.runJSON(f.repo, "ensure", "author", "--role", "implementation")
	require.Equal(t, 0, code, stderr)
	author := field(t, envelope, "data", "worktree", "path").(string)
	_, stderr, code = f.runJSON(author, "check", "fast")
	require.Equal(t, 0, code, stderr)

	envelope, stderr, code = f.runJSON(f.repo, "ensure", "--review", "HEAD")
	require.Equal(t, 0, code, stderr)
	review := field(t, envelope, "data", "worktree", "path").(string)
	// --reuse-trust replaces the trust level but never drops configured exclusions.
	envelope, stderr, code = f.runJSON(review, "check", "fast", "--reuse", "--reuse-trust", "any")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, false, field(t, envelope, "data", "results", 0, "reused"))
	require.Contains(t, strings.Join(stringsOf(envelope["warnings"]), "\n"), "results produced by implementation worktrees are excluded")

	// The author's own worktree is never excluded from its own results.
	envelope, stderr, code = f.runJSON(author, "check", "fast", "--reuse", "--reuse-exclude-role", "implementation")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, field(t, envelope, "data", "results", 0, "reused"))
	require.Equal(t, author, field(t, envelope, "data", "results", 0, "assessment", "reused_from", "path"))
}

// anvil.yaml is ignored by Git here and written into each worktree, so the
// check definition changes while the committed tree stays identical.
func TestCheckReuseInvalidatedByDefinitionOnlyChange(t *testing.T) {
	skipWithoutCheckExecution(t)
	runLog := filepath.Join(t.TempDir(), "runs.log")
	f := newAgentFixture(t, "", map[string]string{".gitignore": ".anvil.local\nanvil.yaml\n"})
	f = withEnv(f, "RUN_LOG="+runLog, "CHECK_MODE=fast")
	runs := func() int {
		content, err := os.ReadFile(runLog)
		require.NoError(t, err)
		return strings.Count(string(content), "run\n")
	}
	worktree := func(args ...string) string {
		envelope, stderr, code := f.runJSON(f.repo, append([]string{"ensure"}, args...)...)
		require.Equal(t, 0, code, stderr)
		return field(t, envelope, "data", "worktree", "path").(string)
	}
	declare := func(dir, yaml string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte(yaml), 0o644))
		require.Empty(t, gitRun(t, dir, "status", "--porcelain"), "the declaration is outside the tree")
	}
	author := worktree("author", "--role", "implementation")
	declare(author, crossReuseFixtureYAML)
	_, stderr, code := f.runJSON(author, "check", "fast")
	require.Equal(t, 0, code, stderr)

	same := worktree("--review", "HEAD")
	declare(same, crossReuseFixtureYAML)
	envelope, stderr, code := f.runJSON(same, "check", "fast", "--reuse")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, field(t, envelope, "data", "results", 0, "reused"), "control: same definition is reused")

	changed := worktree("--review", "HEAD")
	declare(changed, strings.Replace(crossReuseFixtureYAML, "reusable: true", "reusable: true\n    description: changed", 1))
	envelope, stderr, code = f.runJSON(changed, "check", "fast", "--reuse")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, false, field(t, envelope, "data", "results", 0, "reused"))
	require.Equal(t, 2, runs())
}

func TestRemoveForgetsReuseIndexEntries(t *testing.T) {
	skipWithoutCheckExecution(t)
	f := withEnv(newAgentFixture(t, crossReuseFixtureYAML, nil), "RUN_LOG="+filepath.Join(t.TempDir(), "runs.log"), "CHECK_MODE=fast")
	envelope, stderr, code := f.runJSON(f.repo, "ensure", "author", "--role", "implementation")
	require.Equal(t, 0, code, stderr)
	author := field(t, envelope, "data", "worktree", "path").(string)
	id := field(t, envelope, "data", "worktree", "id").(string)
	_, stderr, code = f.runJSON(author, "check", "fast")
	require.Equal(t, 0, code, stderr)
	pattern := filepath.Join(f.runtimeRoot(), "reuse", "*", "fast", "*", id+".json")
	found, err := filepath.Glob(pattern)
	require.NoError(t, err)
	require.Len(t, found, 1)

	_, stderr, code = f.run(f.repo, "remove", "author", "--force")
	require.Equal(t, 0, code, stderr)
	found, err = filepath.Glob(pattern)
	require.NoError(t, err)
	require.Empty(t, found)
}

// A stuck holder of the reuse index lock never fails or hangs a removal; the
// leftover entries are reported and ignored by later lookups.
func TestRemoveWarnsWhenReuseIndexIsBusy(t *testing.T) {
	skipWithoutCheckExecution(t)
	f := withEnv(newAgentFixture(t, crossReuseFixtureYAML, nil), "RUN_LOG="+filepath.Join(t.TempDir(), "runs.log"), "CHECK_MODE=fast")
	envelope, stderr, code := f.runJSON(f.repo, "ensure", "author", "--role", "implementation")
	require.Equal(t, 0, code, stderr)
	author := field(t, envelope, "data", "worktree", "path").(string)
	_, stderr, code = f.runJSON(author, "check", "fast")
	require.Equal(t, 0, code, stderr)
	locks, err := filepath.Glob(filepath.Join(f.runtimeRoot(), "reuse", "*", "fast", ".lock"))
	require.NoError(t, err)
	require.Len(t, locks, 1)
	held, err := lockfile.Acquire(locks[0])
	require.NoError(t, err)
	defer func() { require.NoError(t, held.Release()) }()

	done := make(chan struct{})
	var stdout string
	go func() {
		defer close(done)
		stdout, stderr, code = f.run(f.repo, "remove", "author", "--force")
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("remove blocked on the reuse index lock")
	}
	require.Equal(t, 0, code, stdout+stderr)
	require.Contains(t, stdout+stderr, "reuse index")
	require.NoDirExists(t, author)
}

// skipWithoutCheckExecution skips scenarios that execute check commands:
// they use sh, and Windows check execution is refused until job-object
// supervision exists (see docs/agent-api.md).
func skipWithoutCheckExecution(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("check execution is refused on Windows; scenarios use sh")
	}
}
