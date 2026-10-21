// Review and lease scenarios use sh.

//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReviewPolicySurvivesPlainReEnsure(t *testing.T) {
	f := newAgentFixture(t, lifecycleFixtureYAML, nil)
	envelope, stderr, code := f.runJSON(f.repo, "ensure", "review-basic", "--review", "HEAD")
	require.Equal(t, 0, code, stderr)
	review := field(t, envelope, "data", "worktree", "path").(string)
	id := field(t, envelope, "data", "worktree", "id").(string)
	require.NoFileExists(t, filepath.Join(review, "scaffold.marker"))

	for _, run := range []struct {
		dir   string
		query []string
	}{
		{f.repo, []string{"review-basic"}},
		{f.repo, []string{review}},
		{f.repo, []string{id}},
		{review, nil},
	} {
		args := append([]string{"ensure"}, run.query...)
		envelope, stderr, code := f.runJSON(run.dir, append(args, "--refresh")...)
		require.Equal(t, 0, code, stderr)
		require.Equal(t, "review", field(t, envelope, "data", "metadata", "role"))
		require.NoFileExists(t, filepath.Join(review, "scaffold.marker"), "re-ensure %v keeps minimal review preparation", run.query)
	}
}

func TestExistingUnlabelledWorktreesAreNeverAdoptedAsReviews(t *testing.T) {
	f := newAgentFixture(t, lifecycleFixtureYAML, nil)
	_, stderr, code := f.run(f.repo, "ensure", "legacy", "--skip-prepare")
	require.Equal(t, 0, code, stderr)
	legacy := f.worktreePath("legacy")
	require.NoError(t, os.WriteFile(filepath.Join(legacy, "README.md"), []byte("implementation WIP\n"), 0o644))

	for _, name := range []string{"legacy", "main"} {
		envelope, stderr, code := f.runJSON(f.repo, "ensure", name, "--review", "HEAD")
		require.Equal(t, 2, code, name+": "+stderr)
		require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
	}
	envelope, _, code := f.runJSON(f.repo, "ensure", "legacy", "--skip-prepare")
	require.Equal(t, 0, code)
	require.NotContains(t, field(t, envelope, "data", "metadata").(map[string]any), "role", "no review metadata was written")
	require.Equal(t, "implementation WIP\n", readFile(t, filepath.Join(legacy, "README.md")))
}

func TestReviewReuseVerifiesTheCheckedOutCommit(t *testing.T) {
	f := newAgentFixture(t, lifecycleFixtureYAML, nil)
	commitA := gitRun(t, f.repo, "rev-parse", "HEAD")
	envelope, stderr, code := f.runJSON(f.repo, "ensure", "review-a", "--review", commitA)
	require.Equal(t, 0, code, stderr)
	review := field(t, envelope, "data", "worktree", "path").(string)

	require.NoError(t, os.WriteFile(filepath.Join(f.repo, "README.md"), []byte("B\n"), 0o644))
	gitRun(t, f.repo, "commit", "-am", "B")
	commitB := gitRun(t, f.repo, "rev-parse", "HEAD")
	gitRun(t, review, "checkout", "--detach", commitB)

	for _, args := range [][]string{{"ensure", "review-a", "--review", commitA}, {"ensure", "review-a"}} {
		envelope, stderr, code := f.runJSON(f.repo, args...)
		require.Equal(t, 2, code, stderr)
		require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
		require.Contains(t, field(t, envelope, "error", "message"), commitA[:12])
	}
	require.Equal(t, commitB, gitRun(t, review, "rev-parse", "HEAD"), "reviewer state is never reset")
}

func TestReviewScopeAppliesBeforeRequirements(t *testing.T) {
	yaml := `scaffold:
  override: true
components:
  - name: app
    path: app
    prepare:
      - run: touch app.marker
        outputs: [app.marker]
  - name: desktop
    path: desktop
    requires: [never-installed-desktop-tool]
roles:
  review:
    components: [app]
`
	f := newAgentFixture(t, yaml, map[string]string{"app/.keep": "", "desktop/.keep": ""})
	envelope, stderr, code := f.runJSON(f.repo, "ensure", "--review", "HEAD")
	require.Equal(t, 0, code, stderr)
	review := field(t, envelope, "data", "worktree", "path").(string)
	require.FileExists(t, filepath.Join(review, "app", "app.marker"))
	require.Equal(t, true, field(t, envelope, "data", "prepare", "ready"))

	_, stderr, code = f.run(review, "run", "--require-ready", "--", "true")
	require.Equal(t, 0, code, "readiness uses the same review scope: "+stderr)

	_, _, code = f.runJSON(f.repo, "ensure", "feature/impl")
	require.Equal(t, 6, code, "implementation preparation still requires every component")
}

func TestLeaseCannotBeAcquiredDuringRemoval(t *testing.T) {
	marks := t.TempDir()
	yaml := `cleanup:
  steps:
    - name: bash.run
      condition:
        command: touch ` + filepath.Join(marks, "cleanup.started") + `; sleep 3
`
	f := newAgentFixture(t, yaml, nil)
	_, stderr, code := f.run(f.repo, "ensure", "feature/x", "--skip-prepare")
	require.Equal(t, 0, code, stderr)
	wt := f.worktreePath("feature-x")

	removal := exec.Command(f.bin, "remove", "feature-x", "--force")
	removal.Dir, removal.Env = f.repo, f.env
	var output strings.Builder
	removal.Stdout, removal.Stderr = &output, &output
	require.NoError(t, removal.Start())
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(marks, "cleanup.started"))
		return err == nil
	}, 10*time.Second, 20*time.Millisecond)

	envelope, stderr, code := f.runJSON(f.repo, "lease", "acquire", "feature-x", "--owner", "worker")
	require.NotEqual(t, 0, code, stderr)
	require.Equal(t, false, envelope["ok"])
	require.NoError(t, removal.Wait(), output.String())
	require.NoDirExists(t, wt)
}

func TestStatusAllReportsUnreadableLeases(t *testing.T) {
	f := newAgentFixture(t, lifecycleFixtureYAML, nil)
	_, stderr, code := f.run(f.repo, "ensure", "feature/x", "--skip-prepare")
	require.Equal(t, 0, code, stderr)
	statePath := filepath.Join(f.worktreePath("feature-x"), ".anvil.local")
	state := readFile(t, statePath)
	require.NoError(t, os.WriteFile(statePath, []byte(state+"lease: not-a-lease-object\n"), 0o600))

	envelope, stderr, code := f.runJSON(f.repo, "status", "--all")
	require.Equal(t, 0, code, stderr)
	var entry map[string]any
	for _, raw := range field(t, envelope, "data", "worktrees").([]any) {
		if strings.HasSuffix(field(t, raw, "worktree", "path").(string), "feature-x") {
			entry = raw.(map[string]any)
		}
	}
	require.NotNil(t, entry)
	require.Equal(t, "unknown", entry["lease"], "a failed observation is not reported as no lease")
	require.NotEmpty(t, field(t, envelope, "warnings"))
}
