package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestEnsureRejectsDryRunBeforeEffects(t *testing.T) {
	f := newAgentFixture(t, ensureFixtureYAML, map[string]string{"deps.lock": "v1"})
	for _, args := range [][]string{
		{"ensure", "--dry-run"},
		{"ensure", "dry-run-created", "--dry-run"},
		{"ensure", "dry-run-skipped", "--dry-run", "--skip-prepare"},
	} {
		envelope, stderr, code := f.runJSON(f.repo, args...)
		require.Equal(t, 2, code, stderr)
		require.Equal(t, false, envelope["ok"])
		require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
	}
	require.NoFileExists(t, filepath.Join(f.repo, ".anvil.local"))
	require.NoFileExists(t, filepath.Join(f.repo, "once.count"))
	require.NoDirExists(t, f.worktreePath("dry-run-created"))
	require.NoDirExists(t, f.worktreePath("dry-run-skipped"))
	require.Equal(t, "main", gitRun(t, f.repo, "branch", "--format=%(refname:short)"))
	require.Len(t, strings.Split(gitRun(t, f.repo, "worktree", "list"), "\n"), 1)
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

// Review #24 blocker 1: ensure must not have effects under --dry-run.
func TestEnsureDryRunPreservesExistingPreparationState(t *testing.T) {
	f := newAgentFixture(t, "scaffold:\n  override: true\n  steps:\n    - name: command.run\n      command: touch dry.marker\n", nil)
	envelope, _, code := f.runJSON(f.repo, "ensure", "feature/new", "--dry-run")
	require.Equal(t, 2, code)
	require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
	require.NoDirExists(t, f.worktreePath("feature-new"), "missing target is not created")
	envelope, _, code = f.runJSON(f.repo, "ensure", "feature/new", "--dry-run", "--skip-prepare")
	require.Equal(t, 2, code)
	require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
	require.NoDirExists(t, f.worktreePath("feature-new"))

	_, _, code = f.runJSON(f.repo, "ensure", "feature/existing", "--skip-prepare")
	require.Equal(t, 0, code)
	wt := f.worktreePath("feature-existing")
	before, err := os.ReadFile(filepath.Join(wt, ".anvil.local"))
	require.NoError(t, err)
	_, _, code = f.runJSON(f.repo, "ensure", "feature-existing", "--dry-run")
	require.Equal(t, 2, code)
	require.NoFileExists(t, filepath.Join(wt, "dry.marker"), "existing target is not prepared")
	after, err := os.ReadFile(filepath.Join(wt, ".anvil.local"))
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "no state written")
}

// Review #24 blocker 2: child output must never reach the JSON document.
func TestEnsureFailureNeverEmbedsChildOutputInJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stand-in binary is a POSIX shell script")
	}
	steps := map[string]string{
		"command.run": "    - name: command.run\n      command: echo SYNTHETIC_SECRET_24; exit 1\n",
		"bash.run":    "    - name: bash.run\n      command: echo SYNTHETIC_SECRET_24; exit 1\n",
		"binary":      "    - name: node.bun\n      args: [install]\n",
	}
	for name, step := range steps {
		t.Run(name, func(t *testing.T) {
			f := newAgentFixture(t, "scaffold:\n  override: true\n  steps:\n"+step, nil)
			bin := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(bin, "bun"), []byte("#!/bin/sh\necho SYNTHETIC_SECRET_24\nexit 1\n"), 0o755))
			f.env = subprocessEnv(t, append(append([]string(nil), f.env...), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))...)
			stdout, stderr, code := f.run(f.repo, "ensure", "feature/"+strings.ReplaceAll(name, ".", "-"), "--json")
			require.Equal(t, 6, code, stdout+stderr)
			require.NotContains(t, stdout, "SYNTHETIC_SECRET_24", "JSON carries only a safe summary")
			require.Contains(t, stderr, "SYNTHETIC_SECRET_24", "full diagnostics stay on stderr")
			var envelope map[string]any
			require.NoError(t, json.Unmarshal([]byte(stdout), &envelope))
			require.Equal(t, "prepare_failed", field(t, envelope, "error", "code"))
			state, err := os.ReadFile(filepath.Join(f.worktreePath("feature-"+strings.ReplaceAll(name, ".", "-")), ".anvil.local"))
			require.NoError(t, err)
			require.NotContains(t, string(state), "SYNTHETIC_SECRET_24")
		})
	}
}

// Review #24 blocker 6: argument and flag errors still produce one envelope.
func TestEnsurePreRunErrorsEmitJSONEnvelope(t *testing.T) {
	f := newAgentFixture(t, ensureFixtureYAML, map[string]string{"deps.lock": "v1"})
	for _, args := range [][]string{
		{"ensure", "a", "b", "--json"},
		{"ensure", "--unknown-flag", "--json"},
		{"ensure", "--json", "--unknown-flag"},
		{"ensure", "--lease-ttl-typo=1", "x", "--json"},
	} {
		stdout, stderr, code := f.run(f.repo, args...)
		require.Equal(t, 2, code, "%v: %s%s", args, stdout, stderr)
		var envelope map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &envelope), "%v: stdout must be one JSON document: %q", args, stdout)
		require.Equal(t, "anvil.agent/v1", envelope["schema"])
		require.Equal(t, false, envelope["ok"])
		require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
		require.NotEmpty(t, stderr, "diagnostic on stderr")
	}
	stdout, _, code := f.run(f.repo, "ensure", "--unknown-flag")
	require.Equal(t, 2, code)
	require.Empty(t, stdout, "without --json no document is written")
}
