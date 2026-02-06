package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMigrateDbSuffixToLocal_NoAnvilYaml(t *testing.T) {
	tmpDir := t.TempDir()

	migrated, err := MigrateDbSuffixToLocal(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if migrated {
		t.Error("expected migrated=false when anvil.yaml doesn't exist")
	}
}

func TestMigrateDbSuffixToLocal_NoDbSuffix(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "anvil.yaml")

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
	configPath := filepath.Join(tmpDir, "anvil.yaml")
	localPath := filepath.Join(tmpDir, ".anvil.local")

	// Create anvil.yaml with db_suffix
	anvilContent := map[string]interface{}{
		"preset":    "laravel",
		"site_name": "test",
		"db_suffix": "sunset",
	}
	content, err := yaml.Marshal(anvilContent)
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

	// Verify .anvil.local was created with db_suffix
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		t.Fatal("expected .anvil.local to be created")
	}

	localContent, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("failed to read .anvil.local: %v", err)
	}

	var localData map[string]interface{}
	if err := yaml.Unmarshal(localContent, &localData); err != nil {
		t.Fatalf("failed to parse .anvil.local: %v", err)
	}

	if localData["db_suffix"] != "sunset" {
		t.Errorf("expected db_suffix 'sunset' in .anvil.local, got: %v", localData["db_suffix"])
	}

	// Verify db_suffix was removed from anvil.yaml
	anvilContent2, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read anvil.yaml: %v", err)
	}

	var anvilData map[string]interface{}
	if err := yaml.Unmarshal(anvilContent2, &anvilData); err != nil {
		t.Fatalf("failed to parse anvil.yaml: %v", err)
	}

	if _, hasDbSuffix := anvilData["db_suffix"]; hasDbSuffix {
		t.Error("expected db_suffix to be removed from anvil.yaml")
	}

	// Verify other fields preserved
	if anvilData["preset"] != "laravel" {
		t.Errorf("expected preset 'laravel', got: %v", anvilData["preset"])
	}
	if anvilData["site_name"] != "test" {
		t.Errorf("expected site_name 'test', got: %v", anvilData["site_name"])
	}
}

func TestMigrateDbSuffixToLocal_EmptyDbSuffix(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "anvil.yaml")

	anvilContent := map[string]interface{}{
		"preset":    "laravel",
		"db_suffix": "",
	}
	content, err := yaml.Marshal(anvilContent)
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
