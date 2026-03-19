//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompletionInstallPath(t *testing.T) {
	t.Run("zsh falls back to user dir when brew prefix not set", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		// Ensure HOMEBREW_PREFIX is not set so we use the fallback
		t.Setenv("HOMEBREW_PREFIX", "")

		path, err := completionInstallPath("zsh")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := filepath.Join(tmpDir, ".zsh", "completions", "_anvil")
		if path != expected {
			t.Errorf("expected %q, got %q", expected, path)
		}
	})

	t.Run("bash falls back to user dir when /etc/bash_completion.d not writable", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		path, err := completionInstallPath("bash")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := filepath.Join(tmpDir, ".bash_completion.d", "anvil")
		if path != expected {
			t.Errorf("expected %q, got %q", expected, path)
		}
	})

	t.Run("fish uses XDG_CONFIG_HOME when set", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmpDir)

		path, err := completionInstallPath("fish")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := filepath.Join(tmpDir, "fish", "completions", "anvil.fish")
		if path != expected {
			t.Errorf("expected %q, got %q", expected, path)
		}
	})

	t.Run("fish falls back to HOME/.config when XDG_CONFIG_HOME not set", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		t.Setenv("XDG_CONFIG_HOME", "")

		path, err := completionInstallPath("fish")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := filepath.Join(tmpDir, ".config", "fish", "completions", "anvil.fish")
		if path != expected {
			t.Errorf("expected %q, got %q", expected, path)
		}
	})

	t.Run("unknown shell returns error", func(t *testing.T) {
		_, err := completionInstallPath("tcsh")
		if err == nil {
			t.Error("expected error for unknown shell, got nil")
		}
	})
}

func TestDetectShell(t *testing.T) {
	t.Run("detects zsh from SHELL env", func(t *testing.T) {
		t.Setenv("SHELL", "/bin/zsh")
		if got := detectShell(); got != "zsh" {
			t.Errorf("expected 'zsh', got %q", got)
		}
	})

	t.Run("detects bash from SHELL env", func(t *testing.T) {
		t.Setenv("SHELL", "/usr/local/bin/bash")
		if got := detectShell(); got != "bash" {
			t.Errorf("expected 'bash', got %q", got)
		}
	})

	t.Run("detects fish from SHELL env", func(t *testing.T) {
		t.Setenv("SHELL", "/usr/local/bin/fish")
		if got := detectShell(); got != "fish" {
			t.Errorf("expected 'fish', got %q", got)
		}
	})

	t.Run("defaults to zsh for unknown shell", func(t *testing.T) {
		t.Setenv("SHELL", "/bin/sh")
		if got := detectShell(); got != "zsh" {
			t.Errorf("expected 'zsh' as default, got %q", got)
		}
	})
}

func TestRebuildZshCompletionCache(t *testing.T) {
	t.Run("removes .zcompdump when it exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		dump := filepath.Join(tmpDir, ".zcompdump")
		if err := os.WriteFile(dump, []byte("cache"), 0644); err != nil {
			t.Fatal(err)
		}

		rebuildZshCompletionCache()

		if _, err := os.Stat(dump); !os.IsNotExist(err) {
			t.Error("expected .zcompdump to be removed")
		}
	})

	t.Run("removes versioned .zcompdump-* files", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		versioned := filepath.Join(tmpDir, ".zcompdump-testhost-5.9")
		if err := os.WriteFile(versioned, []byte("cache"), 0644); err != nil {
			t.Fatal(err)
		}

		rebuildZshCompletionCache()

		if _, err := os.Stat(versioned); !os.IsNotExist(err) {
			t.Error("expected versioned .zcompdump to be removed")
		}
	})

	t.Run("does not error when no dump files exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		// Should not panic or error
		rebuildZshCompletionCache()
	})
}

func TestInstallCompletionWritesFile(t *testing.T) {
	t.Run("writes completion script to target path", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		t.Setenv("HOMEBREW_PREFIX", "")

		targetPath := filepath.Join(tmpDir, ".zsh", "completions", "_anvil")

		err := installCompletionToPath(rootCmd, "zsh", targetPath)
		if err != nil {
			t.Fatalf("installCompletionToPath failed: %v", err)
		}

		content, err := os.ReadFile(targetPath)
		if err != nil {
			t.Fatalf("failed to read installed completion file: %v", err)
		}

		if len(content) == 0 {
			t.Error("completion file is empty")
		}
	})
}
