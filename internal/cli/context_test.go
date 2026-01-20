package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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

	_, err := OpenProjectFromCWD()
	if err == nil {
		t.Error("expected error when not in worktree, got nil")
	}
	_ = tmpDir
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
	tmpDir := t.TempDir()

	pc := &ProjectContext{
		CWD: tmpDir,
	}

	if pc.IsInWorktree() {
		t.Error("IsInWorktree() = true, want false for non-worktree directory")
	}

	worktreePath, _ := createTestWorktree(t)

	pc.CWD = worktreePath
	if !pc.IsInWorktree() {
		t.Error("IsInWorktree() = false, want true for worktree directory")
	}
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
