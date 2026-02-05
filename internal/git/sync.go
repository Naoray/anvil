package git

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// FetchRemote runs git fetch for the specified remote
func FetchRemote(barePath, remote string) error {
	cmd := exec.Command("git", "-C", barePath, "fetch", remote)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch failed: %w\n%s", err, string(output))
	}
	return nil
}

// RebaseOnto runs git rebase from the current worktree onto the specified remote/branch
func RebaseOnto(worktreePath, remote, upstream string) error {
	ref := fmt.Sprintf("%s/%s", remote, upstream)
	cmd := exec.Command("git", "-C", worktreePath, "rebase", ref)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it's a conflict by looking at output
		outputStr := string(output)
		if strings.Contains(outputStr, "CONFLICT") || strings.Contains(outputStr, "conflict") {
			return &RebaseConflictError{Output: outputStr}
		}
		return fmt.Errorf("git rebase failed: %w\n%s", err, outputStr)
	}
	return nil
}

// MergeInto runs git merge from the current worktree with the specified remote/branch
func MergeInto(worktreePath, remote, upstream string) error {
	ref := fmt.Sprintf("%s/%s", remote, upstream)
	cmd := exec.Command("git", "-C", worktreePath, "merge", ref)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it's a conflict
		outputStr := string(output)
		if strings.Contains(outputStr, "CONFLICT") || strings.Contains(outputStr, "conflict") {
			return &MergeConflictError{Output: outputStr}
		}
		return fmt.Errorf("git merge failed: %w\n%s", err, outputStr)
	}
	return nil
}

// RebaseConflictError represents a rebase that failed due to conflicts
type RebaseConflictError struct {
	Output string
}

func (e *RebaseConflictError) Error() string {
	return fmt.Sprintf("rebase has conflicts:\n%s\n\nResolve the conflicts and run 'git rebase --continue', or run 'git rebase --abort' to cancel", e.Output)
}

// MergeConflictError represents a merge that failed due to conflicts
type MergeConflictError struct {
	Output string
}

func (e *MergeConflictError) Error() string {
	return fmt.Sprintf("merge has conflicts:\n%s\n\nResolve the conflicts, stage the changes with 'git add', then run 'git commit' to complete the merge, or run 'git merge --abort' to cancel", e.Output)
}

// IsRebaseInProgress checks if a rebase is currently in progress in the worktree
func IsRebaseInProgress(worktreePath string) bool {
	cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--git-path", "rebase-apply")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	rebaseApply := strings.TrimSpace(string(output))

	cmd = exec.Command("git", "-C", worktreePath, "rev-parse", "--git-path", "rebase-merge")
	output, err = cmd.Output()
	if err != nil {
		return false
	}
	rebaseMerge := strings.TrimSpace(string(output))

	// Check if either directory exists
	if _, err := os.Stat(rebaseApply); err == nil {
		return true
	}
	if _, err := os.Stat(rebaseMerge); err == nil {
		return true
	}

	return false
}

// IsMergeInProgress checks if a merge is currently in progress in the worktree
func IsMergeInProgress(worktreePath string) bool {
	cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--git-path", "MERGE_HEAD")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	mergeHead := strings.TrimSpace(string(output))

	// Check if MERGE_HEAD file exists
	if _, err := os.Stat(mergeHead); err == nil {
		return true
	}

	return false
}

// IsDetachedHEAD checks if the worktree is on a detached HEAD
func IsDetachedHEAD(worktreePath string) (bool, error) {
	cmd := exec.Command("git", "-C", worktreePath, "symbolic-ref", "-q", "HEAD")
	err := cmd.Run()
	if err != nil {
		// If symbolic-ref fails, we're likely on detached HEAD
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true, nil
		}
		return false, fmt.Errorf("checking HEAD status: %w", err)
	}
	return false, nil
}

// GetCurrentBranch returns the current branch name, or empty string if detached HEAD
func GetCurrentBranch(worktreePath string) (string, error) {
	cmd := exec.Command("git", "-C", worktreePath, "symbolic-ref", "--short", "-q", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		// If symbolic-ref fails, we're likely on detached HEAD
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("getting current branch: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// IsWorktreeDirty checks if the worktree has uncommitted changes
func IsWorktreeDirty(worktreePath string) (bool, error) {
	cmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("checking worktree status: %w", err)
	}
	return len(strings.TrimSpace(string(output))) > 0, nil
}
