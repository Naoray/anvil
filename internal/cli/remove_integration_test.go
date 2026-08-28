package cli

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	fullArgs := append([]string{
		"-c", "user.name=anvil-test",
		"-c", "user.email=anvil-test@example.com",
		"-c", "commit.gpgsign=false",
	}, args...)
	cmd := exec.Command("git", fullArgs...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, output)
}

// snapshotTree captures every file under root as relpath -> contents so a
// dry-run can be proven byte-identical.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		snapshot[rel] = string(contents)
		return nil
	})
	require.NoError(t, err)
	return snapshot
}

// TestRemoveBinary_DryRunLiveEnumerationNoMutation is the B1-mandated
// binary-level test: `remove --dry-run` through the shipped binary must
// execute DbDestroyStep.Run live through Herd-managed service binaries, print
// the exact owned names, exit successfully, avoid dropdb, and mutate nothing.
func TestRemoveBinary_DryRunLiveEnumerationNoMutation(t *testing.T) {
	bin := getAnvilBinary(t)
	base := t.TempDir()
	fakeBin := filepath.Join(base, "bin")
	require.NoError(t, os.MkdirAll(fakeBin, 0o755))
	commandLog := filepath.Join(base, "commands.log")
	writeExecutable := func(name, contents string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(fakeBin, name), []byte(contents), 0o755))
	}
	writeExecutable("herd", "#!/bin/sh\nprintf 'herd %s\\n' \"$*\" >> \"$ANVIL_TEST_COMMAND_LOG\"\n")
	writeExecutable("psql", "#!/bin/sh\nprintf 'psql %s\\n' \"$*\" >> \"$ANVIL_TEST_COMMAND_LOG\"\nprintf '%s\\n' demo_pg_fixture demo_pg_fixture_test demo_pg_fixture_test_1 other\n")
	writeExecutable("dropdb", "#!/bin/sh\nprintf 'dropdb %s\\n' \"$*\" >> \"$ANVIL_TEST_COMMAND_LOG\"\nexit 99\n")

	repoDir := filepath.Join(base, "projects", "demo")
	require.NoError(t, os.MkdirAll(repoDir, 0o755))
	runGitCommand(t, base, "init", "-b", "main", repoDir)
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("fixture\n"), 0o644))
	runGitCommand(t, repoDir, "add", ".")
	runGitCommand(t, repoDir, "commit", "-m", "fixture commit")

	worktreeBase := filepath.Join(base, "worktrees")
	worktreeDir := filepath.Join(worktreeBase, "demo", "feature-x")
	require.NoError(t, os.MkdirAll(filepath.Dir(worktreeDir), 0o755))
	runGitCommand(t, repoDir, "worktree", "add", worktreeDir, "-b", "feature-x")

	appDb := "demo_pg_fixture"
	testDb := "demo_pg_fixture_test"
	require.NoError(t, config.WriteLocalState(worktreeDir, config.LocalState{
		DbSuffix: "pg_fixture",
		Databases: []config.OwnedDatabase{
			{Name: appDb, Engine: "pgsql", Role: config.DbRoleApplication},
			{Name: testDb, Engine: "pgsql", Role: config.DbRoleTesting},
		},
	}))

	configHome := filepath.Join(base, "config-home")
	require.NoError(t, os.MkdirAll(filepath.Join(configHome, "anvil"), 0o755))
	globalConfig := fmt.Sprintf(`default_branch: main
worktree_base: %q
setup_complete: true
site_driver: herd
projects:
  demo:
    path: %q
    default_branch: main
    preset: controlled-test
    site_name: demo
    cleanup:
      steps:
        - name: db.destroy
`, worktreeBase, repoDir)
	require.NoError(t, os.WriteFile(
		filepath.Join(configHome, "anvil", config.ProjectConfigFile),
		[]byte(globalConfig), 0o644))

	before := snapshotTree(t, worktreeDir)

	cmd := exec.Command(bin, "remove", "feature-x", "--dry-run", "--force")
	cmd.Dir = repoDir
	cmd.Env = subprocessEnv(t,
		"XDG_CONFIG_HOME="+configHome,
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"ANVIL_TEST_COMMAND_LOG="+commandLog,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	require.NoError(t, runErr,
		"remove --dry-run must exit successfully\nstdout: %s\nstderr: %s", stdout.String(), stderr.String())
	combined := stdout.String() + stderr.String()

	assert.Contains(t, combined, "Would drop database: "+appDb+"\n")
	assert.Contains(t, combined, "Would drop database: "+testDb+"\n")
	commands, err := os.ReadFile(commandLog)
	require.NoError(t, err)
	assert.Contains(t, string(commands), "herd services:start postgresql\n")
	assert.Equal(t, 2, strings.Count(string(commands), "psql "))
	assert.NotContains(t, string(commands), "dropdb ")

	require.DirExists(t, worktreeDir, "dry-run must not remove the worktree")
	after := snapshotTree(t, worktreeDir)
	assert.Equal(t, before, after, "dry-run must leave the worktree byte-identical")
}
