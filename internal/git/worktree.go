package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/michaeldyrynda/arbor/internal/config"
	arborerrors "github.com/michaeldyrynda/arbor/internal/errors"
)

// Worktree represents a git worktree
type Worktree struct {
	Path      string
	Branch    string
	IsMain    bool
	IsCurrent bool
	IsMerged  bool
}

// CreateWorktree creates a new worktree from a branch
func CreateWorktree(barePath, worktreePath, branch, baseBranch string) error {
	// Create worktree directory parent if needed
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0755); err != nil {
		return err
	}

	// Check if branch already exists
	cmd := exec.Command("git", "-C", barePath, "rev-parse", "--verify", "--quiet", branch)
	if err := cmd.Run(); err == nil {
		// Branch exists, just checkout
		cmd = exec.Command("git", "-C", barePath, "worktree", "add", worktreePath, branch)
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

	gitArgs := []string{"-C", barePath, "worktree", "add", "-b", branch, worktreePath, baseBranch}
	cmd = exec.Command("git", gitArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add failed: %w\n%s", err, string(output))
	}
	return nil
}

// RemoveWorktree removes a worktree
func RemoveWorktree(worktreePath string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, worktreePath)

	barePath, err := FindBarePath(worktreePath)
	if err != nil {
		return fmt.Errorf("finding bare repository: %w", err)
	}

	cmd := exec.Command("git", append([]string{"-C", barePath}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove failed: %w\n%s", err, string(output))
	}
	return nil
}

// ListWorktrees lists all worktrees in a bare repository
func ListWorktrees(barePath string) ([]Worktree, error) {
	cmd := exec.Command("git", "-C", barePath, "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	parentDir := filepath.Dir(barePath)

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
			if !filepath.IsAbs(currentPath) && parentDir != "" {
				currentPath = filepath.Join(parentDir, currentPath)
			}
		} else if strings.HasPrefix(line, "branch refs/heads/") {
			currentBranch = strings.TrimPrefix(line, "branch refs/heads/")
			currentBranch = strings.TrimSpace(currentBranch)
			if currentPath != "" && currentBranch != "" {
				worktrees = append(worktrees, Worktree{
					Path:   currentPath,
					Branch: currentBranch,
				})
				currentPath = ""
			}
		}
	}

	return worktrees, nil
}

// ListWorktreesDetailed lists all worktrees with additional metadata
func ListWorktreesDetailed(barePath, currentWorktreePath, defaultBranch string) ([]Worktree, error) {
	worktrees, err := ListWorktrees(barePath)
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
				featureInDefault, err = IsMerged(barePath, wt.Branch, defaultBranch)
				mergeStatusCache[cacheKey1] = featureInDefault
			}
			if err != nil {
				wt.IsMerged = false
				continue
			}
			cacheKey2 := defaultBranch + "->" + wt.Branch
			defaultInFeature, ok := mergeStatusCache[cacheKey2]
			if !ok {
				defaultInFeature, err = IsMerged(barePath, defaultBranch, wt.Branch)
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
func GetDefaultBranch(barePath string) (string, error) {
	// Try main first, then master, then HEAD
	for _, branch := range config.DefaultBranchCandidates {
		cmd := exec.Command("git", "-C", barePath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
		if err := cmd.Run(); err == nil {
			return branch, nil
		}
	}

	// Fall back to symbolic-ref
	cmd := exec.Command("git", "-C", barePath, "symbolic-ref", "HEAD", "--short")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// CloneRepo clones a repository to a bare directory
func CloneRepo(repoURL, barePath string) error {
	if err := os.MkdirAll(barePath, 0755); err != nil {
		return err
	}

	cmd := exec.Command("git", "clone", "--bare", repoURL, barePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w\n%s", err, string(output))
	}
	return nil
}

// CloneRepoWithGH clones a repository using gh CLI (supports short format)
func CloneRepoWithGH(repo, barePath string) error {
	if err := os.MkdirAll(barePath, 0755); err != nil {
		return err
	}

	cmd := exec.Command("gh", "repo", "clone", repo, barePath, "--", "--bare")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gh repo clone failed: %w\n%s", err, string(output))
	}
	return nil
}

// IsMerged checks if a branch is merged into another branch
func IsMerged(barePath, branch, targetBranch string) (bool, error) {
	cmd := exec.Command("git", "-C", barePath, "merge-base", "--is-ancestor", branch, targetBranch)
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
func BranchExists(barePath, branch string) bool {
	cmd := exec.Command("git", "-C", barePath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return cmd.Run() == nil
}

// DeleteBranch deletes a branch from the repository
func DeleteBranch(barePath, branch string, force bool) error {
	args := []string{"branch"}
	if force {
		args = append(args, "-D")
	} else {
		args = append(args, "-d")
	}
	args = append(args, branch)

	cmd := exec.Command("git", append([]string{"-C", barePath}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("deleting branch: %w\n%s", err, string(output))
	}
	return nil
}

// ListBranches lists all local branches in the repository (excluding current branch)
func ListBranches(barePath string) ([]string, error) {
	cmd := exec.Command("git", "-C", barePath, "branch", "--list")
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
func ListAllBranches(barePath string) ([]string, error) {
	cmd := exec.Command("git", "-C", barePath, "branch", "--list")
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
func ListRemoteBranches(barePath string) ([]string, error) {
	cmd := exec.Command("git", "-C", barePath, "branch", "-r", "--list")
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

// FindBarePath finds the bare repository path from a worktree directory
// by searching for .bare in the current directory or parent directories
func FindBarePath(worktreePath string) (string, error) {
	absPath, err := filepath.Abs(worktreePath)
	if err != nil {
		return "", err
	}

	barePath := filepath.Join(absPath, ".bare")
	if _, err := os.Stat(barePath); err == nil {
		return barePath, nil
	}

	// Search parents
	current := absPath
	for {
		barePath = filepath.Join(current, ".bare")
		if _, err := os.Stat(barePath); err == nil {
			return barePath, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf(".bare not found in %s or any parent directory: %w", absPath, arborerrors.ErrWorktreeNotFound)
		}
		current = parent
	}
}
