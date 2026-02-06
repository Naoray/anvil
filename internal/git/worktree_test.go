package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// createTestRepo creates a regular git repo (with .git directory)
func createTestRepo(t *testing.T) string {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")

	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("creating repo dir: %v", err)
	}

	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("initializing git repo: %v", err)
	}

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("setting git user.email: %v", err)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("setting git user.name: %v", err)
	}

	readmePath := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("test"), 0644); err != nil {
		t.Fatalf("writing README: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("staging files: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("committing: %v", err)
	}

	return repoDir
}

func TestBranchExists(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	if !BranchExists(gitDir, "main") {
		t.Error("main branch should exist after creating from repo with commit")
	}

	if BranchExists(gitDir, "nonexistent") {
		t.Error("nonexistent branch should not exist")
	}
}

func TestListBranches(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	tmpDir := filepath.Dir(repoDir)

	featurePath := filepath.Join(tmpDir, "feature")
	if err := CreateWorktree(gitDir, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	branches, err := ListBranches(gitDir)
	if err != nil {
		t.Fatalf("listing branches: %v", err)
	}

	featureFound := false
	for _, b := range branches {
		if b == "feature" {
			featureFound = true
			break
		}
	}

	if !featureFound {
		t.Error("feature branch should be in list")
	}
}

func TestListAllBranches(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	branches, err := ListAllBranches(gitDir)
	if err != nil {
		t.Fatalf("listing all branches: %v", err)
	}

	found := false
	for _, b := range branches {
		if b == "main" {
			found = true
			break
		}
	}

	if !found {
		t.Error("main branch should be in list")
	}
}

func TestListRemoteBranches(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	branches, err := ListRemoteBranches(gitDir)
	if err != nil {
		t.Fatalf("listing remote branches: %v", err)
	}

	if len(branches) != 0 {
		t.Errorf("expected 0 remote branches, got %d", len(branches))
	}
}

func TestIsMerged(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	tmpDir := filepath.Dir(repoDir)

	featurePath := filepath.Join(tmpDir, "feature")
	if err := CreateWorktree(gitDir, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	cmd := exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("setting git user.email: %v", err)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("setting git user.name: %v", err)
	}

	readmePath := filepath.Join(featurePath, "README.md")
	if err := os.WriteFile(readmePath, []byte("test\nfeature"), 0644); err != nil {
		t.Fatalf("writing README: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("staging files: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Feature commit")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("committing: %v", err)
	}

	merged, err := IsMerged(gitDir, "main", "main")
	if err != nil {
		t.Fatalf("checking merge status: %v", err)
	}
	if !merged {
		t.Error("main should be merged into main")
	}

	merged, err = IsMerged(gitDir, "feature", "main")
	if err != nil {
		t.Fatalf("checking merge status: %v", err)
	}
	if merged {
		t.Error("feature should not be merged into main yet")
	}

	cmd = exec.Command("git", "checkout", "main")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("switching to main: %v", err)
	}

	cmd = exec.Command("git", "merge", "feature", "--no-edit")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("merging feature into main: %v", err)
	}

	merged, err = IsMerged(gitDir, "feature", "main")
	if err != nil {
		t.Fatalf("checking merge status after merge: %v", err)
	}
	if !merged {
		t.Error("feature should be merged into main after merge")
	}
}

func TestListWorktrees(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	tmpDir := filepath.Dir(repoDir)

	featurePath := filepath.Join(tmpDir, "feature-wt")
	if err := CreateWorktree(gitDir, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	worktrees, err := ListWorktrees(gitDir)
	assert.NoError(t, err)
	assert.Len(t, worktrees, 2, "should have main and feature worktrees")

	branches := make(map[string]bool)
	for _, wt := range worktrees {
		branches[wt.Branch] = true
	}

	assert.True(t, branches["main"], "should have main worktree")
	assert.True(t, branches["feature"], "should have feature worktree")
}

func TestRemoveWorktree(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	tmpDir := filepath.Dir(repoDir)

	featurePath := filepath.Join(tmpDir, "feature-wt")
	if err := CreateWorktree(gitDir, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	// Verify it exists
	_, err := os.Stat(featurePath)
	assert.NoError(t, err, "worktree should exist before removal")

	// Remove it
	err = RemoveWorktree(gitDir, featurePath, true)
	assert.NoError(t, err)

	// Verify it's gone
	_, err = os.Stat(featurePath)
	assert.True(t, os.IsNotExist(err), "worktree should be removed")

	// Verify worktree list only has main
	worktrees, err := ListWorktrees(gitDir)
	assert.NoError(t, err)
	assert.Len(t, worktrees, 1, "should only have main worktree")
}

func TestCreateWorktree(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	tmpDir := filepath.Dir(repoDir)

	worktreePath := filepath.Join(tmpDir, "feature-branch")

	err := CreateWorktree(gitDir, worktreePath, "feature", "main")

	assert.NoError(t, err)

	// Verify worktree was created
	_, err = os.Stat(worktreePath)
	assert.NoError(t, err, "worktree directory should exist")

	// Verify it has the README
	_, err = os.Stat(filepath.Join(worktreePath, "README.md"))
	assert.NoError(t, err, "worktree should have README.md")

	// Verify branch was created
	assert.True(t, BranchExists(repoDir, "feature"), "feature branch should exist")
}

func TestCreateWorktree_ExistingBranch(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	tmpDir := filepath.Dir(repoDir)

	// Create a branch first
	cmd := exec.Command("git", "branch", "existing-branch")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("creating branch: %v", err)
	}

	worktreePath := filepath.Join(tmpDir, "existing-branch-wt")

	err := CreateWorktree(gitDir, worktreePath, "existing-branch", "main")

	assert.NoError(t, err)

	// Verify worktree was created
	_, err = os.Stat(worktreePath)
	assert.NoError(t, err, "worktree directory should exist")
}

func TestCreateWorktreeBranchNaming(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	tmpDir := filepath.Dir(repoDir)

	featurePath := filepath.Join(tmpDir, "my-feature-branch")
	if err := CreateWorktree(gitDir, featurePath, "original/slash/branch", "main"); err != nil {
		t.Fatalf("creating worktree: %v", err)
	}

	worktrees, err := ListWorktrees(gitDir)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	found := false
	for _, wt := range worktrees {
		if wt.Branch == "original/slash/branch" {
			found = true
			featurePathEval, _ := filepath.EvalSymlinks(featurePath)
			wtPathEval, _ := filepath.EvalSymlinks(wt.Path)
			if featurePathEval != wtPathEval {
				t.Errorf("worktree path expected %s (resolved: %s), got %s (resolved: %s)", featurePath, featurePathEval, wt.Path, wtPathEval)
			}
			break
		}
	}

	if !found {
		t.Error("worktree with original branch name should exist")
	}

	if !BranchExists(repoDir, "original/slash/branch") {
		t.Error("original branch name with slashes should exist")
	}
}

func TestFindWorktreeByFolderName(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	tmpDir := filepath.Dir(repoDir)

	featurePath := filepath.Join(tmpDir, "my-feature-branch")
	if err := CreateWorktree(gitDir, featurePath, "feature/test-change", "main"); err != nil {
		t.Fatalf("creating worktree: %v", err)
	}

	worktrees, err := ListWorktrees(gitDir)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	var targetWorktree *Worktree
	for _, wt := range worktrees {
		if filepath.Base(wt.Path) == "my-feature-branch" {
			targetWorktree = &wt
			break
		}
	}

	if targetWorktree == nil {
		t.Fatal("should find worktree by folder name")
	}

	if targetWorktree.Branch != "feature/test-change" {
		t.Errorf("expected branch 'feature/test-change', got '%s'", targetWorktree.Branch)
	}

	featurePathEval, _ := filepath.EvalSymlinks(featurePath)
	wtPathEval, _ := filepath.EvalSymlinks(targetWorktree.Path)
	if wtPathEval != featurePathEval {
		t.Errorf("expected path %s (resolved: %s), got %s (resolved: %s)", featurePath, featurePathEval, targetWorktree.Path, wtPathEval)
	}
}

func TestListWorktreesDetailed(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	tmpDir := filepath.Dir(repoDir)

	featurePath := filepath.Join(tmpDir, "feature-wt")
	if err := CreateWorktree(gitDir, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	cmd := exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("setting git user.email: %v", err)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("setting git user.name: %v", err)
	}

	readmePath := filepath.Join(featurePath, "README.md")
	if err := os.WriteFile(readmePath, []byte("test\nfeature"), 0644); err != nil {
		t.Fatalf("writing README: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("staging files: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Feature commit")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("committing: %v", err)
	}

	worktrees, err := ListWorktreesDetailed(gitDir, repoDir, "main")
	if err != nil {
		t.Fatalf("listing worktrees detailed: %v", err)
	}

	if len(worktrees) != 2 {
		t.Errorf("expected 2 worktrees, got %d", len(worktrees))
	}

	repoPathEval, _ := filepath.EvalSymlinks(repoDir)
	for _, wt := range worktrees {
		switch wt.Branch {
		case "main":
			if !wt.IsMain {
				t.Error("main worktree should have IsMain=true")
			}
			wtPathEval, _ := filepath.EvalSymlinks(wt.Path)
			if wtPathEval == repoPathEval && !wt.IsCurrent {
				t.Error("main worktree should have IsCurrent=true when it's the current path")
			}
		case "feature":
			if wt.IsMain {
				t.Error("feature worktree should have IsMain=false")
			}
			if wt.IsMerged {
				t.Error("feature worktree should not be merged")
			}
		}
	}
}

func TestListWorktreesDetailed_CurrentWorktree(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	tmpDir := filepath.Dir(repoDir)

	featurePath := filepath.Join(tmpDir, "feature-wt")
	if err := CreateWorktree(gitDir, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	featurePathEval, _ := filepath.EvalSymlinks(featurePath)
	worktrees, err := ListWorktreesDetailed(gitDir, featurePath, "main")
	if err != nil {
		t.Fatalf("listing worktrees detailed: %v", err)
	}

	for _, wt := range worktrees {
		wtPathEval, _ := filepath.EvalSymlinks(wt.Path)
		switch wt.Branch {
		case "main":
			if wt.IsCurrent {
				t.Error("main worktree should not be current when feature path is passed")
			}
		case "feature":
			if wtPathEval != featurePathEval || !wt.IsCurrent {
				t.Errorf("feature worktree should be current when feature path is passed (path: %s vs %s)", wtPathEval, featurePathEval)
			}
		}
	}
}

func TestListWorktreesDetailed_ShowsMergedWhenMerged(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	tmpDir := filepath.Dir(repoDir)

	featurePath := filepath.Join(tmpDir, "feature-wt")
	if err := CreateWorktree(gitDir, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	cmd := exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("setting git user.email: %v", err)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("setting git user.name: %v", err)
	}

	readmePath := filepath.Join(featurePath, "README.md")
	if err := os.WriteFile(readmePath, []byte("test\nfeature"), 0644); err != nil {
		t.Fatalf("writing README: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("staging files: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Feature commit")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("committing: %v", err)
	}

	cmd = exec.Command("git", "checkout", "main")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("switching to main: %v", err)
	}

	cmd = exec.Command("git", "merge", "feature", "--no-ff", "-m", "Merge feature branch")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("merging feature into main: %v", err)
	}

	worktrees, err := ListWorktreesDetailed(gitDir, repoDir, "main")
	if err != nil {
		t.Fatalf("listing worktrees detailed: %v", err)
	}

	for _, wt := range worktrees {
		if wt.Branch == "feature" {
			if !wt.IsMerged {
				t.Error("feature worktree should be marked as merged after being merged into main")
			}
		}
	}
}

func TestSortWorktrees_ByName(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	tmpDir := filepath.Dir(repoDir)

	featureZPath := filepath.Join(tmpDir, "feature-z")
	if err := CreateWorktree(gitDir, featureZPath, "feature-z", "main"); err != nil {
		t.Fatalf("creating feature-z worktree: %v", err)
	}

	featureAPath := filepath.Join(tmpDir, "feature-a")
	if err := CreateWorktree(gitDir, featureAPath, "feature-a", "main"); err != nil {
		t.Fatalf("creating feature-a worktree: %v", err)
	}

	worktrees, err := ListWorktrees(gitDir)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	sorted := SortWorktrees(worktrees, "name", false)

	if len(sorted) != 3 {
		t.Fatalf("expected 3 worktrees, got %d", len(sorted))
	}

	names := []string{filepath.Base(sorted[0].Path), filepath.Base(sorted[1].Path), filepath.Base(sorted[2].Path)}
	expected := []string{"feature-a", "feature-z", "repo"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("expected worktree %d to be %s, got %s", i, expected[i], name)
		}
	}
}

func TestSortWorktrees_ByBranch(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	tmpDir := filepath.Dir(repoDir)

	zuluPath := filepath.Join(tmpDir, "zulu")
	if err := CreateWorktree(gitDir, zuluPath, "zulu", "main"); err != nil {
		t.Fatalf("creating zulu worktree: %v", err)
	}

	alphaPath := filepath.Join(tmpDir, "alpha")
	if err := CreateWorktree(gitDir, alphaPath, "alpha", "main"); err != nil {
		t.Fatalf("creating alpha worktree: %v", err)
	}

	worktrees, err := ListWorktrees(gitDir)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	sorted := SortWorktrees(worktrees, "branch", false)

	if len(sorted) != 3 {
		t.Fatalf("expected 3 worktrees, got %d", len(sorted))
	}

	branches := []string{sorted[0].Branch, sorted[1].Branch, sorted[2].Branch}
	expected := []string{"alpha", "main", "zulu"}
	for i, branch := range branches {
		if branch != expected[i] {
			t.Errorf("expected worktree %d to have branch %s, got %s", i, expected[i], branch)
		}
	}
}

func TestSortWorktrees_ByCreated(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	tmpDir := filepath.Dir(repoDir)

	featurePath := filepath.Join(tmpDir, "feature")
	if err := CreateWorktree(gitDir, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	worktrees, err := ListWorktrees(gitDir)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	sorted := SortWorktrees(worktrees, "created", false)

	if len(sorted) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(sorted))
	}

	// Main repo was created first
	if sorted[0].Branch != "main" {
		t.Error("main worktree should be first (oldest)")
	}
	if sorted[1].Branch != "feature" {
		t.Error("feature worktree should be second (newer)")
	}
}

func TestIsMerged_InvalidBranch(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	merged, err := IsMerged(gitDir, "nonexistent-branch-12345", "main")

	assert.False(t, merged, "invalid branch should return false for merged status")
	assert.Error(t, err, "invalid branch should return an error")
	assert.Contains(t, err.Error(), "git", "error should mention git failure")
}

func TestIsMerged_GitFailure(t *testing.T) {
	invalidPath := "/nonexistent/path/that/is/not/a/git/repo"

	merged, err := IsMerged(invalidPath, "main", "develop")

	assert.False(t, merged, "invalid repository should return false for merged status")
	assert.Error(t, err, "invalid repository should return an error")
}

func TestIsGitRepo(t *testing.T) {
	repoDir := createTestRepo(t)

	assert.True(t, IsGitRepo(repoDir), "should detect .git directory")
	assert.False(t, IsGitRepo("/nonexistent/path"), "should return false for nonexistent path")
	assert.False(t, IsGitRepo(t.TempDir()), "should return false for non-git directory")
}

func TestFindGitDir_WithGitRepo(t *testing.T) {
	repoDir := createTestRepo(t)

	gitDir, err := FindGitDir(repoDir)

	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(repoDir, ".git"), gitDir)
}

func TestFindGitDir_NoRepo(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := FindGitDir(tmpDir)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no .git found")
}

func TestGetRepoPath(t *testing.T) {
	tests := []struct {
		gitDir   string
		expected string
	}{
		{"/home/user/project/.git", "/home/user/project"},
		{"/var/repos/myrepo/.git", "/var/repos/myrepo"},
	}

	for _, tt := range tests {
		t.Run(tt.gitDir, func(t *testing.T) {
			result := GetRepoPath(tt.gitDir)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestListWorktrees_PathsWithSpaces(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")
	tmpDir := filepath.Dir(repoDir)

	spacePath := filepath.Join(tmpDir, "my feature branch")
	if err := CreateWorktree(gitDir, spacePath, "feature", "main"); err != nil {
		t.Fatalf("creating worktree with spaces in path: %v", err)
	}

	worktrees, err := ListWorktrees(gitDir)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	found := false
	for _, wt := range worktrees {
		if wt.Branch == "feature" {
			found = true
			assert.Contains(t, wt.Path, "my feature branch", "path with spaces should be preserved")
			break
		}
	}

	assert.True(t, found, "feature worktree should be in list")
}
