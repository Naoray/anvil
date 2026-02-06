package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// ConfigureFetchRefspec sets up remote.origin.url and fetch refspec in bare repo.
// This is idempotent - safe to call multiple times.
func ConfigureFetchRefspec(gitDir, remoteURL string) error {
	// Set remote.origin.url
	cmd := exec.Command("git", "-C", gitDir, "config", "remote.origin.url", remoteURL)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("setting remote.origin.url: %w\n%s", err, string(output))
	}

	// Set fetch refspec
	cmd = exec.Command("git", "-C", gitDir, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("setting fetch refspec: %w\n%s", err, string(output))
	}

	return nil
}

// GetRemoteURL retrieves the remote URL for a given remote name.
// Returns empty string and nil error if remote is not configured.
func GetRemoteURL(gitDir, remote string) (string, error) {
	cmd := exec.Command("git", "-C", gitDir, "config", "--get", fmt.Sprintf("remote.%s.url", remote))
	output, err := cmd.Output()
	if err != nil {
		// Not configured is not an error
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("getting remote URL: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// GetRemoteURLFromWorktree extracts remote URL from a worktree's git config.
func GetRemoteURLFromWorktree(worktreePath string) (string, error) {
	cmd := exec.Command("git", "-C", worktreePath, "config", "--get", "remote.origin.url")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting remote URL from worktree: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// HasFetchRefspec checks if fetch refspec is already configured.
func HasFetchRefspec(gitDir string) (bool, error) {
	cmd := exec.Command("git", "-C", gitDir, "config", "--get", "remote.origin.fetch")
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("checking fetch refspec: %w", err)
	}
	return true, nil
}
