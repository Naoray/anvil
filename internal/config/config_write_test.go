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

func TestGlobalConfigNewFields(t *testing.T) {
	t.Run("round-trip SetupComplete and DefaultProjectsRoot", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmpDir)

		cfg := &GlobalConfig{
			DefaultBranch:       "main",
			DetectedTools:       map[string]bool{},
			SetupComplete:       true,
			DefaultProjectsRoot: "~/Projects",
		}

		if err := SaveGlobalConfig(cfg); err != nil {
			t.Fatalf("SaveGlobalConfig failed: %v", err)
		}

		loaded, err := LoadOrCreateGlobalConfig()
		if err != nil {
			t.Fatalf("LoadOrCreateGlobalConfig failed: %v", err)
		}

		if !loaded.SetupComplete {
			t.Error("expected SetupComplete to be true after round-trip")
		}
		if loaded.DefaultProjectsRoot != "~/Projects" {
			t.Errorf("expected DefaultProjectsRoot '~/Projects', got '%s'", loaded.DefaultProjectsRoot)
		}
	})

	t.Run("SetupComplete defaults to false when not set", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmpDir)

		cfg, err := LoadOrCreateGlobalConfig()
		if err != nil {
			t.Fatalf("LoadOrCreateGlobalConfig failed: %v", err)
		}

		if cfg.SetupComplete {
			t.Error("expected SetupComplete to default to false")
		}
	})
}

func TestSaveGlobalConfig_PreservesProjectNamesWithDots(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cfg := &GlobalConfig{
		DefaultBranch: "main",
		DetectedTools: map[string]bool{},
		Projects: map[string]*ProjectInfo{
			"virovet-diagnostik.de": {
				Path:          "/Users/test/Workspace/virovet-diagnostik.de",
				DefaultBranch: "main",
				Preset:        "laravel",
				SiteName:      "virovet-diagnostik.de",
			},
		},
	}

	if err := SaveGlobalConfig(cfg); err != nil {
		t.Fatalf("SaveGlobalConfig failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, "anvil", ProjectConfigFile))
	if err != nil {
		t.Fatalf("reading saved config: %v", err)
	}

	contentStr := string(content)
	if !contains(contentStr, "virovet-diagnostik.de:") {
		t.Fatalf("expected dotted project name to be preserved as a literal key, got:\n%s", contentStr)
	}
	if contains(contentStr, "virovet-diagnostik:\n") {
		t.Fatalf("expected dotted project name not to be split into nested keys, got:\n%s", contentStr)
	}

	loaded, err := LoadOrCreateGlobalConfig()
	if err != nil {
		t.Fatalf("LoadOrCreateGlobalConfig failed: %v", err)
	}

	project := loaded.GetLinkedProjectByName("virovet-diagnostik.de")
	if project == nil {
		t.Fatalf("expected dotted project to load by full name")
	}
	if project.Path != "/Users/test/Workspace/virovet-diagnostik.de" {
		t.Errorf("expected project path to round-trip, got %q", project.Path)
	}
}

func TestLoadGlobalConfig_RecoversNestedDottedProjectNames(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	configDir := filepath.Join(tmpDir, "anvil")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}

	content := []byte(`default_branch: main
projects:
    gaze:
        default_branch: main
        editor_cmd: ""
        path: /Users/test/Workspace/Gaze
        preset: rust
        site_name: gaze
    virovet-diagnostik:
        de:
            default_branch: main
            editor_cmd: ""
            path: /Users/test/Workspace/virovet-diagnostik.de
            preset: laravel
            site_name: virovet-diagnostik.de
        default_branch: ""
        editor_cmd: ""
        path: ""
        preset: ""
        site_name: ""
`)
	if err := os.WriteFile(filepath.Join(configDir, ProjectConfigFile), content, 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	loaded, err := LoadOrCreateGlobalConfig()
	if err != nil {
		t.Fatalf("LoadOrCreateGlobalConfig failed: %v", err)
	}

	if loaded.GetLinkedProjectByName("virovet-diagnostik") != nil {
		t.Fatalf("expected malformed empty parent project to be removed")
	}

	project := loaded.GetLinkedProjectByName("virovet-diagnostik.de")
	if project == nil {
		t.Fatalf("expected nested dotted project to be recovered")
	}
	if project.Path != "/Users/test/Workspace/virovet-diagnostik.de" {
		t.Errorf("expected recovered project path, got %q", project.Path)
	}
}

func TestLoadGlobalConfig_PrefersLiteralDottedProjectOverNestedStaleCopy(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	configDir := filepath.Join(tmpDir, "anvil")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}

	content := []byte(`default_branch: main
projects:
    virovet-diagnostik.de:
        default_branch: main
        editor_cmd: ""
        path: /Users/test/Workspace/current-virovet
        preset: laravel
        site_name: current-virovet
    virovet-diagnostik:
        de:
            default_branch: main
            editor_cmd: ""
            path: /Users/test/Workspace/stale-virovet
            preset: php
            site_name: stale-virovet
        default_branch: ""
        editor_cmd: ""
        path: ""
        preset: ""
        site_name: ""
`)
	if err := os.WriteFile(filepath.Join(configDir, ProjectConfigFile), content, 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	loaded, err := LoadOrCreateGlobalConfig()
	if err != nil {
		t.Fatalf("LoadOrCreateGlobalConfig failed: %v", err)
	}

	project := loaded.GetLinkedProjectByName("virovet-diagnostik.de")
	if project == nil {
		t.Fatalf("expected literal dotted project")
	}
	if project.Path != "/Users/test/Workspace/current-virovet" {
		t.Errorf("expected literal project to win, got %q", project.Path)
	}
}

func TestSaveGlobalConfig_CleansNestedDottedProjectNames(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	configDir := filepath.Join(tmpDir, "anvil")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}

	configPath := filepath.Join(configDir, ProjectConfigFile)
	content := []byte(`default_branch: main
projects:
    virovet-diagnostik:
        de:
            default_branch: main
            editor_cmd: ""
            path: /Users/test/Workspace/stale-virovet
            preset: laravel
            site_name: stale-virovet
        default_branch: ""
        editor_cmd: ""
        path: ""
        preset: ""
        site_name: ""
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	loaded, err := LoadOrCreateGlobalConfig()
	if err != nil {
		t.Fatalf("LoadOrCreateGlobalConfig failed: %v", err)
	}
	if err := SaveGlobalConfig(loaded); err != nil {
		t.Fatalf("SaveGlobalConfig failed: %v", err)
	}

	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading saved config: %v", err)
	}
	savedStr := string(saved)
	if contains(savedStr, "virovet-diagnostik:\n") {
		t.Fatalf("expected stale nested project to be removed, got:\n%s", savedStr)
	}
	if !contains(savedStr, "virovet-diagnostik.de:") {
		t.Fatalf("expected literal dotted project to be saved, got:\n%s", savedStr)
	}

	reloaded, err := LoadOrCreateGlobalConfig()
	if err != nil {
		t.Fatalf("reloading config: %v", err)
	}
	project := reloaded.GetLinkedProjectByName("virovet-diagnostik.de")
	if project == nil {
		t.Fatalf("expected recovered project after reload")
	}
	if project.Path != "/Users/test/Workspace/stale-virovet" {
		t.Errorf("expected recovered project path, got %q", project.Path)
	}
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
