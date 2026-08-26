package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestRepo creates a temporary git repository for testing
func setupStashTestRepo(t *testing.T) string {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "anvil-stash-test-*")
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

func mustStashAll(t *testing.T, repoPath string, message string) string {
	t.Helper()
	stashOID, err := StashAll(repoPath, message)
	if err != nil {
		t.Fatalf("StashAll(%q) failed: %v", message, err)
	}
	return stashOID
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
			_, err := StashAll(repoPath, "test stash message")
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

func TestStashAllReturnsStableCreatedOID(t *testing.T) {
	repoPath := setupStashTestRepo(t)
	defer os.RemoveAll(repoPath)

	readmePath := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Pre-existing\n"), 0644); err != nil {
		t.Fatalf("failed to create pre-existing stash change: %v", err)
	}
	if _, err := StashAll(repoPath, "pre-existing stash"); err != nil {
		t.Fatalf("failed to create pre-existing stash: %v", err)
	}

	if err := os.WriteFile(readmePath, []byte("# Auto-stash\n"), 0644); err != nil {
		t.Fatalf("failed to create auto-stash change: %v", err)
	}
	autoStashOID, err := StashAll(repoPath, "auto-stash")
	if err != nil {
		t.Fatalf("StashAll() failed: %v", err)
	}
	if autoStashOID == "" {
		t.Fatal("StashAll() returned an empty stash OID")
	}

	if err := os.WriteFile(readmePath, []byte("# Later stash\n"), 0644); err != nil {
		t.Fatalf("failed to create later stash change: %v", err)
	}
	if _, err := StashAll(repoPath, "later stash"); err != nil {
		t.Fatalf("failed to create later stash: %v", err)
	}

	cmd := exec.Command("git", "-C", repoPath, "stash", "list", "--format=%H")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to list stash OIDs: %v", err)
	}
	for _, oid := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if oid == autoStashOID {
			return
		}
	}
	t.Fatalf("auto-stash OID %q was not preserved in stash list %q", autoStashOID, string(output))
}

func TestApplyAndDropStashByOID(t *testing.T) {
	repoPath := setupStashTestRepo(t)
	defer os.RemoveAll(repoPath)

	readmePath := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Pre-existing\n"), 0644); err != nil {
		t.Fatalf("failed to create pre-existing stash change: %v", err)
	}
	preExistingOID, err := StashAll(repoPath, "pre-existing stash")
	if err != nil {
		t.Fatalf("failed to create pre-existing stash: %v", err)
	}

	if err := os.WriteFile(readmePath, []byte("# Auto-stash\n"), 0644); err != nil {
		t.Fatalf("failed to create auto-stash change: %v", err)
	}
	autoStashOID, err := StashAll(repoPath, "auto-stash")
	if err != nil {
		t.Fatalf("failed to create auto-stash: %v", err)
	}

	if err := os.WriteFile(readmePath, []byte("# Later stash\n"), 0644); err != nil {
		t.Fatalf("failed to create later stash change: %v", err)
	}
	laterOID, err := StashAll(repoPath, "later stash")
	if err != nil {
		t.Fatalf("failed to create later stash: %v", err)
	}

	if err := ApplyStash(repoPath, autoStashOID); err != nil {
		t.Fatalf("ApplyStash() failed: %v", err)
	}
	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}
	if string(content) != "# Auto-stash\n" {
		t.Fatalf("restored file = %q, want auto-stash content", string(content))
	}

	if err := DropStash(repoPath, autoStashOID); err != nil {
		t.Fatalf("DropStash() failed: %v", err)
	}

	cmd := exec.Command("git", "-C", repoPath, "stash", "list", "--format=%H")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to list remaining stashes: %v", err)
	}
	remaining := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, oid := range remaining {
		if oid == autoStashOID {
			t.Fatalf("auto-stash OID %q was dropped by OID", autoStashOID)
		}
	}
	for _, wantOID := range []string{preExistingOID, laterOID} {
		found := false
		for _, oid := range remaining {
			if oid == wantOID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("stash OID %q was not preserved after dropping auto-stash; remaining=%q", wantOID, string(output))
		}
	}
}

func TestApplyStashConflictPreservesOID(t *testing.T) {
	repoPath := setupStashTestRepo(t)
	defer os.RemoveAll(repoPath)

	readmePath := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Stashed\n"), 0644); err != nil {
		t.Fatalf("failed to create stash change: %v", err)
	}
	stashOID, err := StashAll(repoPath, "conflicting stash")
	if err != nil {
		t.Fatalf("failed to create stash: %v", err)
	}

	if err := os.WriteFile(readmePath, []byte("# Conflicting\n"), 0644); err != nil {
		t.Fatalf("failed to create conflicting change: %v", err)
	}
	cmd := exec.Command("git", "-C", repoPath, "add", "README.md")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to stage conflicting change: %v", err)
	}
	cmd = exec.Command("git", "-C", repoPath, "commit", "-m", "Conflicting change")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to commit conflicting change: %v", err)
	}

	err = ApplyStash(repoPath, stashOID)
	if _, ok := err.(*StashConflictError); !ok {
		t.Fatalf("ApplyStash() error = %T, want *StashConflictError", err)
	}

	cmd = exec.Command("git", "-C", repoPath, "stash", "list", "--format=%H")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to list stashes: %v", err)
	}
	if !strings.Contains(string(output), stashOID) {
		t.Fatalf("stash OID %q was not preserved after conflict: %q", stashOID, string(output))
	}
}

func TestStashConflictErrorDoesNotSuggestUnsafeRecovery(t *testing.T) {
	errText := (&StashConflictError{Output: "CONFLICT (content): Merge conflict in README.md"}).Error()

	for _, unsafeGuidance := range []string{
		"git reset --hard",
		"git stash apply",
		"git stash drop",
	} {
		if strings.Contains(errText, unsafeGuidance) {
			t.Fatalf("StashConflictError() = %q, must not suggest %q", errText, unsafeGuidance)
		}
	}
}

func TestDropStashMissingOIDPreservesExistingStash(t *testing.T) {
	repoPath := setupStashTestRepo(t)
	defer os.RemoveAll(repoPath)

	readmePath := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Stashed\n"), 0644); err != nil {
		t.Fatalf("failed to create stash change: %v", err)
	}
	stashOID, err := StashAll(repoPath, "existing stash")
	if err != nil {
		t.Fatalf("failed to create stash: %v", err)
	}

	err = DropStash(repoPath, "0000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("DropStash() error = nil, want missing OID error")
	}
	if !strings.Contains(err.Error(), "was not dropped") {
		t.Fatalf("DropStash() error = %q, want preserved-state guidance", err)
	}

	cmd := exec.Command("git", "-C", repoPath, "stash", "list", "--format=%H")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to list stashes: %v", err)
	}
	if strings.TrimSpace(string(output)) != stashOID {
		t.Fatalf("existing stash OID %q was changed by missing drop: %q", stashOID, string(output))
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
				mustStashAll(t, repoPath, "test stash")
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
				mustStashAll(t, repoPath, "test stash")

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
				mustStashAll(t, repoPath, "test stash")
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
				mustStashAll(t, repoPath, "test stash 1")

				// Second stash
				os.WriteFile(readmePath, []byte("# Modified 2\n"), 0644)
				mustStashAll(t, repoPath, "test stash 2")
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
