package cli

import (
	"testing"

	"github.com/michaeldyrynda/arbor/internal/utils"
	"github.com/stretchr/testify/assert"
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
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := utils.IsGitShortFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
