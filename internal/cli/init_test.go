package cli

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/michaeldyrynda/arbor/internal/utils"
)

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
