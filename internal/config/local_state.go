package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DbRoleApplication = "application"
	DbRoleTesting     = "testing"
	DbRoleAuxiliary   = "auxiliary"
)

var databaseIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// OwnedDatabase records a database that Anvil created for a worktree.
type OwnedDatabase struct {
	Endpoint *DatabaseEndpoint `yaml:"endpoint,omitempty"`
	Name     string            `yaml:"name"`
	Engine   string            `yaml:"engine"`
	Role     string            `yaml:"role"`
}

// DatabaseEndpoint is the nonsecret managed connection verified at creation.
type DatabaseEndpoint struct {
	Driver  string `yaml:"driver"`
	Service string `yaml:"service"`
	Host    string `yaml:"host"`
	Port    string `yaml:"port"`
}

func (e DatabaseEndpoint) Validate(engine string) error {
	if e.Driver != "yerd" && e.Driver != "herd" {
		return fmt.Errorf("unsupported database driver %q", e.Driver)
	}
	if (engine != "mysql" || e.Service != "mysql" && e.Service != "mariadb") && (engine != "pgsql" || e.Driver == "yerd" && e.Service != "postgres" || e.Driver == "herd" && e.Service != "postgresql") {
		return fmt.Errorf("inconsistent database engine/service identity")
	}
	port, err := strconv.Atoi(e.Port)
	if e.Host == "" || err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid database endpoint host/port")
	}
	return nil
}

// LocalState represents worktree-local state that should never be committed.
type LocalState struct {
	DbSuffix            string               `yaml:"db_suffix"`
	Databases           []OwnedDatabase      `yaml:"databases,omitempty"`
	BorrowedApplication *BorrowedApplication `yaml:"borrowed_application,omitempty"`
}

// BorrowedApplication records an exact nonsecret identity, never ownership.
type BorrowedApplication struct {
	SourcePath string `yaml:"source_path"`
	Name       string `yaml:"name"`
	Engine     string `yaml:"engine"`
	Driver     string `yaml:"driver"`
	Service    string `yaml:"service"`
	Host       string `yaml:"host,omitempty"`
	Port       string `yaml:"port,omitempty"`
}

func ValidateBorrowedApplication(b *BorrowedApplication) error {
	if b == nil {
		return nil
	}
	if !filepath.IsAbs(b.SourcePath) || filepath.Clean(b.SourcePath) != b.SourcePath || !IsValidDatabaseIdentifier(b.Name) {
		return fmt.Errorf("invalid borrowed application path or database name")
	}
	if b.Driver != "yerd" && b.Driver != "herd" {
		return fmt.Errorf("unsupported borrowed database driver %q", b.Driver)
	}
	if (b.Engine != "mysql" || (b.Service != "mysql" && b.Service != "mariadb")) && (b.Engine != "pgsql" || (b.Service != "postgres" && b.Service != "postgresql")) {
		return fmt.Errorf("inconsistent borrowed engine/service")
	}
	if b.Host == "" {
		return fmt.Errorf("borrowed database endpoint host is missing")
	}
	port, err := strconv.Atoi(b.Port)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("borrowed database endpoint port is invalid")
	}
	if b.Engine == "pgsql" && ((b.Driver == "yerd" && b.Service != "postgres") || (b.Driver == "herd" && b.Service != "postgresql")) {
		return fmt.Errorf("borrowed database driver/service mismatch")
	}
	return nil
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
		case DbRoleApplication, DbRoleTesting, DbRoleAuxiliary:
			if firstIndex, exists := seenRoles[database.Role]; exists {
				if database.Role != DbRoleAuxiliary {
					validationErrors = append(validationErrors,
						fmt.Errorf("duplicate database role %q in record %d; first seen in record %d", database.Role, index, firstIndex))
				}
			} else {
				seenRoles[database.Role] = index
			}
		default:
			validationErrors = append(validationErrors,
				fmt.Errorf("unsupported database record role %q in record %q; supported roles: application, testing, auxiliary", database.Role, database.Name))
		}

		if database.Endpoint != nil {
			if err := database.Endpoint.Validate(database.Engine); err != nil {
				validationErrors = append(validationErrors, fmt.Errorf("database %q endpoint: %w", database.Name, err))
			}
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
	doc, err := ReadLocalStateMap(worktreePath)
	if err != nil {
		return nil, err
	}
	var state LocalState
	if err := DecodeSection(doc, &state); err != nil {
		return nil, fmt.Errorf("parsing local state: %w", err)
	}

	return &state, nil
}

// WriteLocalState merges worktree-local state into .anvil.local. The first
// record for each canonical role remains canonical, while distinct records
// are retained as auxiliary cleanup records. Unknown YAML fields are
// preserved.
func WriteLocalState(worktreePath string, data LocalState) error {
	return UpdateLocalState(worktreePath, func(existing map[string]any) error {
		if data.DbSuffix != "" {
			existing["db_suffix"] = data.DbSuffix
		}

		if data.BorrowedApplication != nil {
			if err := ValidateBorrowedApplication(data.BorrowedApplication); err != nil {
				return err
			}
			existing["borrowed_application"] = data.BorrowedApplication
		}
		existingDatabases, hasExistingDatabases := existing["databases"]
		if hasExistingDatabases || len(data.Databases) > 0 {
			merged, err := mergeOwnedDatabases(existingDatabases, data.Databases)
			if err != nil {
				return err
			}
			existing["databases"] = merged
		}
		return nil
	})
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
		for _, raw := range records {
			record := raw.(map[string]any)
			if record["name"] != update.Name || update.Endpoint == nil {
				continue
			}
			if record["endpoint"] == nil {
				return nil, fmt.Errorf("database %q has unknown legacy endpoint identity and cannot be reattested", update.Name)
			}
			encoded, err := yaml.Marshal(record["endpoint"])
			if err != nil {
				return nil, err
			}
			var endpoint DatabaseEndpoint
			if err := yaml.Unmarshal(encoded, &endpoint); err != nil {
				return nil, err
			}
			if endpoint != *update.Endpoint {
				return nil, fmt.Errorf("database %q endpoint identity cannot be replaced", update.Name)
			}
		}
		var err error
		switch update.Role {
		case DbRoleApplication, DbRoleTesting:
			records, err = mergeCanonicalDatabase(records, update)
		case DbRoleAuxiliary:
			records, err = mergeAuxiliaryDatabase(records, update)
		default:
			records = mergeDatabaseByRole(records, update)
		}
		if err != nil {
			return nil, err
		}
		if err := validateMergedDatabaseNames(records); err != nil {
			return nil, err
		}
	}

	return records, nil
}

func mergeCanonicalDatabase(records []any, update OwnedDatabase) ([]any, error) {
	canonicalIndex := -1
	for index, raw := range records {
		record := raw.(map[string]any)
		role, _ := record["role"].(string)
		if role == update.Role {
			canonicalIndex = index
			break
		}
	}

	if canonicalIndex == -1 {
		for _, raw := range records {
			record := raw.(map[string]any)
			name, _ := record["name"].(string)
			role, _ := record["role"].(string)
			if name == update.Name && role == DbRoleAuxiliary {
				setOwnedDatabaseFields(record, update)
				return records, nil
			}
		}
		return appendOwnedDatabase(records, update), nil
	}

	canonical := records[canonicalIndex].(map[string]any)
	canonicalName, _ := canonical["name"].(string)
	if canonicalName == update.Name {
		next := make([]any, 0, len(records))
		for index, raw := range records {
			record := raw.(map[string]any)
			role, _ := record["role"].(string)
			if role == update.Role {
				if index != canonicalIndex {
					continue
				}
				setOwnedDatabaseFields(record, update)
			}
			next = append(next, record)
		}
		return next, nil
	}

	for _, raw := range records {
		record := raw.(map[string]any)
		name, _ := record["name"].(string)
		role, _ := record["role"].(string)
		if name != update.Name {
			continue
		}
		if role != DbRoleAuxiliary {
			return nil, fmt.Errorf("duplicate database name %q already belongs to role %q", update.Name, role)
		}
		canonicalRole := update.Role
		update.Role = DbRoleAuxiliary
		setOwnedDatabaseFields(record, update)
		return removeDuplicateRole(records, canonicalRole, canonicalIndex), nil
	}

	records = removeDuplicateRole(records, update.Role, canonicalIndex)
	update.Role = DbRoleAuxiliary
	return appendOwnedDatabase(records, update), nil
}

func mergeAuxiliaryDatabase(records []any, update OwnedDatabase) ([]any, error) {
	for _, raw := range records {
		record := raw.(map[string]any)
		name, _ := record["name"].(string)
		if name != update.Name {
			continue
		}
		role, _ := record["role"].(string)
		if role != DbRoleAuxiliary {
			return nil, fmt.Errorf("duplicate database name %q already belongs to role %q", update.Name, role)
		}
		setOwnedDatabaseFields(record, update)
		return records, nil
	}

	return appendOwnedDatabase(records, update), nil
}

func mergeDatabaseByRole(records []any, update OwnedDatabase) []any {
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
		setOwnedDatabaseFields(record, update)
		next = append(next, record)
		updated = true
	}
	if !updated {
		return appendOwnedDatabase(next, update)
	}
	return next
}

func appendOwnedDatabase(records []any, database OwnedDatabase) []any {
	record := make(map[string]any)
	setOwnedDatabaseFields(record, database)
	return append(records, record)
}

func setOwnedDatabaseFields(record map[string]any, database OwnedDatabase) {
	record["name"] = database.Name
	record["engine"] = database.Engine
	record["role"] = database.Role
	if database.Endpoint != nil {
		record["endpoint"] = database.Endpoint
	}
}

func removeDuplicateRole(records []any, role string, keepIndex int) []any {
	next := make([]any, 0, len(records))
	for index, raw := range records {
		record := raw.(map[string]any)
		if recordRole, _ := record["role"].(string); recordRole == role && index != keepIndex {
			continue
		}
		next = append(next, record)
	}
	return next
}

func validateMergedDatabaseNames(records []any) error {
	seen := make(map[string]int, len(records))
	for index, raw := range records {
		record := raw.(map[string]any)
		name, _ := record["name"].(string)
		if firstIndex, exists := seen[name]; exists {
			return fmt.Errorf("duplicate database name %q in merged local state at record %d; first seen in record %d", name, index, firstIndex)
		}
		seen[name] = index
	}
	return nil
}
