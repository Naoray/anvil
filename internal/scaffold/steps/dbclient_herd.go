package steps

import (
	"context"
	"fmt"
	"sort"
	"strings"

	anvilexec "github.com/naoray/anvil/internal/exec"
	"github.com/naoray/anvil/internal/utils"
)

type herdDatabaseClient struct {
	service  string
	options  DatabaseOptions
	executor *anvilexec.CommandExecutor
}

// NewHerdDatabaseClientFactory creates database clients backed by Herd-managed
// services and the database binaries that Herd provides.
func NewHerdDatabaseClientFactory(commander anvilexec.Commander) DatabaseClientFactory {
	return func(engine string, options DatabaseOptions) (DatabaseClient, error) {
		service := options.Service
		if service == "" {
			switch engine {
			case "mysql", "mariadb":
				service = engine
			case "pgsql":
				service = "postgresql"
			default:
				return nil, fmt.Errorf("unsupported Herd database engine: %s", engine)
			}
		}
		env := utils.ReadEnvFile(options.WorktreePath, ".env")
		if options.Host == "" {
			options.Host = env["DB_HOST"]
		}
		if options.Port == "" {
			options.Port = env["DB_PORT"]
		}
		if options.Username == "" {
			options.Username = env["DB_USERNAME"]
		}
		if !options.PasswordSet {
			options.Password = env["DB_PASSWORD"]
		}
		if options.Host == "" {
			options.Host = "127.0.0.1"
		}
		if options.Username == "" {
			options.Username = "root"
		}
		return &herdDatabaseClient{
			service:  service,
			options:  options,
			executor: anvilexec.NewCommandExecutor(commander),
		}, nil
	}
}

func (c *herdDatabaseClient) Ping() error {
	output, err := c.executor.RunBinary(
		context.Background(),
		"",
		"herd",
		[]string{"services:start", c.service},
	)
	if err != nil {
		return newManagedServiceError("starting Herd service %s: %w\n%s", c.service, err, output)
	}
	return nil
}

func (c *herdDatabaseClient) CreateDatabase(name string) error {
	if c.service == "postgresql" {
		output, err := c.runDatabaseBinary(
			context.Background(),
			"createdb",
			append(c.postgreSQLConnectionArgs(), name),
		)
		if err != nil {
			if IsDatabaseExistsError(fmt.Errorf("%s", output)) {
				return &DatabaseExistsError{Name: name}
			}
			return fmt.Errorf("creating database %s with Herd service %s: %w\n%s", name, c.service, err, output)
		}
		return nil
	}
	if c.service != "mysql" && c.service != "mariadb" {
		return fmt.Errorf("unsupported Herd database service: %s", c.service)
	}
	output, err := c.runDatabaseBinary(
		context.Background(),
		c.service,
		append(c.mysqlConnectionArgs(),
			"--batch",
			"--skip-column-names",
			"--execute",
			fmt.Sprintf("CREATE DATABASE `%s`", escapeMySQLIdentifier(name)),
		),
	)
	if err != nil {
		if IsDatabaseExistsError(fmt.Errorf("%s", output)) {
			return &DatabaseExistsError{Name: name}
		}
		return fmt.Errorf("creating database %s with Herd service %s: %w\n%s", name, c.service, err, output)
	}
	return nil
}

func (c *herdDatabaseClient) mysqlConnectionArgs() []string {
	args := []string{
		"--host=" + c.options.Host,
	}
	if c.options.Port != "" {
		args = append(args, "--port="+c.options.Port)
	}
	args = append(args, "--user="+c.options.Username)
	return args
}

func (c *herdDatabaseClient) postgreSQLConnectionArgs() []string {
	args := []string{
		"--host=" + c.options.Host,
	}
	if c.options.Port != "" {
		args = append(args, "--port="+c.options.Port)
	}
	args = append(args, "--username="+c.options.Username)
	return args
}

func (c *herdDatabaseClient) DropDatabase(name string) error {
	if c.service == "postgresql" {
		output, err := c.runDatabaseBinary(
			context.Background(),
			"dropdb",
			append(c.postgreSQLConnectionArgs(), "--if-exists", name),
		)
		if err != nil {
			return fmt.Errorf("dropping database %s with Herd service %s: %w\n%s", name, c.service, err, output)
		}
		return nil
	}
	if c.service != "mysql" && c.service != "mariadb" {
		return fmt.Errorf("unsupported Herd database service: %s", c.service)
	}
	output, err := c.runDatabaseBinary(
		context.Background(),
		c.service,
		append(c.mysqlConnectionArgs(),
			"--batch",
			"--skip-column-names",
			"--execute",
			fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", escapeMySQLIdentifier(name)),
		),
	)
	if err != nil {
		return fmt.Errorf("dropping database %s with Herd service %s: %w\n%s", name, c.service, err, output)
	}
	return nil
}

func (c *herdDatabaseClient) ListDatabases(pattern string) ([]string, error) {
	if c.service == "postgresql" {
		output, err := c.runDatabaseBinary(
			context.Background(),
			"psql",
			append(c.postgreSQLConnectionArgs(),
				"--dbname=postgres",
				"--tuples-only",
				"--no-align",
				"--command",
				"SELECT datname FROM pg_database WHERE datistemplate = false",
			),
		)
		if err != nil {
			return nil, fmt.Errorf("listing databases with Herd service %s: %w\n%s", c.service, err, output)
		}
		matcher, err := compileSQLLikePattern(pattern)
		if err != nil {
			return nil, err
		}
		databases := make([]string, 0)
		for _, line := range strings.Split(string(output), "\n") {
			name := strings.TrimSpace(line)
			if name != "" && matcher.MatchString(name) {
				databases = append(databases, name)
			}
		}
		sort.Strings(databases)
		return databases, nil
	}
	if c.service != "mysql" && c.service != "mariadb" {
		return nil, fmt.Errorf("unsupported Herd database service: %s", c.service)
	}
	output, err := c.runDatabaseBinary(
		context.Background(),
		c.service,
		append(c.mysqlConnectionArgs(),
			"--batch",
			"--skip-column-names",
			"--execute",
			"SHOW DATABASES",
		),
	)
	if err != nil {
		return nil, fmt.Errorf("listing databases with Herd service %s: %w\n%s", c.service, err, output)
	}
	matcher, err := compileSQLLikePattern(pattern)
	if err != nil {
		return nil, err
	}
	databases := make([]string, 0)
	for _, line := range strings.Split(string(output), "\n") {
		name := strings.TrimSpace(line)
		if name != "" && matcher.MatchString(name) {
			databases = append(databases, name)
		}
	}
	sort.Strings(databases)
	return databases, nil
}

func (c *herdDatabaseClient) Close() error {
	return nil
}

func (c *herdDatabaseClient) runDatabaseBinary(
	ctx context.Context,
	binary string,
	args []string,
) ([]byte, error) {
	environment := make(map[string]string)
	if c.options.Password != "" {
		if c.service == "postgresql" {
			environment["PGPASSWORD"] = c.options.Password
		} else {
			environment["MYSQL_PWD"] = c.options.Password
		}
	}
	return c.executor.RunBinaryWithEnv(ctx, "", environment, binary, args)
}

func escapeMySQLIdentifier(identifier string) string {
	return strings.ReplaceAll(identifier, "`", "``")
}
