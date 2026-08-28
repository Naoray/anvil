package steps

import (
	"fmt"
	"strings"
)

// DatabaseClient abstracts database operations for testability
type DatabaseClient interface {
	CreateDatabase(name string) error
	DropDatabase(name string) error
	ListDatabases(pattern string) ([]string, error)
	Ping() error
	Close() error
}

// DatabaseClientFactory creates DatabaseClient instances
type DatabaseClientFactory func(engine string, opts DatabaseOptions) (DatabaseClient, error)

type managedServiceError struct {
	err error
}

func (e *managedServiceError) Error() string {
	return e.err.Error()
}

func (e *managedServiceError) Unwrap() error {
	return e.err
}

func newManagedServiceError(format string, args ...any) error {
	return &managedServiceError{err: fmt.Errorf(format, args...)}
}

// DatabaseOptions holds connection parameters
type DatabaseOptions struct {
	Host         string
	Port         string
	Username     string
	Password     string
	PasswordSet  bool
	Service      string
	WorktreePath string
}

// EscapeLikePattern escapes literal characters that SQL LIKE treats as
// metacharacters. Callers add only the wildcards they intentionally need.
func EscapeLikePattern(pattern string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		"%", `\%`,
		"_", `\_`,
	)
	return replacer.Replace(pattern)
}

// DatabaseExistsError indicates a database already exists
type DatabaseExistsError struct {
	Name string
}

func (e *DatabaseExistsError) Error() string {
	return fmt.Sprintf("database %s already exists", e.Name)
}

// IsDatabaseExistsError checks if an error indicates a database already exists
func IsDatabaseExistsError(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*DatabaseExistsError); ok {
		return true
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "already exists") ||
		strings.Contains(errStr, "database exists") ||
		strings.Contains(errStr, "1007")
}
