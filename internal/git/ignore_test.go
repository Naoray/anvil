package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsIgnored(t *testing.T) {
	// Create a temporary directory with git repo
	tmpDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Create .gitignore with .anvil.local
	gitignorePath := filepath.Join(tmpDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(".anvil.local\n"), 0644); err != nil {
		t.Fatalf("failed to write .gitignore: %v", err)
	}

	// Test ignored file
	ignored, err := IsIgnored(tmpDir, ".anvil.local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ignored {
		t.Error("expected .anvil.local to be ignored")
	}

	// Test non-ignored file
	ignored, err = IsIgnored(tmpDir, "some-file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ignored {
		t.Error("expected some-file.txt to not be ignored")
	}
}
