package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const lifecycleFixtureYAML = `env:
  STRIPE_SECRET: sk_test_fixture
  PUBLIC_NAME: demo
scaffold:
  override: true
  steps:
    - name: command.run
      command: touch scaffold.marker
components:
  - name: app
    prepare:
      - run: touch component.marker
        outputs: [component.marker]
roles:
  review:
    env_unset: [DEPLOY_TOKEN]
checks:
  fast:
    commands: ["true"]
`

func withEnv(f *agentFixture, entries ...string) *agentFixture {
	copied := *f
	copied.env = subprocessEnvFrom(append(append([]string(nil), f.env...), entries...))
	return &copied
}

func TestReviewWorktreesArePinnedIndependentAndMinimal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	f := newAgentFixture(t, lifecycleFixtureYAML, nil)
	f = withEnv(f, "DEPLOY_TOKEN=prod-credential")
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/impl", "--role", "implementation", "--task-id", "SOLO-7", "--pr", "42")
	require.Equal(t, 0, code, stderr)
	impl := f.worktreePath("feature-impl")
	require.FileExists(t, filepath.Join(impl, "scaffold.marker"))
	require.FileExists(t, filepath.Join(impl, "component.marker"))

	// The implementation worker commits a candidate, then keeps editing.
	require.NoError(t, os.WriteFile(filepath.Join(impl, "README.md"), []byte("candidate\n"), 0o644))
	gitRun(t, impl, "commit", "-am", "candidate")
	candidate := gitRun(t, impl, "rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(impl, "README.md"), []byte("work in progress\n"), 0o644))

	first, stderr, code := f.runJSON(f.repo, "ensure", "--review", candidate)
	require.Equal(t, 0, code, stderr)
	second, _, code := f.runJSON(f.repo, "ensure", "--review", "feature/impl")
	require.Equal(t, 0, code)
	reviewA := field(t, first, "data", "worktree", "path").(string)
	reviewB := field(t, second, "data", "worktree", "path").(string)
	require.NotEqual(t, reviewA, reviewB, "independent reviewers of the same commit")
	require.Equal(t, "review", field(t, first, "data", "metadata", "role"))
	require.Equal(t, candidate, field(t, first, "data", "metadata", "pinned_commit"))
	require.Equal(t, candidate, field(t, second, "data", "metadata", "pinned_commit"), "branch names resolve to the exact commit")
	require.Equal(t, true, field(t, first, "data", "worktree", "detached"))
	require.Regexp(t, `review-`+candidate[:12]+`-[0-9a-f]{4}$`, reviewA)

	content, err := os.ReadFile(filepath.Join(reviewA, "README.md"))
	require.NoError(t, err)
	require.Equal(t, "candidate\n", string(content), "no inherited dirty changes")
	require.NoFileExists(t, filepath.Join(reviewA, "scaffold.marker"), "minimal preparation skips scaffold steps")
	require.FileExists(t, filepath.Join(reviewA, "component.marker"))

	// The implementation worker is untouched.
	require.Equal(t, "feature/impl", gitRun(t, impl, "symbolic-ref", "--short", "HEAD"))
	require.Equal(t, candidate, gitRun(t, impl, "rev-parse", "HEAD"))
	content, err = os.ReadFile(filepath.Join(impl, "README.md"))
	require.NoError(t, err)
	require.Equal(t, "work in progress\n", string(content))

	// Role-declared credentials are removed from commands in review worktrees only.
	stdout, _, code := f.run(f.repo, "run", reviewA, "--", "sh", "-c", `printf '%s' "${DEPLOY_TOKEN-unset}"`)
	require.Equal(t, 0, code)
	require.Equal(t, "unset", stdout)
	stdout, _, code = f.run(f.repo, "run", "feature-impl", "--", "sh", "-c", `printf '%s' "${DEPLOY_TOKEN-unset}"`)
	require.Equal(t, 0, code)
	require.Equal(t, "prod-credential", stdout)

	// Named review: reuse for the same commit, refuse a different commit.
	named, _, code := f.runJSON(f.repo, "ensure", "pr-42-review", "--review", candidate)
	require.Equal(t, 0, code)
	require.Equal(t, true, field(t, named, "data", "created"))
	named, _, code = f.runJSON(f.repo, "ensure", "pr-42-review", "--review", candidate)
	require.Equal(t, 0, code)
	require.Equal(t, false, field(t, named, "data", "created"))
	mainSHA := gitRun(t, f.repo, "rev-parse", "main")
	envelope, _, code := f.runJSON(f.repo, "ensure", "pr-42-review", "--review", mainSHA)
	require.Equal(t, 2, code)
	require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
	envelope, _, code = f.runJSON(f.repo, "ensure", "feature-impl", "--review", candidate)
	require.Equal(t, 2, code, "an implementation worktree is never turned into a review")
	require.Contains(t, field(t, envelope, "error", "message"), "implementation")
	_, _, code = f.runJSON(f.repo, "ensure", "x", "--review", "no-such-commit")
	require.Equal(t, 4, code)

	// Status reports whether HEAD still matches the pin.
	status, _, code := f.runJSON(f.repo, "status", reviewA)
	require.Equal(t, 0, code)
	require.Equal(t, true, field(t, status, "data", "git", "pinned_commit_matches"))
	gitRun(t, reviewA, "checkout", "--detach", mainSHA)
	status, _, _ = f.runJSON(f.repo, "status", reviewA)
	require.Equal(t, false, field(t, status, "data", "git", "pinned_commit_matches"))

	// Review cleanup is straightforward and leaves branches alone.
	stdout, stderr, code = f.run(f.repo, "remove", filepath.Base(reviewA), "--force")
	require.Equal(t, 0, code, stdout+stderr)
	require.NoDirExists(t, reviewA)
	require.Equal(t, candidate, gitRun(t, f.repo, "rev-parse", "feature/impl"))
}

func TestLeasesProtectWorktreesFromOtherAgents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	f := newAgentFixture(t, lifecycleFixtureYAML, nil)
	envelope, stderr, code := f.runJSON(f.repo, "ensure", "feature/leased", "--lease-owner", "codex", "--task-id", "SOLO-1", "--lease-ttl", "1h")
	require.Equal(t, 0, code, stderr)
	token := field(t, envelope, "data", "lease", "token").(string)
	require.Regexp(t, `^lt_[0-9a-f]{48}$`, token)
	require.Equal(t, "active", field(t, envelope, "data", "lease", "state"))
	wt := f.worktreePath("feature-leased")
	state, err := os.ReadFile(filepath.Join(wt, ".anvil.local"))
	require.NoError(t, err)
	require.NotContains(t, string(state), token, "tokens are never stored")

	// Another agent (even claiming the same task) cannot take or release it.
	other := withEnv(f, "ANVIL_LEASE_TOKEN=lt_not_the_token")
	envelope, _, code = other.runJSON(f.repo, "lease", "acquire", "feature-leased", "--owner", "claude", "--task-id", "SOLO-1")
	require.Equal(t, 7, code)
	require.Equal(t, "lease_held", field(t, envelope, "error", "code"))
	require.Equal(t, "codex", field(t, envelope, "error", "details", "lease", "owner"))
	envelope, _, code = other.runJSON(f.repo, "lease", "release", "feature-leased")
	require.Equal(t, 7, code)
	require.Equal(t, "lease_not_owned", field(t, envelope, "error", "code"))

	// Cleanup refuses an active lease; status previews the blocker.
	status, _, _ := f.runJSON(f.repo, "status", "feature-leased")
	require.Equal(t, false, field(t, status, "data", "cleanup", "allowed"))
	require.Contains(t, field(t, status, "data", "cleanup", "blocked_by", 0), "active lease")
	stdout, stderr, code := f.run(f.repo, "remove", "feature-leased", "--force")
	require.Equal(t, 7, code, stdout+stderr)
	require.Contains(t, stderr, "leased by")
	require.DirExists(t, wt)

	owner := withEnv(f, "ANVIL_LEASE_TOKEN="+token)
	envelope, _, code = owner.runJSON(f.repo, "lease", "renew", "feature-leased", "--ttl", "2h")
	require.Equal(t, 0, code)
	require.Equal(t, "active", field(t, envelope, "data", "lease", "state"))
	envelope, _, code = owner.runJSON(f.repo, "lease", "release", "feature-leased")
	require.Equal(t, 0, code)
	require.Equal(t, "none", field(t, envelope, "data", "lease", "state"))
	require.Equal(t, "released", field(t, envelope, "data", "lease", "previous", "how"))

	// Stale leases are diagnosed and need an explicit takeover.
	_, _, code = f.runJSON(f.repo, "lease", "acquire", "feature-leased", "--owner", "codex", "--ttl", "1s")
	require.Equal(t, 0, code)
	time.Sleep(1200 * time.Millisecond)
	envelope, _, code = f.runJSON(f.repo, "lease", "show", "feature-leased")
	require.Equal(t, 0, code)
	require.Equal(t, false, envelope["mutating"])
	require.Equal(t, "stale", field(t, envelope, "data", "lease", "state"))
	envelope, _, code = f.runJSON(f.repo, "lease", "acquire", "feature-leased", "--owner", "claude")
	require.Equal(t, 7, code)
	require.Equal(t, "lease_stale", field(t, envelope, "error", "code"))
	envelope, _, code = f.runJSON(f.repo, "lease", "acquire", "feature-leased", "--owner", "claude", "--takeover-stale")
	require.Equal(t, 0, code)
	require.Equal(t, "taken_over_stale", field(t, envelope, "data", "lease", "previous", "how"))

	// Breaking requires a reason; explicit override removes a leased worktree.
	_, _, code = f.runJSON(f.repo, "lease", "break", "feature-leased")
	require.Equal(t, 2, code)
	stdout, stderr, code = f.run(f.repo, "remove", "feature-leased", "--force", "--ignore-lease")
	require.Equal(t, 0, code, stdout+stderr)
	require.Contains(t, stdout+stderr, "--ignore-lease")
	require.NoDirExists(t, wt)
}

func TestContextAndStatusAreReadOnlyAndRedacted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	f := newAgentFixture(t, lifecycleFixtureYAML, nil)
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/ctx", "--lease-owner", "codex")
	require.Equal(t, 0, code, stderr)
	_, _, code = f.runJSON(f.worktreePath("feature-ctx"), "check", "fast")
	require.Equal(t, 0, code)
	statePath := filepath.Join(f.worktreePath("feature-ctx"), ".anvil.local")
	before, err := os.ReadFile(statePath)
	require.NoError(t, err)

	envelope, stdout, code := f.runJSON(f.repo, "context", "feature-ctx")
	require.Equal(t, 0, code, stdout)
	require.Equal(t, false, envelope["mutating"])
	vars := map[string]map[string]any{}
	for _, raw := range field(t, envelope, "data", "environment").([]any) {
		v := raw.(map[string]any)
		vars[v["name"].(string)] = v
	}
	require.Equal(t, "demo", vars["PUBLIC_NAME"]["value"])
	require.Equal(t, true, vars["STRIPE_SECRET"]["secret"])
	require.Nil(t, vars["STRIPE_SECRET"]["value"], "secret values are never emitted")
	require.Contains(t, vars, "ANVIL_WORKTREE_ID")
	require.Contains(t, vars, "ANVIL_STATE_DIR")
	require.Equal(t, "active", field(t, envelope, "data", "components", 0, "status"))
	require.Equal(t, "fast", field(t, envelope, "data", "checks", 0, "name"))
	require.Equal(t, "codex", field(t, envelope, "data", "lease", "owner"))
	raw, _, _ := f.run(f.repo, "context", "feature-ctx", "--json")
	require.NotContains(t, raw, "sk_test_fixture")
	require.NotContains(t, raw, "token_sha256")

	status, _, code := f.runJSON(f.repo, "status", "feature-ctx")
	require.Equal(t, 0, code)
	require.Equal(t, true, field(t, status, "data", "ready"))
	require.Equal(t, "complete", field(t, status, "data", "prepare", "status"))
	require.Equal(t, "passed", field(t, status, "data", "checks", 0, "status"))
	require.Equal(t, true, field(t, status, "data", "checks", 0, "passing_and_current"))
	require.NotEmpty(t, field(t, status, "data", "cleanup", "remove_runtime_dir"))

	all, _, code := f.runJSON(f.repo, "status", "--all")
	require.Equal(t, 0, code)
	names := []string{}
	for _, entry := range field(t, all, "data", "worktrees").([]any) {
		names = append(names, field(t, entry, "worktree", "name").(string))
	}
	require.Contains(t, strings.Join(names, ","), "feature-ctx")

	_, _, code = f.runJSON(f.repo, "check", "--list", "--worktree", "feature-ctx")
	require.Equal(t, 0, code)
	after, err := os.ReadFile(statePath)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "inspection never writes .anvil.local")

	envelope, _, code = f.runJSON(f.repo, "status", "nope")
	require.Equal(t, 3, code)
	require.Equal(t, "worktree_not_found", field(t, envelope, "error", "code"))
}
