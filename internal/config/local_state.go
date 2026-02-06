package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LocalState represents worktree-local state that should never be committed
type LocalState struct {
	DbSuffix string `yaml:"db_suffix"`
}

// ReadLocalState reads worktree-local state from .anvil.local
func ReadLocalState(worktreePath string) (*LocalState, error) {
	configPath := filepath.Join(worktreePath, ".anvil.local")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return &LocalState{}, nil
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading local state: %w", err)
	}

	var state LocalState
	if err := yaml.Unmarshal(content, &state); err != nil {
		return nil, fmt.Errorf("parsing local state: %w", err)
	}

	return &state, nil
}

// WriteLocalState writes worktree-local state to .anvil.local
func WriteLocalState(worktreePath string, data LocalState) error {
	configPath := filepath.Join(worktreePath, ".anvil.local")

	// Read existing state if it exists
	var existing map[string]interface{}
	if content, err := os.ReadFile(configPath); err == nil {
		if err := yaml.Unmarshal(content, &existing); err != nil {
			return fmt.Errorf("parsing existing local state: %w", err)
		}
	}

	if existing == nil {
		existing = make(map[string]interface{})
	}

	// Merge new data into existing state
	if data.DbSuffix != "" {
		existing["db_suffix"] = data.DbSuffix
	}

	// Marshal and write
	content, err := yaml.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshaling local state: %w", err)
	}

	if err := os.WriteFile(configPath, content, 0644); err != nil {
		return fmt.Errorf("writing local state: %w", err)
	}

	return nil
}
