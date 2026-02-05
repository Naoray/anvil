package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMigrateDbSuffixToLocal_NoArborYaml(t *testing.T) {
	tmpDir := t.TempDir()

	migrated, err := MigrateDbSuffixToLocal(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if migrated {
		t.Error("expected migrated=false when arbor.yaml doesn't exist")
	}
}

func TestMigrateDbSuffixToLocal_NoDbSuffix(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "arbor.yaml")

	content := []byte("preset: laravel\nsite_name: test\n")
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	migrated, err := MigrateDbSuffixToLocal(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if migrated {
		t.Error("expected migrated=false when db_suffix doesn't exist")
	}
}

func TestMigrateDbSuffixToLocal_Success(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "arbor.yaml")
	localPath := filepath.Join(tmpDir, ".arbor.local")

	// Create arbor.yaml with db_suffix
	arborContent := map[string]interface{}{
		"preset":    "laravel",
		"site_name": "test",
		"db_suffix": "sunset",
	}
	content, err := yaml.Marshal(arborContent)
	if err != nil {
		t.Fatalf("failed to marshal content: %v", err)
	}
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Run migration
	migrated, err := MigrateDbSuffixToLocal(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !migrated {
		t.Fatal("expected migrated=true")
	}

	// Verify .arbor.local was created with db_suffix
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		t.Fatal("expected .arbor.local to be created")
	}

	localContent, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("failed to read .arbor.local: %v", err)
	}

	var localData map[string]interface{}
	if err := yaml.Unmarshal(localContent, &localData); err != nil {
		t.Fatalf("failed to parse .arbor.local: %v", err)
	}

	if localData["db_suffix"] != "sunset" {
		t.Errorf("expected db_suffix 'sunset' in .arbor.local, got: %v", localData["db_suffix"])
	}

	// Verify db_suffix was removed from arbor.yaml
	arborContent2, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read arbor.yaml: %v", err)
	}

	var arborData map[string]interface{}
	if err := yaml.Unmarshal(arborContent2, &arborData); err != nil {
		t.Fatalf("failed to parse arbor.yaml: %v", err)
	}

	if _, hasDbSuffix := arborData["db_suffix"]; hasDbSuffix {
		t.Error("expected db_suffix to be removed from arbor.yaml")
	}

	// Verify other fields preserved
	if arborData["preset"] != "laravel" {
		t.Errorf("expected preset 'laravel', got: %v", arborData["preset"])
	}
	if arborData["site_name"] != "test" {
		t.Errorf("expected site_name 'test', got: %v", arborData["site_name"])
	}
}

func TestMigrateDbSuffixToLocal_EmptyDbSuffix(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "arbor.yaml")

	arborContent := map[string]interface{}{
		"preset":    "laravel",
		"db_suffix": "",
	}
	content, err := yaml.Marshal(arborContent)
	if err != nil {
		t.Fatalf("failed to marshal content: %v", err)
	}
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	migrated, err := MigrateDbSuffixToLocal(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if migrated {
		t.Error("expected migrated=false when db_suffix is empty")
	}
}
