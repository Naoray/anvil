package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DbRoleApplication = "application"
	DbRoleTesting     = "testing"
)

var databaseIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// OwnedDatabase records a database that Anvil created for a worktree.
type OwnedDatabase struct {
	Name   string `yaml:"name"`
	Engine string `yaml:"engine"`
	Role   string `yaml:"role"`
}

// LocalState represents worktree-local state that should never be committed.
type LocalState struct {
	DbSuffix  string          `yaml:"db_suffix"`
	Databases []OwnedDatabase `yaml:"databases,omitempty"`
}

// IsValidDatabaseIdentifier reports whether a name is safe for Anvil's
// identifier-quoted create/drop statements.
func IsValidDatabaseIdentifier(name string) bool {
	return databaseIdentifierPattern.MatchString(name)
}

// ValidateOwnedDatabases is the shared fail-closed gate for database ownership
// records. It reports every record error in state order.
func ValidateOwnedDatabases(databases []OwnedDatabase) error {
	var validationErrors []error
	seenEngines := make(map[string]struct{})
	engineOrder := make([]string, 0, 2)
	seenNames := make(map[string]int, len(databases))
	seenRoles := make(map[string]int, 2)

	for index, database := range databases {
		if !IsValidDatabaseIdentifier(database.Name) {
			validationErrors = append(validationErrors,
				fmt.Errorf("invalid database name %q in record %d", database.Name, index))
		}
		if firstIndex, exists := seenNames[database.Name]; exists {
			validationErrors = append(validationErrors,
				fmt.Errorf("duplicate database name %q in record %d; first seen in record %d", database.Name, index, firstIndex))
		} else {
			seenNames[database.Name] = index
		}
		switch database.Role {
		case DbRoleApplication, DbRoleTesting:
			if firstIndex, exists := seenRoles[database.Role]; exists {
				validationErrors = append(validationErrors,
					fmt.Errorf("duplicate database role %q in record %d; first seen in record %d", database.Role, index, firstIndex))
			} else {
				seenRoles[database.Role] = index
			}
		default:
			validationErrors = append(validationErrors,
				fmt.Errorf("unsupported database record role %q in record %q; supported roles: application, testing", database.Role, database.Name))
		}

		switch DatabaseEngine(database.Engine) {
		case DBEngineMySQL, DBEnginePgSQL:
			if _, exists := seenEngines[database.Engine]; !exists {
				seenEngines[database.Engine] = struct{}{}
				engineOrder = append(engineOrder, database.Engine)
			}
		default:
			validationErrors = append(validationErrors,
				fmt.Errorf("unsupported database engine %q in record %q; supported engines: mysql, pgsql", database.Engine, database.Name))
		}
	}

	if len(engineOrder) > 1 {
		validationErrors = append(validationErrors,
			fmt.Errorf("database records must use exactly one supported engine; found %s", strings.Join(engineOrder, ", ")))
	}

	return errors.Join(validationErrors...)
}

// ReadLocalState reads worktree-local state from .anvil.local.
func ReadLocalState(worktreePath string) (*LocalState, error) {
	configPath := filepath.Join(worktreePath, LocalStateFile)

	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			return &LocalState{}, nil
		}
		return nil, fmt.Errorf("stating local state: %w", err)
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

// WriteLocalState merges worktree-local state into .anvil.local. Canonical
// database records replace by role while unknown YAML fields are preserved.
func WriteLocalState(worktreePath string, data LocalState) error {
	configPath := filepath.Join(worktreePath, LocalStateFile)

	existing := make(map[string]any)
	content, err := os.ReadFile(configPath)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(content, &existing); err != nil {
			return fmt.Errorf("parsing existing local state: %w", err)
		}
	case os.IsNotExist(err):
	default:
		return fmt.Errorf("reading existing local state: %w", err)
	}
	if existing == nil {
		existing = make(map[string]any)
	}

	if data.DbSuffix != "" {
		existing["db_suffix"] = data.DbSuffix
	}

	existingDatabases, hasExistingDatabases := existing["databases"]
	if hasExistingDatabases || len(data.Databases) > 0 {
		merged, err := mergeOwnedDatabases(existingDatabases, data.Databases)
		if err != nil {
			return err
		}
		existing["databases"] = merged
	}

	content, err = yaml.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshaling local state: %w", err)
	}
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		return fmt.Errorf("writing local state: %w", err)
	}
	return nil
}

func mergeOwnedDatabases(existingRaw any, updates []OwnedDatabase) ([]any, error) {
	var records []any
	if existingRaw != nil {
		var ok bool
		records, ok = existingRaw.([]any)
		if !ok {
			return nil, fmt.Errorf("malformed databases in local state: expected a list, got %T", existingRaw)
		}
	}

	for index, raw := range records {
		record, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("malformed databases in local state: record %d is %T, want mapping", index, raw)
		}
		for _, field := range []string{"name", "engine", "role"} {
			if value, exists := record[field]; exists {
				if _, ok := value.(string); !ok {
					return nil, fmt.Errorf("malformed databases in local state: record %d field %q is %T, want string", index, field, value)
				}
			}
		}
	}

	for _, update := range updates {
		updated := false
		next := make([]any, 0, len(records)+1)
		for _, raw := range records {
			record := raw.(map[string]any)
			role, _ := record["role"].(string)
			if role != update.Role {
				next = append(next, record)
				continue
			}
			if updated {
				continue
			}
			record["name"] = update.Name
			record["engine"] = update.Engine
			record["role"] = update.Role
			next = append(next, record)
			updated = true
		}
		if !updated {
			next = append(next, map[string]any{
				"name": update.Name, "engine": update.Engine, "role": update.Role,
			})
		}
		records = next
	}

	return records, nil
}
