package steps

import (
	"fmt"

	"github.com/naoray/anvil/internal/config"
	anvilexec "github.com/naoray/anvil/internal/exec"
	"github.com/naoray/anvil/internal/utils"
)

func normalizeDatabaseHost(host string) string {
	if host == "localhost" || host == "" {
		return "127.0.0.1"
	}
	return host
}

func managedDatabaseFactory(driver config.SiteDriver, commander anvilexec.Commander) DatabaseClientFactory {
	return func(engine string, options DatabaseOptions) (DatabaseClient, error) {
		selected := string(driver)
		if options.Endpoint != nil {
			selected = options.Endpoint.Driver
		}
		if selected == "herd" {
			return NewHerdDatabaseClientFactory(commander)(engine, options)
		}
		return NewYerdDatabaseClientFactory(commander)(engine, options)
	}
}

func verifiedClientEndpoint(client DatabaseClient, engine, path string, expected *config.DatabaseEndpoint) (config.DatabaseEndpoint, error) {
	actual, err := client.Endpoint()
	if err != nil {
		return actual, err
	}
	if err = actual.Validate(engine); err != nil {
		return actual, fmt.Errorf("invalid managed endpoint: %w", err)
	}
	if expected != nil {
		if actual != *expected {
			return actual, fmt.Errorf("managed endpoint differs from recorded database identity")
		}
		return actual, nil
	}
	env := utils.ReadEnvFile(path, ".env")
	if len(env) == 0 {
		env = utils.ReadEnvFile(path, ".env.example")
	}
	if env["DB_SOCKET"] != "" || env["DB_URL"] != "" || env["DATABASE_URL"] != "" {
		return actual, fmt.Errorf("database socket/URL endpoint overrides are unverifiable")
	}
	if actual.Driver == "yerd" {
		if env["DB_HOST"] != "" && normalizeDatabaseHost(env["DB_HOST"]) != actual.Host || env["DB_PORT"] != "" && env["DB_PORT"] != actual.Port {
			return actual, fmt.Errorf("configured endpoint differs from actual Yerd service endpoint")
		}
	}
	return actual, nil
}

func checkRecordedEndpoint(path, name string, endpoint config.DatabaseEndpoint, creating bool) error {
	state, err := config.ReadLocalState(path)
	if err != nil {
		return err
	}
	for _, db := range state.Databases {
		if db.Name == name && db.Endpoint == nil && creating {
			return fmt.Errorf("database %q has unknown legacy endpoint identity; use a distinct new database name", name)
		}
		if db.Name == name && db.Endpoint != nil && *db.Endpoint != endpoint {
			return fmt.Errorf("database %q endpoint differs from recorded ownership identity", name)
		}
	}
	return nil
}
