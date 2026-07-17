package steps

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
	"github.com/naoray/anvil/internal/scaffold/words"
	"github.com/naoray/anvil/internal/utils"
)

// detectDatabaseEngine determines the database engine from explicit type or .env file.
// This is shared between DbCreateStep and DbDestroyStep.
func detectDatabaseEngine(dbType string, ctx *types.ScaffoldContext) (config.DatabaseEngine, error) {
	if dbType != "" {
		switch dbType {
		case string(config.DBEngineMySQL), string(config.DBEnginePgSQL), string(config.DBEngineSQLite):
			return config.DatabaseEngine(dbType), nil
		default:
			return "", fmt.Errorf("unsupported database type: %s", dbType)
		}
	}

	env := utils.ReadEnvFile(ctx.WorktreePath, ".env")
	if conn := env["DB_CONNECTION"]; conn != "" {
		switch conn {
		case "mysql", "mariadb":
			return config.DBEngineMySQL, nil
		case "pgsql", "postgres", "postgresql":
			return config.DBEnginePgSQL, nil
		case "sqlite":
			return config.DBEngineSQLite, nil
		}
	}

	return "", fmt.Errorf("database type not specified and DB_CONNECTION not found in .env")
}

type DbCreateStep struct {
	name          string
	args          []string
	dbType        string
	role          string
	clientFactory DatabaseClientFactory
}

func NewDbCreateStep(cfg config.StepConfig) *DbCreateStep {
	return &DbCreateStep{
		name:          config.StepDbCreate,
		args:          cfg.Args,
		dbType:        cfg.Type,
		role:          cfg.Role,
		clientFactory: DefaultDatabaseClientFactory,
	}
}

func NewDbCreateStepWithFactory(cfg config.StepConfig, factory DatabaseClientFactory) *DbCreateStep {
	return &DbCreateStep{
		name:          config.StepDbCreate,
		args:          cfg.Args,
		dbType:        cfg.Type,
		role:          cfg.Role,
		clientFactory: factory,
	}
}

func (s *DbCreateStep) Name() string {
	return s.name
}

func (s *DbCreateStep) Condition(ctx *types.ScaffoldContext) bool {
	return true
}

func (s *DbCreateStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	engine, err := detectDatabaseEngine(s.dbType, ctx)
	if err != nil {
		if opts.Verbose {
			fmt.Printf("  %v\n", err)
		}
		return nil
	}

	role := s.role
	if role == "" {
		role = config.DbRoleApplication
	}
	if role != config.DbRoleApplication && role != config.DbRoleTesting {
		return fmt.Errorf("unsupported database role %q", role)
	}

	if opts.Verbose {
		fmt.Printf("  Creating %s database (%s)...\n", role, engine)
	}

	if engine == config.DBEngineSQLite {
		if role == config.DbRoleTesting {
			if opts.Verbose {
				fmt.Printf("  SQLite databases are worktree-local files; skipping testing database creation.\n")
			}
			return nil
		}
		dbName := ""
		for i, arg := range s.args {
			if arg == "--database" && i+1 < len(s.args) {
				dbName = s.args[i+1]
			}
		}
		if dbName == "" {
			env := utils.ReadEnvFile(ctx.WorktreePath, ".env")
			dbName = env["DB_DATABASE"]
		}
		if dbName == "" {
			dbName = "database/database.sqlite"
		}
		return s.createSqlite(ctx, dbName, opts)
	}

	if role == config.DbRoleTesting {
		return s.createTestDatabase(ctx, engine, opts)
	}
	return s.createWithRetry(ctx, engine, opts)
}

func (s *DbCreateStep) getPrefixOrSiteName(ctx *types.ScaffoldContext) string {
	for i, arg := range s.args {
		if arg == "--prefix" && i+1 < len(s.args) {
			return s.args[i+1]
		}
	}

	siteName := ctx.SiteName
	if siteName == "" {
		env := utils.ReadEnvFile(ctx.WorktreePath, ".env")
		siteName = env["APP_NAME"]
	}
	if siteName == "" {
		siteName = "app"
	}
	return siteName
}

func (s *DbCreateStep) parseConnectionOptions() DatabaseOptions {
	opts := DatabaseOptions{
		Host:     "127.0.0.1",
		Username: "root",
	}

	for i, arg := range s.args {
		if arg == "--username" && i+1 < len(s.args) {
			opts.Username = s.args[i+1]
		}
		if arg == "--password" && i+1 < len(s.args) {
			opts.Password = s.args[i+1]
		}
		if arg == "--host" && i+1 < len(s.args) {
			opts.Host = s.args[i+1]
		}
		if arg == "--port" && i+1 < len(s.args) {
			opts.Port = s.args[i+1]
		}
	}

	return opts
}

const maxDbCreateRetries = 5

func (s *DbCreateStep) createWithRetry(ctx *types.ScaffoldContext, engine config.DatabaseEngine, opts types.StepOptions) (runErr error) {
	siteName := s.getPrefixOrSiteName(ctx)
	dbOpts := s.parseConnectionOptions()

	client, err := s.clientFactory(string(engine), dbOpts)
	if err != nil {
		return fmt.Errorf("creating database client: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, client.Close()) }()

	if err := client.Ping(); err != nil {
		if opts.Verbose {
			fmt.Printf("  Could not connect to %s database: %v\n", engine, err)
		}
		return nil
	}

	var lastErr error
	for attempt := 0; attempt < maxDbCreateRetries; attempt++ {
		var dbName string
		var suffix string

		existingSuffix := ctx.GetDbSuffix()
		if existingSuffix != "" {
			suffix = existingSuffix
			dbName = words.BuildDatabaseName(siteName, suffix, 0)
		} else {
			dbName = words.GenerateDatabaseName(siteName, 0)
			suffix = words.ExtractSuffix(dbName)
			ctx.SetDbSuffix(suffix)
		}

		if opts.Verbose {
			fmt.Printf("  Generated database name: %s (attempt %d/%d)\n", dbName, attempt+1, maxDbCreateRetries)
		}

		err := client.CreateDatabase(dbName)
		if err == nil {
			if opts.Verbose {
				fmt.Printf("  Database '%s' created successfully.\n", dbName)
			}
			return s.persistOwnedDatabase(ctx, dbName, engine, config.DbRoleApplication)
		}

		if !IsDatabaseExistsError(err) {
			return fmt.Errorf("failed to create database: %w", err)
		}

		if ctx.DbSuffixFromState() {
			return s.persistOwnedDatabase(ctx, dbName, engine, config.DbRoleApplication)
		}

		if opts.Verbose {
			fmt.Printf("  Database '%s' already exists, retrying...\n", dbName)
		}
		ctx.SetDbSuffix("")
		lastErr = err
	}

	return fmt.Errorf("failed to create database after %d attempts: %w", maxDbCreateRetries, lastErr)
}

func (s *DbCreateStep) createTestDatabase(ctx *types.ScaffoldContext, engine config.DatabaseEngine, opts types.StepOptions) (runErr error) {
	suffix := ctx.GetDbSuffix()
	if suffix == "" {
		if opts.Verbose {
			fmt.Printf("  No database suffix found, skipping testing database creation.\n")
		}
		return nil
	}

	dbName := words.BuildTestDatabaseName(s.getPrefixOrSiteName(ctx), suffix)
	client, err := s.clientFactory(string(engine), s.parseConnectionOptions())
	if err != nil {
		return fmt.Errorf("creating database client: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, client.Close()) }()

	if err := client.Ping(); err != nil {
		if opts.Verbose {
			fmt.Printf("  Could not connect to %s database: %v\n", engine, err)
		}
		return nil
	}
	if opts.DryRun {
		if opts.Verbose {
			fmt.Printf("  Would create testing database: %s\n", dbName)
		}
		return nil
	}

	if err := client.CreateDatabase(dbName); err != nil && !IsDatabaseExistsError(err) {
		return fmt.Errorf("failed to create testing database: %w", err)
	}
	return s.persistOwnedDatabase(ctx, dbName, engine, config.DbRoleTesting)
}

func (s *DbCreateStep) persistOwnedDatabase(ctx *types.ScaffoldContext, name string, engine config.DatabaseEngine, role string) error {
	if err := config.WriteLocalState(ctx.WorktreePath, config.LocalState{
		DbSuffix: ctx.GetDbSuffix(),
		Databases: []config.OwnedDatabase{{
			Name: name, Engine: string(engine), Role: role,
		}},
	}); err != nil {
		return fmt.Errorf("recording ownership of database %q: %w", name, err)
	}
	return nil
}

func (s *DbCreateStep) createSqlite(ctx *types.ScaffoldContext, dbName string, opts types.StepOptions) error {
	dbPath := filepath.Join(ctx.WorktreePath, dbName)

	if opts.Verbose {
		fmt.Printf("  Creating SQLite database: %s\n", dbPath)
	}

	if opts.DryRun {
		return nil
	}

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating database directory: %w", err)
	}

	file, err := os.Create(dbPath)
	if err != nil {
		return fmt.Errorf("creating SQLite file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing SQLite file: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("  SQLite database created at: %s\n", dbPath)
	}

	return nil
}

type DbDestroyStep struct {
	name          string
	args          []string
	dbType        string
	clientFactory DatabaseClientFactory
	output        io.Writer
}

func NewDbDestroyStep(cfg config.StepConfig) *DbDestroyStep {
	return NewDbDestroyStepWithFactoryAndWriter(cfg, DefaultDatabaseClientFactory, os.Stdout)
}

func NewDbDestroyStepWithFactory(cfg config.StepConfig, factory DatabaseClientFactory) *DbDestroyStep {
	return NewDbDestroyStepWithFactoryAndWriter(cfg, factory, os.Stdout)
}

func NewDbDestroyStepWithFactoryAndWriter(
	cfg config.StepConfig,
	factory DatabaseClientFactory,
	output io.Writer,
) *DbDestroyStep {
	if output == nil {
		output = os.Stdout
	}
	return &DbDestroyStep{
		name:          config.StepDbDestroy,
		args:          cfg.Args,
		dbType:        cfg.Type,
		clientFactory: factory,
		output:        output,
	}
}

func (s *DbDestroyStep) Name() string {
	return s.name
}

func (s *DbDestroyStep) Condition(ctx *types.ScaffoldContext) bool {
	return true
}

func (s *DbDestroyStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	localState, err := config.ReadLocalState(ctx.WorktreePath)
	if err != nil {
		return fmt.Errorf("reading local state for database cleanup: %w", err)
	}
	if len(localState.Databases) > 0 {
		return s.destroyOwnedDatabases(localState.Databases, opts)
	}

	suffix := ctx.GetDbSuffix()
	if suffix == "" {
		suffix = localState.DbSuffix
	}
	if suffix == "" {
		if opts.Verbose {
			return writeDatabaseCleanupOutput(s.output, "  No database suffix found, skipping cleanup.\n")
		}
		return nil
	}
	if !config.IsValidDatabaseIdentifier(suffix) {
		return fmt.Errorf("invalid legacy database suffix %q", suffix)
	}

	ctx.SetDbSuffix(suffix)
	engine, err := detectDatabaseEngine(s.dbType, ctx)
	if err != nil {
		if opts.Verbose {
			return writeDatabaseCleanupOutput(s.output, "  %v\n", err)
		}
		return nil
	}
	if engine == config.DBEngineSQLite {
		return nil
	}
	return s.destroyLegacyDatabases(engine, suffix, opts)
}

func (s *DbDestroyStep) parseConnectionOptions(engine config.DatabaseEngine) DatabaseOptions {
	opts := DatabaseOptions{
		Host: "127.0.0.1",
	}

	if engine == config.DBEnginePgSQL {
		opts.Username = "postgres"
		opts.Port = "5432"
	} else {
		opts.Username = "root"
		opts.Port = "3306"
	}

	for i, arg := range s.args {
		if arg == "--username" && i+1 < len(s.args) {
			opts.Username = s.args[i+1]
		}
		if arg == "--password" && i+1 < len(s.args) {
			opts.Password = s.args[i+1]
		}
		if arg == "--host" && i+1 < len(s.args) {
			opts.Host = s.args[i+1]
		}
		if arg == "--port" && i+1 < len(s.args) {
			opts.Port = s.args[i+1]
		}
	}

	return opts
}

type ownedTargetSelection struct {
	engine   config.DatabaseEngine
	exact    []string
	families []config.OwnedDatabase
}

func selectOwnedTargets(databases []config.OwnedDatabase) (ownedTargetSelection, error) {
	if len(databases) == 0 {
		return ownedTargetSelection{}, fmt.Errorf("no owned database records")
	}
	if err := config.ValidateOwnedDatabases(databases); err != nil {
		return ownedTargetSelection{}, fmt.Errorf("validating owned database records: %w", err)
	}

	selection := ownedTargetSelection{
		engine:   config.DatabaseEngine(databases[0].Engine),
		exact:    make([]string, 0, len(databases)),
		families: append([]config.OwnedDatabase(nil), databases...),
	}
	seen := make(map[string]struct{}, len(databases))
	for _, database := range databases {
		if _, exists := seen[database.Name]; exists {
			continue
		}
		seen[database.Name] = struct{}{}
		selection.exact = append(selection.exact, database.Name)
	}
	return selection, nil
}

func (s *DbDestroyStep) destroyOwnedDatabases(databases []config.OwnedDatabase, opts types.StepOptions) (runErr error) {
	selection, err := selectOwnedTargets(databases)
	if err != nil {
		return err
	}

	client, err := s.clientFactory(string(selection.engine), s.parseConnectionOptions(selection.engine))
	if err != nil {
		if opts.DryRun {
			return errors.Join(
				printDryRunTargets(s.output, selection.exact),
				writeDatabaseCleanupOutput(s.output, "cannot enumerate parallel-worker databases: %v\n", err),
			)
		}
		return fmt.Errorf("creating database cleanup client: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, client.Close()) }()

	if err := client.Ping(); err != nil {
		if opts.DryRun {
			return errors.Join(
				printDryRunTargets(s.output, selection.exact),
				writeDatabaseCleanupOutput(s.output, "cannot enumerate parallel-worker databases: %v\n", err),
			)
		}
		return fmt.Errorf("connecting to %s for database cleanup: %w", selection.engine, err)
	}

	targets := append([]string(nil), selection.exact...)
	seenTargets := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		seenTargets[target] = struct{}{}
	}
	var runtimeErrors []error
	for _, family := range selection.families {
		pattern := EscapeLikePattern(family.Name) + `\_test\_%`
		candidates, listErr := client.ListDatabases(pattern)
		if listErr != nil {
			if opts.DryRun {
				if outputErr := writeDatabaseCleanupOutput(s.output, "cannot enumerate parallel-worker databases: %v\n", listErr); outputErr != nil {
					runtimeErrors = append(runtimeErrors, outputErr)
				}
			} else {
				runtimeErrors = append(runtimeErrors,
					fmt.Errorf("listing parallel-worker databases for %q: %w", family.Name, listErr))
			}
			continue
		}
		prefix := family.Name + "_test_"
		for _, candidate := range candidates {
			if !config.IsValidDatabaseIdentifier(candidate) || !strings.HasPrefix(candidate, prefix) {
				continue
			}
			if _, exists := seenTargets[candidate]; exists {
				continue
			}
			seenTargets[candidate] = struct{}{}
			targets = append(targets, candidate)
		}
	}

	if opts.DryRun {
		return errors.Join(append(runtimeErrors, printDryRunTargets(s.output, targets))...)
	}
	runtimeErrors = append(runtimeErrors, dropDatabaseTargets(client, targets, opts, s.output)...)
	return errors.Join(runtimeErrors...)
}

func (s *DbDestroyStep) destroyLegacyDatabases(engine config.DatabaseEngine, suffix string, opts types.StepOptions) (runErr error) {
	client, err := s.clientFactory(string(engine), s.parseConnectionOptions(engine))
	if err != nil {
		if opts.DryRun {
			return writeDatabaseCleanupOutput(s.output, "cannot enumerate parallel-worker databases: %v\n", err)
		}
		return fmt.Errorf("creating legacy database cleanup client: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, client.Close()) }()

	if err := client.Ping(); err != nil {
		if opts.DryRun {
			return writeDatabaseCleanupOutput(s.output, "cannot enumerate parallel-worker databases: %v\n", err)
		}
		return fmt.Errorf("connecting to %s for legacy database cleanup: %w", engine, err)
	}

	pattern := `%\_` + EscapeLikePattern(suffix)
	candidates, err := client.ListDatabases(pattern)
	if err != nil {
		if opts.DryRun {
			return writeDatabaseCleanupOutput(s.output, "cannot enumerate parallel-worker databases: %v\n", err)
		}
		return fmt.Errorf("listing legacy databases for suffix %q: %w", suffix, err)
	}
	targets := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if config.IsValidDatabaseIdentifier(candidate) && strings.HasSuffix(candidate, "_"+suffix) {
			targets = append(targets, candidate)
		}
	}
	if opts.DryRun {
		return printDryRunTargets(s.output, targets)
	}
	return errors.Join(dropDatabaseTargets(client, targets, opts, s.output)...)
}

func dropDatabaseTargets(client DatabaseClient, targets []string, opts types.StepOptions, output io.Writer) []error {
	var dropErrors []error
	for _, database := range targets {
		if err := client.DropDatabase(database); err != nil {
			dropErrors = append(dropErrors, fmt.Errorf("dropping database %q: %w", database, err))
			continue
		}
		if opts.Verbose {
			if err := writeDatabaseCleanupOutput(output, "  Dropped database: %s\n", database); err != nil {
				dropErrors = append(dropErrors, err)
			}
		}
	}
	return dropErrors
}

func printDryRunTargets(output io.Writer, targets []string) error {
	var outputErrors []error
	for _, database := range targets {
		if err := writeDatabaseCleanupOutput(output, "Would drop database: %s\n", database); err != nil {
			outputErrors = append(outputErrors, err)
		}
	}
	return errors.Join(outputErrors...)
}

func writeDatabaseCleanupOutput(output io.Writer, format string, args ...any) error {
	if _, err := fmt.Fprintf(output, format, args...); err != nil {
		return fmt.Errorf("writing database cleanup output: %w", err)
	}
	return nil
}
