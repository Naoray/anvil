package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/git"
)

func TestResolveDefaultBranchWorktree_UsesRegisteredPath(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		path string
	}{
		{name: "conventional", path: filepath.Join(root, "project", "main")},
		{name: "custom", path: filepath.Join(root, "checkout", "configuration-source")},
		{name: "primary", path: filepath.Join(root, "project")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveDefaultBranchWorktree("main", []git.Worktree{
				{Path: filepath.Join(root, "stale-main"), Branch: "feature/stale"},
				{Path: tc.path, Branch: "main"},
			})
			require.NoError(t, err)
			assert.Equal(t, tc.path, got.Path)
		})
	}
}

func TestResolveDefaultBranchWorktree_IgnoresUnregisteredConventionalPath(t *testing.T) {
	conventionalPath := filepath.Join(t.TempDir(), "project", "main")

	_, err := resolveDefaultBranchWorktree("main", []git.Worktree{
		{Path: filepath.Join(filepath.Dir(conventionalPath), "feature"), Branch: "feature/current"},
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, `default branch "main" is absent from the registered worktree inventory`)
}

func TestResolveDefaultBranchWorktree_ReportsAbsentAndDetachedOnly(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		_, err := resolveDefaultBranchWorktree("main", []git.Worktree{
			{Path: filepath.Join(t.TempDir(), "feature"), Branch: "feature/current"},
		})

		require.Error(t, err)
		assert.ErrorContains(t, err, `default branch "main" is absent from the registered worktree inventory`)
	})

	t.Run("detached-only", func(t *testing.T) {
		_, err := resolveDefaultBranchWorktree("main", []git.Worktree{
			{Path: filepath.Join(t.TempDir(), "detached"), Detached: true},
		})

		require.Error(t, err)
		assert.ErrorContains(t, err, `default branch "main" has no attached registered worktree; all registered worktrees are detached`)
	})
}

func TestResolveDefaultBranchWorktree_RejectsAmbiguityDeterministically(t *testing.T) {
	root := t.TempDir()
	first := git.Worktree{Path: filepath.Join(root, "z-source"), Branch: "main"}
	second := git.Worktree{Path: filepath.Join(root, "a-source"), Branch: "main"}

	_, firstErr := resolveDefaultBranchWorktree("main", []git.Worktree{first, second})
	_, secondErr := resolveDefaultBranchWorktree("main", []git.Worktree{second, first})

	require.Error(t, firstErr)
	require.Error(t, secondErr)
	assert.Equal(t, firstErr.Error(), secondErr.Error())
	assert.Equal(t, `multiple registered worktrees match default branch "main": `+second.Path+", "+first.Path, firstErr.Error())
}

func TestPullConfig_MissingConfig(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "project")
	sourcePath := filepath.Join(root, "custom-source")
	require.NoError(t, os.MkdirAll(projectPath, 0755))
	require.NoError(t, os.MkdirAll(sourcePath, 0755))

	err := runPullConfigForProject(
		&ProjectContext{GitDir: "unused", ProjectPath: projectPath, DefaultBranch: "main"},
		false,
		false,
		false,
		newPullConfigTestDependencies([]git.Worktree{{Path: sourcePath, Branch: "main"}}),
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "no anvil.yaml found in default branch worktree")
}

func TestPullConfig_SamePathIsSuccessfulNoOpBeforePrompt(t *testing.T) {
	projectPath := t.TempDir()
	prompted := false
	infoMessages := []string{}
	deps := newPullConfigTestDependencies([]git.Worktree{{Path: projectPath, Branch: "main"}})
	deps.printInfo = func(message string) { infoMessages = append(infoMessages, message) }
	deps.isInteractive = func() bool { return true }
	deps.confirm = func(string) (bool, error) {
		prompted = true
		return false, errors.New("prompt should not be called")
	}

	err := runPullConfigForProject(
		&ProjectContext{GitDir: "unused", ProjectPath: projectPath, DefaultBranch: "main"},
		false,
		false,
		false,
		deps,
	)

	require.NoError(t, err)
	assert.False(t, prompted)
	assert.Contains(t, strings.Join(infoMessages, "\n"), "same")
}

func TestPullConfig_ForceOverwritesExistingDestination(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "project")
	sourcePath := filepath.Join(root, "source")
	require.NoError(t, os.MkdirAll(projectPath, 0755))
	require.NoError(t, os.MkdirAll(sourcePath, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sourcePath, "anvil.yaml"), []byte("source"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "anvil.yaml"), []byte("old"), 0644))

	err := runPullConfigForProject(
		&ProjectContext{GitDir: "unused", ProjectPath: projectPath, DefaultBranch: "main"},
		true,
		false,
		false,
		newPullConfigTestDependencies([]git.Worktree{{Path: sourcePath, Branch: "main"}}),
	)

	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(projectPath, "anvil.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "source", string(got))
}

func TestPullConfig_DryRunDoesNotCopy(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "project")
	sourcePath := filepath.Join(root, "source")
	require.NoError(t, os.MkdirAll(projectPath, 0755))
	require.NoError(t, os.MkdirAll(sourcePath, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sourcePath, "anvil.yaml"), []byte("source"), 0644))

	err := runPullConfigForProject(
		&ProjectContext{GitDir: "unused", ProjectPath: projectPath, DefaultBranch: "main"},
		false,
		true,
		false,
		newPullConfigTestDependencies([]git.Worktree{{Path: sourcePath, Branch: "main"}}),
	)

	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(projectPath, "anvil.yaml"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestPullConfig_NoninteractiveExistingDestinationRequiresForce(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "project")
	sourcePath := filepath.Join(root, "source")
	require.NoError(t, os.MkdirAll(projectPath, 0755))
	require.NoError(t, os.MkdirAll(sourcePath, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sourcePath, "anvil.yaml"), []byte("source"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "anvil.yaml"), []byte("old"), 0644))

	err := runPullConfigForProject(
		&ProjectContext{GitDir: "unused", ProjectPath: projectPath, DefaultBranch: "main"},
		false,
		false,
		false,
		newPullConfigTestDependencies([]git.Worktree{{Path: sourcePath, Branch: "main"}}),
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "use --force to overwrite")
	got, readErr := os.ReadFile(filepath.Join(projectPath, "anvil.yaml"))
	require.NoError(t, readErr)
	assert.Equal(t, "old", string(got))
}

func newPullConfigTestDependencies(worktrees []git.Worktree) pullConfigDependencies {
	return pullConfigDependencies{
		listWorktrees: func(string) ([]git.Worktree, error) {
			return worktrees, nil
		},
		isInteractive: func() bool { return false },
		confirm:       func(string) (bool, error) { return false, nil },
		printInfo:     func(message string) {},
		printDone:     func(message string) {},
	}
}
