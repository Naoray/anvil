//go:build windows

package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecCommand_WindowsCaseVariantEnvReplaced(t *testing.T) {
	bin := getAnvilBinary(t)
	dir := t.TempDir()
	_, testDb := writeExecIntegrationState(t, dir)

	cmd := exec.Command(bin, "exec", "--", helperSelf(t))
	cmd.Dir = dir
	cmd.Env = subprocessEnv(t,
		"db_database=stale_case_variant",
		"ANVIL_TEST_HELPER=1",
		"ANVIL_HELPER_MODE=env",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Run(), "stdout: %s\nstderr: %s", stdout.String(), stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "DB_DATABASE="+testDb+"\n")
	assert.NotContains(t, out, "stale_case_variant")
}

func TestExecCommand_ExistingNonLaunchableFile126(t *testing.T) {
	bin := getAnvilBinary(t)
	dir := t.TempDir()
	writeExecIntegrationState(t, dir)
	target := filepath.Join(t.TempDir(), "not-launchable.txt")
	require.NoError(t, os.WriteFile(target, []byte("unreachable\n"), 0o644))

	cmd := exec.Command(bin, "exec", "--", target)
	cmd.Dir = dir
	cmd.Env = subprocessEnv(t)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 126, exitErr.ExitCode())
	assert.Contains(t, stderr.String(), "executing "+target)
	assert.Contains(t, stderr.String(), target)
	assert.NotContains(t, stderr.String(), "command not found")
}

func TestExecCommand_ExistingDirectory126(t *testing.T) {
	bin := getAnvilBinary(t)
	dir := t.TempDir()
	writeExecIntegrationState(t, dir)
	target := t.TempDir()

	cmd := exec.Command(bin, "exec", "--", target)
	cmd.Dir = dir
	cmd.Env = subprocessEnv(t)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 126, exitErr.ExitCode())
	assert.Contains(t, stderr.String(), "cannot execute")
	assert.Contains(t, stderr.String(), target)
	assert.NotContains(t, stderr.String(), "command not found")
}
