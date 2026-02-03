package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/git"
	"github.com/michaeldyrynda/arbor/internal/utils"
)

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsCommandAvailable(t *testing.T) {
	assert.True(t, isCommandAvailable("ls"))
	assert.False(t, isCommandAvailable("this-command-does-not-exist-at-all-12345"))
}

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "SSH URL",
			input:    "git@github.com:michaeldyrynda/arbor.git",
			expected: "arbor",
		},
		{
			name:     "HTTPS URL",
			input:    "https://github.com/michaeldyrynda/arbor.git",
			expected: "arbor",
		},
		{
			name:     "Short format",
			input:    "michaeldyrynda/arbor",
			expected: "arbor",
		},
		{
			name:     "Just repo name",
			input:    "arbor",
			expected: "arbor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := utils.ExtractRepoName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsGitShortFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "Short format with user/repo",
			input:    "michaeldyrynda/arbor",
			expected: true,
		},
		{
			name:     "SSH URL",
			input:    "git@github.com:user/repo.git",
			expected: false,
		},
		{
			name:     "Full HTTPS URL",
			input:    "https://github.com/user/repo.git",
			expected: false,
		},
		{
			name:     "Just repo name",
			input:    "arbor",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := utils.IsGitShortFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInitWithShortFormatUsesGhClone(t *testing.T) {
	if !isCommandAvailable("gh") {
		t.Skip("gh CLI not available, skipping test")
	}

	tmpDir := t.TempDir()
	testRepo := "michaeldyrynda/laravel"

	barePath := filepath.Join(tmpDir, ".bare")

	ghAvailable := isCommandAvailable("gh")
	shortFormat := utils.IsGitShortFormat(testRepo)

	assert.True(t, ghAvailable, "gh should be available")
	assert.True(t, shortFormat, "testRepo should be detected as short format")

	cloneCmd := exec.Command("gh", "repo", "clone", testRepo, barePath, "--", "--bare")
	err := cloneCmd.Run()
	if err != nil {
		t.Skipf("gh repo clone skipped (may be private or not exist): %v", err)
	}

	assert.DirExists(t, barePath, "bare repo should exist after gh clone")
}

func TestGitCloneRepoWithShortFormatFails(t *testing.T) {
	tmpDir := t.TempDir()
	testRepo := "michaeldyrynda/laravel"

	barePath := filepath.Join(tmpDir, ".bare")

	gitCmd := exec.Command("git", "clone", "--bare", testRepo, barePath)
	err := gitCmd.Run()

	assert.Error(t, err, "git clone with short format should fail")
}

func TestInitShortFormatShouldUseGhClone(t *testing.T) {
	if !isCommandAvailable("gh") {
		t.Skip("gh CLI not available, skipping test")
	}

	tmpDir := t.TempDir()
	testRepo := "michaeldyrynda/laravel"

	barePath := filepath.Join(tmpDir, ".bare")

	shortFormat := utils.IsGitShortFormat(testRepo)
	assert.True(t, shortFormat, "testRepo should be detected as short format")

	err := exec.Command("gh", "repo", "clone", testRepo, barePath, "--", "--bare").Run()
	if err != nil {
		t.Skipf("gh repo clone skipped (may be private or not exist): %v", err)
	}

	assert.DirExists(t, barePath, "gh repo clone --bare should succeed")
	assert.DirExists(t, filepath.Join(barePath, "refs"), "bare repo should have refs directory")
}

func TestInitCommand_ConfiguresFetchRefspec(t *testing.T) {
	// Create a local test repo to clone from
	sourceDir := t.TempDir()
	barePath := filepath.Join(t.TempDir(), ".bare")

	// Initialize source repo
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	// Configure git user
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
	cmd = exec.Command("git", "clone", "--bare", sourceDir, barePath)
	requireNoError(t, cmd.Run())

	// Configure fetch refspec (simulating what init command does)
	remoteURL := sourceDir
	err := git.ConfigureFetchRefspec(barePath, remoteURL)
	assert.NoError(t, err)

	// Verify remote.origin.url is set
	cmd = exec.Command("git", "-C", barePath, "config", "--get", "remote.origin.url")
	output, err := cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, remoteURL, strings.TrimSpace(string(output)))

	// Verify fetch refspec is set
	cmd = exec.Command("git", "-C", barePath, "config", "--get", "remote.origin.fetch")
	output, err = cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, "+refs/heads/*:refs/remotes/origin/*", strings.TrimSpace(string(output)))
}

func TestCheckAndCopyRepoConfig_SkipsWhenProjectConfigExists(t *testing.T) {
	projectDir := t.TempDir()
	mainPath := filepath.Join(projectDir, "main")
	requireNoError(t, os.MkdirAll(mainPath, 0755))

	repoConfigPath := filepath.Join(mainPath, "arbor.yaml")
	repoContent := []byte("preset: laravel\n")
	requireNoError(t, os.WriteFile(repoConfigPath, repoContent, 0644))

	projectConfigPath := filepath.Join(projectDir, "arbor.yaml")
	projectContent := []byte("preset: php\nsite_name: local\n")
	requireNoError(t, os.WriteFile(projectConfigPath, projectContent, 0644))

	cmd := &cobra.Command{}
	cmd.Flags().Bool("use-repo-config", true, "")

	cfg := &config.Config{SiteName: "local"}
	copied, err := checkAndCopyRepoConfig(cmd, mainPath, projectDir, cfg)
	requireNoError(t, err)
	assert.False(t, copied, "expected repo config copy to be skipped when project config exists")

	content, err := os.ReadFile(projectConfigPath)
	requireNoError(t, err)
	assert.Equal(t, string(projectContent), string(content))
}
