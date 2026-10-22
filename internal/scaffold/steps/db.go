package steps

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/dblifecycle"
	"github.com/naoray/anvil/internal/scaffold/types"
	"github.com/naoray/anvil/internal/scaffold/words"
	"github.com/naoray/anvil/internal/utils"
)

// detectDatabaseEngine determines the database engine from explicit type or .env file.
// This is shared between DbCreateStep and DbDestroyStep.
func detectDatabaseEngine(dbType string, ctx *types.ScaffoldContext) (config.DatabaseEngine, error) {
	if dbType != "" {
		return config.ParseDatabaseType(dbType)
	}

	env := utils.ReadEnvFile(ctx.WorktreePath, ".env")
	if engine, ok := config.DatabaseConnectionEngine(env["DB_CONNECTION"]); ok {
		return engine, nil
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
	if config.IsMariaDBType(connection) {
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
		clientFactory: managedDatabaseFactory(config.SiteDriverYerd, nil),
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

func (s *DbCreateStep) Condition(ctx *types.ScaffoldContext) (bool, error) {
	return true, nil
}

func (s *DbCreateStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) (runErr error) {
	databaseArgs, err := parseDatabaseArgs(s.args)
	if err != nil {
		return err
	}

	engine, err := detectDatabaseEngine(s.dbType, ctx)
	if err != nil && opts.DryRun {
		connection := utils.ReadEnvFile(ctx.WorktreePath, ".env.example")["DB_CONNECTION"]
		switch connection {
		case "mysql", "pgsql", "sqlite":
			engine = config.DatabaseEngine(connection)
			err = nil
		case "mariadb":
			engine = config.DBEngineMySQL
			err = nil
		}
	}
	if err != nil {
		// An explicit but unsupported type is a configuration error; only a
		// project without any engine declaration skips database creation.
		if s.dbType != "" {
			return err
		}
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

	if !ctx.DatabaseLockHeld {
		release, err := dblifecycle.Lock(ctx.WorktreePath)
		if err != nil {
			return fmt.Errorf("locking database local state: %w", err)
		}
		ctx.DatabaseLockHeld = true
		defer func() { ctx.DatabaseLockHeld = false; runErr = errors.Join(runErr, release()) }()
	}
	if role == config.DbRoleApplication {
		if handled, err := s.borrowApplication(ctx, opts); handled || err != nil {
			return err
		}
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
			if opts.DryRun && len(env) == 0 {
				env = utils.ReadEnvFile(ctx.WorktreePath, ".env.example")
			}
			dbName = env["DB_DATABASE"]
		}
		if dbName == "" {
			dbName = "database/database.sqlite"
		}
		return s.createSqlite(ctx, dbName, opts)
	}

	if opts.DryRun {
		name := words.BuildDatabaseName(s.getPrefixOrSiteName(ctx, databaseArgs), ctx.GetDbSuffix(), 0)
		if role == config.DbRoleTesting {
			name = words.BuildTestDatabaseName(s.getPrefixOrSiteName(ctx, databaseArgs), ctx.GetDbSuffix())
			ctx.SetVar("TestDatabaseName", name)
		} else {
			ctx.SetVar("DatabaseName", name)
		}
		client, err := s.clientFactory(string(engine), databaseArgs.connectionOptions(ctx, s.dbType))
		if err != nil {
			return err
		}
		defer func() { runErr = errors.Join(runErr, client.Close()) }()
		names, err := client.ListDatabases(EscapeLikePattern(name))
		if err != nil {
			return fmt.Errorf("checking dry-run database existence: %w", err)
		}
		present := false
		for _, candidate := range names {
			if candidate == name {
				present = true
			}
		}
		ctx.SetVar("DatabaseNew:"+name, fmt.Sprint(!present))
		if role == config.DbRoleApplication {
			ctx.SetVar("ApplicationDatabaseNew", fmt.Sprint(!present))
		}
		fmt.Printf("Would ensure database %s role=%s engine=%s\n", name, role, engine)
		return nil
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

// databaseUnavailableError reports that a configured server engine could not
// be reached. The step fails so scaffolding never reports a worktree ready
// without the database it declared; the hint stays on the first line because
// preparation records keep only that line.
func databaseUnavailableError(engine config.DatabaseEngine, role string, err error) error {
	const hint = "start the database service or check its connection settings"
	var managedServiceErr *managedServiceError
	if errors.As(err, &managedServiceErr) {
		return fmt.Errorf("preparing managed database service for %s database (%s role; %s): %w", engine, role, hint, err)
	}
	return fmt.Errorf("connecting to %s database (%s role; %s): %w", engine, role, hint, err)
}

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
		return databaseUnavailableError(engine, config.DbRoleApplication, err)
	}

	endpoint, err := verifiedClientEndpoint(client, string(engine), ctx.WorktreePath, nil)
	if err != nil {
		return err
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

		existing, err := client.ListDatabases(EscapeLikePattern(dbName))
		if err != nil {
			return fmt.Errorf("checking pre-create database existence: %w", err)
		}
		wasPresent := false
		for _, name := range existing {
			if name == dbName {
				wasPresent = true
			}
		}
		ctx.SetVar("ApplicationDatabaseNew", fmt.Sprint(!wasPresent))
		if wasPresent {
			decision, err := s.classifyDatabaseCollision(ctx, dbName, engine, config.DbRoleApplication)
			if err != nil {
				return err
			}
			switch decision {
			case databaseCollisionKnown:
				// Confirmed ownership allows the adapter's idempotent create path.
			case databaseCollisionAuthoritativeUnowned:
				return fmt.Errorf("database %q already exists but is not owned by this worktree", dbName)
			case databaseCollisionFreshUnknown:
				ctx.SetDbSuffix("")
				lastErr = &DatabaseExistsError{Name: dbName}
				continue
			}
		}

		if err := checkRecordedEndpoint(ctx.WorktreePath, dbName, endpoint, !wasPresent); err != nil {
			return err
		}
		err = client.CreateDatabase(dbName)
		if err == nil {
			if opts.Verbose {
				fmt.Printf("  Database '%s' created successfully.\n", dbName)
			}
			return s.persistOwnedDatabase(ctx, dbName, engine, config.DbRoleApplication, endpoint)
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
			ctx.SetVar("ApplicationDatabaseNew", "false")
			return s.persistOwnedDatabase(ctx, dbName, engine, config.DbRoleApplication, endpoint)
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
	// Scaffolding assigns a suffix before any step runs, so a missing one is
	// an inconsistent state rather than an opt-out: skipping would report the
	// worktree ready without its testing database.
	suffix := ctx.GetDbSuffix()
	if suffix == "" {
		return fmt.Errorf("cannot create %s testing database: no database suffix is assigned to this worktree", engine)
	}

	dbName := words.BuildTestDatabaseName(s.getPrefixOrSiteName(ctx, databaseArgs), suffix)
	client, err := s.clientFactory(string(engine), databaseArgs.connectionOptions(ctx, s.dbType))
	if err != nil {
		return fmt.Errorf("creating database client: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, client.Close()) }()

	if err := client.Ping(); err != nil {
		return databaseUnavailableError(engine, config.DbRoleTesting, err)
	}
	endpoint, err := verifiedClientEndpoint(client, string(engine), ctx.WorktreePath, nil)
	if err != nil {
		return err
	}

	if opts.DryRun {
		if opts.Verbose {
			fmt.Printf("  Would create testing database: %s\n", dbName)
		}
		return nil
	}

	existing, err := client.ListDatabases(EscapeLikePattern(dbName))
	if err != nil {
		return fmt.Errorf("checking testing database existence: %w", err)
	}
	present := false
	for _, name := range existing {
		if name == dbName {
			present = true
		}
	}
	if present {
		decision, err := s.classifyDatabaseCollision(ctx, dbName, engine, config.DbRoleTesting)
		if err != nil {
			return err
		}
		if decision != databaseCollisionKnown {
			return fmt.Errorf("database %q already exists but is not owned by this worktree", dbName)
		}
	}
	ctx.SetVar("DatabaseNew:"+dbName, fmt.Sprint(!present))
	if err := checkRecordedEndpoint(ctx.WorktreePath, dbName, endpoint, !present); err != nil {
		return err
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
	return s.persistOwnedDatabase(ctx, dbName, engine, config.DbRoleTesting, endpoint)
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

func (s *DbCreateStep) persistOwnedDatabase(ctx *types.ScaffoldContext, name string, engine config.DatabaseEngine, role string, endpoint config.DatabaseEndpoint) error {
	switch role {
	case config.DbRoleApplication:
		ctx.SetVar("DatabaseName", name)
	case config.DbRoleTesting:
		ctx.SetVar("TestDatabaseName", name)
	}
	var recorded *config.DatabaseEndpoint
	isNew := ctx.GetVar("DatabaseNew:"+name) == "true" || role == config.DbRoleApplication && ctx.GetVar("ApplicationDatabaseNew") == "true"
	if isNew {
		recorded = &endpoint
	} else {
		state, err := config.ReadLocalState(ctx.WorktreePath)
		if err != nil {
			return err
		}
		for _, db := range state.Databases {
			if db.Name == name {
				recorded = db.Endpoint
			}
		}
	}
	if err := config.WriteLocalState(ctx.WorktreePath, config.LocalState{
		DbSuffix: ctx.GetDbSuffix(),
		Databases: []config.OwnedDatabase{{
			Name: name, Engine: string(engine), Role: role, Endpoint: recorded,
		}},
	}); err != nil {
		return fmt.Errorf("recording ownership of database %q: %w", name, err)
	}
	return nil
}

func (s *DbCreateStep) createSqlite(ctx *types.ScaffoldContext, dbName string, opts types.StepOptions) (runErr error) {
	if dbName == "" || !filepath.IsLocal(dbName) || strings.Contains(dbName, ":") {
		return fmt.Errorf("SQLite database must be a relative workspace file path: %q", dbName)
	}
	for _, part := range strings.Split(filepath.ToSlash(dbName), "/") {
		if part == ".." {
			return fmt.Errorf("SQLite database path cannot contain traversal: %q", dbName)
		}
	}
	dbName = filepath.Clean(dbName)
	if dbName == "." {
		return fmt.Errorf("SQLite database path must name a file")
	}
	root, err := os.OpenRoot(ctx.WorktreePath)
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, root.Close()) }()
	existingFile := false
	parts := strings.Split(dbName, string(filepath.Separator))
	for i := range parts {
		path := filepath.Join(parts[:i+1]...)
		info, err := root.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			if i == len(parts)-1 {
				existingFile = true
			}
			if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) || (i == len(parts)-1 && !info.Mode().IsRegular()) {
				return fmt.Errorf("unsafe SQLite path %q: symlinks and special files are unsupported", path)
			}
		}
		if i < len(parts)-1 && os.IsNotExist(err) && !opts.DryRun {
			if err := root.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
				return err
			}
		}
	}
	ctx.SetVar("DatabaseName", dbName)
	ctx.SetVar("ApplicationDatabaseNew", fmt.Sprint(!existingFile))
	if opts.DryRun {
		return nil
	}
	file, err := root.OpenFile(dbName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if os.IsExist(err) {
		ctx.SetVar("ApplicationDatabaseNew", "false")
		return nil
	}
	if err != nil {
		return fmt.Errorf("creating SQLite file: %w", err)
	}
	ctx.SetVar("ApplicationDatabaseNew", "true")
	return file.Close()
}

type DbDestroyStep struct {
	name          string
	args          []string
	dbType        string
	clientFactory DatabaseClientFactory
	output        io.Writer
}

func NewDbDestroyStep(cfg config.StepConfig) *DbDestroyStep {
	return NewDbDestroyStepWithFactoryAndWriter(cfg, managedDatabaseFactory(config.SiteDriverYerd, nil), os.Stdout)
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

func (s *DbDestroyStep) Condition(ctx *types.ScaffoldContext) (bool, error) {
	return true, nil
}

func (s *DbDestroyStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) (runErr error) {
	databaseArgs, err := parseDatabaseArgs(s.args)
	if err != nil {
		return err
	}

	if !ctx.DatabaseLockHeld {
		release, err := dblifecycle.Lock(ctx.WorktreePath)
		if err != nil {
			return err
		}
		defer func() { runErr = errors.Join(runErr, release()) }()
	}
	if err := dblifecycle.CheckCleanup(ctx.WorktreePath); err != nil {
		return err
	}

	localState, err := config.ReadLocalState(ctx.WorktreePath)
	if err != nil {
		return fmt.Errorf("reading local state for database cleanup: %w", err)
	}
	if len(localState.Databases) == 0 && localState.BorrowedApplication != nil {
		return nil
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

func (s *DbDestroyStep) destroyOwnedDatabases(ctx *types.ScaffoldContext, databases []config.OwnedDatabase, databaseArgs databaseStepArgs, opts types.StepOptions) error {
	groups := make(map[config.DatabaseEndpoint][]config.OwnedDatabase)
	var order []config.DatabaseEndpoint
	for _, db := range databases {
		if db.Endpoint == nil {
			return fmt.Errorf("database %q has unknown legacy endpoint identity; use --keep-db and inspect it manually", db.Name)
		}
		if err := db.Endpoint.Validate(db.Engine); err != nil {
			return err
		}
		if _, ok := groups[*db.Endpoint]; !ok {
			order = append(order, *db.Endpoint)
		}
		groups[*db.Endpoint] = append(groups[*db.Endpoint], db)
	}
	for _, endpoint := range order {
		if err := s.destroyOwnedGroup(ctx, groups[endpoint], databaseArgs, opts, endpoint); err != nil {
			return err
		}
	}
	return nil
}

func (s *DbDestroyStep) destroyOwnedGroup(
	ctx *types.ScaffoldContext,
	databases []config.OwnedDatabase,
	databaseArgs databaseStepArgs,
	opts types.StepOptions,
	endpoint config.DatabaseEndpoint,
) (runErr error) {
	selection, err := selectOwnedTargets(databases)
	if err != nil {
		return err
	}

	connection := databaseArgs.connectionOptions(ctx, s.dbType)
	connection.Endpoint = &endpoint
	client, err := s.clientFactory(string(selection.engine), connection)
	if err != nil {
		if opts.DryRun {
			return s.reportDryRun(ctx, selection.exact, fmt.Sprintf("cannot enumerate parallel-worker databases: %v", err))
		}
		return fmt.Errorf("creating database cleanup client: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, client.Close()) }()

	if opts.DryRun {
		// A preview never starts a service (Ping may); it enumerates only a
		// service that is already running.
		if warning := dryRunServiceWarning(client, "parallel-worker databases"); warning != "" {
			return s.reportDryRun(ctx, selection.exact, warning)
		}
	} else if err := client.Ping(); err != nil {
		return fmt.Errorf("connecting to %s for database cleanup: %w", selection.engine, err)
	}

	if _, err := verifiedClientEndpoint(client, string(selection.engine), ctx.WorktreePath, &endpoint); err != nil {
		return err
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
				warning := fmt.Sprintf("cannot enumerate parallel-worker databases: %v", listErr)
				ctx.CleanupReport.Warn(warning)
				if outputErr := writeDatabaseCleanupOutput(s.output, "%s\n", warning); outputErr != nil {
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
		return errors.Join(append(runtimeErrors, s.reportDryRun(ctx, targets, ""))...)
	}
	runtimeErrors = append(runtimeErrors, dropDatabaseTargets(ctx, client, targets, opts, s.output)...)
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
			return s.reportDryRun(ctx, nil, fmt.Sprintf("cannot enumerate parallel-worker databases: %v", err))
		}
		return fmt.Errorf("creating legacy database cleanup client: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, client.Close()) }()

	if opts.DryRun {
		if warning := dryRunServiceWarning(client, "legacy databases"); warning != "" {
			return s.reportDryRun(ctx, nil, warning)
		}
	} else if err := client.Ping(); err != nil {
		return fmt.Errorf("connecting to %s for legacy database cleanup: %w", engine, err)
	}

	pattern := `%\_` + EscapeLikePattern(suffix)
	candidates, err := client.ListDatabases(pattern)
	if err != nil {
		if opts.DryRun {
			return s.reportDryRun(ctx, nil, fmt.Sprintf("cannot enumerate parallel-worker databases: %v", err))
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
		return s.reportDryRun(ctx, targets, "")
	}
	return errors.Join(dropDatabaseTargets(ctx, client, targets, opts, s.output)...)
}

// dryRunServiceWarning probes without starting the service and explains why
// enumeration is skipped; empty means the service is running.
func dryRunServiceWarning(client DatabaseClient, what string) string {
	running, err := client.Running()
	if err != nil {
		return fmt.Sprintf("%s not enumerated: cannot check service: %v", what, err)
	}
	if !running {
		return what + " not enumerated: service not running"
	}
	return ""
}

// reportDryRun prints and reports the exact dry-run targets plus an optional
// warning.
func (s *DbDestroyStep) reportDryRun(ctx *types.ScaffoldContext, targets []string, warning string) error {
	ctx.CleanupReport.AddDatabases(targets...)
	err := printDryRunTargets(s.output, targets)
	if warning != "" {
		ctx.CleanupReport.Warn(warning)
		err = errors.Join(err, writeDatabaseCleanupOutput(s.output, "%s\n", warning))
	}
	return err
}

func dropDatabaseTargets(ctx *types.ScaffoldContext, client DatabaseClient, targets []string, opts types.StepOptions, output io.Writer) []error {
	var dropErrors []error
	for _, database := range targets {
		if err := client.DropDatabase(database); err != nil {
			dropErrors = append(dropErrors, fmt.Errorf("dropping database %q: %w", database, err))
			continue
		}
		ctx.CleanupReport.AddDatabases(database)
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
