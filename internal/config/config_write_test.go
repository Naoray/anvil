package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveProject(t *testing.T) {
	t.Run("creates new project config", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg := &Config{
			SiteName:      "MyProject",
			Preset:        "laravel",
			DefaultBranch: "main",
		}

		err := SaveProject(tmpDir, cfg)
		if err != nil {
			t.Fatalf("SaveProject failed: %v", err)
		}

		// Verify file was created
		configPath := filepath.Join(tmpDir, "anvil.yaml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Error("config file was not created")
		}

		// Load it back and verify
		loaded, err := LoadProject(tmpDir)
		if err != nil {
			t.Fatalf("failed to load project: %v", err)
		}

		if loaded.SiteName != "MyProject" {
			t.Errorf("expected SiteName 'MyProject', got '%s'", loaded.SiteName)
		}
		if loaded.Preset != "laravel" {
			t.Errorf("expected Preset 'laravel', got '%s'", loaded.Preset)
		}
		if loaded.DefaultBranch != "main" {
			t.Errorf("expected DefaultBranch 'main', got '%s'", loaded.DefaultBranch)
		}
	})

	t.Run("preserves existing config data", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "anvil.yaml")

		// Create initial config with extra fields
		initialContent := `site_name: OldSite
preset: old_preset
default_branch: old_branch
custom_field: custom_value
`
		if err := os.WriteFile(configPath, []byte(initialContent), 0644); err != nil {
			t.Fatalf("failed to create initial config: %v", err)
		}

		// Save with only some fields updated
		cfg := &Config{
			SiteName: "NewSite",
			// Preset and DefaultBranch left empty - should preserve existing
		}

		err := SaveProject(tmpDir, cfg)
		if err != nil {
			t.Fatalf("SaveProject failed: %v", err)
		}

		// Read back the raw content
		content, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("failed to read config file: %v", err)
		}

		contentStr := string(content)
		if !contains(contentStr, "site_name: NewSite") {
			t.Errorf("expected updated site_name not found in:\n%s", contentStr)
		}
		if !contains(contentStr, "preset: old_preset") {
			t.Errorf("expected preserved preset not found in:\n%s", contentStr)
		}
		if !contains(contentStr, "default_branch: old_branch") {
			t.Errorf("expected preserved default_branch not found in:\n%s", contentStr)
		}
	})

	t.Run("round-trip preserves data", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Save initial config
		cfg := &Config{
			SiteName:      "TestProject",
			Preset:        "php",
			DefaultBranch: "develop",
		}

		err := SaveProject(tmpDir, cfg)
		if err != nil {
			t.Fatalf("SaveProject failed: %v", err)
		}

		// Load it back
		loaded, err := LoadProject(tmpDir)
		if err != nil {
			t.Fatalf("LoadProject failed: %v", err)
		}

		// Verify all fields
		if loaded.SiteName != "TestProject" {
			t.Errorf("SiteName mismatch: expected 'TestProject', got '%s'", loaded.SiteName)
		}
		if loaded.Preset != "php" {
			t.Errorf("Preset mismatch: expected 'php', got '%s'", loaded.Preset)
		}
		if loaded.DefaultBranch != "develop" {
			t.Errorf("DefaultBranch mismatch: expected 'develop', got '%s'", loaded.DefaultBranch)
		}
	})
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
