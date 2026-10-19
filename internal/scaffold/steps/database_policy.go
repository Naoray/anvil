package steps

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
	"github.com/naoray/anvil/internal/utils"
)

type databaseAction struct {
	Name, Role, Engine, Service, Action string
	Driver                              string
	Owned, Verified                     bool
	Host, Port                          string
	Borrowed, Seed                      bool
}

func planDatabaseActions(ctx *types.ScaffoldContext) ([]databaseAction, error) {
	state, err := config.ReadLocalState(ctx.WorktreePath)
	if err != nil {
		return nil, err
	}
	if err = config.ValidateOwnedDatabases(state.Databases); err != nil {
		return nil, err
	}
	if err = config.ValidateBorrowedApplication(state.BorrowedApplication); err != nil {
		return nil, err
	}
	env := utils.ReadEnvFile(ctx.WorktreePath, ".env")
	if len(env) == 0 {
		env = utils.ReadEnvFile(ctx.WorktreePath, ".env.example")
	}
	service := env["DB_CONNECTION"]
	var targets []databaseAction
	for _, db := range state.Databases {
		role := db.Role
		if current := ctx.GetVar("DatabaseName"); current != "" && state.BorrowedApplication == nil {
			if db.Name == current {
				role = config.DbRoleApplication
			} else if role == config.DbRoleApplication {
				role = config.DbRoleAuxiliary
			}
		}
		target := databaseAction{Name: db.Name, Role: role, Engine: db.Engine, Owned: true}
		if db.Endpoint != nil {
			target.Driver = db.Endpoint.Driver
			target.Service = db.Endpoint.Service
			target.Host = db.Endpoint.Host
			target.Port = db.Endpoint.Port
			target.Verified = true
		}
		targets = append(targets, target)
	}

	// Dry-run db.create resolves identities without claiming ownership.
	for _, entry := range []struct{ key, role string }{{"DatabaseName", "application"}, {"TestDatabaseName", "testing"}} {
		name := ctx.GetVar(entry.key)
		if name == "" || service == "sqlite" || state.BorrowedApplication != nil && entry.role == "application" {
			continue
		}
		found := false
		for _, target := range targets {
			if target.Name == name {
				found = true
			}
		}
		if !found {
			engine := service
			if engine == "mariadb" {
				engine = "mysql"
			}
			driver := ctx.SiteDriver
			if driver == "" {
				driver = "yerd"
			}
			identity, err := applicationIdentity(ctx.WorktreePath, name, engine, driver)
			if err != nil {
				return nil, err
			}
			targets = append(targets, databaseAction{Name: name, Role: entry.role, Engine: engine, Driver: identity.Driver, Service: identity.Service, Host: identity.Host, Port: identity.Port})
			ctx.SetVar("UnrecordedDatabase:"+name, "true")
		}
	}
	if b := state.BorrowedApplication; b != nil {
		for i := range targets {
			if targets[i].Role == config.DbRoleApplication {
				targets[i].Role = config.DbRoleAuxiliary
			}
		}

		targets = append(targets, databaseAction{Name: b.Name, Role: "application", Engine: b.Engine, Driver: b.Driver, Service: b.Service, Host: b.Host, Port: b.Port, Borrowed: true, Verified: true})
	} else if service == "sqlite" || (len(targets) == 0 && env["DB_DATABASE"] != "") {
		name := ctx.GetVar("DatabaseName")
		if name == "" {
			name = env["DB_DATABASE"]
		}
		if service == "sqlite" && name == "" {
			name = "database/database.sqlite"
		}
		target := databaseAction{Name: name, Role: "application", Engine: service, Service: service, Borrowed: service != "sqlite"}
		if service != "sqlite" {
			engine := service
			if engine == "mariadb" {
				engine = "mysql"
			}
			driver := ctx.SiteDriver
			if driver == "" {
				driver = "yerd"
			}
			identity, err := applicationIdentity(ctx.WorktreePath, name, engine, driver)
			if err != nil {
				return nil, err
			}
			target.Driver = identity.Driver
			target.Engine = engine
			target.Service = identity.Service
			target.Host = identity.Host
			target.Port = identity.Port
		}
		targets = append(targets, target)
	}
	selected := make(map[int]string)
	keys := make([]string, 0, len(ctx.DatabaseActions))
	for k := range ctx.DatabaseActions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	selectTarget := func(selector, action string, exact bool) error {
		index := -1
		for i, t := range targets {
			if t.Name == selector || (!exact && t.Role == selector && (selector == "application" || selector == "testing")) {
				if index != -1 {
					return fmt.Errorf("ambiguous database target %q", selector)
				}
				index = i
			}
		}
		if index < 0 {
			return fmt.Errorf("database target %q is absent; use application, testing, or an exact recorded database name", selector)
		}
		if previous, ok := selected[index]; ok {
			return fmt.Errorf("overlapping database selectors %q and %q", previous, selector)
		}
		target := &targets[index]
		if action == "reset" && (target.Borrowed || ctx.GetVar("UnrecordedDatabase:"+target.Name) == "true" || ctx.GetVar("DatabaseNew:"+target.Name) == "true" || (target.Role == "application" && ctx.GetVar("ApplicationDatabaseNew") == "true")) {
			return fmt.Errorf("reset requires an existing exact owned database; %q is borrowed/shared or newly created", selector)
		}
		selected[index] = selector
		target.Action = action
		return nil
	}
	for _, key := range keys {
		action := ctx.DatabaseActions[key]
		if action != "skip" && action != "migrate" {
			return nil, fmt.Errorf("invalid database action %q for %q: use skip or migrate", action, key)
		}
		if err := selectTarget(key, action, false); err != nil {
			return nil, err
		}
	}
	for _, name := range ctx.ResetDatabases {
		if err := selectTarget(name, "reset", true); err != nil {
			return nil, err
		}
	}
	var actions []databaseAction
	for _, target := range targets {
		if target.Action == "" {
			if target.Role != "application" {
				continue
			}
			target.Action = "skip"
			if !target.Borrowed && ctx.GetVar("ApplicationDatabaseNew") == "true" {
				target.Action = "migrate"
				target.Seed = true
			}
		}
		if target.Action != "skip" && target.Owned && !target.Verified {
			return nil, fmt.Errorf("database %q has unknown legacy endpoint identity; cannot authorize migration/reset", target.Name)
		}
		actions = append(actions, target)
	}
	return actions, nil
}

type DbMigrateStep struct{ clientFactory DatabaseClientFactory }

func (*DbMigrateStep) Name() string                                   { return "db.migrate" }
func (*DbMigrateStep) Condition(*types.ScaffoldContext) (bool, error) { return true, nil }
func (s *DbMigrateStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	actions, err := planDatabaseActions(ctx)
	if err != nil {
		return err
	}
	// Check every selected physical target before running the first command.
	for i := range actions {
		action := &actions[i]
		plannedNew := opts.DryRun && action.Action != "reset" && (ctx.GetVar("DatabaseNew:"+action.Name) == "true" || ctx.GetVar("DatabaseName") == action.Name && ctx.GetVar("ApplicationDatabaseNew") == "true")
		if action.Action == "skip" {
			continue
		}
		if action.Engine == "sqlite" {
			if err := (&DbCreateStep{}).createSqlite(ctx, action.Name, types.StepOptions{DryRun: true}); err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(ctx.WorktreePath, action.Name)); err != nil && !plannedNew {
				return fmt.Errorf("database target %q is unavailable: %w", action.Name, err)
			}
			continue
		}
		factory := s.clientFactory
		if factory == nil {
			factory = managedDatabaseFactory(config.SiteDriver(action.Driver), nil)
		}
		var expected *config.DatabaseEndpoint
		if action.Verified {
			expected = &config.DatabaseEndpoint{Driver: action.Driver, Service: action.Service, Host: action.Host, Port: action.Port}
		}
		client, err := factory(action.Engine, DatabaseOptions{WorktreePath: ctx.WorktreePath, Service: action.Service, Endpoint: expected})
		if err != nil {
			return err
		}
		endpoint, verifyErr := verifiedClientEndpoint(client, action.Engine, ctx.WorktreePath, expected)
		if verifyErr != nil {
			return errors.Join(verifyErr, client.Close())
		}
		action.Driver, action.Service, action.Host, action.Port = endpoint.Driver, endpoint.Service, endpoint.Host, endpoint.Port
		if plannedNew {
			if err := client.Close(); err != nil {
				return err
			}
			continue
		}
		names, listErr := client.ListDatabases(EscapeLikePattern(action.Name))
		closeErr := client.Close()
		if err := errors.Join(listErr, closeErr); err != nil {
			return fmt.Errorf("checking database target %q: %w", action.Name, err)
		}
		exists := false
		for _, name := range names {
			if name == action.Name {
				exists = true
			}
		}
		if !exists {
			return fmt.Errorf("database target %q is absent; no migration/reset commands ran", action.Name)
		}
	}
	for _, action := range actions {
		fmt.Printf("Database %s role=%s engine=%s service=%s action=%s driver=%s endpoint=%s:%s seed=%t\n", action.Name, action.Role, action.Engine, action.Service, action.Action, action.Driver, action.Host, action.Port, action.Seed)
		if action.Action == "skip" || opts.DryRun {
			continue
		}
		args := []string{"artisan", "migrate", "--no-interaction"}
		if action.Action == "reset" {
			args[1] = "migrate:fresh"
			args = append(args, "--seed")
		} else if action.Seed {
			args = append(args, "--seed")
		}
		if err := runDatabaseAction(ctx, action, args); err != nil {
			return err
		}
	}
	return nil
}

func runDatabaseAction(ctx *types.ScaffoldContext, action databaseAction, args []string) (runErr error) {
	cacheDir, err := os.MkdirTemp("", "anvil-migration-")
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, os.RemoveAll(cacheDir)) }()
	env := utils.ReadEnvFile(ctx.WorktreePath, ".env")
	overrides := map[string]string{"DB_DATABASE": action.Name, "DB_CONNECTION": action.Engine, "DB_HOST": action.Host, "DB_PORT": action.Port, "DB_SOCKET": "", "DB_URL": "", "DATABASE_URL": "", "APP_CONFIG_CACHE": filepath.Join(cacheDir, "config.php")}
	if action.Service == "mariadb" {
		overrides["DB_CONNECTION"] = "mariadb"
	}
	for _, key := range []string{"DB_USERNAME", "DB_PASSWORD"} {
		if value, ok := env[key]; ok {
			overrides[key] = value
		}
	}
	cmd := exec.Command("php", args...)
	cmd.Dir = ctx.WorktreePath
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, ok := overrides[key]; !ok {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		cmd.Env = append(cmd.Env, key+"="+overrides[key])
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("database %q %s failed: %w\n%s", action.Name, action.Action, err, output)
	}
	return nil
}
