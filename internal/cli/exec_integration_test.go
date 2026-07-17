package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

// subprocessEnv returns os.Environ with HOME and XDG_CONFIG_HOME isolated to a
// fresh temp dir and the given KEY=VALUE overrides applied, replacing any
// inherited entry with the same key so the slice never carries duplicates.
func subprocessEnv(t *testing.T, overrides ...string) []string {
	t.Helper()
	fakeHome := t.TempDir()
	base := append(os.Environ(),
		"HOME="+fakeHome,
		"XDG_CONFIG_HOME="+filepath.Join(fakeHome, "config"),
	)
	base = append(base, overrides...)

	seen := make(map[string]int)
	out := make([]string, 0, len(base))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			out = append(out, entry)
			continue
		}
		if index, exists := seen[key]; exists {
			out[index] = entry
			continue
		}
		seen[key] = len(out)
		out = append(out, entry)
	}
	return out
}

// helperSelf returns the running test binary, re-invoked as the exec child via
// the ANVIL_TEST_HELPER/ANVIL_HELPER_MODE dispatch in TestMain.
func helperSelf(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	require.NoError(t, err)
	return self
}

func writeExecIntegrationState(t *testing.T, dir string) (appDb, testDb string) {
	t.Helper()
	appDb = "demo_brave_otter"
	testDb = "demo_brave_otter_test"
	require.NoError(t, config.WriteLocalState(dir, config.LocalState{
		DbSuffix: "brave_otter",
		Databases: []config.OwnedDatabase{
			{Name: appDb, Engine: "mysql", Role: config.DbRoleApplication},
			{Name: testDb, Engine: "mysql", Role: config.DbRoleTesting},
		},
	}))
	return appDb, testDb
}

func TestExecCommand_ExportsTestDatabase(t *testing.T) {
	bin := getAnvilBinary(t)
	dir := t.TempDir()
	appDb, testDb := writeExecIntegrationState(t, dir)

	cmd := exec.Command(bin, "exec", "--", helperSelf(t))
	cmd.Dir = dir
	cmd.Env = subprocessEnv(t,
		"DB_DATABASE=should_be_replaced",
		"ANVIL_TEST_HELPER=1",
		"ANVIL_HELPER_MODE=env",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Run(), "stdout: %s\nstderr: %s", stdout.String(), stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "DB_DATABASE="+testDb+"\n")
	assert.Contains(t, out, "ANVIL_TEST_DB_DATABASE="+testDb+"\n")
	assert.Contains(t, out, "ANVIL_DB_DATABASE="+appDb+"\n")
	assert.NotContains(t, out, "should_be_replaced")
}

func TestExecCommand_ArgvFidelity(t *testing.T) {
	bin := getAnvilBinary(t)
	dir := t.TempDir()
	writeExecIntegrationState(t, dir)

	cmd := exec.Command(bin, "exec", "--", helperSelf(t), "hello world", "--flag", "-x", "trailing space ")
	cmd.Dir = dir
	cmd.Env = subprocessEnv(t,
		"ANVIL_TEST_HELPER=1",
		"ANVIL_HELPER_MODE=env",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Run(), "stdout: %s\nstderr: %s", stdout.String(), stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "argv[0]=hello world\n")
	assert.Contains(t, out, "argv[1]=--flag\n")
	assert.Contains(t, out, "argv[2]=-x\n")
	assert.Contains(t, out, "argv[3]=trailing space \n")
}

func TestExecCommand_PropagatesExitCode(t *testing.T) {
	bin := getAnvilBinary(t)
	dir := t.TempDir()
	writeExecIntegrationState(t, dir)

	cmd := exec.Command(bin, "exec", "--", helperSelf(t))
	cmd.Dir = dir
	cmd.Env = subprocessEnv(t,
		"ANVIL_TEST_HELPER=1",
		"ANVIL_HELPER_MODE=exit",
		"ANVIL_HELPER_EXIT=7",
	)

	err := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 7, exitErr.ExitCode())
}

func TestExecCommand_StdinStdoutFidelity(t *testing.T) {
	bin := getAnvilBinary(t)
	dir := t.TempDir()
	writeExecIntegrationState(t, dir)
	payload := []byte("ref deadbeef refs/heads/main\nline2 with spaces\nfinal line no newline")

	cmd := exec.Command(bin, "exec", "--", helperSelf(t))
	cmd.Dir = dir
	cmd.Env = subprocessEnv(t,
		"ANVIL_TEST_HELPER=1",
		"ANVIL_HELPER_MODE=cat",
	)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Run(), "stderr: %s", stderr.String())
	assert.Equal(t, payload, stdout.Bytes(), "stdin must reach the child and stdout must return verbatim")
}

func TestExecCommand_StderrFidelity(t *testing.T) {
	bin := getAnvilBinary(t)
	dir := t.TempDir()
	writeExecIntegrationState(t, dir)
	message := "worker 3 failed: deadlock detected\nsecond stderr line"

	cmd := exec.Command(bin, "exec", "--", helperSelf(t))
	cmd.Dir = dir
	cmd.Env = subprocessEnv(t,
		"ANVIL_TEST_HELPER=1",
		"ANVIL_HELPER_MODE=stderr",
		"ANVIL_HELPER_STDERR="+message,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Run(), "stderr: %s", stderr.String())
	assert.Equal(t, message, stderr.String(), "child stderr bytes must arrive on the outer stderr exactly")
	assert.Empty(t, stdout.String(), "stderr output must not leak onto stdout")
}

func TestExecCommand_CommandNotFound127(t *testing.T) {
	bin := getAnvilBinary(t)
	dir := t.TempDir()
	writeExecIntegrationState(t, dir)

	cmd := exec.Command(bin, "exec", "--", "definitely-not-a-real-command-anvil-2273")
	cmd.Dir = dir
	cmd.Env = subprocessEnv(t)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 127, exitErr.ExitCode())
	assert.Contains(t, stderr.String(), "command not found: definitely-not-a-real-command-anvil-2273")
}

func TestExecCommand_NoCommandPrintsValidationErrorOnce(t *testing.T) {
	bin := getAnvilBinary(t)
	cmd := exec.Command(bin, "exec")
	cmd.Env = subprocessEnv(t)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 1, exitErr.ExitCode())
	assert.Empty(t, stdout.String())
	const validation = "requires at least 1 arg(s), only received 0"
	assert.Equal(t, 1, strings.Count(stderr.String(), validation), stderr.String())
	assert.NotContains(t, stderr.String(), "Usage:")
	assert.NotContains(t, stderr.String(), "Error:")
}

func TestExecCommand_UnknownAnvilFlagPrintsValidationErrorOnce(t *testing.T) {
	bin := getAnvilBinary(t)
	cmd := exec.Command(bin, "exec", "--anvil-invalid")
	cmd.Env = subprocessEnv(t)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 1, exitErr.ExitCode())
	assert.Empty(t, stdout.String())
	const validation = "unknown flag: --anvil-invalid"
	assert.Equal(t, 1, strings.Count(stderr.String(), validation), stderr.String())
	assert.NotContains(t, stderr.String(), "Usage:")
	assert.NotContains(t, stderr.String(), "Error:")
}

func TestExecCommand_ErrorsOutsideScaffoldedWorktree(t *testing.T) {
	bin := getAnvilBinary(t)
	dir := t.TempDir()

	cmd := exec.Command(bin, "exec", "--", helperSelf(t))
	cmd.Dir = dir
	cmd.Env = subprocessEnv(t,
		"ANVIL_TEST_HELPER=1",
		"ANVIL_HELPER_MODE=env",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 1, exitErr.ExitCode())
	assert.Contains(t, stderr.String(), "no .anvil.local found")
	assert.Contains(t, stderr.String(), "anvil scaffold")
}

func TestExecCommand_LegacyWorktreeFailSafe(t *testing.T) {
	bin := getAnvilBinary(t)
	dir := t.TempDir()
	require.NoError(t, config.WriteLocalState(dir, config.LocalState{DbSuffix: "top_provider"}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"),
		[]byte("DB_CONNECTION=mysql\nDB_DATABASE=demo_top_provider\n"), 0o644))
	before, err := os.ReadFile(filepath.Join(dir, config.LocalStateFile))
	require.NoError(t, err)

	cmd := exec.Command(bin, "exec", "--", helperSelf(t))
	cmd.Dir = dir
	cmd.Env = subprocessEnv(t,
		"ANVIL_TEST_HELPER=1",
		"ANVIL_HELPER_MODE=env",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, runErr, &exitErr)
	assert.Equal(t, 1, exitErr.ExitCode())
	assert.Contains(t, stderr.String(), "v1.8")
	assert.Contains(t, stderr.String(), "migrate:fresh")

	after, err := os.ReadFile(filepath.Join(dir, config.LocalStateFile))
	require.NoError(t, err)
	assert.Equal(t, before, after, "anvil exec must never modify .anvil.local")
}

func TestExecCommand_NoFirstRunWizard(t *testing.T) {
	bin := getAnvilBinary(t)
	dir := t.TempDir()
	_, testDb := writeExecIntegrationState(t, dir)

	configHome := t.TempDir()
	cmd := exec.Command(bin, "exec", "--", helperSelf(t))
	cmd.Dir = dir
	cmd.Env = subprocessEnv(t,
		"XDG_CONFIG_HOME="+configHome,
		"ANVIL_TEST_HELPER=1",
		"ANVIL_HELPER_MODE=env",
	)
	cmd.Stdin = nil // closed stdin: a wizard prompt would fail, not hang
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Run(), "stdout: %s\nstderr: %s", stdout.String(), stderr.String())
	assert.Contains(t, stdout.String(), "DB_DATABASE="+testDb+"\n")

	entries, err := os.ReadDir(configHome)
	require.NoError(t, err)
	assert.Empty(t, entries, "exec must not create global config state")
}
