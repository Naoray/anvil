package steps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
)

type yerdCommandCall struct {
	dir     string
	command string
	args    []string
}

type yerdRecordingCommander struct {
	output []byte
	err    error
	calls  []yerdCommandCall
}

type yerdCommandResponse struct {
	output []byte
	err    error
}

type yerdScriptedCommander struct {
	responses []yerdCommandResponse
	calls     []yerdCommandCall
}

func (c *yerdScriptedCommander) Run(_ context.Context, dir, command string, args ...string) ([]byte, error) {
	c.calls = append(c.calls, yerdCommandCall{
		dir:     dir,
		command: command,
		args:    append([]string(nil), args...),
	})
	if len(c.calls) > len(c.responses) {
		return nil, fmt.Errorf("unexpected Yerd command: %s %v", command, args)
	}
	response := c.responses[len(c.calls)-1]
	return response.output, response.err
}

func (c *yerdRecordingCommander) Run(_ context.Context, dir, command string, args ...string) ([]byte, error) {
	c.calls = append(c.calls, yerdCommandCall{
		dir:     dir,
		command: command,
		args:    append([]string(nil), args...),
	})
	return c.output, c.err
}

func TestYerdDatabaseClientPingUsesRunningManagedService(t *testing.T) {
	commander := &yerdRecordingCommander{output: []byte(`{
		"type": "services",
		"services": [{"service": "mysql", "state": "running"}]
	}`)}
	factory := NewYerdDatabaseClientFactory(commander)
	client, err := factory("mysql", DatabaseOptions{})
	if err != nil {
		t.Fatalf("creating Yerd database client: %v", err)
	}

	if err := client.Ping(); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}

	want := []yerdCommandCall{{command: "yerd", args: []string{"--json", "services"}}}
	if !slices.EqualFunc(commander.calls, want, func(got, want yerdCommandCall) bool {
		return got.dir == want.dir && got.command == want.command && slices.Equal(got.args, want.args)
	}) {
		t.Fatalf("Yerd calls = %#v, want %#v", commander.calls, want)
	}
}

func TestYerdDatabaseClientPingStartsInstalledStoppedService(t *testing.T) {
	commander := &yerdScriptedCommander{responses: []yerdCommandResponse{
		{output: []byte(`{
			"services":[{
				"service":"mysql",
				"state":"stopped",
				"installed_versions":["8.4.9"]
			}]
		}`)},
		{output: []byte(`{"service":"mysql","state":"running"}`)},
	}}
	factory := NewYerdDatabaseClientFactory(commander)
	client, err := factory("mysql", DatabaseOptions{})
	if err != nil {
		t.Fatalf("creating Yerd database client: %v", err)
	}

	if err := client.Ping(); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}

	want := []string{"--json", "service", "start", "mysql"}
	if len(commander.calls) != 2 || !slices.Equal(commander.calls[1].args, want) {
		t.Fatalf("Yerd calls = %#v, want service start args %v", commander.calls, want)
	}
}

func TestYerdDatabaseClientMapsApplicationEnginesToManagedServices(t *testing.T) {
	tests := []struct {
		engine  string
		service string
	}{
		{engine: "mysql", service: "mysql"},
		{engine: "mariadb", service: "mariadb"},
		{engine: "pgsql", service: "postgres"},
	}

	for _, tt := range tests {
		t.Run(tt.engine, func(t *testing.T) {
			commander := &yerdRecordingCommander{output: []byte(fmt.Sprintf(
				`{"services":[{"service":%q,"state":"running"}]}`,
				tt.service,
			))}
			factory := NewYerdDatabaseClientFactory(commander)
			client, err := factory(tt.engine, DatabaseOptions{})
			if err != nil {
				t.Fatalf("creating Yerd database client: %v", err)
			}

			if err := client.Ping(); err != nil {
				t.Fatalf("Ping() error = %v", err)
			}
		})
	}
}

func TestYerdDatabaseClientCreatesDatabaseThroughManagedService(t *testing.T) {
	commander := &yerdRecordingCommander{output: []byte(`{"name":"app_test"}`)}
	factory := NewYerdDatabaseClientFactory(commander)
	client, err := factory("mariadb", DatabaseOptions{})
	if err != nil {
		t.Fatalf("creating Yerd database client: %v", err)
	}

	if err := client.CreateDatabase("app_test"); err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}

	want := []yerdCommandCall{{
		command: "yerd",
		args:    []string{"--json", "db", "create", "mariadb", "app_test"},
	}}
	if !slices.EqualFunc(commander.calls, want, func(got, want yerdCommandCall) bool {
		return got.dir == want.dir && got.command == want.command && slices.Equal(got.args, want.args)
	}) {
		t.Fatalf("Yerd calls = %#v, want %#v", commander.calls, want)
	}
}

func TestYerdDatabaseClientPreservesCollisionSignal(t *testing.T) {
	commander := &yerdRecordingCommander{
		output: []byte(`{"error":"database app_test already exists"}`),
		err:    errors.New("exit status 1"),
	}
	factory := NewYerdDatabaseClientFactory(commander)
	client, err := factory("mysql", DatabaseOptions{})
	if err != nil {
		t.Fatalf("creating Yerd database client: %v", err)
	}

	err = client.CreateDatabase("app_test")
	if !IsDatabaseExistsError(err) {
		t.Fatalf("CreateDatabase() error = %v, want database-exists signal", err)
	}
}

func TestYerdDatabaseClientRejectsDirectConnectionOverrides(t *testing.T) {
	factory := NewYerdDatabaseClientFactory(&yerdRecordingCommander{})
	_, err := factory("mysql", DatabaseOptions{Host: "db.example.test"})
	if err == nil || !strings.Contains(err.Error(), "yerd manages database connections") {
		t.Fatalf("factory error = %v, want Yerd-managed connection guidance", err)
	}
}

func TestYerdDatabaseClientDropsDatabaseThroughManagedService(t *testing.T) {
	commander := &yerdRecordingCommander{output: []byte(`{"dropped":"app_test"}`)}
	factory := NewYerdDatabaseClientFactory(commander)
	client, err := factory("pgsql", DatabaseOptions{})
	if err != nil {
		t.Fatalf("creating Yerd database client: %v", err)
	}

	if err := client.DropDatabase("app_test"); err != nil {
		t.Fatalf("DropDatabase() error = %v", err)
	}

	want := []yerdCommandCall{{
		command: "yerd",
		args:    []string{"--json", "db", "drop", "postgres", "app_test"},
	}}
	if !slices.EqualFunc(commander.calls, want, func(got, want yerdCommandCall) bool {
		return got.dir == want.dir && got.command == want.command && slices.Equal(got.args, want.args)
	}) {
		t.Fatalf("Yerd calls = %#v, want %#v", commander.calls, want)
	}
}

func TestYerdDatabaseClientDropRemainsIdempotent(t *testing.T) {
	commander := &yerdRecordingCommander{
		output: []byte(`{"error":"database app_test does not exist"}`),
		err:    errors.New("exit status 1"),
	}
	factory := NewYerdDatabaseClientFactory(commander)
	client, err := factory("mysql", DatabaseOptions{})
	if err != nil {
		t.Fatalf("creating Yerd database client: %v", err)
	}

	if err := client.DropDatabase("app_test"); err != nil {
		t.Fatalf("DropDatabase() missing database error = %v, want nil", err)
	}
}

func TestYerdDatabaseClientListsOnlyDatabasesMatchingCleanupPattern(t *testing.T) {
	commander := &yerdRecordingCommander{output: []byte(`{
		"type": "databases",
		"databases": [
			{"name": "app_test_2"},
			{"name": "other_test_1"},
			{"name": "app_test"},
			{"name": "app_test_1"}
		]
	}`)}
	factory := NewYerdDatabaseClientFactory(commander)
	client, err := factory("mysql", DatabaseOptions{})
	if err != nil {
		t.Fatalf("creating Yerd database client: %v", err)
	}

	got, err := client.ListDatabases(`app\_test\_%`)
	if err != nil {
		t.Fatalf("ListDatabases() error = %v", err)
	}
	if want := []string{"app_test_1", "app_test_2"}; !slices.Equal(got, want) {
		t.Fatalf("ListDatabases() = %v, want %v", got, want)
	}
}

func TestDbCreateStepUsesYerdManagedConnectionWithoutDirectDefaults(t *testing.T) {
	dir := t.TempDir()
	commander := &yerdScriptedCommander{responses: []yerdCommandResponse{
		{output: []byte(`{"services":[{"service":"mysql","state":"running"}]}`)},
		{output: []byte(`{"name":"app_swift_runner"}`)},
	}}
	step := NewDbCreateStepWithFactory(
		config.StepConfig{Type: "mysql"},
		NewYerdDatabaseClientFactory(commander),
	)
	ctx := &types.ScaffoldContext{WorktreePath: dir, SiteName: "app"}
	ctx.SetDbSuffix("swift_runner")

	if err := step.Run(ctx, types.StepOptions{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	state, err := config.ReadLocalState(filepath.Clean(dir))
	if err != nil {
		t.Fatalf("reading local state: %v", err)
	}
	want := []config.OwnedDatabase{{Name: "app_swift_runner", Engine: "mysql", Role: config.DbRoleApplication}}
	if !slices.Equal(state.Databases, want) {
		t.Fatalf("owned databases = %#v, want %#v", state.Databases, want)
	}
}

func TestDbCreateStepFailsWhenYerdServiceIsNotInstalled(t *testing.T) {
	commander := &yerdRecordingCommander{output: []byte(`{
		"services":[{
			"service":"mysql",
			"state":"stopped",
			"installed_versions":[]
		}]
	}`)}
	step := NewDbCreateStepWithFactory(
		config.StepConfig{Type: "mysql"},
		NewYerdDatabaseClientFactory(commander),
	)
	ctx := &types.ScaffoldContext{WorktreePath: t.TempDir(), SiteName: "app"}
	ctx.SetDbSuffix("swift_runner")

	err := step.Run(ctx, types.StepOptions{})
	if err == nil || !strings.Contains(err.Error(), "Yerd service mysql is not installed") {
		t.Fatalf("Run() error = %v, want missing Yerd service failure", err)
	}
}

func TestDbCreateStepUsesMariaDBServiceWithoutInvalidatingLegacyOwnership(t *testing.T) {
	dir := t.TempDir()
	if err := config.WriteLocalState(dir, config.LocalState{
		DbSuffix: "swift_runner",
		Databases: []config.OwnedDatabase{{
			Name: "app_swift_runner", Engine: "mysql", Role: config.DbRoleApplication,
		}},
	}); err != nil {
		t.Fatalf("writing legacy local state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DB_CONNECTION=mariadb\n"), 0o644); err != nil {
		t.Fatalf("writing environment: %v", err)
	}
	commander := &yerdScriptedCommander{responses: []yerdCommandResponse{
		{output: []byte(`{"services":[{"service":"mariadb","state":"running"}]}`)},
		{
			output: []byte(`{"error":"database app_swift_runner already exists"}`),
			err:    errors.New("exit status 1"),
		},
	}}
	step := NewDbCreateStepWithFactory(
		config.StepConfig{},
		NewYerdDatabaseClientFactory(commander),
	)
	ctx := &types.ScaffoldContext{WorktreePath: dir, SiteName: "app"}
	ctx.SetDbSuffix("swift_runner")
	ctx.SetDbSuffixLoadedFromState()

	if err := step.Run(ctx, types.StepOptions{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{"--json", "db", "create", "mariadb", "app_swift_runner"}
	if len(commander.calls) != 2 || !slices.Equal(commander.calls[1].args, want) {
		t.Fatalf("Yerd calls = %#v, want MariaDB create args %v", commander.calls, want)
	}
}

func TestRegistryYerdSiteDriverUsesManagedDatabaseLifecycle(t *testing.T) {
	commander := &yerdScriptedCommander{responses: []yerdCommandResponse{
		{output: []byte(`{"services":[{"service":"mysql","state":"running"}]}`)},
		{output: []byte(`{"name":"app_swift_runner"}`)},
	}}
	registry := NewRegistry()
	registry.RegisterDefaultsForSiteDriver(config.SiteDriverYerd, commander)
	step, err := registry.Create(config.StepDbCreate, config.StepConfig{Type: "mysql"})
	if err != nil {
		t.Fatalf("creating db.create step: %v", err)
	}
	ctx := &types.ScaffoldContext{WorktreePath: t.TempDir(), SiteName: "app"}
	ctx.SetDbSuffix("swift_runner")

	if err := step.Run(ctx, types.StepOptions{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantCreate := yerdCommandCall{
		command: "yerd",
		args:    []string{"--json", "db", "create", "mysql", "app_swift_runner"},
	}
	if len(commander.calls) != 2 || !slices.Equal(commander.calls[1].args, wantCreate.args) {
		t.Fatalf("Yerd calls = %#v, want create call %#v", commander.calls, wantCreate)
	}
}

func TestRegistryYerdSiteDriverDropsOwnedDatabaseFamilies(t *testing.T) {
	databaseList := []byte(`{
		"databases":[
			{"name":"app_swift_runner"},
			{"name":"app_swift_runner_test"},
			{"name":"app_swift_runner_test_1"}
		]
	}`)
	commander := &yerdScriptedCommander{responses: []yerdCommandResponse{
		{output: []byte(`{"services":[{"service":"mariadb","state":"running"}]}`)},
		{output: databaseList},
		{output: databaseList},
		{output: []byte(`{}`)},
		{output: []byte(`{}`)},
		{output: []byte(`{}`)},
	}}
	dir := t.TempDir()
	requireNoError := func(err error) {
		if err != nil {
			t.Helper()
			t.Fatal(err)
		}
	}
	requireNoError(config.WriteLocalState(dir, config.LocalState{Databases: []config.OwnedDatabase{
		{Name: "app_swift_runner", Engine: "mysql", Role: config.DbRoleApplication},
		{Name: "app_swift_runner_test", Engine: "mysql", Role: config.DbRoleTesting},
	}}))
	requireNoError(os.WriteFile(filepath.Join(dir, ".env"), []byte("DB_CONNECTION=mariadb\n"), 0o644))
	registry := NewRegistry()
	registry.RegisterDefaultsForSiteDriver(config.SiteDriverYerd, commander)
	step, err := registry.Create(config.StepDbDestroy, config.StepConfig{})
	requireNoError(err)

	requireNoError(step.Run(&types.ScaffoldContext{WorktreePath: dir}, types.StepOptions{}))

	wantDrops := [][]string{
		{"--json", "db", "drop", "mariadb", "app_swift_runner"},
		{"--json", "db", "drop", "mariadb", "app_swift_runner_test"},
		{"--json", "db", "drop", "mariadb", "app_swift_runner_test_1"},
	}
	if len(commander.calls) != 6 {
		t.Fatalf("Yerd calls = %#v, want six lifecycle calls", commander.calls)
	}
	for index, want := range wantDrops {
		if got := commander.calls[index+3].args; !slices.Equal(got, want) {
			t.Fatalf("drop call %d = %v, want %v", index, got, want)
		}
	}
}
