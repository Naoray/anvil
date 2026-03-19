package cli

import (
	"testing"

	"github.com/naoray/anvil/internal/config"
)

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
