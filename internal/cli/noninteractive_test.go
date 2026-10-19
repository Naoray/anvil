package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScaffoldPromptPolicy(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprintf("pty=%t", terminal), func(t *testing.T) {
			python := ""
			if terminal {
				if runtime.GOOS == "windows" {
					t.Skip("POSIX controlling terminal required")
				}
				var err error
				python, err = exec.LookPath("python3")
				if err != nil {
					t.Skip("python3 required for PTY integration")
				}
			}
			cases := []struct{ name, control, ci string }{
				{"no-interactive", "--no-interactive", ""},
				{"CI", "", "true"},
				{"force", "--force", ""},
			}
			for _, tc := range cases {
				for _, target := range []string{"current", "named", "unresolved", "work"} {
					t.Run(tc.name+"/"+target, func(t *testing.T) {
						if target == "work" && tc.control == "--force" {
							t.Skip("work has no force flag")
						}
						repo, wt, env := promptPolicyFixture(t, false)
						env = append(env, "CI="+tc.ci)
						args := []string{"scaffold"}
						cwd := wt
						switch target {
						case "named":
							cwd = repo
							args = append(args, "feature-x")
						case "unresolved":
							cwd = repo
						case "work":
							cwd = repo
							args = []string{"work"}
						}
						if tc.control != "" {
							args = append(args, tc.control)
						}
						output, err := runPromptPolicyCommand(t, python, cwd, env, "", args...)
						assert.NotContains(t, output, "Welcome to Anvil!")
						assert.NotContains(t, output, "Scaffold current worktree")
						assert.NotContains(t, output, "PTY TIMEOUT")
						if target == "unresolved" || target == "work" {
							require.Error(t, err, output)
							if target == "work" {
								assert.Contains(t, output, "branch name required")
							} else {
								assert.Contains(t, output, "worktree path required")
							}
							assert.NoFileExists(t, filepath.Join(wt, ".anvil.local"))
						} else {
							require.NoError(t, err, output)
							assert.Contains(t, output, "Scaffold complete: feature-x")
							assert.FileExists(t, filepath.Join(wt, ".anvil.local"))
						}
					})
				}
			}
			if terminal {
				for _, answer := range []struct {
					name           string
					quiet, confirm bool
				}{{"decline", false, false}, {"confirm", false, true}, {"quiet", true, false}} {
					t.Run("interactive/"+answer.name, func(t *testing.T) {
						_, wt, env := promptPolicyFixture(t, true)
						args := []string{"scaffold"}
						if answer.quiet {
							args = append(args, "--quiet")
						}
						if answer.confirm {
							env = append(env, "ANVIL_TEST_PROMPT_RESPONSE=y\r")
						}
						output, err := runPromptPolicyCommand(t, python, wt, env, "Scaffold current worktree", args...)
						require.NoError(t, err, output)
						assert.Contains(t, output, "Scaffold current worktree")
						if answer.confirm {
							assert.Contains(t, output, "Scaffold complete: feature-x")
							assert.FileExists(t, filepath.Join(wt, ".anvil.local"))
						} else {
							assert.Contains(t, output, "Scaffold cancelled")
							assert.NoFileExists(t, filepath.Join(wt, ".anvil.local"))
						}
					})
				}
			}
		})
	}
}

func promptPolicyFixture(t *testing.T, setupComplete bool) (repo, worktree string, env []string) {
	t.Helper()
	repo = evalSymlinks(createLinkedProject(t))
	worktreeBase := evalSymlinks(t.TempDir())
	worktree = filepath.Join(worktreeBase, "demo", "feature-x")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature-x", worktree)
	cmd.Dir = repo
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	configHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(configHome, "anvil"), 0o755))
	cfg := fmt.Sprintf(`worktree_base: %q
setup_complete: %t
projects:
  demo:
    path: %q
    default_branch: main
`, worktreeBase, setupComplete, repo)
	require.NoError(t, os.WriteFile(filepath.Join(configHome, "anvil", "anvil.yaml"), []byte(cfg), 0o644))
	env = subprocessEnv(t, "XDG_CONFIG_HOME="+configHome, "CI=", "TERM=xterm")
	return repo, worktree, env
}

func runPromptPolicyCommand(t *testing.T, python, cwd string, env []string, prompt string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := getAnvilBinary(t)
	if python != "" {
		args = append([]string{"testdata/prompt_pty.py", prompt, command}, args...)
		script, err := filepath.Abs(args[0])
		require.NoError(t, err)
		args[0] = script
		command = python
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = cwd
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	require.NoError(t, ctx.Err(), "command timed out: %s", output)
	return string(output), err
}
