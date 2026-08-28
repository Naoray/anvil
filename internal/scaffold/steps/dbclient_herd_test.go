package steps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
)

type herdEnvironmentCommandCall struct {
	yerdCommandCall
	environment map[string]string
}

type herdEnvironmentCommander struct {
	calls []herdEnvironmentCommandCall
}

func (c *herdEnvironmentCommander) Run(_ context.Context, dir, command string, args ...string) ([]byte, error) {
	c.calls = append(c.calls, herdEnvironmentCommandCall{yerdCommandCall: yerdCommandCall{
		dir:     dir,
		command: command,
		args:    append([]string(nil), args...),
	}})
	return nil, nil
}

func (c *herdEnvironmentCommander) RunWithEnv(
	_ context.Context,
	dir string,
	environment map[string]string,
	command string,
	args ...string,
) ([]byte, error) {
	c.calls = append(c.calls, herdEnvironmentCommandCall{
		yerdCommandCall: yerdCommandCall{
			dir:     dir,
			command: command,
			args:    append([]string(nil), args...),
		},
		environment: environment,
	})
	return nil, nil
}

func TestRegistryHerdSiteDriverCreatesDatabaseThroughManagedService(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(
		"DB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_PORT=3307\nDB_USERNAME=root\nDB_PASSWORD=\n",
	), 0o644); err != nil {
		t.Fatalf("writing environment: %v", err)
	}
	commander := &yerdScriptedCommander{responses: []yerdCommandResponse{
		{output: []byte("MySQL started")},
		{output: []byte{}},
	}}
	registry := NewRegistry()
	registry.RegisterDefaultsForSiteDriver(config.SiteDriverHerd, commander)
	step, err := registry.Create(config.StepDbCreate, config.StepConfig{Type: "mysql"})
	if err != nil {
		t.Fatalf("creating db.create step: %v", err)
	}
	ctx := &types.ScaffoldContext{WorktreePath: dir, SiteName: "app"}
	ctx.SetDbSuffix("swift_runner")

	if err := step.Run(ctx, types.StepOptions{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []yerdCommandCall{
		{command: "herd", args: []string{"services:start", "mysql"}},
		{
			command: "mysql",
			args: []string{
				"--host=127.0.0.1",
				"--port=3307",
				"--user=root",
				"--batch",
				"--skip-column-names",
				"--execute",
				"CREATE DATABASE `app_swift_runner`",
			},
		},
	}
	if !slices.EqualFunc(commander.calls, want, func(got, want yerdCommandCall) bool {
		return got.dir == want.dir && got.command == want.command && slices.Equal(got.args, want.args)
	}) {
		t.Fatalf("Herd calls = %#v, want %#v", commander.calls, want)
	}
}

func TestRegistryHerdSiteDriverDropsOwnedDatabaseFamiliesThroughManagedService(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(
		"DB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_PORT=3307\nDB_USERNAME=root\nDB_PASSWORD=\n",
	), 0o644); err != nil {
		t.Fatalf("writing environment: %v", err)
	}
	if err := config.WriteLocalState(dir, config.LocalState{Databases: []config.OwnedDatabase{
		{Name: "app_swift_runner", Engine: "mysql", Role: config.DbRoleApplication},
		{Name: "app_swift_runner_test", Engine: "mysql", Role: config.DbRoleTesting},
	}}); err != nil {
		t.Fatalf("writing local state: %v", err)
	}
	listing := []byte("app_swift_runner\napp_swift_runner_test\napp_swift_runner_test_1\nother\n")
	commander := &yerdScriptedCommander{responses: []yerdCommandResponse{
		{output: []byte("MySQL started")},
		{output: listing},
		{output: listing},
		{},
		{},
		{},
	}}
	registry := NewRegistry()
	registry.RegisterDefaultsForSiteDriver(config.SiteDriverHerd, commander)
	step, err := registry.Create(config.StepDbDestroy, config.StepConfig{})
	if err != nil {
		t.Fatalf("creating db.destroy step: %v", err)
	}

	if err := step.Run(&types.ScaffoldContext{WorktreePath: dir}, types.StepOptions{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	connectionArgs := []string{"--host=127.0.0.1", "--port=3307", "--user=root"}
	wantList := append(append([]string(nil), connectionArgs...),
		"--batch", "--skip-column-names", "--execute", "SHOW DATABASES",
	)
	wantDrops := []string{
		"app_swift_runner",
		"app_swift_runner_test",
		"app_swift_runner_test_1",
	}
	if len(commander.calls) != 6 {
		t.Fatalf("Herd calls = %#v, want six lifecycle calls", commander.calls)
	}
	if got := commander.calls[0]; got.command != "herd" || !slices.Equal(got.args, []string{"services:start", "mysql"}) {
		t.Fatalf("service call = %#v", got)
	}
	for index := 1; index <= 2; index++ {
		if got := commander.calls[index]; got.command != "mysql" || !slices.Equal(got.args, wantList) {
			t.Fatalf("list call %d = %#v, want args %v", index, got, wantList)
		}
	}
	for index, name := range wantDrops {
		wantArgs := append(append([]string(nil), connectionArgs...),
			"--batch", "--skip-column-names", "--execute", "DROP DATABASE IF EXISTS `"+name+"`",
		)
		if got := commander.calls[index+3]; got.command != "mysql" || !slices.Equal(got.args, wantArgs) {
			t.Fatalf("drop call %d = %#v, want args %v", index, got, wantArgs)
		}
	}
}

func TestRegistryHerdSiteDriverCreatesPostgreSQLDatabaseThroughManagedService(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(
		"DB_CONNECTION=pgsql\nDB_HOST=127.0.0.1\nDB_PORT=5433\nDB_USERNAME=root\nDB_PASSWORD=\n",
	), 0o644); err != nil {
		t.Fatalf("writing environment: %v", err)
	}
	commander := &yerdScriptedCommander{responses: []yerdCommandResponse{
		{output: []byte("PostgreSQL started")},
		{},
	}}
	registry := NewRegistry()
	registry.RegisterDefaultsForSiteDriver(config.SiteDriverHerd, commander)
	step, err := registry.Create(config.StepDbCreate, config.StepConfig{})
	if err != nil {
		t.Fatalf("creating db.create step: %v", err)
	}
	ctx := &types.ScaffoldContext{WorktreePath: dir, SiteName: "app"}
	ctx.SetDbSuffix("swift_runner")

	if err := step.Run(ctx, types.StepOptions{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []yerdCommandCall{
		{command: "herd", args: []string{"services:start", "postgresql"}},
		{
			command: "createdb",
			args: []string{
				"--host=127.0.0.1",
				"--port=5433",
				"--username=root",
				"app_swift_runner",
			},
		},
	}
	if !slices.EqualFunc(commander.calls, want, func(got, want yerdCommandCall) bool {
		return got.dir == want.dir && got.command == want.command && slices.Equal(got.args, want.args)
	}) {
		t.Fatalf("Herd calls = %#v, want %#v", commander.calls, want)
	}
}

func TestRegistryHerdSiteDriverDropsPostgreSQLDatabaseFamiliesThroughManagedService(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(
		"DB_CONNECTION=pgsql\nDB_HOST=127.0.0.1\nDB_PORT=5433\nDB_USERNAME=root\nDB_PASSWORD=\n",
	), 0o644); err != nil {
		t.Fatalf("writing environment: %v", err)
	}
	if err := config.WriteLocalState(dir, config.LocalState{Databases: []config.OwnedDatabase{
		{Name: "app_swift_runner", Engine: "pgsql", Role: config.DbRoleApplication},
		{Name: "app_swift_runner_test", Engine: "pgsql", Role: config.DbRoleTesting},
	}}); err != nil {
		t.Fatalf("writing local state: %v", err)
	}
	listing := []byte("app_swift_runner\napp_swift_runner_test\napp_swift_runner_test_1\nother\n")
	commander := &yerdScriptedCommander{responses: []yerdCommandResponse{
		{output: []byte("PostgreSQL started")},
		{output: listing},
		{output: listing},
		{},
		{},
		{},
	}}
	registry := NewRegistry()
	registry.RegisterDefaultsForSiteDriver(config.SiteDriverHerd, commander)
	step, err := registry.Create(config.StepDbDestroy, config.StepConfig{})
	if err != nil {
		t.Fatalf("creating db.destroy step: %v", err)
	}

	if err := step.Run(&types.ScaffoldContext{WorktreePath: dir}, types.StepOptions{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	connectionArgs := []string{"--host=127.0.0.1", "--port=5433", "--username=root"}
	wantList := append(append([]string(nil), connectionArgs...),
		"--dbname=postgres",
		"--tuples-only",
		"--no-align",
		"--command",
		"SELECT datname FROM pg_database WHERE datistemplate = false",
	)
	wantDrops := []string{
		"app_swift_runner",
		"app_swift_runner_test",
		"app_swift_runner_test_1",
	}
	if len(commander.calls) != 6 {
		t.Fatalf("Herd calls = %#v, want six lifecycle calls", commander.calls)
	}
	for index := 1; index <= 2; index++ {
		if got := commander.calls[index]; got.command != "psql" || !slices.Equal(got.args, wantList) {
			t.Fatalf("list call %d = %#v, want args %v", index, got, wantList)
		}
	}
	for index, name := range wantDrops {
		wantArgs := append(append([]string(nil), connectionArgs...), "--if-exists", name)
		if got := commander.calls[index+3]; got.command != "dropdb" || !slices.Equal(got.args, wantArgs) {
			t.Fatalf("drop call %d = %#v, want args %v", index, got, wantArgs)
		}
	}
}

func TestRegistryHerdSiteDriverKeepsDatabasePasswordOutOfProcessArguments(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(
		"DB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_PORT=3307\nDB_USERNAME=root\nDB_PASSWORD=secret-value\n",
	), 0o644); err != nil {
		t.Fatalf("writing environment: %v", err)
	}
	commander := &herdEnvironmentCommander{}
	registry := NewRegistry()
	registry.RegisterDefaultsForSiteDriver(config.SiteDriverHerd, commander)
	step, err := registry.Create(config.StepDbCreate, config.StepConfig{Type: "mysql"})
	if err != nil {
		t.Fatalf("creating db.create step: %v", err)
	}
	ctx := &types.ScaffoldContext{WorktreePath: dir, SiteName: "app"}
	ctx.SetDbSuffix("swift_runner")

	if err := step.Run(ctx, types.StepOptions{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(commander.calls) != 2 {
		t.Fatalf("Herd calls = %#v, want service and database calls", commander.calls)
	}
	databaseCall := commander.calls[1]
	if got := databaseCall.environment["MYSQL_PWD"]; got != "secret-value" {
		t.Fatalf("MYSQL_PWD = %q, want password from .env", got)
	}
	for _, arg := range databaseCall.args {
		if arg == "secret-value" || arg == "--password=secret-value" {
			t.Fatalf("database password leaked into process arguments: %v", databaseCall.args)
		}
	}
}

func TestRegistryHerdSiteDriverFailsWhenManagedServiceCannotStart(t *testing.T) {
	commander := &yerdRecordingCommander{
		output: []byte("MySQL service is not installed"),
		err:    errors.New("exit status 1"),
	}
	registry := NewRegistry()
	registry.RegisterDefaultsForSiteDriver(config.SiteDriverHerd, commander)
	step, err := registry.Create(config.StepDbCreate, config.StepConfig{Type: "mysql"})
	if err != nil {
		t.Fatalf("creating db.create step: %v", err)
	}
	ctx := &types.ScaffoldContext{WorktreePath: t.TempDir(), SiteName: "app"}
	ctx.SetDbSuffix("swift_runner")

	err = step.Run(ctx, types.StepOptions{})
	if err == nil || !strings.Contains(err.Error(), "preparing managed database service") {
		t.Fatalf("Run() error = %v, want managed-service failure", err)
	}
	if !strings.Contains(err.Error(), "MySQL service is not installed") {
		t.Fatalf("Run() error = %v, want Herd output", err)
	}
}

func TestRegistryHerdSiteDriverHonorsExplicitEmptyPassword(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(
		"DB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_PORT=3307\nDB_USERNAME=root\nDB_PASSWORD=secret-value\n",
	), 0o644); err != nil {
		t.Fatalf("writing environment: %v", err)
	}
	commander := &herdEnvironmentCommander{}
	registry := NewRegistry()
	registry.RegisterDefaultsForSiteDriver(config.SiteDriverHerd, commander)
	step, err := registry.Create(config.StepDbCreate, config.StepConfig{
		Type: "mysql",
		Args: []string{"--password="},
	})
	if err != nil {
		t.Fatalf("creating db.create step: %v", err)
	}
	ctx := &types.ScaffoldContext{WorktreePath: dir, SiteName: "app"}
	ctx.SetDbSuffix("swift_runner")

	if err := step.Run(ctx, types.StepOptions{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(commander.calls) != 2 {
		t.Fatalf("Herd calls = %#v, want service and database calls", commander.calls)
	}
	if commander.calls[1].environment != nil {
		t.Fatalf("database environment = %#v, want no inherited DB_PASSWORD", commander.calls[1].environment)
	}
}
