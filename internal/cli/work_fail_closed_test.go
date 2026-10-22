package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireFailedPreparation asserts that machine-readable readiness agrees
// with a failed scaffold.
func requireFailedPreparation(t *testing.T, f *agentFixture, name string) {
	t.Helper()
	status, stderr, code := f.runJSON(f.repo, "status", name)
	require.Equal(t, 0, code, stderr)
	require.Equal(t, false, field(t, status, "data", "ready"))
	require.Equal(t, false, field(t, status, "data", "prepare", "ready"))
	require.Equal(t, "failed", field(t, status, "data", "prepare", "status"))
	require.NotEmpty(t, field(t, status, "data", "prepare", "failed_step"))
}

func TestWorkScaffoldFailureNeverReportsReady(t *testing.T) {
	yaml := "scaffold:\n  override: true\n  steps:\n    - name: command.run\n      command: echo run >> once.count\n    - name: command.run\n      command: \"false\"\n"
	f := newAgentFixture(t, yaml, nil)

	stdout, stderr, code := f.run(f.repo, "work", "feature/broken")
	require.NotEqual(t, 0, code, stdout+stderr)
	require.NotContains(t, stdout+stderr, "Worktree ready")
	require.Contains(t, stdout+stderr, "Scaffold steps failed")
	requireFailedPreparation(t, f, "feature-broken")

	// Re-scaffolding an existing worktree fails the same way.
	stdout, stderr, code = f.run(f.repo, "scaffold", "feature-broken")
	require.NotEqual(t, 0, code, stdout+stderr)
	require.NotContains(t, stdout+stderr, "no worktree found", "the scaffold must reach the failing step")
	require.Contains(t, stdout+stderr, "step command.run failed")
	require.NotContains(t, stdout+stderr, "Scaffold complete")
	requireFailedPreparation(t, f, "feature-broken")
}

// A configured server database that cannot be reached must fail `anvil
// work` instead of silently continuing without a database.
func TestWorkFailsWhenConfiguredDatabaseIsUnreachable(t *testing.T) {
	for _, engine := range []string{"mysql", "pgsql"} {
		t.Run(engine, func(t *testing.T) {
			yaml := "scaffold:\n  override: true\n  steps:\n    - name: db.create\n      type: " + engine + "\n    - name: command.run\n      command: echo run >> after.count\n"
			f := newAgentFixture(t, yaml, nil)
			fakeBin := t.TempDir()
			// exec.LookPath only finds PATHEXT executables on Windows.
			name, script := "yerd", "#!/bin/sh\necho \"yerd: connection refused\" >&2\nexit 1\n"
			if runtime.GOOS == "windows" {
				name, script = "yerd.bat", "@echo yerd: connection refused 1>&2\r\n@exit /b 1\r\n"
			}
			require.NoError(t, os.WriteFile(filepath.Join(fakeBin, name), []byte(script), 0o755))
			f.env = subprocessEnvFrom(append(f.env, "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH")))

			stdout, stderr, code := f.run(f.repo, "work", "feature/db")
			require.NotEqual(t, 0, code, stdout+stderr)
			require.NotContains(t, stdout+stderr, "Worktree ready")
			require.Contains(t, stdout+stderr, "connection refused")
			require.Contains(t, stdout+stderr, "start the database service or check its connection settings")
			wt := f.worktreePath("feature-db")
			require.NoFileExists(t, filepath.Join(wt, "after.count"), "steps after the failed db.create must not run")
			requireFailedPreparation(t, f, "feature-db")
		})
	}
}
