package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// MigrateDbSuffixToLocal migrates db_suffix from arbor.yaml to .arbor.local if present.
// Returns true if migration occurred, false otherwise.
func MigrateDbSuffixToLocal(worktreePath string) (bool, error) {
	configPath := filepath.Join(worktreePath, "arbor.yaml")

	// Check if arbor.yaml exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return false, nil
	}

	// Read arbor.yaml
	content, err := os.ReadFile(configPath)
	if err != nil {
		return false, fmt.Errorf("reading arbor.yaml: %w", err)
	}

	var data map[string]interface{}
	if err := yaml.Unmarshal(content, &data); err != nil {
		return false, fmt.Errorf("parsing arbor.yaml: %w", err)
	}

	// Check if db_suffix exists
	dbSuffix, hasDbSuffix := data["db_suffix"].(string)
	if !hasDbSuffix || dbSuffix == "" {
		return false, nil
	}

	// Write to .arbor.local
	localState := LocalState{DbSuffix: dbSuffix}
	if err := WriteLocalState(worktreePath, localState); err != nil {
		return false, fmt.Errorf("writing local state: %w", err)
	}

	// Remove db_suffix from arbor.yaml
	delete(data, "db_suffix")
	newContent, err := yaml.Marshal(data)
	if err != nil {
		return false, fmt.Errorf("marshaling arbor.yaml: %w", err)
	}

	if err := os.WriteFile(configPath, newContent, 0644); err != nil {
		return false, fmt.Errorf("writing arbor.yaml: %w", err)
	}

	return true, nil
}
