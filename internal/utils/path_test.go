package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitisePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple branch",
			input:    "feature-user-auth",
			expected: "feature-user-auth",
		},
		{
			name:     "branch with slash",
			input:    "feature/user-auth",
			expected: "feature-user-auth",
		},
		{
			name:     "multiple slashes",
			input:    "feature/user/auth/test",
			expected: "feature-user-auth-test",
		},
		{
			name:     "just slash",
			input:    "/",
			expected: "-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitisePath(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "SSH URL",
			input:    "git@github.com:michaeldyrynda/anvil.git",
			expected: "anvil",
		},
		{
			name:     "HTTPS URL",
			input:    "https://github.com/naoray/anvil.git",
			expected: "anvil",
		},
		{
			name:     "Short format",
			input:    "michaeldyrynda/anvil",
			expected: "anvil",
		},
		{
			name:     "Just repo name",
			input:    "anvil.git",
			expected: "anvil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractRepoName(tt.input)
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
			input:    "michaeldyrynda/anvil",
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
			input:    "anvil",
			expected: true,
		},
		{
			name:     "Single name with dash",
			input:    "my-repo-name",
			expected: true,
		},
		{
			name:     "HTTP URL",
			input:    "http://github.com/user/repo",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsGitShortFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
