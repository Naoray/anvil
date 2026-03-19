package cli

import (
	"os"
	"testing"

	"github.com/naoray/anvil/internal/config"
)

func TestSkillDiff(t *testing.T) {
	t.Run("returns non-empty diff when files differ", func(t *testing.T) {
		f, err := os.CreateTemp("", "anvil-skill-test-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(f.Name())
		if _, err := f.WriteString("old content\n"); err != nil {
			t.Fatal(err)
		}
		f.Close()

		diff := skillDiff(f.Name(), []byte("new content\n"))
		if diff == "" {
			t.Error("expected non-empty diff for differing files")
		}
	})

	t.Run("returns empty string when files are identical", func(t *testing.T) {
		f, err := os.CreateTemp("", "anvil-skill-test-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(f.Name())
		if _, err := f.WriteString("same content\n"); err != nil {
			t.Fatal(err)
		}
		f.Close()

		diff := skillDiff(f.Name(), []byte("same content\n"))
		if diff != "" {
			t.Errorf("expected empty diff for identical files, got: %q", diff)
		}
	})

	t.Run("returns empty string when existing file does not exist", func(t *testing.T) {
		diff := skillDiff("/nonexistent/path/skill.md", []byte("some content\n"))
		if diff != "" {
			t.Errorf("expected empty diff for missing file, got: %q", diff)
		}
	})
}

func TestShouldRunSetupWizard(t *testing.T) {
	t.Run("returns false when setup is complete", func(t *testing.T) {
		cfg := &config.GlobalConfig{SetupComplete: true}
		if shouldRunSetupWizard(cfg) {
			t.Error("expected false when SetupComplete=true")
		}
	})

	t.Run("returns false in CI environment", func(t *testing.T) {
		t.Setenv("CI", "true")
		cfg := &config.GlobalConfig{SetupComplete: false}
		if shouldRunSetupWizard(cfg) {
			t.Error("expected false in CI environment")
		}
	})

	t.Run("returns true when setup not complete, not CI, not interactive checked separately", func(t *testing.T) {
		t.Setenv("CI", "")
		cfg := &config.GlobalConfig{SetupComplete: false}
		// shouldRunSetupWizard only checks config and CI, interactivity is checked by the caller
		if !shouldRunSetupWizard(cfg) {
			t.Error("expected true when SetupComplete=false and not CI")
		}
	})
}
