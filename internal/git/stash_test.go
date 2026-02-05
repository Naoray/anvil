package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// setupTestRepo creates a temporary git repository for testing
func setupStashTestRepo(t *testing.T) string {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "arbor-stash-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Configure git user for commits
	exec.Command("git", "-C", tmpDir, "config", "user.name", "Test User").Run()
	exec.Command("git", "-C", tmpDir, "config", "user.email", "test@example.com").Run()

	// Create initial commit
	readmePath := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0644); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create README: %v", err)
	}

	exec.Command("git", "-C", tmpDir, "add", "README.md").Run()
	exec.Command("git", "-C", tmpDir, "commit", "-m", "Initial commit").Run()

	return tmpDir
}

func TestStashAll(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(repoPath string)
		wantErr     bool
		expectStash bool
	}{
		{
			name: "stash tracked modifications",
			setup: func(repoPath string) {
				// Modify tracked file
				readmePath := filepath.Join(repoPath, "README.md")
				os.WriteFile(readmePath, []byte("# Modified\n"), 0644)
			},
			wantErr:     false,
			expectStash: true,
		},
		{
			name: "stash untracked files",
			setup: func(repoPath string) {
				// Create untracked file
				untrackedPath := filepath.Join(repoPath, "untracked.txt")
				os.WriteFile(untrackedPath, []byte("untracked content"), 0644)
			},
			wantErr:     false,
			expectStash: true,
		},
		{
			name: "ignored files are not stashed",
			setup: func(repoPath string) {
				// Create .gitignore
				gitignorePath := filepath.Join(repoPath, ".gitignore")
				os.WriteFile(gitignorePath, []byte("*.env\n"), 0644)
				exec.Command("git", "-C", repoPath, "add", ".gitignore").Run()
				exec.Command("git", "-C", repoPath, "commit", "-m", "Add gitignore").Run()

				// Create ignored file - this should NOT be stashed
				ignoredPath := filepath.Join(repoPath, ".env")
				os.WriteFile(ignoredPath, []byte("SECRET=123"), 0644)
			},
			wantErr:     false,
			expectStash: false, // No stash created because ignored files are skipped
		},
		{
			name: "no changes to stash",
			setup: func(repoPath string) {
				// No changes
			},
			wantErr:     false,
			expectStash: false, // No stash created when there's nothing to stash
		},
		{
			name: "stash mixed changes (tracked and untracked only)",
			setup: func(repoPath string) {
				// Tracked modification - WILL be stashed
				readmePath := filepath.Join(repoPath, "README.md")
				os.WriteFile(readmePath, []byte("# Modified\n"), 0644)

				// Untracked file - WILL be stashed
				untrackedPath := filepath.Join(repoPath, "untracked.txt")
				os.WriteFile(untrackedPath, []byte("untracked"), 0644)

				// Ignored file - will NOT be stashed
				gitignorePath := filepath.Join(repoPath, ".gitignore")
				os.WriteFile(gitignorePath, []byte("*.env\n"), 0644)
				exec.Command("git", "-C", repoPath, "add", ".gitignore").Run()
				exec.Command("git", "-C", repoPath, "commit", "-m", "Add gitignore").Run()

				ignoredPath := filepath.Join(repoPath, ".env")
				os.WriteFile(ignoredPath, []byte("SECRET=123"), 0644)
			},
			wantErr:     false,
			expectStash: true, // Stash created for tracked and untracked
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoPath := setupStashTestRepo(t)
			defer os.RemoveAll(repoPath)

			// Setup test conditions
			if tt.setup != nil {
				tt.setup(repoPath)
			}

			// Run StashAll
			err := StashAll(repoPath, "test stash message")
			if (err != nil) != tt.wantErr {
				t.Errorf("StashAll() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Check if stash was created
			hasStash, err := HasStash(repoPath)
			if err != nil {
				t.Fatalf("HasStash() failed: %v", err)
			}

			if hasStash != tt.expectStash {
				t.Errorf("HasStash() = %v, expected %v", hasStash, tt.expectStash)
			}

			// Verify working tree is clean after stash (if stash was created)
			// Note: We only check for tracked/untracked files, not ignored files
			if tt.expectStash {
				cmd := exec.Command("git", "-C", repoPath, "status", "--porcelain")
				output, _ := cmd.Output()
				if len(output) > 0 {
					t.Errorf("Working tree not clean after stash (tracked/untracked): %s", string(output))
				}
			}
		})
	}
}

func TestPopStash(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(repoPath string)
		wantErr    bool
		isConflict bool
	}{
		{
			name: "pop successful",
			setup: func(repoPath string) {
				// Create and stash a change
				readmePath := filepath.Join(repoPath, "README.md")
				os.WriteFile(readmePath, []byte("# Modified\n"), 0644)
				StashAll(repoPath, "test stash")
			},
			wantErr:    false,
			isConflict: false,
		},
		{
			name: "pop with conflict",
			setup: func(repoPath string) {
				// Modify README
				readmePath := filepath.Join(repoPath, "README.md")
				os.WriteFile(readmePath, []byte("# Changed A\n"), 0644)
				StashAll(repoPath, "test stash")

				// Make conflicting change
				os.WriteFile(readmePath, []byte("# Changed B\n"), 0644)
				exec.Command("git", "-C", repoPath, "add", "README.md").Run()
				exec.Command("git", "-C", repoPath, "commit", "-m", "Conflicting change").Run()
			},
			wantErr:    true,
			isConflict: true,
		},
		{
			name: "no stash to pop",
			setup: func(repoPath string) {
				// No stash exists
			},
			wantErr:    true,
			isConflict: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoPath := setupStashTestRepo(t)
			defer os.RemoveAll(repoPath)

			// Setup test conditions
			if tt.setup != nil {
				tt.setup(repoPath)
			}

			// Run PopStash
			err := PopStash(repoPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("PopStash() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Check if it's a conflict error
			if tt.isConflict {
				if _, ok := err.(*StashConflictError); !ok {
					t.Errorf("Expected StashConflictError, got %T", err)
				}
			}
		})
	}
}

func TestHasStash(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(repoPath string)
		wantStash bool
		wantErr   bool
	}{
		{
			name: "has stash",
			setup: func(repoPath string) {
				readmePath := filepath.Join(repoPath, "README.md")
				os.WriteFile(readmePath, []byte("# Modified\n"), 0644)
				StashAll(repoPath, "test stash")
			},
			wantStash: true,
			wantErr:   false,
		},
		{
			name: "no stash",
			setup: func(repoPath string) {
				// No stash
			},
			wantStash: false,
			wantErr:   false,
		},
		{
			name: "multiple stashes",
			setup: func(repoPath string) {
				// First stash
				readmePath := filepath.Join(repoPath, "README.md")
				os.WriteFile(readmePath, []byte("# Modified 1\n"), 0644)
				StashAll(repoPath, "test stash 1")

				// Second stash
				os.WriteFile(readmePath, []byte("# Modified 2\n"), 0644)
				StashAll(repoPath, "test stash 2")
			},
			wantStash: true,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoPath := setupStashTestRepo(t)
			defer os.RemoveAll(repoPath)

			// Setup test conditions
			if tt.setup != nil {
				tt.setup(repoPath)
			}

			// Run HasStash
			hasStash, err := HasStash(repoPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("HasStash() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if hasStash != tt.wantStash {
				t.Errorf("HasStash() = %v, want %v", hasStash, tt.wantStash)
			}
		})
	}
}

func TestHasChanges(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(repoPath string)
		wantChanges bool
		wantErr     bool
	}{
		{
			name: "tracked modifications",
			setup: func(repoPath string) {
				readmePath := filepath.Join(repoPath, "README.md")
				os.WriteFile(readmePath, []byte("# Modified\n"), 0644)
			},
			wantChanges: true,
			wantErr:     false,
		},
		{
			name: "untracked files",
			setup: func(repoPath string) {
				untrackedPath := filepath.Join(repoPath, "untracked.txt")
				os.WriteFile(untrackedPath, []byte("untracked"), 0644)
			},
			wantChanges: true,
			wantErr:     false,
		},
		{
			name: "ignored files are not detected as changes",
			setup: func(repoPath string) {
				// Create .gitignore
				gitignorePath := filepath.Join(repoPath, ".gitignore")
				os.WriteFile(gitignorePath, []byte("*.env\n"), 0644)
				exec.Command("git", "-C", repoPath, "add", ".gitignore").Run()
				exec.Command("git", "-C", repoPath, "commit", "-m", "Add gitignore").Run()

				// Create ignored file - this should NOT be detected as a change
				ignoredPath := filepath.Join(repoPath, ".env")
				os.WriteFile(ignoredPath, []byte("SECRET=123"), 0644)
			},
			wantChanges: false, // Ignored files are skipped
			wantErr:     false,
		},
		{
			name: "no changes",
			setup: func(repoPath string) {
				// Clean working tree
			},
			wantChanges: false,
			wantErr:     false,
		},
		{
			name: "mixed changes",
			setup: func(repoPath string) {
				// Tracked modification
				readmePath := filepath.Join(repoPath, "README.md")
				os.WriteFile(readmePath, []byte("# Modified\n"), 0644)

				// Untracked file
				untrackedPath := filepath.Join(repoPath, "untracked.txt")
				os.WriteFile(untrackedPath, []byte("untracked"), 0644)
			},
			wantChanges: true,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoPath := setupStashTestRepo(t)
			defer os.RemoveAll(repoPath)

			// Setup test conditions
			if tt.setup != nil {
				tt.setup(repoPath)
			}

			// Run HasChanges
			hasChanges, err := HasChanges(repoPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("HasChanges() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if hasChanges != tt.wantChanges {
				t.Errorf("HasChanges() = %v, want %v", hasChanges, tt.wantChanges)
			}
		})
	}
}
