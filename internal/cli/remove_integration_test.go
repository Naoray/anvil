package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
// A dry run never starts the service (no `herd services:start`): a running
// service is enumerated; a stopped one yields the exact recorded names and a
// warning instead.
func TestRemoveBinary_DryRunLiveEnumerationNoMutation(t *testing.T) {
	t.Run("running service", func(t *testing.T) { testRemoveBinaryDryRun(t, true) })
	t.Run("stopped service", func(t *testing.T) { testRemoveBinaryDryRun(t, false) })
}

func testRemoveBinaryDryRun(t *testing.T, running bool) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	if running {
		t.Cleanup(func() { require.NoError(t, listener.Close()) })
	} else {
		require.NoError(t, listener.Close())
	}
	bin := getAnvilBinary(t)
	base := t.TempDir()
	fakeBin := filepath.Join(base, "bin")
	require.NoError(t, os.MkdirAll(fakeBin, 0o755))
	commandLog := filepath.Join(base, "commands.log")
	helperBinary, err := os.ReadFile(helperSelf(t))
	require.NoError(t, err)
	helperSuffix := ""
	if runtime.GOOS == "windows" {
		helperSuffix = ".exe"
	}
	for _, name := range []string{"herd", "psql", "dropdb"} {
		require.NoError(t, os.WriteFile(
			filepath.Join(fakeBin, name+helperSuffix),
			helperBinary,
			0o755,
		))
	}

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
			{Name: appDb, Engine: "pgsql", Endpoint: &config.DatabaseEndpoint{Driver: "herd", Service: "postgresql", Host: "127.0.0.1", Port: port}, Role: config.DbRoleApplication},
			{Name: testDb, Engine: "pgsql", Endpoint: &config.DatabaseEndpoint{Driver: "herd", Service: "postgresql", Host: "127.0.0.1", Port: port}, Role: config.DbRoleTesting},
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

	env := subprocessEnv(t,
		"XDG_CONFIG_HOME="+configHome,
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"ANVIL_TEST_HELPER=1",
		"ANVIL_HELPER_MODE=database-command",
		"ANVIL_TEST_COMMAND_LOG="+commandLog,
		"ANVIL_RUNTIME_ROOT="+filepath.Join(base, "runtime"),
	)
	runAnvil := func(args ...string) (string, string) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = repoDir
		cmd.Env = env
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.NoError(t, cmd.Run(),
			"remove --dry-run must exit successfully\nstdout: %s\nstderr: %s", stdout.String(), stderr.String())
		return stdout.String(), stderr.String()
	}
	stdout, stderr := runAnvil("remove", "feature-x", "--dry-run", "--force")
	combined := stdout + stderr

	assert.Contains(t, combined, "Would drop database: "+appDb+"\n")
	assert.Contains(t, combined, "Would drop database: "+testDb+"\n")
	commands, err := os.ReadFile(commandLog)
	if !errors.Is(err, fs.ErrNotExist) {
		require.NoError(t, err)
	}
	assert.NotContains(t, string(commands), "services:start", "a dry run never starts the service")
	assert.NotContains(t, string(commands), "dropdb ")
	notEnumerated := "parallel-worker databases not enumerated: service not running"
	if running {
		assert.Equal(t, 2, strings.Count(string(commands), "psql "))
		assert.NotContains(t, combined, notEnumerated)
	} else {
		assert.Zero(t, strings.Count(string(commands), "psql "))
		assert.Contains(t, combined, notEnumerated)
	}

	jsonOut, _ := runAnvil("remove", "feature-x", "--dry-run", "--json")
	var envelope map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &envelope), jsonOut)
	assert.Equal(t, "drop", field(t, envelope, "data", "databases", "action"))
	assert.Equal(t, []any{appDb, testDb}, field(t, envelope, "data", "databases", "names"))
	if running {
		// Enumerated parallel-worker databases are reported too.
		assert.Equal(t, []any{appDb, testDb, appDb + "_test_1"}, field(t, envelope, "data", "databases", "targets"))
		assert.Contains(t, combined, "Would drop database: "+appDb+"_test_1\n")
		assert.NotContains(t, envelope["warnings"], notEnumerated)
	} else {
		assert.Equal(t, []any{appDb, testDb}, field(t, envelope, "data", "databases", "targets"))
		assert.Contains(t, envelope["warnings"], notEnumerated)
	}

	require.DirExists(t, worktreeDir, "dry-run must not remove the worktree")
	after := snapshotTree(t, worktreeDir)
	assert.Equal(t, before, after, "dry-run must leave the worktree byte-identical")
}
