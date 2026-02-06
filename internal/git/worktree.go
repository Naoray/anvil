package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/naoray/anvil/internal/config"
)

// Worktree represents a git worktree
type Worktree struct {
	Path      string
	Branch    string
	IsMain    bool
	IsCurrent bool
	IsMerged  bool
}

// CreateWorktree creates a new worktree from a git directory
func CreateWorktree(gitDir, worktreePath, branch, baseBranch string) error {
	repoPath := GetRepoPath(gitDir)
	if filepath.Base(gitDir) != ".git" {
		if IsGitRepo(gitDir) {
			repoPath = gitDir
		}
	}

	// Create worktree directory parent if needed
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0755); err != nil {
		return err
	}

	// Check if branch already exists
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "--quiet", branch)
	if err := cmd.Run(); err == nil {
		// Branch exists, just checkout
		cmd = exec.Command("git", "-C", repoPath, "worktree", "add", worktreePath, branch)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git worktree add failed: %w\n%s", err, string(output))
		}
		return nil
	}

	// Branch doesn't exist, create from base
	if baseBranch == "" {
		baseBranch = config.DefaultBranch
	}

	gitArgs := []string{"-C", repoPath, "worktree", "add", "-b", branch, worktreePath, baseBranch}
	cmd = exec.Command("git", gitArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add failed: %w\n%s", err, string(output))
	}
	return nil
}

// RemoveWorktree removes a worktree using a specific git directory
func RemoveWorktree(gitDir, worktreePath string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, worktreePath)

	cmd := exec.Command("git", append([]string{"-C", gitDir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove failed: %w\n%s", err, string(output))
	}
	return nil
}

// ListWorktrees lists all worktrees for a git repository
func ListWorktrees(gitDir string) ([]Worktree, error) {
	repoPath := GetRepoPath(gitDir)

	cmd := exec.Command("git", "-C", repoPath, "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var worktrees []Worktree
	var currentPath string
	var currentBranch string
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "worktree ") {
			currentPath = strings.TrimPrefix(line, "worktree ")
			currentPath = strings.TrimSpace(currentPath)
		} else if strings.HasPrefix(line, "branch refs/heads/") {
			currentBranch = strings.TrimPrefix(line, "branch refs/heads/")
			currentBranch = strings.TrimSpace(currentBranch)
			if currentPath != "" && currentBranch != "" {
				worktrees = append(worktrees, Worktree{
					Path:   currentPath,
					Branch: currentBranch,
				})
				currentPath = ""
				currentBranch = "" //nolint:ineffassign // reset for clarity
			}
		}
	}

	return worktrees, nil
}

// ListWorktreesDetailed lists all worktrees with additional metadata
func ListWorktreesDetailed(gitDir, currentWorktreePath, defaultBranch string) ([]Worktree, error) {
	worktrees, err := ListWorktrees(gitDir)
	if err != nil {
		return nil, err
	}

	currentWorktreePathEval, _ := filepath.EvalSymlinks(currentWorktreePath)

	mergeStatusCache := make(map[string]bool)

	for i := range worktrees {
		wt := &worktrees[i]
		wt.IsMain = wt.Branch == defaultBranch
		wtPathEval, _ := filepath.EvalSymlinks(wt.Path)
		wt.IsCurrent = wtPathEval == currentWorktreePathEval
		if wt.Branch != defaultBranch {
			cacheKey1 := wt.Branch + "->" + defaultBranch
			featureInDefault, ok := mergeStatusCache[cacheKey1]
			if !ok {
				featureInDefault, err = IsMerged(gitDir, wt.Branch, defaultBranch)
				mergeStatusCache[cacheKey1] = featureInDefault
			}
			if err != nil {
				wt.IsMerged = false
				continue
			}
			cacheKey2 := defaultBranch + "->" + wt.Branch
			defaultInFeature, ok := mergeStatusCache[cacheKey2]
			if !ok {
				defaultInFeature, err = IsMerged(gitDir, defaultBranch, wt.Branch)
				mergeStatusCache[cacheKey2] = defaultInFeature
			}
			wt.IsMerged = featureInDefault && !defaultInFeature
		}
	}

	return worktrees, nil
}

// SortWorktrees sorts worktrees by the specified criteria
func SortWorktrees(worktrees []Worktree, by string, reverse bool) []Worktree {
	sorted := make([]Worktree, len(worktrees))
	copy(sorted, worktrees)

	var modTimeMap map[string]int64
	if by == "created" {
		modTimeMap = make(map[string]int64, len(sorted))
		for _, wt := range sorted {
			if info, err := os.Stat(wt.Path); err == nil {
				modTimeMap[wt.Path] = info.ModTime().UnixNano()
			}
		}
	}

	sort.Slice(sorted, func(i, j int) bool {
		var cmp int
		switch by {
		case "branch":
			cmp = strings.Compare(sorted[i].Branch, sorted[j].Branch)
		case "created":
			timeI := modTimeMap[sorted[i].Path]
			timeJ := modTimeMap[sorted[j].Path]
			if timeI == 0 || timeJ == 0 {
				cmp = strings.Compare(sorted[i].Path, sorted[j].Path)
			} else {
				cmp = int(timeI - timeJ)
			}
		default: // "name"
			nameI := filepath.Base(sorted[i].Path)
			nameJ := filepath.Base(sorted[j].Path)
			cmp = strings.Compare(nameI, nameJ)
		}
		if reverse {
			cmp = -cmp
		}
		return cmp < 0
	})

	return sorted
}

// GetDefaultBranch returns the default branch name
func GetDefaultBranch(gitDir string) (string, error) {
	// Try main first, then master, then HEAD
	for _, branch := range config.DefaultBranchCandidates {
		cmd := exec.Command("git", "-C", gitDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
		if err := cmd.Run(); err == nil {
			return branch, nil
		}
	}

	// Fall back to symbolic-ref
	cmd := exec.Command("git", "-C", gitDir, "symbolic-ref", "HEAD", "--short")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// IsMerged checks if a branch is merged into another branch
func IsMerged(gitDir, branch, targetBranch string) (bool, error) {
	cmd := exec.Command("git", "-C", gitDir, "merge-base", "--is-ancestor", branch, targetBranch)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git merge-base check failed: %w", err)
	}

	return false, fmt.Errorf("git command failed: %w", err)
}

// BranchExists checks if a branch exists in the repository
func BranchExists(gitDir, branch string) bool {
	cmd := exec.Command("git", "-C", gitDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return cmd.Run() == nil
}

// DeleteBranch deletes a branch from the repository
func DeleteBranch(gitDir, branch string, force bool) error {
	args := []string{"branch"}
	if force {
		args = append(args, "-D")
	} else {
		args = append(args, "-d")
	}
	args = append(args, branch)

	cmd := exec.Command("git", append([]string{"-C", gitDir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("deleting branch: %w\n%s", err, string(output))
	}
	return nil
}

// PruneWorktrees prunes stale worktree refs from the repository
func PruneWorktrees(gitDir string) error {
	cmd := exec.Command("git", "-C", gitDir, "worktree", "prune")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree prune failed: %w\n%s", err, string(output))
	}
	return nil
}

// ListBranches lists all local branches in the repository (excluding current branch)
func ListBranches(gitDir string) ([]string, error) {
	cmd := exec.Command("git", "-C", gitDir, "branch", "--list")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var branches []string
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "*") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			line = strings.TrimPrefix(line, "+ ")
			line = strings.TrimSpace(line)
		}
		if line != "" {
			branches = append(branches, line)
		}
	}
	return branches, nil
}

// ListAllBranches lists all branches including current branch
func ListAllBranches(gitDir string) ([]string, error) {
	cmd := exec.Command("git", "-C", gitDir, "branch", "--list")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var branches []string
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "*") {
			line = strings.TrimPrefix(line, "* ")
		}
		if strings.HasPrefix(line, "+") {
			line = strings.TrimPrefix(line, "+ ")
		}
		line = strings.TrimSpace(line)
		if line != "" {
			branches = append(branches, line)
		}
	}
	return branches, nil
}

// ListRemoteBranches lists all remote branches in the repository
func ListRemoteBranches(gitDir string) ([]string, error) {
	cmd := exec.Command("git", "-C", gitDir, "branch", "-r", "--list")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var branches []string
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			branches = append(branches, line)
		}
	}
	return branches, nil
}

// FindGitDir finds the .git directory from a path
func FindGitDir(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	// Check for .git directory
	gitPath := filepath.Join(absPath, ".git")
	if info, err := os.Stat(gitPath); err == nil {
		if info.IsDir() {
			return gitPath, nil
		}
		// .git could be a file pointing to worktree's actual git dir
		content, err := os.ReadFile(gitPath)
		if err == nil {
			line := strings.TrimSpace(string(content))
			if strings.HasPrefix(line, "gitdir: ") {
				actualGitDir := strings.TrimPrefix(line, "gitdir: ")
				if !filepath.IsAbs(actualGitDir) {
					actualGitDir = filepath.Join(absPath, actualGitDir)
				}
				return actualGitDir, nil
			}
		}
	}

	return "", fmt.Errorf("no .git found in %s", absPath)
}

// IsGitRepo checks if a directory is a git repository (has .git directory)
func IsGitRepo(path string) bool {
	gitPath := filepath.Join(path, ".git")
	_, err := os.Stat(gitPath)
	return err == nil
}

// GetRepoPath returns the repository working directory from a git dir
func GetRepoPath(gitDir string) string {
	return filepath.Dir(gitDir)
}
