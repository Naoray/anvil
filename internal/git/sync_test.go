package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsDetachedHEAD(t *testing.T) {
	// Create a temp directory for the test repo
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "repo")

	// Initialize a git repo
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	exec.Command("git", "-C", repoPath, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", repoPath, "config", "user.name", "Test User").Run()

	// Create initial commit
	testFile := filepath.Join(repoPath, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	exec.Command("git", "-C", repoPath, "add", "test.txt").Run()
	exec.Command("git", "-C", repoPath, "commit", "-m", "initial commit").Run()

	// Test: not on detached HEAD (on main/master branch)
	detached, err := IsDetachedHEAD(repoPath)
	if err != nil {
		t.Fatalf("IsDetachedHEAD failed: %v", err)
	}
	if detached {
		t.Error("expected not to be on detached HEAD, but was")
	}

	// Checkout a detached HEAD
	cmd = exec.Command("git", "-C", repoPath, "checkout", "HEAD~0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to checkout detached HEAD: %v", err)
	}

	// Test: on detached HEAD
	detached, err = IsDetachedHEAD(repoPath)
	if err != nil {
		t.Fatalf("IsDetachedHEAD failed: %v", err)
	}
	if !detached {
		t.Error("expected to be on detached HEAD, but wasn't")
	}
}

func TestGetCurrentBranch(t *testing.T) {
	// Create a temp directory for the test repo
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "repo")

	// Initialize a git repo
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	exec.Command("git", "-C", repoPath, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", repoPath, "config", "user.name", "Test User").Run()

	// Create initial commit on main
	testFile := filepath.Join(repoPath, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	exec.Command("git", "-C", repoPath, "add", "test.txt").Run()
	exec.Command("git", "-C", repoPath, "commit", "-m", "initial commit").Run()

	// Test: should get main or master branch
	branch, err := GetCurrentBranch(repoPath)
	if err != nil {
		t.Fatalf("GetCurrentBranch failed: %v", err)
	}
	if branch != "main" && branch != "master" {
		t.Errorf("expected 'main' or 'master', got %q", branch)
	}

	// Create and checkout a new branch
	cmd = exec.Command("git", "-C", repoPath, "checkout", "-b", "feature-branch")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create branch: %v", err)
	}

	// Test: should get feature-branch
	branch, err = GetCurrentBranch(repoPath)
	if err != nil {
		t.Fatalf("GetCurrentBranch failed: %v", err)
	}
	if branch != "feature-branch" {
		t.Errorf("expected 'feature-branch', got %q", branch)
	}

	// Checkout a detached HEAD
	cmd = exec.Command("git", "-C", repoPath, "checkout", "HEAD~0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to checkout detached HEAD: %v", err)
	}

	// Test: detached HEAD should return empty string
	branch, err = GetCurrentBranch(repoPath)
	if err != nil {
		t.Fatalf("GetCurrentBranch failed: %v", err)
	}
	if branch != "" {
		t.Errorf("expected empty string for detached HEAD, got %q", branch)
	}
}

func TestIsWorktreeDirty(t *testing.T) {
	// Create a temp directory for the test repo
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "repo")

	// Initialize a git repo
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	exec.Command("git", "-C", repoPath, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", repoPath, "config", "user.name", "Test User").Run()

	// Create initial commit
	testFile := filepath.Join(repoPath, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	exec.Command("git", "-C", repoPath, "add", "test.txt").Run()
	exec.Command("git", "-C", repoPath, "commit", "-m", "initial commit").Run()

	// Test: clean worktree
	isDirty, err := IsWorktreeDirty(repoPath)
	if err != nil {
		t.Fatalf("IsWorktreeDirty failed: %v", err)
	}
	if isDirty {
		t.Error("expected clean worktree, but was dirty")
	}

	// Modify file
	if err := os.WriteFile(testFile, []byte("modified"), 0644); err != nil {
		t.Fatalf("failed to modify test file: %v", err)
	}

	// Test: dirty worktree
	isDirty, err = IsWorktreeDirty(repoPath)
	if err != nil {
		t.Fatalf("IsWorktreeDirty failed: %v", err)
	}
	if !isDirty {
		t.Error("expected dirty worktree, but was clean")
	}
}

func TestRebaseConflictError(t *testing.T) {
	err := &RebaseConflictError{Output: "CONFLICT (content): Merge conflict in file.txt"}
	expected := "rebase has conflicts:\nCONFLICT (content): Merge conflict in file.txt\n\nResolve the conflicts and run 'git rebase --continue', or run 'git rebase --abort' to cancel"
	if err.Error() != expected {
		t.Errorf("expected error message:\n%s\n\ngot:\n%s", expected, err.Error())
	}
}

func TestMergeConflictError(t *testing.T) {
	err := &MergeConflictError{Output: "CONFLICT (content): Merge conflict in file.txt"}
	expected := "merge has conflicts:\nCONFLICT (content): Merge conflict in file.txt\n\nResolve the conflicts, stage the changes with 'git add', then run 'git commit' to complete the merge, or run 'git merge --abort' to cancel"
	if err.Error() != expected {
		t.Errorf("expected error message:\n%s\n\ngot:\n%s", expected, err.Error())
	}
}
