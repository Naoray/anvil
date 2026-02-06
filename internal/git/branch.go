package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// SetBranchUpstream configures a branch to track a remote.
// This is idempotent - safe to call multiple times.
func SetBranchUpstream(gitDir, branch, remote string) error {
	cmd := exec.Command("git", "-C", gitDir, "config",
		fmt.Sprintf("branch.%s.remote", branch), remote)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("setting branch remote: %w\n%s", err, string(output))
	}

	cmd = exec.Command("git", "-C", gitDir, "config",
		fmt.Sprintf("branch.%s.merge", branch), fmt.Sprintf("refs/heads/%s", branch))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("setting branch merge: %w\n%s", err, string(output))
	}

	return nil
}

// HasBranchTracking checks if a branch has upstream tracking configured.
func HasBranchTracking(gitDir, branch string) (bool, error) {
	cmd := exec.Command("git", "-C", gitDir, "config", "--get", fmt.Sprintf("branch.%s.remote", branch))
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("checking branch tracking: %w", err)
	}
	return true, nil
}

// GetBranchRefs returns all local and remote branch names.
// Local branches are returned as-is (e.g., "main", "feature/foo").
// Remote branches are returned with remote prefix (e.g., "origin/main").
func GetBranchRefs(gitDir string) (local []string, remote []string, err error) {
	// Get local branches
	cmd := exec.Command("git", "-C", gitDir, "for-each-ref",
		"--format=%(refname:short)", "refs/heads/")
	output, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("listing local branches: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			local = append(local, line)
		}
	}

	// Get remote branches
	cmd = exec.Command("git", "-C", gitDir, "for-each-ref",
		"--format=%(refname:short)", "refs/remotes/")
	output, err = cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("listing remote branches: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" && !strings.HasSuffix(line, "/HEAD") {
			remote = append(remote, line)
		}
	}

	return local, remote, nil
}

// ListLocalBranches returns all local branch names.
func ListLocalBranches(gitDir string) ([]string, error) {
	local, _, err := GetBranchRefs(gitDir)
	return local, err
}
