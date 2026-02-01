package steps

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
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

// DefaultDatabaseClientFactory creates real database clients
func DefaultDatabaseClientFactory(engine string, opts DatabaseOptions) (DatabaseClient, error) {
	switch engine {
	case "mysql":
		return NewMySQLClient(opts)
	case "pgsql":
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
	query := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", name)
	_, err := c.db.Exec(query)
	if err != nil {
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
	query := fmt.Sprintf("SHOW DATABASES LIKE '%s'", pattern)
	rows, err := c.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("listing databases: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var databases []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning database name: %w", err)
		}
		databases = append(databases, name)
	}
	return databases, rows.Err()
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
	query := "SELECT datname FROM pg_database WHERE datname LIKE $1 AND datistemplate = false"
	rows, err := c.db.Query(query, pattern)
	if err != nil {
		return nil, fmt.Errorf("listing databases: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var databases []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning database name: %w", err)
		}
		databases = append(databases, name)
	}
	return databases, rows.Err()
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
