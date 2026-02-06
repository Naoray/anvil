package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/naoray/anvil/internal/config"
)

func evalSymlinks(path string) string {
	evalPath, _ := filepath.EvalSymlinks(path)
	if evalPath == "" {
		return path
	}
	return evalPath
}

// createLinkedProject creates a regular git repo (simulating a linked project)
func createLinkedProject(t *testing.T) string {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "my-project")

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

func TestOpenProjectFromCWD_NotInWorktree(t *testing.T) {
	tmpDir := t.TempDir()

	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalCWD) }()

	err = os.Chdir(tmpDir)
	if err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	_, err = OpenProjectFromCWD()
	if err == nil {
		t.Error("expected error when not in worktree, got nil")
	}
}

func TestOpenProjectFromCWD_LinkedProject(t *testing.T) {
	repoDir := createLinkedProject(t)
	tmpDir := filepath.Dir(repoDir)
	worktreeBase := filepath.Join(tmpDir, "worktrees")

	// Create a mock global config with the linked project
	globalCfg := &config.GlobalConfig{
		WorktreeBase: worktreeBase,
		Projects: map[string]*config.ProjectInfo{
			"my-project": {
				Path:          repoDir,
				DefaultBranch: "main",
				Preset:        "php",
				SiteName:      "my-project",
			},
		},
	}

	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalCWD) }()

	// Change to the linked project directory
	err = os.Chdir(repoDir)
	if err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	// Test openProject directly (since OpenProjectFromCWD loads from disk)
	pc, err := openProject(repoDir, "my-project", globalCfg.Projects["my-project"], globalCfg)
	if err != nil {
		t.Fatalf("openProject() error = %v", err)
	}

	if pc.ProjectName != "my-project" {
		t.Errorf("ProjectName = %v, want my-project", pc.ProjectName)
	}

	expectedProjectPath := evalSymlinks(repoDir)
	if evalSymlinks(pc.ProjectPath) != expectedProjectPath {
		t.Errorf("ProjectPath = %v, want %v", pc.ProjectPath, expectedProjectPath)
	}

	if pc.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %v, want main", pc.DefaultBranch)
	}

	if pc.Config.Preset != "php" {
		t.Errorf("Config.Preset = %v, want php", pc.Config.Preset)
	}

	// Verify GitDir is set
	if pc.GitDir == "" {
		t.Error("GitDir should not be empty")
	}
}

func TestProjectContext_IsInWorktree(t *testing.T) {
	t.Run("returns false for non-worktree directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		pc := &ProjectContext{
			CWD: tmpDir,
		}

		if pc.IsInWorktree() {
			t.Error("IsInWorktree() = true, want false for non-worktree directory")
		}
	})

	t.Run("returns true for worktree directory", func(t *testing.T) {
		repoDir := createLinkedProject(t)
		tmpDir := filepath.Dir(repoDir)
		worktreeBase := filepath.Join(tmpDir, "worktrees")
		worktreeDir := filepath.Join(worktreeBase, "my-project", "feature-test")

		// Create a real worktree
		gitDir := filepath.Join(repoDir, ".git")
		if err := os.MkdirAll(filepath.Dir(worktreeDir), 0755); err != nil {
			t.Fatalf("creating worktree base: %v", err)
		}
		cmd := exec.Command("git", "worktree", "add", "-b", "feature/test", worktreeDir, "main")
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("creating worktree: %v", err)
		}

		pc := &ProjectContext{
			CWD:          worktreeDir,
			GitDir:       gitDir,
			ProjectPath:  repoDir,
			WorktreeBase: worktreeBase,
		}

		if !pc.IsInWorktree() {
			t.Error("IsInWorktree() = false, want true for worktree directory")
		}
	})

	t.Run("returns false for project root", func(t *testing.T) {
		repoDir := createLinkedProject(t)

		pc := &ProjectContext{
			CWD:         repoDir,
			ProjectPath: repoDir,
		}

		if pc.IsInWorktree() {
			t.Error("IsInWorktree() = true, want false for project root")
		}
	})
}

func TestProjectContext_MustBeInWorktree(t *testing.T) {
	tmpDir := t.TempDir()

	pc := &ProjectContext{
		CWD: tmpDir,
	}

	err := pc.MustBeInWorktree()
	if err == nil {
		t.Error("MustBeInWorktree() = nil, want error for non-worktree directory")
	}

	// Create a worktree and verify MustBeInWorktree passes
	repoDir := createLinkedProject(t)
	tmpDir2 := filepath.Dir(repoDir)
	worktreeBase := filepath.Join(tmpDir2, "worktrees")
	worktreeDir := filepath.Join(worktreeBase, "my-project", "feature-test")

	if err := os.MkdirAll(filepath.Dir(worktreeDir), 0755); err != nil {
		t.Fatalf("creating worktree base: %v", err)
	}
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/test", worktreeDir, "main")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("creating worktree: %v", err)
	}

	pc = &ProjectContext{
		CWD:          worktreeDir,
		GitDir:       filepath.Join(repoDir, ".git"),
		ProjectPath:  repoDir,
		WorktreeBase: worktreeBase,
	}

	err = pc.MustBeInWorktree()
	if err != nil {
		t.Errorf("MustBeInWorktree() = %v, want nil for worktree directory", err)
	}
}

func TestProjectContext_Managers(t *testing.T) {
	repoDir := createLinkedProject(t)
	tmpDir := filepath.Dir(repoDir)
	worktreeBase := filepath.Join(tmpDir, "worktrees")

	globalCfg := &config.GlobalConfig{
		WorktreeBase: worktreeBase,
		Projects: map[string]*config.ProjectInfo{
			"my-project": {
				Path:          repoDir,
				DefaultBranch: "main",
			},
		},
	}

	pc, err := openProject(repoDir, "my-project", globalCfg.Projects["my-project"], globalCfg)
	if err != nil {
		t.Fatalf("openProject() error = %v", err)
	}

	pm := pc.PresetManager()
	if pm == nil {
		t.Error("PresetManager() returned nil")
	}

	sm := pc.ScaffoldManager()
	if sm == nil {
		t.Error("ScaffoldManager() returned nil")
	}

	pm2 := pc.PresetManager()
	if pm2 != pm {
		t.Error("PresetManager() called twice returned different instances")
	}

	sm2 := pc.ScaffoldManager()
	if sm2 != sm {
		t.Error("ScaffoldManager() called twice returned different instances")
	}
}

func TestProjectContext_GetWorktreePath(t *testing.T) {
	repoDir := createLinkedProject(t)
	tmpDir := filepath.Dir(repoDir)
	worktreeBase := filepath.Join(tmpDir, "worktrees")

	pc := &ProjectContext{
		ProjectName:  "my-project",
		WorktreeBase: worktreeBase,
		ProjectPath:  repoDir,
	}

	// Test with simple branch name
	path := pc.GetWorktreePath("feature-test")
	expected := filepath.Join(worktreeBase, "my-project", "feature-test")
	if path != expected {
		t.Errorf("GetWorktreePath(feature-test) = %v, want %v", path, expected)
	}

	// Test with branch containing slashes (should be sanitized)
	path = pc.GetWorktreePath("feature/my-feature")
	expected = filepath.Join(worktreeBase, "my-project", "feature-my-feature")
	if path != expected {
		t.Errorf("GetWorktreePath(feature/my-feature) = %v, want %v", path, expected)
	}
}

func TestSanitizeBranchName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"feature-test", "feature-test"},
		{"feature/my-feature", "feature-my-feature"},
		{"feature/nested/branch", "feature-nested-branch"},
		{"main", "main"},
		{"bugfix/issue-123", "bugfix-issue-123"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeBranchName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeBranchName(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
