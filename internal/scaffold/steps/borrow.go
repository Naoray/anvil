package steps

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/dblifecycle"
	"github.com/naoray/anvil/internal/scaffold/types"
	"github.com/naoray/anvil/internal/utils"
)

func applicationIdentity(path, name, engine, driver string) (*config.BorrowedApplication, error) {
	env := utils.ReadEnvFile(path, ".env")
	if len(env) == 0 {
		env = utils.ReadEnvFile(path, ".env.example")
	}
	if env["DB_SOCKET"] != "" || env["DB_URL"] != "" || env["DATABASE_URL"] != "" {
		return nil, fmt.Errorf("database socket/URL overrides are ambiguous for managed database identity; use explicit host, port and DB_DATABASE")
	}
	service := env["DB_CONNECTION"]
	if service == "pgsql" {
		if driver == "herd" {
			service = "postgresql"
		} else {
			service = "postgres"
		}
	}
	identity := &config.BorrowedApplication{SourcePath: path, Name: name, Engine: engine, Driver: driver, Service: service, Host: env["DB_HOST"], Port: env["DB_PORT"]}
	if identity.Host == "" || identity.Host == "localhost" {
		identity.Host = "127.0.0.1"
	}
	if identity.Host != "127.0.0.1" && identity.Host != "::1" && !filepath.IsAbs(identity.Host) {
		return nil, fmt.Errorf("managed database endpoint must be local, got %q", identity.Host)
	}
	if identity.Port == "" {
		if engine == "pgsql" {
			identity.Port = "5432"
		} else {
			identity.Port = "3306"
		}
	}
	return identity, config.ValidateBorrowedApplication(identity)
}

func (s *DbCreateStep) borrowApplication(ctx *types.ScaffoldContext, opts types.StepOptions) (handled bool, runErr error) {
	state, err := config.ReadLocalState(ctx.WorktreePath)
	if err != nil {
		return false, err
	}
	source := ctx.BorrowAppDB
	if source == "" && state.BorrowedApplication != nil {
		source = state.BorrowedApplication.SourcePath
	}
	if source == "" {
		return false, nil
	}
	if !ctx.DatabaseLockHeld {
		release, err := dblifecycle.Lock(ctx.WorktreePath)
		if err != nil {
			return true, err
		}
		defer func() { runErr = errors.Join(runErr, release()) }()
	}
	state, err = config.ReadLocalState(ctx.WorktreePath)
	if err != nil {
		return true, err
	}
	current, err := dblifecycle.Canonical(ctx.WorktreePath)
	if err != nil {
		return true, err
	}
	paths, err := dblifecycle.RegisteredPaths(ctx.WorktreePath)
	if err != nil {
		return true, err
	}
	source, err = dblifecycle.Canonical(source)
	if err != nil {
		return true, fmt.Errorf("borrow source unavailable: %w", err)
	}
	if source == current {
		return true, fmt.Errorf("worktree cannot borrow from itself")
	}
	registered := false
	for _, path := range paths {
		canonical, err := dblifecycle.Canonical(path)
		if err != nil {
			return true, err
		}
		if canonical == source {
			registered = true
		}
	}
	if !registered {
		return true, fmt.Errorf("borrow source is not a registered worktree in this repository")
	}
	owner, err := config.ReadLocalState(source)
	if err != nil {
		return true, fmt.Errorf("reading borrow source: %w", err)
	}
	if owner.BorrowedApplication != nil {
		return true, fmt.Errorf("borrow chains are unsupported; select the original owner")
	}
	if err = config.ValidateOwnedDatabases(owner.Databases); err != nil {
		return true, err
	}
	var app *config.OwnedDatabase
	for i := range owner.Databases {
		if owner.Databases[i].Role == config.DbRoleApplication {
			app = &owner.Databases[i]
		}
	}
	if app == nil {
		return true, fmt.Errorf("borrow source has no recorded application database")
	}
	driver := ctx.SiteDriver
	if driver == "" {
		driver = "yerd"
	}
	if app.Endpoint == nil {
		return true, fmt.Errorf("borrow source database %q has unknown legacy endpoint identity; create an independently owned database", app.Name)
	}
	if err := app.Endpoint.Validate(app.Engine); err != nil {
		return true, err
	}
	identity := &config.BorrowedApplication{SourcePath: source, Name: app.Name, Engine: app.Engine, Driver: app.Endpoint.Driver, Service: app.Endpoint.Service, Host: app.Endpoint.Host, Port: app.Endpoint.Port}
	for _, owned := range state.Databases {
		if owned.Name == identity.Name {
			return true, fmt.Errorf("borrowed database %q already appears in this worktree's ownership history", identity.Name)
		}
	}
	target, err := applicationIdentity(current, app.Name, app.Engine, driver)
	if err != nil {
		return true, err
	}
	if identity.Driver != target.Driver || identity.Service != target.Service || identity.Host != target.Host || identity.Port != target.Port {
		return true, fmt.Errorf("borrow source engine/service endpoint differs from target")
	}
	if previous := state.BorrowedApplication; previous != nil && ctx.BorrowAppDB == "" && *previous != *identity {
		return true, fmt.Errorf("borrow source identity changed; select --borrow-app-db explicitly")
	}
	connection := DatabaseOptions{WorktreePath: source, Service: identity.Service, Endpoint: app.Endpoint}
	client, err := s.clientFactory(app.Engine, connection)
	if err != nil {
		return true, err
	}
	defer func() { runErr = errors.Join(runErr, client.Close()) }()
	if _, err := verifiedClientEndpoint(client, app.Engine, source, app.Endpoint); err != nil {
		return true, err
	}
	// Listing is read-only: borrowing never starts a stopped managed service.
	names, err := client.ListDatabases(EscapeLikePattern(app.Name))
	if err != nil {
		return true, fmt.Errorf("checking borrowed database availability: %w", err)
	}
	exists := false
	for _, name := range names {
		if name == app.Name {
			exists = true
		}
	}
	if !exists {
		return true, fmt.Errorf("borrowed database %q is absent", app.Name)
	}
	ctx.SetVar("DatabaseName", identity.Name)
	ctx.SetVar("ApplicationDatabaseNew", "false")
	ctx.SetVar("BorrowApplicationApplied", "true")
	if opts.DryRun {
		fmt.Printf("Borrow database %s role=application engine=%s service=%s\n", identity.Name, identity.Engine, identity.Service)
		return true, nil
	}
	return true, config.WriteLocalState(ctx.WorktreePath, config.LocalState{BorrowedApplication: identity})
}
