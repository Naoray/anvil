package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree represents a git worktree
type Worktree struct {
	Path   string
	Branch string
}

// BareRepo represents a bare git repository
type BareRepo struct {
	Path string
}

// CreateBareRepo initialises a new bare repository
func CreateBareRepo(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}

	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
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
		return cmd.Run()
	}

	// Branch doesn't exist, create from base
	if baseBranch == "" {
		baseBranch = "main"
	}

	gitArgs := []string{"-C", barePath, "worktree", "add", "-b", branch, worktreePath, baseBranch}
	cmd = exec.Command("git", gitArgs...)
	return cmd.Run()
}

// RemoveWorktree removes a worktree
func RemoveWorktree(worktreePath string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, worktreePath)

	// Find the parent directory with .bare
	parent := findBareParent(worktreePath)
	if parent == "" {
		return nil
	}

	gitArgs := []string{"-C", parent}
	gitArgs = append(gitArgs, args...)

	cmd := exec.Command("git", gitArgs...)
	return cmd.Run()
}

// ListWorktrees lists all worktrees in a bare repository
func ListWorktrees(barePath string) ([]Worktree, error) {
	cmd := exec.Command("git", "-C", barePath, "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var worktrees []Worktree
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "+ ") {
			parts := strings.SplitN(line, " ", 3)
			if len(parts) >= 3 {
				worktrees = append(worktrees, Worktree{
					Path:   parts[1],
					Branch: strings.Trim(parts[2], "[]"),
				})
			}
		}
	}

	return worktrees, nil
}

// FindBareParent finds the parent directory containing .bare
func findBareParent(path string) string {
	for {
		if strings.HasSuffix(path, "/") {
			path = strings.TrimSuffix(path, "/")
		}

		if _, err := os.Stat(filepath.Join(path, ".bare")); err == nil {
			return path
		}

		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
}

// GetDefaultBranch returns the default branch name
func GetDefaultBranch(barePath string) (string, error) {
	// Try main first, then master, then HEAD
	for _, branch := range []string{"main", "master", "develop"} {
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

// CreateGitFile creates a .git file pointing to the bare repository
func CreateGitFile(worktreePath, barePath string) error {
	relPath, err := filepath.Rel(worktreePath, barePath)
	if err != nil {
		relPath = barePath
	}

	content := "gitdir: " + relPath + "\n"
	return os.WriteFile(filepath.Join(worktreePath, ".git"), []byte(content), 0644)
}

// CloneRepo clones a repository to a bare directory
func CloneRepo(repoURL, barePath string) error {
	if err := os.MkdirAll(barePath, 0755); err != nil {
		return err
	}

	cmd := exec.Command("git", "clone", "--bare", repoURL, barePath)
	return cmd.Run()
}

// CloneRepoWithGH clones a repository using gh CLI (supports short format)
func CloneRepoWithGH(repo, barePath string) error {
	if err := os.MkdirAll(barePath, 0755); err != nil {
		return err
	}

	cmd := exec.Command("gh", "repo", "clone", repo, barePath, "--", "--bare")
	return cmd.Run()
}

// IsMerged checks if a branch is merged into another branch
func IsMerged(barePath, branch, targetBranch string) (bool, error) {
	cmd := exec.Command("git", "-C", barePath, "merge-base", "--is-ancestor", branch, targetBranch)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	return false, nil
}

// BranchExists checks if a branch exists in the repository
func BranchExists(barePath, branch string) bool {
	cmd := exec.Command("git", "-C", barePath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return cmd.Run() == nil
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
		line = strings.TrimSpace(strings.TrimPrefix(line, "* "))
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
			return "", fmt.Errorf(".bare not found in %s or any parent directory", absPath)
		}
		current = parent
	}
}

// InitFromWorktree converts an existing worktree to a bare repo structure
func InitFromWorktree(worktreePath, barePath, defaultBranch string) error {
	if err := os.MkdirAll(barePath, 0755); err != nil {
		return err
	}

	if err := CreateBareRepo(barePath); err != nil {
		return err
	}

	cmd := exec.Command("git", "push", "--mirror")
	cmd.Dir = worktreePath
	if err := cmd.Run(); err != nil {
		return err
	}

	mainPath := filepath.Join(filepath.Dir(worktreePath), defaultBranch)
	if err := os.MkdirAll(mainPath, 0755); err != nil {
		return err
	}

	if err := CreateWorktree(barePath, mainPath, defaultBranch, ""); err != nil {
		return err
	}

	files, err := os.ReadDir(worktreePath)
	if err != nil {
		return err
	}

	for _, file := range files {
		src := filepath.Join(worktreePath, file.Name())
		dst := filepath.Join(mainPath, file.Name())

		if src == dst {
			continue
		}

		if err := os.Rename(src, dst); err != nil {
			if err := copyDir(src, dst); err != nil {
				return err
			}
			if err := os.RemoveAll(src); err != nil {
				return err
			}
		}
	}

	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return copyFile(path, dstPath)
	})
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = srcFile.Seek(0, 0)
	if err != nil {
		return err
	}

	_, err = dstFile.Seek(0, 0)
	if err != nil {
		return err
	}

	_, err = srcFile.WriteTo(dstFile)
	return err
}
