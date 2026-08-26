package steps

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/naoray/anvil/internal/config"
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

// DatabaseOptions holds connection parameters
type DatabaseOptions struct {
	Host     string
	Port     string
	Username string
	Password string
}

const (
	mysqlListDatabasesQuery    = "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME LIKE ?"
	postgresListDatabasesQuery = "SELECT datname FROM pg_database WHERE datname LIKE $1 AND datistemplate = false"
)

type databaseRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

type databaseQueryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
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

// DefaultDatabaseClientFactory creates real database clients
func DefaultDatabaseClientFactory(engine string, opts DatabaseOptions) (DatabaseClient, error) {
	switch config.DatabaseEngine(engine) {
	case config.DBEngineMySQL:
		return NewMySQLClient(opts)
	case config.DBEnginePgSQL:
		return NewPostgreSQLClient(opts)
	default:
		return nil, fmt.Errorf("unsupported database engine: %s", engine)
	}
}

// MySQLClient implements DatabaseClient for MySQL
type MySQLClient struct {
	db   *sql.DB
	opts DatabaseOptions
}

// NewMySQLClient creates a new MySQL client
func NewMySQLClient(opts DatabaseOptions) (*MySQLClient, error) {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	if opts.Port == "" {
		opts.Port = "3306"
	}
	if opts.Username == "" {
		opts.Username = "root"
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/", opts.Username, opts.Password, opts.Host, opts.Port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening mysql connection: %w", err)
	}

	return &MySQLClient{db: db, opts: opts}, nil
}

func (c *MySQLClient) Ping() error {
	return c.db.Ping()
}

func (c *MySQLClient) Close() error {
	return c.db.Close()
}

func (c *MySQLClient) CreateDatabase(name string) error {
	query := fmt.Sprintf("CREATE DATABASE `%s`", name)
	_, err := c.db.Exec(query)
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1007 {
			return &DatabaseExistsError{Name: name}
		}
		return fmt.Errorf("creating database %s: %w", name, err)
	}
	return nil
}

func (c *MySQLClient) DropDatabase(name string) error {
	query := fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", name)
	_, err := c.db.Exec(query)
	if err != nil {
		return fmt.Errorf("dropping database %s: %w", name, err)
	}
	return nil
}

func (c *MySQLClient) ListDatabases(pattern string) ([]string, error) {
	return queryDatabaseNames(c.db, mysqlListDatabasesQuery, pattern)
}

// PostgreSQLClient implements DatabaseClient for PostgreSQL
type PostgreSQLClient struct {
	db   *sql.DB
	opts DatabaseOptions
}

// NewPostgreSQLClient creates a new PostgreSQL client
func NewPostgreSQLClient(opts DatabaseOptions) (*PostgreSQLClient, error) {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	if opts.Port == "" {
		opts.Port = "5432"
	}
	if opts.Username == "" {
		opts.Username = "postgres"
	}

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=postgres sslmode=disable",
		opts.Host, opts.Port, opts.Username, opts.Password)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening postgres connection: %w", err)
	}

	return &PostgreSQLClient{db: db, opts: opts}, nil
}

func (c *PostgreSQLClient) Ping() error {
	return c.db.Ping()
}

func (c *PostgreSQLClient) Close() error {
	return c.db.Close()
}

func (c *PostgreSQLClient) CreateDatabase(name string) error {
	var exists bool
	err := c.db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists)
	if err != nil {
		return fmt.Errorf("checking database existence: %w", err)
	}
	if exists {
		return &DatabaseExistsError{Name: name}
	}

	query := fmt.Sprintf("CREATE DATABASE \"%s\"", name)
	_, err = c.db.Exec(query)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return &DatabaseExistsError{Name: name}
		}
		return fmt.Errorf("creating database %s: %w", name, err)
	}
	return nil
}

func (c *PostgreSQLClient) DropDatabase(name string) error {
	query := fmt.Sprintf("DROP DATABASE IF EXISTS \"%s\"", name)
	_, err := c.db.Exec(query)
	if err != nil {
		return fmt.Errorf("dropping database %s: %w", name, err)
	}
	return nil
}

func (c *PostgreSQLClient) ListDatabases(pattern string) ([]string, error) {
	return queryDatabaseNames(c.db, postgresListDatabasesQuery, pattern)
}

func queryDatabaseNames(queryer databaseQueryer, query, pattern string) ([]string, error) {
	rows, err := queryer.Query(query, pattern)
	if err != nil {
		return nil, fmt.Errorf("listing databases: %w", err)
	}
	return collectDatabaseNames(rows)
}

func collectDatabaseNames(rows databaseRows) (databases []string, err error) {
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing database rows: %w", closeErr))
		}
	}()
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			err = errors.Join(err, fmt.Errorf("scanning database name: %w", scanErr))
			break
		}
		databases = append(databases, name)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		err = errors.Join(err, fmt.Errorf("iterating database names: %w", rowsErr))
	}
	sort.Strings(databases)
	return databases, err
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
