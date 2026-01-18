package utils

import (
	"strings"
)

// SanitisePath converts a branch name to a valid directory path
// by replacing / with - to prevent nested directories
func SanitisePath(name string) string {
	return strings.ReplaceAll(name, "/", "-")
}

// ExtractRepoName extracts the repository name from a git URL
func ExtractRepoName(url string) string {
	if strings.HasPrefix(url, "git@") {
		url = strings.TrimPrefix(url, "git@")
		parts := strings.SplitN(url, ":", 2)
		if len(parts) == 2 {
			url = parts[1]
		}
	}

	if strings.HasPrefix(url, "https://") {
		url = strings.TrimPrefix(url, "https://")
		parts := strings.SplitN(url, "/", 4)
		if len(parts) >= 3 {
			url = strings.TrimSuffix(parts[2], ".git")
			return url
		}
	}

	parts := strings.SplitN(url, "/", 2)
	if len(parts) == 2 {
		return strings.TrimSuffix(parts[1], ".git")
	}

	return strings.TrimSuffix(url, ".git")
}

// IsGitShortFormat detects if the input is a GitHub short format (user/repo)
func IsGitShortFormat(repo string) bool {
	return strings.Contains(repo, "/") &&
		!strings.Contains(repo, "@") &&
		!strings.Contains(repo, ":")
}
