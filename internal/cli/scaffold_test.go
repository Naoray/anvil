package cli

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getArborBinary(t *testing.T) string {
	t.Helper()

	binary := "/tmp/arbor"
	if _, err := exec.LookPath(binary); err != nil {
		t.Skip("arbor binary not found at /tmp/arbor - build with: go build -o /tmp/arbor ./cmd/arbor")
	}
	return binary
}

func TestScaffoldRequiresProject(t *testing.T) {
	arborBinary := getArborBinary(t)
	tmpDir := t.TempDir()

	arborCmd := exec.Command(arborBinary, "scaffold", "main", "--dry-run")
	arborCmd.Dir = tmpDir
	output, err := arborCmd.CombinedOutput()
	assert.Error(t, err)
	assert.Contains(t, string(output), "opening project")
}

func TestScaffoldHelp(t *testing.T) {
	arborBinary := getArborBinary(t)

	arborCmd := exec.Command(arborBinary, "scaffold", "--help")
	output, err := arborCmd.CombinedOutput()
	assert.NoError(t, err)
	assert.Contains(t, string(output), "Run scaffold steps for an existing worktree")
	assert.Contains(t, string(output), "[PATH]")
}

func TestScaffoldInvalidWorktree(t *testing.T) {
	arborBinary := getArborBinary(t)
	tmpDir := t.TempDir()

	barePath := filepath.Join(tmpDir, ".bare")
	cmd := exec.Command("git", "init", "--bare", barePath)
	require.NoError(t, cmd.Run())

	arborYamlPath := filepath.Join(tmpDir, "arbor.yaml")
	cmd = exec.Command("bash", "-c", "echo 'default_branch: main' > "+arborYamlPath)
	require.NoError(t, cmd.Run())

	arborCmd := exec.Command(arborBinary, "scaffold", "nonexistent", "--dry-run")
	arborCmd.Dir = tmpDir
	output, err := arborCmd.CombinedOutput()
	assert.Error(t, err)
	assert.Contains(t, string(output), "no worktrees found in project")
}

func TestScaffoldNoWorktreesInProject(t *testing.T) {
	arborBinary := getArborBinary(t)
	tmpDir := t.TempDir()

	barePath := filepath.Join(tmpDir, ".bare")
	cmd := exec.Command("git", "init", "--bare", barePath)
	require.NoError(t, cmd.Run())

	arborYamlPath := filepath.Join(tmpDir, "arbor.yaml")
	cmd = exec.Command("bash", "-c", "echo 'default_branch: main' > "+arborYamlPath)
	require.NoError(t, cmd.Run())

	arborCmd := exec.Command(arborBinary, "scaffold", "--dry-run", "--no-interactive")
	arborCmd.Dir = tmpDir
	output, err := arborCmd.CombinedOutput()
	assert.Error(t, err)
	assert.Contains(t, string(output), "no worktrees found")
}
