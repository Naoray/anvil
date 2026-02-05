package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/artisanexperiences/arbor/internal/config"
)

func evalSymlinks(path string) string {
	evalPath, _ := filepath.EvalSymlinks(path)
	if evalPath == "" {
		return path
	}
	return evalPath
}

func createTestWorktree(t *testing.T) (string, string) {
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

	worktreePath := filepath.Join(tmpDir, "worktree1")
	cmd = exec.Command("git", "worktree", "add", worktreePath, "main")
	cmd.Dir = barePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("creating worktree: %v", err)
	}

	configPath := filepath.Join(tmpDir, "arbor.yaml")
	if err := os.WriteFile(configPath, []byte("preset: php\n"), 0644); err != nil {
		t.Fatalf("writing arbor.yaml: %v", err)
	}

	return worktreePath, barePath
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

func TestOpenProjectFromCWD_Success(t *testing.T) {
	worktreePath, barePath := createTestWorktree(t)
	tmpDir := filepath.Dir(barePath)

	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalCWD) }()

	err = os.Chdir(worktreePath)
	if err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	pc, err := OpenProjectFromCWD()
	if err != nil {
		t.Fatalf("OpenProjectFromCWD() error = %v", err)
	}

	expectedCWD := evalSymlinks(worktreePath)
	if evalSymlinks(pc.CWD) != expectedCWD {
		t.Errorf("CWD = %v, want %v", pc.CWD, expectedCWD)
	}

	expectedBarePath := evalSymlinks(barePath)
	if evalSymlinks(pc.BarePath) != expectedBarePath {
		t.Errorf("BarePath = %v, want %v", pc.BarePath, expectedBarePath)
	}

	expectedProjectPath := evalSymlinks(tmpDir)
	if evalSymlinks(pc.ProjectPath) != expectedProjectPath {
		t.Errorf("ProjectPath = %v, want %v", pc.ProjectPath, expectedProjectPath)
	}

	if pc.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %v, want %v", pc.DefaultBranch, "main")
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
		worktreePath, _ := createTestWorktree(t)

		pc := &ProjectContext{
			CWD: worktreePath,
		}

		if !pc.IsInWorktree() {
			t.Error("IsInWorktree() = false, want true for worktree directory")
		}
	})

	t.Run("returns false for project root (where .bare is located)", func(t *testing.T) {
		worktreePath, barePath := createTestWorktree(t)
		projectRoot := filepath.Dir(barePath)

		pc := &ProjectContext{
			CWD: projectRoot,
		}

		if pc.IsInWorktree() {
			t.Error("IsInWorktree() = true, want false for project root")
		}

		// Also verify that the worktree does work
		pc.CWD = worktreePath
		if !pc.IsInWorktree() {
			t.Error("IsInWorktree() = false, want true for worktree (sanity check)")
		}
	})

	t.Run("returns false for .bare directory itself", func(t *testing.T) {
		_, barePath := createTestWorktree(t)

		pc := &ProjectContext{
			CWD: barePath,
		}

		if pc.IsInWorktree() {
			t.Error("IsInWorktree() = true, want false for .bare directory")
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

	worktreePath, _ := createTestWorktree(t)

	pc.CWD = worktreePath
	err = pc.MustBeInWorktree()
	if err != nil {
		t.Errorf("MustBeInWorktree() = %v, want nil for worktree directory", err)
	}
}

func TestProjectContext_Managers(t *testing.T) {
	worktreePath, _ := createTestWorktree(t)

	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalCWD) }()

	err = os.Chdir(worktreePath)
	if err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	pc, err := OpenProjectFromCWD()
	if err != nil {
		t.Fatalf("OpenProjectFromCWD() error = %v", err)
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

// Tests for linked project workflow

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

	// Test openLinkedProject directly (since OpenProjectFromCWD loads from disk)
	pc, err := openLinkedProject(repoDir, "my-project", globalCfg.Projects["my-project"], globalCfg)
	if err != nil {
		t.Fatalf("openLinkedProject() error = %v", err)
	}

	if !pc.IsLinked {
		t.Error("IsLinked = false, want true for linked project")
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
}

func TestProjectContext_GetWorktreePath_Linked(t *testing.T) {
	repoDir := createLinkedProject(t)
	tmpDir := filepath.Dir(repoDir)
	worktreeBase := filepath.Join(tmpDir, "worktrees")

	pc := &ProjectContext{
		IsLinked:     true,
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

func TestProjectContext_GetWorktreePath_Legacy(t *testing.T) {
	_, barePath := createTestWorktree(t)
	projectPath := filepath.Dir(barePath)

	pc := &ProjectContext{
		IsLinked:    false,
		ProjectPath: projectPath,
	}

	// Test with simple branch name
	path := pc.GetWorktreePath("feature-test")
	expected := filepath.Join(projectPath, "feature-test")
	if path != expected {
		t.Errorf("GetWorktreePath(feature-test) = %v, want %v", path, expected)
	}

	// Test with branch containing slashes
	path = pc.GetWorktreePath("feature/my-feature")
	expected = filepath.Join(projectPath, "feature-my-feature")
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

func TestProjectContext_LinkedVsLegacy_WorktreePaths(t *testing.T) {
	// This test verifies that linked projects use centralized paths
	// while legacy projects use sibling paths

	repoDir := createLinkedProject(t)
	tmpDir := filepath.Dir(repoDir)
	worktreeBase := filepath.Join(tmpDir, "centralized-worktrees")

	// Linked project context
	linkedPC := &ProjectContext{
		IsLinked:     true,
		ProjectName:  "my-project",
		WorktreeBase: worktreeBase,
		ProjectPath:  repoDir,
	}

	// Legacy project context
	_, barePath := createTestWorktree(t)
	legacyProjectPath := filepath.Dir(barePath)
	legacyPC := &ProjectContext{
		IsLinked:    false,
		ProjectPath: legacyProjectPath,
	}

	branch := "feature/test-branch"

	linkedPath := linkedPC.GetWorktreePath(branch)
	legacyPath := legacyPC.GetWorktreePath(branch)

	// Linked should be under worktreeBase
	if !filepath.HasPrefix(linkedPath, worktreeBase) {
		t.Errorf("Linked worktree path %v should be under %v", linkedPath, worktreeBase)
	}

	// Legacy should be under projectPath (sibling to .bare)
	if !filepath.HasPrefix(legacyPath, legacyProjectPath) {
		t.Errorf("Legacy worktree path %v should be under %v", legacyPath, legacyProjectPath)
	}

	// Paths should be different
	if linkedPath == legacyPath {
		t.Errorf("Linked and legacy paths should be different, both are %v", linkedPath)
	}
}
