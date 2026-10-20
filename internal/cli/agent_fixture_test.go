package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// agentFixture is a linked project with a committed anvil.yaml, an isolated
// global configuration and worktree base, driven through the real binary.
type agentFixture struct {
	t    *testing.T
	repo string
	base string
	env  []string
	bin  string
}

func newAgentFixture(t *testing.T, anvilYAML string, files map[string]string) *agentFixture {
	t.Helper()
	repo := evalSymlinks(t.TempDir())
	gitRun(t, repo, "init", "-b", "main")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	gitRun(t, repo, "config", "user.name", "Test")
	files = mergeFiles(files, map[string]string{"README.md": "fixture\n", ".gitignore": ".anvil.local\n*.count\n"})
	if anvilYAML != "" {
		files["anvil.yaml"] = anvilYAML
	}
	for name, content := range files {
		path := filepath.Join(repo, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "fixture")

	base := evalSymlinks(t.TempDir())
	configHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(configHome, "anvil"), 0o755))
	global := fmt.Sprintf("worktree_base: %q\nsetup_complete: true\nprojects:\n  demo:\n    path: %q\n    default_branch: main\n", base, repo)
	require.NoError(t, os.WriteFile(filepath.Join(configHome, "anvil", "anvil.yaml"), []byte(global), 0o644))
	env := subprocessEnv(t,
		"XDG_CONFIG_HOME="+configHome,
		"ANVIL_RUNTIME_ROOT="+filepath.Join(t.TempDir(), "runtime"),
		"CI=true",
	)
	return &agentFixture{t: t, repo: repo, base: base, env: env, bin: getAnvilBinary(t)}
}

func mergeFiles(files, defaults map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range defaults {
		out[k] = v
	}
	for k, v := range files {
		out[k] = v
	}
	return out
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(bytes.TrimSpace(out))
}

func (f *agentFixture) worktreePath(name string) string {
	return filepath.Join(f.base, "demo", name)
}

// run executes anvil in dir and returns stdout, stderr and the exit status.
func (f *agentFixture) run(dir string, args ...string) (string, string, int) {
	f.t.Helper()
	cmd := exec.Command(f.bin, args...)
	cmd.Dir = dir
	cmd.Env = f.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else {
		require.NoError(f.t, err)
	}
	return stdout.String(), stderr.String(), code
}

// runJSON runs a --json command and decodes the single stdout document.
func (f *agentFixture) runJSON(dir string, args ...string) (map[string]any, string, int) {
	f.t.Helper()
	stdout, stderr, code := f.run(dir, append(args, "--json")...)
	var envelope map[string]any
	decoder := json.NewDecoder(bytes.NewBufferString(stdout))
	require.NoError(f.t, decoder.Decode(&envelope), "stdout must be one JSON document:\n%s\nstderr:\n%s", stdout, stderr)
	require.False(f.t, decoder.More(), "stdout must contain exactly one JSON document:\n%s", stdout)
	require.NotContains(f.t, stdout, "\x1b[", "JSON must not contain ANSI escapes")
	require.Equal(f.t, "anvil.agent/v1", envelope["schema"])
	return envelope, stderr, code
}

// field walks a decoded JSON document by keys and indexes.
func field(t *testing.T, value any, path ...any) any {
	t.Helper()
	for _, step := range path {
		switch key := step.(type) {
		case string:
			m, ok := value.(map[string]any)
			require.True(t, ok, "expected object at %v, got %T", key, value)
			value = m[key]
		case int:
			list, ok := value.([]any)
			require.True(t, ok, "expected array at %v, got %T", key, value)
			require.Less(t, key, len(list))
			value = list[key]
		}
	}
	return value
}

func stepResults(t *testing.T, envelope map[string]any) []string {
	t.Helper()
	steps, ok := field(t, envelope, "data", "prepare", "steps").([]any)
	require.True(t, ok)
	out := make([]string, len(steps))
	for i := range steps {
		out[i] = field(t, steps[i], "result").(string)
	}
	return out
}
