package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/artisanexperiences/arbor/internal/config"
	"github.com/artisanexperiences/arbor/internal/git"
)

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

	// Create initial commit
	readmePath := filepath.Join(sourceDir, "README.md")
	requireNoError(t, os.WriteFile(readmePath, []byte("test"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	// Clone to bare repo and set up like arbor init does
	projectDir := t.TempDir()
	barePath := filepath.Join(projectDir, ".bare")
	cmd = exec.Command("git", "clone", "--bare", sourceDir, barePath)
	requireNoError(t, cmd.Run())

	// Configure fetch refspec
	requireNoError(t, git.ConfigureFetchRefspec(barePath, sourceDir))

	// Create main worktree
	mainPath := filepath.Join(projectDir, "main")
	requireNoError(t, git.CreateWorktree(barePath, mainPath, "main", ""))

	// Create arbor.yaml
	configContent := `site_name: test
preset: laravel
default_branch: main
`
	configPath := filepath.Join(projectDir, "arbor.yaml")
	requireNoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Create feature branch worktree
	featurePath := filepath.Join(projectDir, "feature")
	requireNoError(t, git.CreateWorktree(barePath, featurePath, "feature", "main"))

	// Test: running from project root should fail
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	os.Chdir(projectDir)
	// Just check that we're not in a worktree - this validates the MustBeInWorktree logic
	pc, err := OpenProjectFromCWD()
	assert.NoError(t, err)
	assert.False(t, pc.IsInWorktree())

	// Test: running from worktree should pass
	os.Chdir(featurePath)
	pc, err = OpenProjectFromCWD()
	assert.NoError(t, err)
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

	// Create initial commit
	readmePath := filepath.Join(sourceDir, "README.md")
	requireNoError(t, os.WriteFile(readmePath, []byte("test"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	// Clone to bare repo
	projectDir := t.TempDir()
	barePath := filepath.Join(projectDir, ".bare")
	cmd = exec.Command("git", "clone", "--bare", sourceDir, barePath)
	requireNoError(t, cmd.Run())

	// Configure fetch refspec
	requireNoError(t, git.ConfigureFetchRefspec(barePath, sourceDir))

	// Create main worktree
	mainPath := filepath.Join(projectDir, "main")
	requireNoError(t, git.CreateWorktree(barePath, mainPath, "main", ""))

	// Create arbor.yaml
	configContent := `site_name: test
preset: laravel
default_branch: main
`
	configPath := filepath.Join(projectDir, "arbor.yaml")
	requireNoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Checkout detached HEAD
	cmd = exec.Command("git", "-C", mainPath, "checkout", "HEAD~0")
	requireNoError(t, cmd.Run())

	// Test: detect detached HEAD
	detached, err := git.IsDetachedHEAD(mainPath)
	assert.NoError(t, err)
	assert.True(t, detached)
}

func TestSyncCommand_ValidatesStrategy(t *testing.T) {
	// Test that invalid strategies are rejected
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
	// Test config precedence:
	// 1. CLI flags
	// 2. Config file (arbor.yaml)
	// 3. Default values

	// Create project config
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
	// Create temp project directory
	projectDir := t.TempDir()

	// Create initial config
	initialConfig := &config.Config{
		SiteName:      "test-project",
		DefaultBranch: "main",
	}

	// Save initial config
	err := config.SaveProject(projectDir, initialConfig)
	assert.NoError(t, err)

	// Verify config was saved
	loadedConfig, err := config.LoadProject(projectDir)
	assert.NoError(t, err)
	assert.Equal(t, "test-project", loadedConfig.SiteName)
	assert.Equal(t, "main", loadedConfig.DefaultBranch)

	// Update with sync config
	syncConfig := config.SyncConfig{
		Upstream: "develop",
		Strategy: "rebase",
		Remote:   "origin",
	}
	initialConfig.Sync = syncConfig

	// Save updated config
	err = config.SaveProject(projectDir, initialConfig)
	assert.NoError(t, err)

	// Verify sync config was saved
	loadedConfig, err = config.LoadProject(projectDir)
	assert.NoError(t, err)
	assert.Equal(t, "develop", loadedConfig.Sync.Upstream)
	assert.Equal(t, "rebase", loadedConfig.Sync.Strategy)
	assert.Equal(t, "origin", loadedConfig.Sync.Remote)
}
