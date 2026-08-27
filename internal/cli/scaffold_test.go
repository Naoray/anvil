package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/git"
)

func getAnvilBinary(t *testing.T) string {
	t.Helper()

	binary := os.Getenv("ANVIL_TEST_BIN")
	if binary == "" {
		t.Fatal("ANVIL_TEST_BIN is not set; TestMain must build the anvil test binary before tests run")
	}
	return binary
}

func TestScaffoldRequiresProject(t *testing.T) {
	anvilBinary := getAnvilBinary(t)
	tmpDir := t.TempDir()

	anvilCmd := exec.Command(anvilBinary, "scaffold", "main", "--dry-run")
	anvilCmd.Dir = tmpDir
	output, err := anvilCmd.CombinedOutput()
	assert.Error(t, err)
	assert.Contains(t, string(output), "opening project")
}

func TestScaffoldHelp(t *testing.T) {
	anvilBinary := getAnvilBinary(t)

	anvilCmd := exec.Command(anvilBinary, "scaffold", "--help")
	output, err := anvilCmd.CombinedOutput()
	assert.NoError(t, err)
	assert.Contains(t, string(output), "Run scaffold steps for an existing worktree")
	assert.Contains(t, string(output), "[WORKTREE]")
}

func TestScaffoldInvalidWorktree(t *testing.T) {
	anvilBinary := getAnvilBinary(t)
	tmpDir := t.TempDir()

	// Create a regular git repo
	repoDir := filepath.Join(tmpDir, "repo")
	cmd := exec.Command("git", "init", "-b", "main", repoDir)
	require.NoError(t, cmd.Run())

	anvilCmd := exec.Command(anvilBinary, "scaffold", "nonexistent", "--dry-run")
	anvilCmd.Dir = repoDir
	output, err := anvilCmd.CombinedOutput()
	assert.Error(t, err)
	// May get "opening project" or "no worktrees" error depending on whether it's linked
	assert.True(t, len(output) > 0)
}

func TestScaffoldNoWorktreesInProject(t *testing.T) {
	anvilBinary := getAnvilBinary(t)
	tmpDir := t.TempDir()

	// Create a regular git repo
	repoDir := filepath.Join(tmpDir, "repo")
	cmd := exec.Command("git", "init", "-b", "main", repoDir)
	require.NoError(t, cmd.Run())

	anvilCmd := exec.Command(anvilBinary, "scaffold", "--dry-run", "--no-interactive")
	anvilCmd.Dir = repoDir
	output, err := anvilCmd.CombinedOutput()
	assert.Error(t, err)
	// May get "opening project" or "no worktrees" error
	assert.True(t, len(output) > 0)
}

func TestSelectWorktreeByContainment_NestedDirectory(t *testing.T) {
	root := t.TempDir()
	worktreePath := filepath.Join(root, "worktrees", "feature-auth")
	nestedPath := filepath.Join(worktreePath, "internal", "cli")
	require.NoError(t, os.MkdirAll(nestedPath, 0755))

	selected, err := selectWorktreeByContainment([]git.Worktree{
		{Path: worktreePath, Branch: "feature/auth"},
	}, nestedPath)

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, "feature/auth", selected.Branch)
}

func TestSelectWorktreeByContainment_ExactRoot(t *testing.T) {
	worktreePath := filepath.Join(t.TempDir(), "feature-auth")
	require.NoError(t, os.MkdirAll(worktreePath, 0755))

	selected, err := selectWorktreeByContainment([]git.Worktree{
		{Path: worktreePath, Branch: "feature/auth"},
	}, worktreePath)

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, "feature/auth", selected.Branch)
}

func TestSelectWorktreeByContainment_ExternalCustomPath(t *testing.T) {
	customRoot := t.TempDir()
	worktreePath := filepath.Join(customRoot, "custom-worktree")
	nestedPath := filepath.Join(worktreePath, "app")
	require.NoError(t, os.MkdirAll(nestedPath, 0755))

	selected, err := selectWorktreeByContainment([]git.Worktree{
		{Path: worktreePath, Branch: "feature/custom"},
	}, nestedPath)

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, "feature/custom", selected.Branch)
}

func TestSelectWorktreeByContainment_OutsideAndComponentBoundary(t *testing.T) {
	root := t.TempDir()
	worktreePath := filepath.Join(root, "feature")
	require.NoError(t, os.MkdirAll(worktreePath, 0755))

	for _, cwd := range []string{
		filepath.Join(root, "outside"),
		filepath.Join(root, "feature-sibling"),
	} {
		selected, err := selectWorktreeByContainment([]git.Worktree{{Path: worktreePath}}, cwd)

		require.NoError(t, err)
		assert.Nil(t, selected, "cwd %q must not be contained by %q", cwd, worktreePath)
	}
}

func TestSelectWorktreeByContainment_PrefersLongestRootAndKeepsPointer(t *testing.T) {
	root := t.TempDir()
	outerPath := filepath.Join(root, "outer")
	innerPath := filepath.Join(outerPath, "inner")
	nestedPath := filepath.Join(innerPath, "app")
	require.NoError(t, os.MkdirAll(nestedPath, 0755))

	selected, err := selectWorktreeByContainment([]git.Worktree{
		{Path: innerPath, Branch: "feature/inner"},
		{Path: outerPath, Branch: "feature/outer"},
	}, nestedPath)

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, innerPath, selected.Path)
	assert.Equal(t, "feature/inner", selected.Branch)
}

func TestSelectWorktreeByContainment_RespectsSymlinkSemantics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}

	root := t.TempDir()
	worktreePath := filepath.Join(root, "worktree")
	aliasPath := filepath.Join(root, "worktree-alias")
	nestedPath := filepath.Join(aliasPath, "app")
	require.NoError(t, os.MkdirAll(filepath.Join(worktreePath, "app"), 0755))
	if err := os.Symlink(worktreePath, aliasPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	selected, err := selectWorktreeByContainment([]git.Worktree{
		{Path: worktreePath, Branch: "feature/symlink"},
	}, nestedPath)

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, "feature/symlink", selected.Branch)
}

func TestScaffoldCommand_RejectsVerboseAndQuietBeforeOutput(t *testing.T) {
	err := executeRootCommandForFlagValidation(t, scaffoldCmd, []string{"scaffold", "--verbose", "--quiet"}, "verbose", "quiet")

	assert.EqualError(t, err, "if any flags in the group [verbose quiet] are set none of the others can be; [quiet verbose] were all set")
}
