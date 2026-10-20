package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

const ensureFixtureYAML = `scaffold:
  override: true
  steps:
    - name: command.run
      command: echo run >> once.count
    - name: command.run
      command: echo run >> install.count
      inputs: [deps.lock]
      outputs: [install.count]
    - name: command.run
      command: test ! -f fail-here
`

func countRuns(t *testing.T, path string) int {
	t.Helper()
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	return strings.Count(string(content), "run")
}

func TestEnsureCreatesPreparesAndIsIdempotent(t *testing.T) {
	f := newAgentFixture(t, ensureFixtureYAML, map[string]string{"deps.lock": "v1"})

	envelope, stderr, code := f.runJSON(f.repo, "ensure", "feature/one")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, envelope["ok"])
	require.Equal(t, true, envelope["mutating"])
	require.Equal(t, true, field(t, envelope, "data", "created"))
	require.Equal(t, true, field(t, envelope, "data", "prepare", "ready"))
	require.Equal(t, "complete", field(t, envelope, "data", "prepare", "status"))
	require.Equal(t, []string{"ran", "ran", "ran"}, stepResults(t, envelope))
	wt := f.worktreePath("feature-one")
	require.Equal(t, wt, field(t, envelope, "data", "worktree", "path"))
	require.Equal(t, "feature/one", field(t, envelope, "data", "worktree", "branch"))
	id := field(t, envelope, "data", "worktree", "id").(string)
	require.Regexp(t, `^wt_[0-9a-f]{16}$`, id)
	require.Len(t, field(t, envelope, "data", "worktree", "head"), 40)

	// Repeated: resolved by branch, folder, path and ID; nothing reruns.
	for _, query := range []string{"feature/one", "feature-one", wt, id} {
		envelope, stderr, code = f.runJSON(f.repo, "ensure", query)
		require.Equal(t, 0, code, stderr)
		require.Equal(t, false, field(t, envelope, "data", "created"))
		require.Equal(t, []string{"up_to_date", "up_to_date", "up_to_date"}, stepResults(t, envelope), query)
		require.Equal(t, id, field(t, envelope, "data", "worktree", "id"))
	}
	require.Equal(t, 1, countRuns(t, filepath.Join(wt, "once.count")))
	require.Equal(t, 1, countRuns(t, filepath.Join(wt, "install.count")))

	// Changed input reruns only the dependent step; ensure from inside works.
	require.NoError(t, os.WriteFile(filepath.Join(wt, "deps.lock"), []byte("v2"), 0o644))
	envelope, _, code = f.runJSON(wt, "ensure")
	require.Equal(t, 0, code)
	require.Equal(t, []string{"up_to_date", "ran", "up_to_date"}, stepResults(t, envelope))
	require.Equal(t, 2, countRuns(t, filepath.Join(wt, "install.count")))

	// --refresh reruns everything.
	envelope, _, code = f.runJSON(wt, "ensure", "--refresh")
	require.Equal(t, 0, code)
	require.Equal(t, []string{"ran", "ran", "ran"}, stepResults(t, envelope))
}

func TestEnsureFailureReportsNotReadyAndResumes(t *testing.T) {
	f := newAgentFixture(t, ensureFixtureYAML, map[string]string{"deps.lock": "v1"})
	stdout, stderr, code := f.run(f.repo, "ensure", "feature/fail", "--skip-prepare", "--json")
	require.Equal(t, 0, code, stdout+stderr)
	wt := f.worktreePath("feature-fail")
	require.NoError(t, os.WriteFile(filepath.Join(wt, "fail-here"), nil, 0o644))

	envelope, stderr, code := f.runJSON(f.repo, "ensure", "feature/fail")
	require.Equal(t, 6, code, stderr)
	require.Equal(t, false, envelope["ok"])
	require.Equal(t, "prepare_failed", field(t, envelope, "error", "code"))
	require.Equal(t, false, field(t, envelope, "data", "prepare", "ready"))
	require.Equal(t, "failed", field(t, envelope, "data", "prepare", "status"))
	require.Equal(t, []string{"ran", "ran", "failed"}, stepResults(t, envelope))
	require.Contains(t, stderr, "error:", "diagnostics go to stderr")

	require.NoError(t, os.Remove(filepath.Join(wt, "fail-here")))
	envelope, _, code = f.runJSON(f.repo, "ensure", "feature/fail")
	require.Equal(t, 0, code)
	require.Equal(t, []string{"up_to_date", "up_to_date", "ran"}, stepResults(t, envelope))
	require.Equal(t, 1, countRuns(t, filepath.Join(wt, "once.count")))
}

func TestEnsureConcurrentCreationConverges(t *testing.T) {
	f := newAgentFixture(t, ensureFixtureYAML, map[string]string{"deps.lock": "v1"})
	const workers = 4
	type outcome struct {
		stdout, stderr string
		code           int
	}
	results := make([]outcome, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(f.bin, "ensure", "feature/race", "--json")
			cmd.Dir = f.repo
			cmd.Env = f.env
			var stdout, stderr strings.Builder
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			code := 0
			if exitErr, ok := err.(*exec.ExitError); ok {
				code = exitErr.ExitCode()
			}
			results[i] = outcome{stdout.String(), stderr.String(), code}
		}(i)
	}
	wg.Wait()
	created := 0
	for _, result := range results {
		require.Equal(t, 0, result.code, result.stderr)
		if strings.Contains(result.stdout, `"created": true`) {
			created++
		}
	}
	require.Equal(t, 1, created, "exactly one process creates the worktree")
	wt := f.worktreePath("feature-race")
	require.Equal(t, 1, countRuns(t, filepath.Join(wt, "once.count")), "preparation is serialized and not repeated")
	require.Equal(t, 1, countRuns(t, filepath.Join(wt, "install.count")))
}

func TestEnsureJSONErrors(t *testing.T) {
	f := newAgentFixture(t, "scaffold:\n  override: true\n  steps:\n    - name: no.such.step\n", nil)
	envelope, _, code := f.runJSON(f.repo, "ensure", "feature/bad")
	require.Equal(t, 5, code)
	require.Equal(t, "config_invalid", field(t, envelope, "error", "code"))
	wt := f.worktreePath("feature-bad")
	require.NoFileExists(t, filepath.Join(wt, ".anvil.local"), "invalid config fails before effects")

	outside := t.TempDir()
	envelope, _, code = f.runJSON(outside, "ensure", "x")
	require.Equal(t, 5, code)
	require.Equal(t, "project_not_found", field(t, envelope, "error", "code"))

	_, stderr, code := f.run(f.repo, "ensure", "a", "b", "--json")
	require.Equal(t, 2, code, stderr)
}

func TestEnsureHumanOutputAndAmbiguity(t *testing.T) {
	f := newAgentFixture(t, ensureFixtureYAML, map[string]string{"deps.lock": "v1"})
	stdout, stderr, code := f.run(f.repo, "ensure", "feature/human")
	require.Equal(t, 0, code, stderr)
	require.Contains(t, stdout, "Worktree ready at")

	// A folder name and a different branch name that collide are ambiguous.
	gitRun(t, f.repo, "worktree", "add", "-b", "feature-human", filepath.Join(f.base, "demo", "other"))
	envelope, _, code := f.runJSON(f.repo, "ensure", "feature-human")
	require.Equal(t, 2, code)
	require.Equal(t, "ambiguous_worktree", field(t, envelope, "error", "code"))
}
