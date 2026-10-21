package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/wtstate"
)

// cleanupBarrierHelper signals cleanup entry, then blocks on the parent's
// release byte. The ordering is deterministic across the two CLI processes.
func cleanupBarrierHelper() (err error) {
	conn, err := net.DialTimeout("tcp", os.Getenv("ANVIL_CLEANUP_BARRIER"), 30*time.Second)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, conn.Close()) }()
	if err := conn.SetDeadline(time.Now().Add(time.Minute)); err != nil {
		return err
	}
	if _, err := conn.Write([]byte{1}); err != nil {
		return err
	}
	var release [1]byte
	_, err = io.ReadFull(conn, release[:])
	return err
}

func TestLeaseMutationsRefuseDuringCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cleanup fixture uses sh")
	}
	for _, cleanup := range []string{"remove", "prune", "unlink"} {
		for _, operation := range []string{"acquire", "renew", "acquire-renew", "ensure", "release", "break"} {
			t.Run(cleanup+"/"+operation, func(t *testing.T) {
				f := newAgentFixture(t, `scaffold:
  override: true
cleanup:
  override: true
  steps:
    - name: command.run
      command: "true"
      condition:
        command: '"$ANVIL_CLEANUP_HELPER"'
`, nil)
				gitRun(t, f.repo, "remote", "add", "origin", f.repo)
				_, stderr, code := f.runJSON(f.repo, "ensure", "racing", "--skip-prepare")
				require.Equal(t, 0, code, stderr)
				path := f.worktreePath("racing")
				args := []string{"lease", "acquire", path, "--owner", "worker"}
				if operation == "ensure" {
					args = []string{"ensure", path, "--skip-prepare", "--lease-owner", "worker"}
				} else if operation != "acquire" {
					lease, stderr, code := f.runJSON(f.repo, args...)
					require.Equal(t, 0, code, stderr)
					token := field(t, lease, "data", "lease", "token").(string)
					_, err := wtstate.RenewLease(path, token, time.Hour, time.Now().Add(-2*time.Hour))
					require.NoError(t, err, "make the lease stale without a timing assumption")
					f = withEnv(f, "ANVIL_LEASE_TOKEN="+token)
					if operation != "acquire-renew" {
						args = []string{"lease", operation, path}
						if operation == "break" {
							args = append(args, "--reason", "operator")
						}
					}
				}
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				require.NoError(t, err)
				defer func() { require.NoError(t, listener.Close()) }()
				require.NoError(t, listener.(*net.TCPListener).SetDeadline(time.Now().Add(time.Minute)))
				f = withEnv(f, "ANVIL_CLEANUP_BARRIER="+listener.Addr().String(), "ANVIL_CLEANUP_HELPER="+os.Args[0], "ANVIL_TEST_HELPER=1", "ANVIL_HELPER_MODE=cleanup-barrier")
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				cleanupArgs := []string{cleanup, "--force"}
				switch cleanup {
				case "remove":
					cleanupArgs = append(cleanupArgs, "racing")
				case "unlink":
					cleanupArgs = append(cleanupArgs, "--clean")
				}
				remove := exec.CommandContext(ctx, f.bin, cleanupArgs...)
				remove.Dir, remove.Env = f.repo, f.env
				var output bytes.Buffer
				remove.Stdout, remove.Stderr = &output, &output
				require.NoError(t, remove.Start())
				waited := false
				defer func() {
					if !waited {
						cancel()
						if err := remove.Wait(); err != nil {
							t.Logf("cleanup interrupted after test failure: %v", err)
						}
					}
				}()
				conn, err := listener.Accept()
				require.NoError(t, err)
				defer func() { require.NoError(t, conn.Close()) }()
				require.NoError(t, conn.SetDeadline(time.Now().Add(time.Minute)))
				var entered [1]byte
				_, err = io.ReadFull(conn, entered[:])
				require.NoError(t, err)
				before, err := os.ReadFile(filepath.Join(path, ".anvil.local"))
				require.NoError(t, err)
				envelope, stderr, code := f.runJSON(f.repo, args...)
				require.Equal(t, 7, code, stderr)
				require.Equal(t, "cleanup_blocked", field(t, envelope, "error", "code"))
				after, err := os.ReadFile(filepath.Join(path, ".anvil.local"))
				require.NoError(t, err)
				require.Equal(t, before, after, "cleanup exclusion precedes identity and lease writes")
				_, err = conn.Write([]byte{1})
				require.NoError(t, err)
				err = remove.Wait()
				waited = true
				require.NoError(t, err, output.String())
				require.NoDirExists(t, path)
				_, _, code = f.runJSON(f.repo, "lease", "acquire", path, "--owner", "late-worker")
				expected := 3
				if cleanup == "unlink" {
					expected = 5
				}
				require.Equal(t, expected, code, "a removed target cannot be leased or recreated")
				require.NoDirExists(t, path)
			})
		}
	}
}
