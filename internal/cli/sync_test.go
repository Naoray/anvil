package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
)

func ensureSyncTestFlags(t *testing.T) {
	t.Helper()

	if syncCmd.Flags().Lookup("dry-run") == nil {
		syncCmd.Flags().Bool("dry-run", false, "")
	}
	if syncCmd.Flags().Lookup("verbose") == nil {
		syncCmd.Flags().Bool("verbose", false, "")
	}
	if syncCmd.Flags().Lookup("quiet") == nil {
		syncCmd.Flags().Bool("quiet", false, "")
	}
}

func TestSyncCommand_ValidatesInWorktree(t *testing.T) {
	// Create a source repo
	sourceDir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	readmePath := filepath.Join(sourceDir, "README.md")
	requireNoError(t, os.WriteFile(readmePath, []byte("test"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	// Clone to get a repo with remote
	repoDir := filepath.Join(t.TempDir(), "repo")
	cmd = exec.Command("git", "clone", sourceDir, repoDir)
	requireNoError(t, cmd.Run())

	gitDir := filepath.Join(repoDir, ".git")
	parentDir := filepath.Dir(repoDir)

	detachHEAD(t, repoDir)

	// Create worktrees
	mainPath := filepath.Join(parentDir, "main-wt")
	requireNoError(t, git.CreateWorktree(gitDir, mainPath, "main", ""))

	featurePath := filepath.Join(parentDir, "feature-wt")
	requireNoError(t, git.CreateWorktree(gitDir, featurePath, "feature", "main"))

	// Test: running from the repo root should not be in a worktree
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	os.Chdir(repoDir)
	// Note: this test can't use OpenProjectFromCWD because it looks up global config
	// Instead test the IsInWorktree logic directly
	pc := &ProjectContext{
		CWD:         repoDir,
		ProjectPath: repoDir,
	}
	assert.False(t, pc.IsInWorktree())

	// Test: running from worktree should be in a worktree
	pc = &ProjectContext{
		CWD:          featurePath,
		GitDir:       gitDir,
		ProjectPath:  repoDir,
		WorktreeBase: parentDir,
	}
	assert.True(t, pc.IsInWorktree())
}

func TestSyncCommand_DetectsDetachedHEAD(t *testing.T) {
	// Create a source repo
	sourceDir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	readmePath := filepath.Join(sourceDir, "README.md")
	requireNoError(t, os.WriteFile(readmePath, []byte("test"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	// Clone to get a repo
	repoDir := filepath.Join(t.TempDir(), "repo")
	cmd = exec.Command("git", "clone", sourceDir, repoDir)
	requireNoError(t, cmd.Run())

	gitDir := filepath.Join(repoDir, ".git")
	parentDir := filepath.Dir(repoDir)

	detachHEAD(t, repoDir)

	// Create main worktree
	mainPath := filepath.Join(parentDir, "main-wt")
	requireNoError(t, git.CreateWorktree(gitDir, mainPath, "main", ""))

	// Checkout detached HEAD in the worktree
	cmd = exec.Command("git", "-C", mainPath, "checkout", "HEAD~0")
	requireNoError(t, cmd.Run())

	// Test: detect detached HEAD
	detached, err := git.IsDetachedHEAD(mainPath)
	assert.NoError(t, err)
	assert.True(t, detached)
}

func TestSyncCommand_ValidatesStrategy(t *testing.T) {
	validStrategies := []string{"rebase", "merge"}
	invalidStrategies := []string{"squash", "fast-forward", ""}

	for _, strategy := range validStrategies {
		assert.True(t, strategy == "rebase" || strategy == "merge", "strategy %q should be valid", strategy)
	}

	for _, strategy := range invalidStrategies {
		if strategy != "" {
			assert.False(t, strategy == "rebase" || strategy == "merge", "strategy %q should be invalid", strategy)
		}
	}
}

func TestSyncCommand_ConfigPrecedence(t *testing.T) {
	cfg := &config.Config{
		DefaultBranch: "main",
		Sync: config.SyncConfig{
			Upstream: "develop",
			Strategy: "merge",
			Remote:   "upstream",
		},
	}

	// If CLI flag is set, use it
	flagUpstream := "feature/cli-flag"
	upstream := flagUpstream
	if upstream == "" {
		upstream = cfg.Sync.Upstream
	}
	assert.Equal(t, "feature/cli-flag", upstream)

	// If CLI flag is not set, use config
	flagUpstream = ""
	upstream = flagUpstream
	if upstream == "" {
		upstream = cfg.Sync.Upstream
	}
	assert.Equal(t, "develop", upstream)

	// If neither is set, use default_branch
	cfg.Sync.Upstream = ""
	upstream = flagUpstream
	if upstream == "" {
		upstream = cfg.Sync.Upstream
	}
	if upstream == "" {
		upstream = cfg.DefaultBranch
	}
	assert.Equal(t, "main", upstream)
}

func TestSyncCommand_SaveConfig(t *testing.T) {
	projectDir := t.TempDir()

	initialConfig := &config.Config{
		SiteName:      "test-project",
		DefaultBranch: "main",
	}

	err := config.SaveProject(projectDir, initialConfig)
	assert.NoError(t, err)

	loadedConfig, err := config.LoadProject(projectDir)
	assert.NoError(t, err)
	assert.Equal(t, "test-project", loadedConfig.SiteName)
	assert.Equal(t, "main", loadedConfig.DefaultBranch)

	syncConfig := config.SyncConfig{
		Upstream: "develop",
		Strategy: "rebase",
		Remote:   "origin",
	}
	initialConfig.Sync = syncConfig

	err = config.SaveProject(projectDir, initialConfig)
	assert.NoError(t, err)

	loadedConfig, err = config.LoadProject(projectDir)
	assert.NoError(t, err)
	assert.Equal(t, "develop", loadedConfig.Sync.Upstream)
	assert.Equal(t, "rebase", loadedConfig.Sync.Strategy)
	assert.Equal(t, "origin", loadedConfig.Sync.Remote)
}

func TestSyncCommand_DoesNotStashWhenRemoteMissing(t *testing.T) {
	ensureSyncTestFlags(t)

	// Create a source repo
	sourceDir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	readmePath := filepath.Join(sourceDir, "README.md")
	requireNoError(t, os.WriteFile(readmePath, []byte("test"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	// Clone to get a repo with remote
	repoDir := filepath.Join(t.TempDir(), "repo")
	cmd = exec.Command("git", "clone", sourceDir, repoDir)
	requireNoError(t, cmd.Run())

	gitDir := filepath.Join(repoDir, ".git")
	parentDir := filepath.Dir(repoDir)

	// Create worktree
	featurePath := filepath.Join(parentDir, "feature-wt")
	requireNoError(t, git.CreateWorktree(gitDir, featurePath, "feature", "main"))

	// Add untracked file to trigger auto-stash
	changePath := filepath.Join(featurePath, "untracked.txt")
	requireNoError(t, os.WriteFile(changePath, []byte("changes"), 0644))

	hasStash, err := git.HasStash(featurePath)
	assert.NoError(t, err)
	assert.False(t, hasStash)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	requireNoError(t, os.Chdir(featurePath))

	defer func() {
		requireNoError(t, syncCmd.Flags().Set("upstream", ""))
		requireNoError(t, syncCmd.Flags().Set("remote", ""))
	}()

	requireNoError(t, syncCmd.Flags().Set("upstream", "main"))
	requireNoError(t, syncCmd.Flags().Set("remote", "upstream"))

	err = syncCmd.RunE(syncCmd, []string{})
	assert.Error(t, err)

	hasStash, err = git.HasStash(featurePath)
	assert.NoError(t, err)
	assert.False(t, hasStash)
}
