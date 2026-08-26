//go:build !windows

package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestCompletionInstallPath(t *testing.T) {
	t.Run("zsh uses writable brew site-functions directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("HOMEBREW_PREFIX", tmpDir)
		dir := filepath.Join(tmpDir, "share", "zsh", "site-functions")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("create brew completion directory: %v", err)
		}

		path, err := completionInstallPath("zsh")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := filepath.Join(dir, "_anvil")
		if path != expected {
			t.Errorf("expected %q, got %q", expected, path)
		}
	})

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

func TestInstallCompletionWritesSupportedShellFiles(t *testing.T) {
	for _, shell := range []string{"zsh", "bash", "fish"} {
		t.Run(shell, func(t *testing.T) {
			targetPath := filepath.Join(t.TempDir(), shell)
			if err := installCompletionToPath(rootCmd, shell, targetPath); err != nil {
				t.Fatalf("installCompletionToPath failed: %v", err)
			}

			content, err := os.ReadFile(targetPath)
			if err != nil {
				t.Fatalf("read installed completion file: %v", err)
			}
			if len(content) == 0 {
				t.Error("completion file is empty")
			}
		})
	}
}

func TestInstallCompletionPreservesZshCaches(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("HOMEBREW_PREFIX", "")
	t.Setenv("TERM", "dumb")

	caches := map[string][]byte{
		filepath.Join(tmpDir, ".zcompdump"):          []byte("default zsh cache"),
		filepath.Join(tmpDir, ".zcompdump-host-5.9"): []byte("versioned zsh cache"),
	}
	for path, contents := range caches {
		if err := os.WriteFile(path, contents, 0644); err != nil {
			t.Fatalf("write cache sentinel %q: %v", path, err)
		}
	}

	oldStdin := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	os.Stdin = reader
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = reader.Close()
	})
	if _, err := writer.WriteString("y\n"); err != nil {
		t.Fatalf("confirm completion installation: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdin pipe: %v", err)
	}

	var installErr error
	output := captureStderr(t, func() {
		installErr = installCompletion(rootCmd, "zsh")
	})
	if installErr != nil {
		t.Fatalf("installCompletion failed: %v", installErr)
	}

	for _, want := range []string{
		"Restart your shell",
		"fpath",
		"autoload -Uz compinit && compinit",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected install output to contain %q, got %q", want, output)
		}
	}
	if strings.Contains(output, "Cleared zsh completion cache") {
		t.Errorf("install output claims zsh cache was cleared: %q", output)
	}

	targetPath := filepath.Join(tmpDir, ".zsh", "completions", "_anvil")
	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("expected owned completion file at %q: %v", targetPath, err)
	}
	for path, want := range caches {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read cache sentinel %q: %v", path, err)
		}
		if string(got) != string(want) {
			t.Errorf("cache sentinel %q changed: got %q, want %q", path, got, want)
		}
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}

	stderrFD := int(os.Stderr.Fd())
	originalFD, err := syscall.Dup(stderrFD)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		t.Fatalf("duplicate stderr: %v", err)
	}
	if err := syscall.Dup2(int(writer.Fd()), stderrFD); err != nil {
		_ = syscall.Close(originalFD)
		_ = reader.Close()
		_ = writer.Close()
		t.Fatalf("redirect stderr: %v", err)
	}

	defer func() {
		_ = syscall.Dup2(originalFD, stderrFD)
		_ = syscall.Close(originalFD)
		_ = reader.Close()
		_ = writer.Close()
	}()

	fn()
	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	if err := syscall.Dup2(originalFD, stderrFD); err != nil {
		t.Fatalf("restore stderr: %v", err)
	}

	captured, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	return string(captured)
}
