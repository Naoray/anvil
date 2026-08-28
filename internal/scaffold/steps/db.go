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
		case string(config.DBEngineMariaDB):
			return config.DBEngineMySQL, nil
		default:
			return "", fmt.Errorf("unsupported database type: %s", dbType)
		}
	}

	env := utils.ReadEnvFile(ctx.WorktreePath, ".env")
	if conn := env["DB_CONNECTION"]; conn != "" {
		switch conn {
		case "mysql":
			return config.DBEngineMySQL, nil
		case "mariadb":
			return config.DBEngineMySQL, nil
		case "pgsql", "postgres", "postgresql":
			return config.DBEnginePgSQL, nil
		case "sqlite":
			return config.DBEngineSQLite, nil
		}
	}

	return "", fmt.Errorf("database type not specified and DB_CONNECTION not found in .env")
}

type databaseStepArgs struct {
	database   string
	prefix     string
	connection DatabaseOptions
}

func parseDatabaseArgs(args []string) (databaseStepArgs, error) {
	var parsed databaseStepArgs
	seen := make(map[string]struct{}, len(args))

	for i := 0; i < len(args); i++ {
		token := args[i]
		if !strings.HasPrefix(token, "--") {
			return databaseStepArgs{}, fmt.Errorf("invalid database argument %q: expected an option", token)
		}

		optionAndValue := token[2:]
		option, value, hasValue := strings.Cut(optionAndValue, "=")
		if option == "" {
			return databaseStepArgs{}, fmt.Errorf("invalid database argument %q: option name is empty", token)
		}
		switch option {
		case "database", "prefix", "username", "password", "host", "port":
		default:
			return databaseStepArgs{}, fmt.Errorf("invalid database argument %q: unknown option", token)
		}
		if _, exists := seen[option]; exists {
			return databaseStepArgs{}, fmt.Errorf("invalid database argument %q: duplicate option --%s", token, option)
		}
		seen[option] = struct{}{}

		if !hasValue {
			if i+1 >= len(args) {
				return databaseStepArgs{}, fmt.Errorf("database option %q is missing a value", token)
			}
			i++
			value = args[i]
			if strings.HasPrefix(value, "--") {
				return databaseStepArgs{}, fmt.Errorf("database option %q cannot use option-looking value %q", token, value)
			}
		}
		if value == "" && option != "password" {
			return databaseStepArgs{}, fmt.Errorf("database option %q requires a non-empty value", token)
		}

		switch option {
		case "database":
			parsed.database = value
		case "prefix":
			parsed.prefix = value
		case "username":
			parsed.connection.Username = value
		case "password":
			parsed.connection.Password = value
			parsed.connection.PasswordSet = true
		case "host":
			parsed.connection.Host = value
		case "port":
			parsed.connection.Port = value
		}
	}

	return parsed, nil
}

func (args databaseStepArgs) connectionOptions(ctx *types.ScaffoldContext, dbType string) DatabaseOptions {
	options := args.connection
	options.WorktreePath = ctx.WorktreePath
	connection := dbType
	if connection == "" {
		connection = utils.ReadEnvFile(ctx.WorktreePath, ".env")["DB_CONNECTION"]
	}
	if connection == string(config.DBEngineMariaDB) {
		options.Service = string(config.DBEngineMariaDB)
	}
	return options
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
		clientFactory: NewYerdDatabaseClientFactory(nil),
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
	databaseArgs, err := parseDatabaseArgs(s.args)
	if err != nil {
		return err
	}

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
		dbName := databaseArgs.database
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
		return s.createTestDatabase(ctx, engine, databaseArgs, opts)
	}
	return s.createWithRetry(ctx, engine, databaseArgs, opts)
}

func (s *DbCreateStep) getPrefixOrSiteName(ctx *types.ScaffoldContext, databaseArgs databaseStepArgs) string {
	if databaseArgs.prefix != "" {
		return databaseArgs.prefix
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

const maxDbCreateRetries = 5

type databaseCollisionDecision uint8

const (
	databaseCollisionKnown databaseCollisionDecision = iota
	databaseCollisionAuthoritativeUnowned
	databaseCollisionFreshUnknown
)

func (s *DbCreateStep) createWithRetry(
	ctx *types.ScaffoldContext,
	engine config.DatabaseEngine,
	databaseArgs databaseStepArgs,
	opts types.StepOptions,
) (runErr error) {
	siteName := s.getPrefixOrSiteName(ctx, databaseArgs)
	dbOpts := databaseArgs.connectionOptions(ctx, s.dbType)

	client, err := s.clientFactory(string(engine), dbOpts)
	if err != nil {
		return fmt.Errorf("creating database client: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, client.Close()) }()

	if err := client.Ping(); err != nil {
		var managedServiceErr *managedServiceError
		if errors.As(err, &managedServiceErr) {
			return fmt.Errorf("preparing managed database service: %w", err)
		}
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

		decision, err := s.classifyDatabaseCollision(ctx, dbName, engine, config.DbRoleApplication)
		if err != nil {
			return err
		}
		switch decision {
		case databaseCollisionKnown:
			return s.persistOwnedDatabase(ctx, dbName, engine, config.DbRoleApplication)
		case databaseCollisionAuthoritativeUnowned:
			return fmt.Errorf("database %q already exists but is not owned by this worktree", dbName)
		case databaseCollisionFreshUnknown:
			// Fresh, record-free collisions retain the original suffix rotation contract.
		}

		if opts.Verbose {
			fmt.Printf("  Database '%s' already exists, retrying...\n", dbName)
		}
		ctx.SetDbSuffix("")
		lastErr = err
	}

	return fmt.Errorf("failed to create database after %d attempts: %w", maxDbCreateRetries, lastErr)
}

func (s *DbCreateStep) createTestDatabase(
	ctx *types.ScaffoldContext,
	engine config.DatabaseEngine,
	databaseArgs databaseStepArgs,
	opts types.StepOptions,
) (runErr error) {
	suffix := ctx.GetDbSuffix()
	if suffix == "" {
		if opts.Verbose {
			fmt.Printf("  No database suffix found, skipping testing database creation.\n")
		}
		return nil
	}

	dbName := words.BuildTestDatabaseName(s.getPrefixOrSiteName(ctx, databaseArgs), suffix)
	client, err := s.clientFactory(string(engine), databaseArgs.connectionOptions(ctx, s.dbType))
	if err != nil {
		return fmt.Errorf("creating database client: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, client.Close()) }()

	if err := client.Ping(); err != nil {
		var managedServiceErr *managedServiceError
		if errors.As(err, &managedServiceErr) {
			return fmt.Errorf("preparing managed database service: %w", err)
		}
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

	if err := client.CreateDatabase(dbName); err != nil {
		if !IsDatabaseExistsError(err) {
			return fmt.Errorf("failed to create testing database: %w", err)
		}

		decision, err := s.classifyDatabaseCollision(ctx, dbName, engine, config.DbRoleTesting)
		if err != nil {
			return err
		}
		if decision != databaseCollisionKnown {
			return fmt.Errorf("database %q already exists but is not owned by this worktree", dbName)
		}
	}
	return s.persistOwnedDatabase(ctx, dbName, engine, config.DbRoleTesting)
}

func (s *DbCreateStep) classifyDatabaseCollision(
	ctx *types.ScaffoldContext,
	name string,
	engine config.DatabaseEngine,
	role string,
) (databaseCollisionDecision, error) {
	state, err := config.ReadLocalState(ctx.WorktreePath)
	if err != nil {
		return databaseCollisionAuthoritativeUnowned, fmt.Errorf("reading local state for database collision: %w", err)
	}
	if len(state.Databases) > 0 {
		if err := config.ValidateOwnedDatabases(state.Databases); err != nil {
			return databaseCollisionAuthoritativeUnowned, fmt.Errorf("validating owned database records: %w", err)
		}

		for _, database := range state.Databases {
			if database.Name != name || database.Engine != string(engine) {
				continue
			}
			if database.Role == role || database.Role == config.DbRoleAuxiliary {
				return databaseCollisionKnown, nil
			}
		}
		if ctx.DbSuffixFromLegacyState() {
			return databaseCollisionKnown, nil
		}
		return databaseCollisionAuthoritativeUnowned, nil
	}

	if ctx.DbSuffixFromLegacyState() {
		return databaseCollisionKnown, nil
	}
	if ctx.DbSuffixFromState() {
		return databaseCollisionAuthoritativeUnowned, nil
	}
	return databaseCollisionFreshUnknown, nil
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
	return NewDbDestroyStepWithFactoryAndWriter(cfg, NewYerdDatabaseClientFactory(nil), os.Stdout)
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
	databaseArgs, err := parseDatabaseArgs(s.args)
	if err != nil {
		return err
	}

	localState, err := config.ReadLocalState(ctx.WorktreePath)
	if err != nil {
		return fmt.Errorf("reading local state for database cleanup: %w", err)
	}
	if len(localState.Databases) > 0 {
		return s.destroyOwnedDatabases(ctx, localState.Databases, databaseArgs, opts)
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
	return s.destroyLegacyDatabases(ctx, engine, suffix, databaseArgs, opts)
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
	for _, database := range databases {
		selection.exact = append(selection.exact, database.Name)
	}
	return selection, nil
}

func (s *DbDestroyStep) destroyOwnedDatabases(
	ctx *types.ScaffoldContext,
	databases []config.OwnedDatabase,
	databaseArgs databaseStepArgs,
	opts types.StepOptions,
) (runErr error) {
	selection, err := selectOwnedTargets(databases)
	if err != nil {
		return err
	}

	client, err := s.clientFactory(
		string(selection.engine),
		databaseArgs.connectionOptions(ctx, s.dbType),
	)
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

func (s *DbDestroyStep) destroyLegacyDatabases(
	ctx *types.ScaffoldContext,
	engine config.DatabaseEngine,
	suffix string,
	databaseArgs databaseStepArgs,
	opts types.StepOptions,
) (runErr error) {
	client, err := s.clientFactory(
		string(engine),
		databaseArgs.connectionOptions(ctx, s.dbType),
	)
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
