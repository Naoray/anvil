package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	anvilexec "github.com/naoray/anvil/internal/exec"
)

type yerdDatabaseClient struct {
	service  string
	executor *anvilexec.CommandExecutor
}

// NewYerdDatabaseClientFactory creates database clients backed by Yerd's
// managed SQL services.
func NewYerdDatabaseClientFactory(commander anvilexec.Commander) DatabaseClientFactory {
	return func(engine string, options DatabaseOptions) (DatabaseClient, error) {
		if options.Host != "" || options.Port != "" || options.Username != "" || options.PasswordSet {
			return nil, fmt.Errorf("yerd manages database connections; remove host, port, username, and password overrides")
		}
		service := options.Service
		if service == "" {
			switch engine {
			case "mysql", "mariadb":
				service = engine
			case "pgsql":
				service = "postgres"
			default:
				return nil, fmt.Errorf("unsupported Yerd database engine: %s", engine)
			}
		}
		switch service {
		case "mysql", "mariadb", "postgres":
		default:
			return nil, fmt.Errorf("unsupported Yerd database service: %s", service)
		}
		return &yerdDatabaseClient{
			service:  service,
			executor: anvilexec.NewCommandExecutor(commander),
		}, nil
	}
}

func (c *yerdDatabaseClient) Ping() error {
	output, err := c.executor.RunBinary(context.Background(), "", "yerd", []string{"--json", "services"})
	if err != nil {
		return newManagedServiceError("listing Yerd services: %w\n%s", err, output)
	}

	var response struct {
		Services []struct {
			Service           string   `json:"service"`
			State             string   `json:"state"`
			InstalledVersions []string `json:"installed_versions"`
		} `json:"services"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return newManagedServiceError("parsing Yerd services: %w", err)
	}
	for _, service := range response.Services {
		if service.Service != c.service {
			continue
		}
		if service.State == "running" {
			return nil
		}
		if len(service.InstalledVersions) == 0 {
			return newManagedServiceError("Yerd service %s is not installed", c.service)
		}
		if service.State != "stopped" {
			return newManagedServiceError("Yerd service %s is %s and cannot be started", c.service, service.State)
		}

		startOutput, startErr := c.executor.RunBinary(
			context.Background(),
			"",
			"yerd",
			[]string{"--json", "service", "start", c.service},
		)
		if startErr != nil {
			return newManagedServiceError("starting Yerd service %s: %w\n%s", c.service, startErr, startOutput)
		}
		return nil
	}
	return newManagedServiceError("Yerd service %s is not installed", c.service)
}

func (c *yerdDatabaseClient) CreateDatabase(name string) error {
	output, err := c.executor.RunBinary(
		context.Background(),
		"",
		"yerd",
		[]string{"--json", "db", "create", c.service, name},
	)
	if err != nil {
		if IsDatabaseExistsError(fmt.Errorf("%s", output)) {
			return &DatabaseExistsError{Name: name}
		}
		return fmt.Errorf("creating database %s with Yerd: %w\n%s", name, err, output)
	}
	return nil
}

func (c *yerdDatabaseClient) DropDatabase(name string) error {
	output, err := c.executor.RunBinary(
		context.Background(),
		"",
		"yerd",
		[]string{"--json", "db", "drop", c.service, name},
	)
	if err != nil {
		if isDatabaseMissingOutput(output) {
			return nil
		}
		return fmt.Errorf("dropping database %s with Yerd: %w\n%s", name, err, output)
	}
	return nil
}

func (c *yerdDatabaseClient) ListDatabases(pattern string) ([]string, error) {
	output, err := c.executor.RunBinary(
		context.Background(),
		"",
		"yerd",
		[]string{"--json", "db", "list", c.service},
	)
	if err != nil {
		return nil, fmt.Errorf("listing %s databases with Yerd: %w\n%s", c.service, err, output)
	}

	var response struct {
		Databases []struct {
			Name string `json:"name"`
		} `json:"databases"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("parsing Yerd databases: %w", err)
	}

	matcher, err := compileSQLLikePattern(pattern)
	if err != nil {
		return nil, err
	}
	databases := make([]string, 0, len(response.Databases))
	for _, database := range response.Databases {
		if matcher.MatchString(database.Name) {
			databases = append(databases, database.Name)
		}
	}
	sort.Strings(databases)
	return databases, nil
}

func (c *yerdDatabaseClient) Close() error {
	return nil
}

func isDatabaseMissingOutput(output []byte) bool {
	message := strings.ToLower(string(output))
	return strings.Contains(message, "does not exist") ||
		strings.Contains(message, "not found") ||
		strings.Contains(message, "unknown database")
}

func compileSQLLikePattern(pattern string) (*regexp.Regexp, error) {
	var expression strings.Builder
	expression.WriteByte('^')
	for index := 0; index < len(pattern); index++ {
		character := pattern[index]
		switch character {
		case '\\':
			if index+1 < len(pattern) {
				index++
				expression.WriteString(regexp.QuoteMeta(string(pattern[index])))
				continue
			}
			expression.WriteString(`\\`)
		case '%':
			expression.WriteString(".*")
		case '_':
			expression.WriteByte('.')
		default:
			expression.WriteString(regexp.QuoteMeta(string(character)))
		}
	}
	expression.WriteByte('$')

	matcher, err := regexp.Compile(expression.String())
	if err != nil {
		return nil, fmt.Errorf("compiling database pattern %q: %w", pattern, err)
	}
	return matcher, nil
}
