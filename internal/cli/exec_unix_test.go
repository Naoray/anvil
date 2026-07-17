//go:build !windows

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

// TestFindWorktreeRoot_StatErrorSurfaced needs a permission-denied stat, which
// os.Chmod cannot produce on Windows; this unix build leg carries the
// stat-error contract.
func TestFindWorktreeRoot_StatErrorSurfaced(t *testing.T) {
	root := t.TempDir()
	denied := filepath.Join(root, "denied")
	require.NoError(t, os.MkdirAll(denied, 0o755))
	require.NoError(t, os.Chmod(denied, 0o000))
	t.Cleanup(func() {
		require.NoError(t, os.Chmod(denied, 0o755))
	})

	_, err := findWorktreeRoot(denied)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "anvil scaffold",
		"a stat failure must surface as an error, not report not-found")
}

// TestExecCommand_ExistingNonExecutableFile126 is the B2 real-binary unix leg:
// an explicit path to an existing file without execute permission must exit
// 126, not 127.
func TestExecCommand_ExistingNonExecutableFile126(t *testing.T) {
	bin := getAnvilBinary(t)
	dir := t.TempDir()
	writeExecIntegrationState(t, dir)
	target := filepath.Join(t.TempDir(), "not-executable.sh")
	require.NoError(t, os.WriteFile(target, []byte("#!/bin/sh\necho unreachable\n"), 0o644))

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

func TestExecCommand_PathResolvedNonExecutableFile126(t *testing.T) {
	bin := getAnvilBinary(t)
	dir := t.TempDir()
	writeExecIntegrationState(t, dir)
	commandDir := t.TempDir()
	const commandName = "anvil-non-executable-fixture"
	target := filepath.Join(commandDir, commandName)
	require.NoError(t, os.WriteFile(target, []byte("unreachable\n"), 0o644))

	pathValue := commandDir + string(os.PathListSeparator) + os.Getenv("PATH")
	cmd := exec.Command(bin, "exec", "--", commandName)
	cmd.Dir = dir
	cmd.Env = subprocessEnv(t, "PATH="+pathValue)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 126, exitErr.ExitCode())
	assert.Contains(t, stderr.String(), "cannot execute")
	assert.Contains(t, stderr.String(), commandName)
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
