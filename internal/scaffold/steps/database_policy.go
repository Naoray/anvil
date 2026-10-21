package steps

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/naoray/anvil/internal/checks"
	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/dblifecycle"
	"github.com/naoray/anvil/internal/resources"
	"github.com/naoray/anvil/internal/scaffold/types"
	"github.com/naoray/anvil/internal/utils"
	"github.com/naoray/anvil/internal/wtstate"
)

type databaseAction struct {
	Name, Role, Engine, Service, Action string
	Driver                              string
	Owned, Verified                     bool
	Host, Port                          string
	Borrowed, Seed                      bool
	// Unrecorded marks a dry-run identity db.create would create and record;
	// it never authorizes a real command.
	Unrecorded bool
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
			targets = append(targets, databaseAction{Name: name, Role: entry.role, Engine: engine, Driver: identity.Driver, Service: identity.Service, Host: identity.Host, Port: identity.Port, Unrecorded: true})
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
		if action == "fresh" && !isolatedTestingTarget(ctx, *target) {
			return fmt.Errorf("database action fresh for %q: fresh is only allowed for this worktree's owned, isolated testing database (its canonical role: testing record with a verified endpoint, named for this worktree's suffix and ensured by db.create in this run)", selector)
		}
		if action == "reset" && (target.Borrowed || ctx.GetVar("UnrecordedDatabase:"+target.Name) == "true" || ctx.GetVar("DatabaseNew:"+target.Name) == "true" || (target.Role == "application" && ctx.GetVar("ApplicationDatabaseNew") == "true")) {
			return fmt.Errorf("reset requires an existing exact owned database; %q is borrowed/shared or newly created", selector)
		}
		selected[index] = selector
		target.Action = action
		return nil
	}
	for _, key := range keys {
		action := ctx.DatabaseActions[key]
		if action != "skip" && action != "migrate" && action != "fresh" {
			return nil, fmt.Errorf("invalid database action %q for %q: use skip, migrate or fresh", action, key)
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
			switch {
			case target.Role == "application":
				target.Action = "skip"
				if !target.Borrowed && ctx.GetVar("ApplicationDatabaseNew") == "true" {
					target.Action = "migrate"
					target.Seed = true
				}
			case isolatedTestingTarget(ctx, target):
				target.Action = "migrate"
			default:
				continue
			}
		}
		if target.Action != "skip" && target.Owned && !target.Verified {
			return nil, fmt.Errorf("database %q has unknown legacy endpoint identity; cannot authorize migration/reset", target.Name)
		}
		actions = append(actions, target)
	}
	return actions, nil
}

// isolatedTestingTarget reports whether target is this worktree's own testing
// database: the canonical testing record whose name is this run's
// TestDatabaseName for the worktree suffix, with a verified endpoint, not
// borrowed, and ensured by db.create in this run. Only such a target is
// migrated by default (non-destructive migrate, so preparation is safe to
// repeat while tests run) and may opt into a destructive fresh rebuild. In a
// dry-run the identity db.create would create and record also qualifies; it
// never authorizes a real command.
func isolatedTestingTarget(ctx *types.ScaffoldContext, target databaseAction) bool {
	name := ctx.GetVar("TestDatabaseName")
	suffix := ctx.GetDbSuffix()
	if target.Role != config.DbRoleTesting || target.Borrowed || name == "" || target.Name != name ||
		suffix == "" || !strings.HasSuffix(name, "_"+suffix+"_test") {
		return false
	}
	switch ctx.GetVar("DatabaseNew:" + name) {
	case "true":
		return target.Owned && target.Verified || target.Unrecorded
	case "false":
		return target.Owned && target.Verified
	}
	return false
}

type DbMigrateStep struct{ clientFactory DatabaseClientFactory }

func (*DbMigrateStep) Name() string                                   { return "db.migrate" }
func (*DbMigrateStep) Condition(*types.ScaffoldContext) (bool, error) { return true, nil }
func (s *DbMigrateStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) (runErr error) {
	actions, err := planDatabaseActions(ctx)
	if err != nil {
		return err
	}
	// A fresh rebuild holds the repository database lock from its isolation
	// check through the command, so no sibling can bind the name in between.
	for _, action := range actions {
		if action.Action != "fresh" || opts.DryRun || ctx.DatabaseLockHeld {
			continue
		}
		release, err := dblifecycle.Lock(ctx.WorktreePath)
		if err != nil {
			return fmt.Errorf("locking database local state: %w", err)
		}
		ctx.DatabaseLockHeld = true
		defer func() { ctx.DatabaseLockHeld = false; runErr = errors.Join(runErr, release()) }()
		break
	}
	// Check every selected physical target before running the first command.
	for i := range actions {
		action := &actions[i]
		plannedNew := opts.DryRun && action.Action != "reset" && (ctx.GetVar("DatabaseNew:"+action.Name) == "true" || ctx.GetVar("DatabaseName") == action.Name && ctx.GetVar("ApplicationDatabaseNew") == "true")
		if action.Action == "skip" {
			continue
		}
		if action.Unrecorded && action.Role == config.DbRoleTesting && !opts.DryRun {
			return fmt.Errorf("database %q role=testing is not recorded as owned by this worktree; refusing %s", action.Name, action.Action)
		}
		if action.Action == "fresh" {
			if !opts.DryRun {
				if err := ensureNoRunningChecks(ctx.WorktreePath, action.Name); err != nil {
					return err
				}
			}
			if err := ensureUnsharedDatabase(ctx, action.Name); err != nil {
				return err
			}
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
		} else if action.Action == "fresh" {
			args[1] = "migrate:fresh"
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
		// The remedy stays on the first line: preparation records keep only it.
		return fmt.Errorf("database %q role=%s action=%s failed: %w; %s\n%s", action.Name, action.Role, action.Action, err, migrationRemedy(action.Role), output)
	}
	return nil
}

// ensureUnsharedDatabase refuses a destructive rebuild when any other
// registered worktree records or borrows the same database name. Ownership
// already proves this worktree created the database; this re-checks isolation
// against sibling state just before migrate:fresh. Unreadable sibling state
// fails closed, like destructive cleanup. Outside dry-run the caller holds the
// repository database lock.
func ensureUnsharedDatabase(ctx *types.ScaffoldContext, name string) error {
	if _, err := os.Lstat(filepath.Join(ctx.WorktreePath, ".git")); err != nil {
		if os.IsNotExist(err) {
			// Without a Git registry there are no sibling worktrees.
			return nil
		}
		return fmt.Errorf("checking worktree registry before rebuilding database %q: %w", name, err)
	}
	paths, err := dblifecycle.RegisteredPaths(ctx.WorktreePath)
	if err != nil {
		return fmt.Errorf("checking sibling worktrees before rebuilding database %q: %w", name, err)
	}
	own, err := dblifecycle.Canonical(ctx.WorktreePath)
	if err != nil {
		return err
	}
	for _, sibling := range paths {
		siblingPath, err := dblifecycle.Canonical(sibling)
		if errors.Is(err, fs.ErrNotExist) {
			// A deleted worktree cannot use the database.
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot inspect sibling %q before rebuilding database %q: %w", sibling, name, err)
		}
		if siblingPath == own {
			continue
		}
		state, err := config.ReadLocalState(siblingPath)
		if err != nil {
			return fmt.Errorf("cannot inspect sibling %q before rebuilding database %q: %w", siblingPath, name, err)
		}
		for _, db := range state.Databases {
			if db.Name == name {
				return fmt.Errorf("database %q is also recorded by worktree %q; refusing migrate:fresh; %s", name, siblingPath, freshRefusalRemedy)
			}
		}
		if borrowed := state.BorrowedApplication; borrowed != nil && borrowed.Name == name {
			return fmt.Errorf("database %q is also borrowed by worktree %q; refusing migrate:fresh; %s", name, siblingPath, freshRefusalRemedy)
		}
	}
	return nil
}

const freshRefusalRemedy = "rerun without --db-action testing=fresh for a non-destructive migration, or remove the other worktree's record of it"

// ensureNoRunningChecks refuses a fresh rebuild while any check of this
// worktree runs, since checks may use the testing database. Checks only hold
// the shared lifecycle gate, like preparation, so this is the sole guard; a
// runtime directory that cannot be resolved fails closed. anvil exec sessions
// hold no lock and cannot be detected.
func ensureNoRunningChecks(worktreePath, name string) error {
	const unverified = "cannot verify that no checks are running in this worktree; refusing migrate:fresh of %q: %s"
	identity, status, err := wtstate.ReadIdentity(worktreePath)
	if err != nil {
		return fmt.Errorf(unverified, name, err)
	}
	if identity == nil || status == wtstate.IdentityCopied {
		return fmt.Errorf(unverified, name, "the worktree has no verified Anvil identity")
	}
	root, err := resources.Root()
	if err != nil {
		return fmt.Errorf(unverified, name, err)
	}
	dir, err := resources.WorktreeDir(root, identity.ID)
	if err != nil {
		return fmt.Errorf(unverified, name, err)
	}
	running, err := checks.RunningChecks(dir)
	if err != nil {
		return fmt.Errorf(unverified, name, err)
	}
	if len(running) > 0 {
		return fmt.Errorf("checks are running in this worktree (%s); refusing migrate:fresh of %q while they may use it — wait for them to finish, then rerun with --db-action testing=fresh", strings.Join(running, ", "), name)
	}
	return nil
}

// migrationRemedy names the corrective command for a failed migration.
func migrationRemedy(role string) string {
	if role == config.DbRoleTesting {
		return "fix the cause and rerun 'anvil scaffold' (or set database_actions: {testing: skip} to opt out)"
	}
	return "fix the cause and rerun 'anvil scaffold'"
}
