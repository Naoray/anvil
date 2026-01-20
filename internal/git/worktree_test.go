package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func createTestRepo(t *testing.T) (string, string) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	barePath := filepath.Join(tmpDir, ".bare")

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

	cmd = exec.Command("git", "clone", "--bare", repoDir, barePath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("cloning to bare: %v", err)
	}

	return barePath, repoDir
}

func TestBranchExists(t *testing.T) {
	barePath, _ := createTestRepo(t)

	if !BranchExists(barePath, "main") {
		t.Error("main branch should exist after creating from repo with commit")
	}

	if BranchExists(barePath, "nonexistent") {
		t.Error("nonexistent branch should not exist")
	}
}

func TestListBranches(t *testing.T) {
	barePath, _ := createTestRepo(t)

	mainPath := filepath.Join(filepath.Dir(barePath), "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	branches, err := ListBranches(barePath)
	if err != nil {
		t.Fatalf("listing branches: %v", err)
	}

	for _, b := range branches {
		if b == "main" {
			t.Error("main branch (current) should not be in ListBranches output")
		}
	}

	featurePath := filepath.Join(filepath.Dir(barePath), "feature")
	if err := CreateWorktree(barePath, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	branches, err = ListBranches(barePath)
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
	barePath, _ := createTestRepo(t)

	branches, err := ListAllBranches(barePath)
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
	barePath, _ := createTestRepo(t)

	branches, err := ListRemoteBranches(barePath)
	if err != nil {
		t.Fatalf("listing remote branches: %v", err)
	}

	if len(branches) != 0 {
		t.Errorf("expected 0 remote branches, got %d", len(branches))
	}
}

func TestFindBarePath(t *testing.T) {
	barePath, _ := createTestRepo(t)

	mainPath := filepath.Join(filepath.Dir(barePath), "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	found, err := FindBarePath(mainPath)
	if err != nil {
		t.Fatalf("finding bare path: %v", err)
	}

	if found != barePath {
		t.Errorf("expected %s, got %s", barePath, found)
	}

	_, err = FindBarePath("/nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestIsMerged(t *testing.T) {
	barePath, _ := createTestRepo(t)

	mainPath := filepath.Join(filepath.Dir(barePath), "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	featurePath := filepath.Join(filepath.Dir(barePath), "feature")
	if err := CreateWorktree(barePath, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	cmd := exec.Command("git", "checkout", "-b", "dev")
	cmd.Dir = featurePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("creating dev branch: %v", err)
	}

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
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

	merged, err := IsMerged(barePath, "main", "main")
	if err != nil {
		t.Fatalf("checking merge status: %v", err)
	}
	if !merged {
		t.Error("main should be merged into main")
	}

	merged, err = IsMerged(barePath, "dev", "main")
	if err != nil {
		t.Fatalf("checking merge status: %v", err)
	}
	if merged {
		t.Error("dev should not be merged into main yet (no commits on dev)")
	}

	cmd = exec.Command("git", "checkout", "main")
	cmd.Dir = mainPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("switching to main: %v", err)
	}

	cmd = exec.Command("git", "merge", "dev", "--no-edit")
	cmd.Dir = mainPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("merging dev into main: %v", err)
	}

	merged, err = IsMerged(barePath, "dev", "main")
	if err != nil {
		t.Fatalf("checking merge status after merge: %v", err)
	}
	if !merged {
		t.Error("dev should be merged into main after merge")
	}
}

func TestFindBarePathParentSearch(t *testing.T) {
	barePath, _ := createTestRepo(t)

	mainPath := filepath.Join(filepath.Dir(barePath), "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	subdirPath := filepath.Join(mainPath, "subdir")
	if err := os.MkdirAll(subdirPath, 0755); err != nil {
		t.Fatalf("creating subdir: %v", err)
	}

	found, err := FindBarePath(subdirPath)
	if err != nil {
		t.Fatalf("finding bare path from subdir: %v", err)
	}

	if found != barePath {
		t.Errorf("expected %s, got %s", barePath, found)
	}
}

func TestListWorktrees(t *testing.T) {
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	mainPath := filepath.Join(projectDir, "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	featurePath := filepath.Join(projectDir, "feature")
	if err := CreateWorktree(barePath, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	worktrees, err := ListWorktrees(barePath)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	if len(worktrees) != 2 {
		t.Errorf("expected 2 worktrees, got %d", len(worktrees))
	}

	mainFound := false
	featureFound := false
	for _, wt := range worktrees {
		if wt.Branch == "main" {
			mainFound = true
			mainPathEval, _ := filepath.EvalSymlinks(mainPath)
			wtPathEval, _ := filepath.EvalSymlinks(wt.Path)
			if mainPathEval != wtPathEval {
				t.Errorf("main worktree path expected %s (resolved: %s), got %s (resolved: %s)", mainPath, mainPathEval, wt.Path, wtPathEval)
			}
		}
		if wt.Branch == "feature" {
			featureFound = true
			featurePathEval, _ := filepath.EvalSymlinks(featurePath)
			wtPathEval, _ := filepath.EvalSymlinks(wt.Path)
			if featurePathEval != wtPathEval {
				t.Errorf("feature worktree path expected %s (resolved: %s), got %s (resolved: %s)", featurePath, featurePathEval, wt.Path, wtPathEval)
			}
		}
	}

	if !mainFound {
		t.Error("main worktree should be in list")
	}
	if !featureFound {
		t.Error("feature worktree should be in list")
	}
}

func TestRemoveWorktree(t *testing.T) {
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	mainPath := filepath.Join(projectDir, "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	featurePath := filepath.Join(projectDir, "feature")
	if err := CreateWorktree(barePath, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	if _, err := os.Stat(featurePath); err != nil {
		t.Fatalf("feature worktree should exist: %v", err)
	}

	if err := RemoveWorktree(featurePath, true); err != nil {
		t.Fatalf("removing worktree: %v", err)
	}

	if _, err := os.Stat(featurePath); err == nil {
		t.Error("feature worktree should have been removed")
	}

	worktrees, err := ListWorktrees(barePath)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	if len(worktrees) != 1 {
		t.Errorf("expected 1 worktree after removal, got %d", len(worktrees))
	}

	for _, wt := range worktrees {
		if wt.Branch == "feature" {
			t.Error("feature worktree should have been removed from list")
		}
	}
}

func TestCreateWorktreeBranchNaming(t *testing.T) {
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	featurePath := filepath.Join(projectDir, "my-feature-branch")
	if err := CreateWorktree(barePath, featurePath, "original/slash/branch", "main"); err != nil {
		t.Fatalf("creating worktree: %v", err)
	}

	worktrees, err := ListWorktrees(barePath)
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

	if !BranchExists(barePath, "original/slash/branch") {
		t.Error("original branch name with slashes should exist")
	}
}

func TestFindWorktreeByFolderName(t *testing.T) {
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	featurePath := filepath.Join(projectDir, "my-feature-branch")
	if err := CreateWorktree(barePath, featurePath, "feature/test-change", "main"); err != nil {
		t.Fatalf("creating worktree: %v", err)
	}

	worktrees, err := ListWorktrees(barePath)
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
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	mainPath := filepath.Join(projectDir, "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	featurePath := filepath.Join(projectDir, "feature")
	if err := CreateWorktree(barePath, featurePath, "feature", "main"); err != nil {
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

	worktrees, err := ListWorktreesDetailed(barePath, mainPath, "main")
	if err != nil {
		t.Fatalf("listing worktrees detailed: %v", err)
	}

	if len(worktrees) != 2 {
		t.Errorf("expected 2 worktrees, got %d", len(worktrees))
	}

	mainPathEval, _ := filepath.EvalSymlinks(mainPath)
	for _, wt := range worktrees {
		if wt.Branch == "main" {
			if !wt.IsMain {
				t.Error("main worktree should have IsMain=true")
			}
			wtPathEval, _ := filepath.EvalSymlinks(wt.Path)
			if wtPathEval != mainPathEval {
				t.Errorf("main worktree path mismatch: expected %s (resolved: %s), got %s (resolved: %s)", mainPath, mainPathEval, wt.Path, wtPathEval)
			}
			if wtPathEval == mainPathEval && !wt.IsCurrent {
				t.Error("main worktree should have IsCurrent=true when it's the current path")
			}
		} else if wt.Branch == "feature" {
			if wt.IsMain {
				t.Error("feature worktree should have IsMain=false")
			}
			if wt.IsCurrent {
				t.Error("feature worktree should have IsCurrent=false")
			}
			if wt.IsMerged {
				t.Error("feature worktree should not be merged (at same commit as main)")
			}
		}
	}
}

func TestListWorktreesDetailed_CurrentWorktree(t *testing.T) {
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	mainPath := filepath.Join(projectDir, "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	featurePath := filepath.Join(projectDir, "feature")
	if err := CreateWorktree(barePath, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	featurePathEval, _ := filepath.EvalSymlinks(featurePath)
	mainPathEval, _ := filepath.EvalSymlinks(mainPath)
	worktrees, err := ListWorktreesDetailed(barePath, featurePath, "main")
	if err != nil {
		t.Fatalf("listing worktrees detailed: %v", err)
	}

	for _, wt := range worktrees {
		wtPathEval, _ := filepath.EvalSymlinks(wt.Path)
		if wt.Branch == "main" {
			if wtPathEval == mainPathEval && wt.IsCurrent {
				t.Error("main worktree should not be current when feature path is passed")
			}
		} else if wt.Branch == "feature" {
			if wtPathEval != featurePathEval || !wt.IsCurrent {
				t.Errorf("feature worktree should be current when feature path is passed (path: %s vs %s)", wtPathEval, featurePathEval)
			}
		}
	}
}

func TestListWorktreesDetailed_ShowsMergedWhenMerged(t *testing.T) {
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	mainPath := filepath.Join(projectDir, "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	featurePath := filepath.Join(projectDir, "feature")
	if err := CreateWorktree(barePath, featurePath, "feature", "main"); err != nil {
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
	cmd.Dir = mainPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("switching to main: %v", err)
	}

	cmd = exec.Command("git", "merge", "feature", "--no-ff", "-m", "Merge feature branch")
	cmd.Dir = mainPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("merging feature into main: %v", err)
	}

	worktrees, err := ListWorktreesDetailed(barePath, mainPath, "main")
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
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	mainPath := filepath.Join(projectDir, "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	featureZPath := filepath.Join(projectDir, "feature-z")
	if err := CreateWorktree(barePath, featureZPath, "feature-z", "main"); err != nil {
		t.Fatalf("creating feature-z worktree: %v", err)
	}

	featureAPath := filepath.Join(projectDir, "feature-a")
	if err := CreateWorktree(barePath, featureAPath, "feature-a", "main"); err != nil {
		t.Fatalf("creating feature-a worktree: %v", err)
	}

	worktrees, err := ListWorktrees(barePath)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	sorted := SortWorktrees(worktrees, "name", false)

	if len(sorted) != 3 {
		t.Fatalf("expected 3 worktrees, got %d", len(sorted))
	}

	names := []string{filepath.Base(sorted[0].Path), filepath.Base(sorted[1].Path), filepath.Base(sorted[2].Path)}
	expected := []string{"feature-a", "feature-z", "main"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("expected worktree %d to be %s, got %s", i, expected[i], name)
		}
	}
}

func TestSortWorktrees_ByName_Reverse(t *testing.T) {
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	mainPath := filepath.Join(projectDir, "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	featureAPath := filepath.Join(projectDir, "feature-a")
	if err := CreateWorktree(barePath, featureAPath, "feature-a", "main"); err != nil {
		t.Fatalf("creating feature-a worktree: %v", err)
	}

	worktrees, err := ListWorktrees(barePath)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	sorted := SortWorktrees(worktrees, "name", true)

	if len(sorted) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(sorted))
	}

	names := []string{filepath.Base(sorted[0].Path), filepath.Base(sorted[1].Path)}
	expected := []string{"main", "feature-a"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("expected worktree %d to be %s, got %s", i, expected[i], name)
		}
	}
}

func TestSortWorktrees_ByBranch(t *testing.T) {
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	mainPath := filepath.Join(projectDir, "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	zuluPath := filepath.Join(projectDir, "zulu")
	if err := CreateWorktree(barePath, zuluPath, "zulu", "main"); err != nil {
		t.Fatalf("creating zulu worktree: %v", err)
	}

	alphaPath := filepath.Join(projectDir, "alpha")
	if err := CreateWorktree(barePath, alphaPath, "alpha", "main"); err != nil {
		t.Fatalf("creating alpha worktree: %v", err)
	}

	worktrees, err := ListWorktrees(barePath)
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
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	mainPath := filepath.Join(projectDir, "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	featurePath := filepath.Join(projectDir, "feature")
	if err := CreateWorktree(barePath, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	worktrees, err := ListWorktrees(barePath)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	sorted := SortWorktrees(worktrees, "created", false)

	if len(sorted) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(sorted))
	}

	if filepath.Base(sorted[0].Path) != "main" {
		t.Error("main worktree should be first (oldest)")
	}
	if filepath.Base(sorted[1].Path) != "feature" {
		t.Error("feature worktree should be second (newer)")
	}
}

func TestSortWorktrees_DefaultIsByName(t *testing.T) {
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	mainPath := filepath.Join(projectDir, "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	zetaPath := filepath.Join(projectDir, "zeta")
	if err := CreateWorktree(barePath, zetaPath, "zeta", "main"); err != nil {
		t.Fatalf("creating zeta worktree: %v", err)
	}

	alphaPath := filepath.Join(projectDir, "alpha")
	if err := CreateWorktree(barePath, alphaPath, "alpha", "main"); err != nil {
		t.Fatalf("creating alpha worktree: %v", err)
	}

	worktrees, err := ListWorktrees(barePath)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	sorted := SortWorktrees(worktrees, "", false)

	if len(sorted) != 3 {
		t.Fatalf("expected 3 worktrees, got %d", len(sorted))
	}

	names := []string{filepath.Base(sorted[0].Path), filepath.Base(sorted[1].Path), filepath.Base(sorted[2].Path)}
	expected := []string{"alpha", "main", "zeta"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("expected worktree %d to be %s, got %s", i, expected[i], name)
		}
	}
}

func TestIsMerged_InvalidBranch(t *testing.T) {
	barePath, _ := createTestRepo(t)

	merged, err := IsMerged(barePath, "nonexistent-branch-12345", "main")

	t.Logf("New behavior: IsMerged with invalid branch returns (%v, %v)", merged, err)

	assert.False(t, merged, "invalid branch should return false for merged status")
	assert.Error(t, err, "invalid branch should return an error")
	assert.Contains(t, err.Error(), "git", "error should mention git failure")
}

func TestIsMerged_GitFailure(t *testing.T) {
	invalidPath := "/nonexistent/path/that/is/not/a/git/repo"

	merged, err := IsMerged(invalidPath, "main", "develop")

	t.Logf("New behavior: IsMerged with invalid repo path returns (%v, %v)", merged, err)

	assert.False(t, merged, "invalid repository should return false for merged status")
	assert.Error(t, err, "invalid repository should return an error")
}

func TestListWorktrees_PorcelainParsing_AbsolutePaths(t *testing.T) {
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	mainPath := filepath.Join(projectDir, "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	absMainPath, err := filepath.Abs(mainPath)
	if err != nil {
		t.Fatalf("getting absolute path: %v", err)
	}

	worktrees, err := ListWorktrees(barePath)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	assert.Len(t, worktrees, 1)

	found := false
	for _, wt := range worktrees {
		if wt.Branch == "main" {
			found = true
			t.Logf("Current path handling: got path %s", wt.Path)
			absMainPathEval, _ := filepath.EvalSymlinks(absMainPath)
			wtPathEval, _ := filepath.EvalSymlinks(wt.Path)
			assert.Equal(t, absMainPathEval, wtPathEval, "absolute path should be preserved")
			break
		}
	}

	assert.True(t, found, "main worktree should be in list")
}

func TestListWorktrees_PorcelainParsing_PathsWithSpaces(t *testing.T) {
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	spacePath := filepath.Join(projectDir, "my feature branch")
	if err := CreateWorktree(barePath, spacePath, "feature", "main"); err != nil {
		t.Fatalf("creating worktree with spaces in path: %v", err)
	}

	worktrees, err := ListWorktrees(barePath)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	found := false
	for _, wt := range worktrees {
		if wt.Branch == "feature" {
			found = true
			t.Logf("Current path handling for spaces: got path %s", wt.Path)
			assert.Contains(t, wt.Path, "my feature branch", "path with spaces should be preserved")
			break
		}
	}

	assert.True(t, found, "feature worktree should be in list")
}

func TestListWorktrees_PorcelainParsing_CurrentBehavior(t *testing.T) {
	barePath, _ := createTestRepo(t)
	projectDir := filepath.Dir(barePath)

	mainPath := filepath.Join(projectDir, "main")
	if err := CreateWorktree(barePath, mainPath, "main", ""); err != nil {
		t.Fatalf("creating main worktree: %v", err)
	}

	featurePath := filepath.Join(projectDir, "feature")
	if err := CreateWorktree(barePath, featurePath, "feature", "main"); err != nil {
		t.Fatalf("creating feature worktree: %v", err)
	}

	worktrees, err := ListWorktrees(barePath)
	if err != nil {
		t.Fatalf("listing worktrees: %v", err)
	}

	assert.Len(t, worktrees, 2, "should have main and feature worktrees")

	for _, wt := range worktrees {
		t.Logf("Parsed worktree: branch=%s, path=%s", wt.Branch, wt.Path)
	}

	mainWt := func() *Worktree {
		for _, wt := range worktrees {
			if wt.Branch == "main" {
				return &wt
			}
		}
		return nil
	}()

	assert.NotNil(t, mainWt, "main worktree should exist")
	assert.Equal(t, "main", mainWt.Branch)
}
