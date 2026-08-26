package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// StashAll creates a stash including tracked modifications and untracked files
// This captures tracked modifications and untracked files, but skips ignored files
// for better performance (ignored files like node_modules, vendor are not touched by git during sync anyway)
func StashAll(worktreePath string, message string) (string, error) {
	cmd := exec.Command("git", "-C", worktreePath, "stash", "push", "--include-untracked", "-m", message)
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := string(output)
		// Check if the error is because there's nothing to stash
		if strings.Contains(outputStr, "No local changes to save") {
			return "", nil // Not an error, just nothing to stash
		}
		return "", fmt.Errorf("git stash failed: %w\n%s", err, outputStr)
	}
	if strings.Contains(string(output), "No local changes to save") {
		return "", nil // Not an error, just nothing to stash
	}

	cmd = exec.Command("git", "-C", worktreePath, "rev-parse", "refs/stash")
	stashOID, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("identifying created stash: %w", err)
	}
	return strings.TrimSpace(string(stashOID)), nil
}

// PopStash pops the most recent stash
// Returns an error if there are conflicts or if the pop fails
func PopStash(worktreePath string) error {
	cmd := exec.Command("git", "-C", worktreePath, "stash", "pop")
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := string(output)
		// Check if it's a conflict error
		if strings.Contains(outputStr, "CONFLICT") || strings.Contains(outputStr, "conflict") {
			return &StashConflictError{Output: outputStr}
		}
		return fmt.Errorf("git stash pop failed: %w\n%s", err, outputStr)
	}
	return nil
}

// ApplyStash applies the stash identified by its commit OID without removing it.
func ApplyStash(worktreePath string, stashOID string) error {
	cmd := exec.Command("git", "-C", worktreePath, "stash", "apply", stashOID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := string(output)
		if strings.Contains(outputStr, "CONFLICT") || strings.Contains(outputStr, "conflict") {
			return &StashConflictError{Output: outputStr}
		}
		return fmt.Errorf("git stash apply failed: %w\n%s", err, outputStr)
	}
	return nil
}

// DropStash drops only the stash reflog entry whose commit matches stashOID.
func DropStash(worktreePath string, stashOID string) error {
	cmd := exec.Command("git", "-C", worktreePath, "stash", "list", "--format=%H%x09%gd")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("listing stashes for drop: %w\n%s", err, string(output))
	}

	var stashRef string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && parts[0] == stashOID {
			stashRef = parts[1]
			break
		}
	}
	if stashRef == "" {
		return fmt.Errorf("stash %q not found in stash reflog; it was not dropped", stashOID)
	}

	cmd = exec.Command("git", "-C", worktreePath, "stash", "drop", stashRef)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git stash drop failed for %s: %w\n%s", stashRef, err, string(output))
	}
	return nil
}

// HasStash checks if there are any stashes in the repository
func HasStash(worktreePath string) (bool, error) {
	cmd := exec.Command("git", "-C", worktreePath, "stash", "list")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("checking stash list: %w", err)
	}
	return len(strings.TrimSpace(string(output))) > 0, nil
}

// HasChanges checks if there are any changes that would be captured by stash
// This includes tracked modifications and untracked files (but not ignored files)
func HasChanges(worktreePath string) (bool, error) {
	// Check for tracked modifications and untracked files
	// Note: --ignored is NOT used, so we skip ignored files for performance
	cmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("checking for changes: %w", err)
	}
	return len(strings.TrimSpace(string(output))) > 0, nil
}

// StashConflictError represents a stash apply or pop that failed due to conflicts.
type StashConflictError struct {
	Output string
}

func (e *StashConflictError) Error() string {
	return fmt.Sprintf("stash apply has conflicts:\n%s", e.Output)
}
