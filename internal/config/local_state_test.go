package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestReadLocalState_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()

	state, err := ReadLocalState(tmpDir)
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}

	if state.DbSuffix != "" {
		t.Errorf("expected empty DbSuffix, got: %s", state.DbSuffix)
	}
}

func TestReadLocalState_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".anvil.local")

	content := []byte("db_suffix: sunset\n")
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	state, err := ReadLocalState(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if state.DbSuffix != "sunset" {
		t.Errorf("expected DbSuffix 'sunset', got: %s", state.DbSuffix)
	}
}

func TestReadLocalState_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".anvil.local")

	content := []byte("invalid: yaml: content: [")
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := ReadLocalState(tmpDir)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestWriteLocalState_CreateNew(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".anvil.local")

	state := LocalState{DbSuffix: "morning"}
	if err := WriteLocalState(tmpDir, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("expected .anvil.local to be created")
	}

	// Verify content
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var data map[string]interface{}
	if err := yaml.Unmarshal(content, &data); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	if data["db_suffix"] != "morning" {
		t.Errorf("expected db_suffix 'morning', got: %v", data["db_suffix"])
	}
}

func TestWriteLocalState_MergeExisting(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".anvil.local")

	// Create existing file with some data
	existing := map[string]interface{}{
		"db_suffix":  "original",
		"other_data": "preserve",
	}
	existingContent, err := yaml.Marshal(existing)
	if err != nil {
		t.Fatalf("failed to marshal existing data: %v", err)
	}
	if err := os.WriteFile(configPath, existingContent, 0644); err != nil {
		t.Fatalf("failed to write existing file: %v", err)
	}

	// Write new state
	state := LocalState{DbSuffix: "updated"}
	if err := WriteLocalState(tmpDir, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify merge
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var data map[string]interface{}
	if err := yaml.Unmarshal(content, &data); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	if data["db_suffix"] != "updated" {
		t.Errorf("expected db_suffix 'updated', got: %v", data["db_suffix"])
	}
	if data["other_data"] != "preserve" {
		t.Errorf("expected other_data 'preserve', got: %v", data["other_data"])
	}
}

func TestWriteLocalState_EmptyDbSuffix(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".anvil.local")

	// Create existing file
	existing := map[string]interface{}{
		"db_suffix": "original",
	}
	existingContent, err := yaml.Marshal(existing)
	if err != nil {
		t.Fatalf("failed to marshal existing data: %v", err)
	}
	if err := os.WriteFile(configPath, existingContent, 0644); err != nil {
		t.Fatalf("failed to write existing file: %v", err)
	}

	// Write empty state (should not overwrite)
	state := LocalState{DbSuffix: ""}
	if err := WriteLocalState(tmpDir, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify original value preserved
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var data map[string]interface{}
	if err := yaml.Unmarshal(content, &data); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	if data["db_suffix"] != "original" {
		t.Errorf("expected db_suffix 'original' to be preserved, got: %v", data["db_suffix"])
	}
}
