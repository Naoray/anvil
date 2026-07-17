package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

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
// execute DbDestroyStep.Run live (proven by the cannot-enumerate note, which
// only prints when the step attempts the worker-family listing), print the
// exact owned names, exit successfully, and mutate nothing.
//
// The fixture pins db.destroy to a controlled loopback listener that accepts
// and immediately closes every connection. This makes the cannot-enumerate
// branch deterministic without consulting ambient PostgreSQL state.
func TestRemoveBinary_DryRunLiveEnumerationNoMutation(t *testing.T) {
	bin := getAnvilBinary(t)
	base := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	accepted := make(chan struct{}, 1)
	serveDone := make(chan error, 1)
	t.Cleanup(func() {
		if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Errorf("closing fixture listener: %v", closeErr)
		}
		select {
		case serveErr := <-serveDone:
			if serveErr != nil {
				t.Errorf("serving fixture listener: %v", serveErr)
			}
		case <-time.After(time.Second):
			t.Error("fixture listener did not stop")
		}
	})
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				if errors.Is(acceptErr, net.ErrClosed) {
					serveDone <- nil
				} else {
					serveDone <- acceptErr
				}
				return
			}
			select {
			case accepted <- struct{}{}:
			default:
			}
			if closeErr := conn.Close(); closeErr != nil {
				serveDone <- closeErr
				return
			}
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port

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
projects:
  demo:
    path: %q
    default_branch: main
    preset: controlled-test
    site_name: demo
    cleanup:
      steps:
        - name: db.destroy
          args:
            - --host
            - 127.0.0.1
            - --port
            - %q
`, worktreeBase, repoDir, strconv.Itoa(port))
	require.NoError(t, os.WriteFile(
		filepath.Join(configHome, "anvil", config.ProjectConfigFile),
		[]byte(globalConfig), 0o644))

	before := snapshotTree(t, worktreeDir)

	cmd := exec.Command(bin, "remove", "feature-x", "--dry-run", "--force")
	cmd.Dir = repoDir
	cmd.Env = subprocessEnv(t, "XDG_CONFIG_HOME="+configHome)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	require.NoError(t, runErr,
		"remove --dry-run must exit successfully\nstdout: %s\nstderr: %s", stdout.String(), stderr.String())
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("controlled database endpoint was not contacted")
	}
	combined := stdout.String() + stderr.String()

	assert.Contains(t, combined, "Would drop database: "+appDb+"\n")
	assert.Contains(t, combined, "Would drop database: "+testDb+"\n")
	// This note is printed only after DbDestroyStep.Run attempted the live
	// worker-family listing through the shipped binary — the B1 evidence that
	// dry-run reaches the selector; drops are structurally impossible because
	// the server is unreachable.
	assert.Contains(t, combined, "cannot enumerate parallel-worker databases:")

	require.DirExists(t, worktreeDir, "dry-run must not remove the worktree")
	after := snapshotTree(t, worktreeDir)
	assert.Equal(t, before, after, "dry-run must leave the worktree byte-identical")
}
