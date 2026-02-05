package steps

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/artisanexperiences/arbor/internal/config"
	"github.com/artisanexperiences/arbor/internal/scaffold/types"
	"github.com/artisanexperiences/arbor/internal/scaffold/words"
	"github.com/artisanexperiences/arbor/internal/utils"
)

type DbCreateStep struct {
	name          string
	args          []string
	dbType        string
	clientFactory DatabaseClientFactory
}

func NewDbCreateStep(cfg config.StepConfig) *DbCreateStep {
	return &DbCreateStep{
		name:          "db.create",
		args:          cfg.Args,
		dbType:        cfg.Type,
		clientFactory: DefaultDatabaseClientFactory,
	}
}

func NewDbCreateStepWithFactory(cfg config.StepConfig, factory DatabaseClientFactory) *DbCreateStep {
	return &DbCreateStep{
		name:          "db.create",
		args:          cfg.Args,
		dbType:        cfg.Type,
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
	engine, err := s.detectEngine(ctx)
	if err != nil {
		if opts.Verbose {
			fmt.Printf("  %v\n", err)
		}
		return nil
	}

	if opts.Verbose {
		fmt.Printf("  Creating database (%s)...\n", engine)
	}

	if engine == "sqlite" {
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

	return s.createWithRetry(ctx, engine, opts)
}

func (s *DbCreateStep) detectEngine(ctx *types.ScaffoldContext) (string, error) {
	if s.dbType != "" {
		switch s.dbType {
		case "mysql", "pgsql", "sqlite":
			return s.dbType, nil
		default:
			return "", fmt.Errorf("unsupported database type: %s", s.dbType)
		}
	}

	env := utils.ReadEnvFile(ctx.WorktreePath, ".env")
	if conn := env["DB_CONNECTION"]; conn != "" {
		switch conn {
		case "mysql", "mariadb":
			return "mysql", nil
		case "pgsql", "postgres", "postgresql":
			return "pgsql", nil
		case "sqlite":
			return "sqlite", nil
		}
	}

	return "", fmt.Errorf("database type not specified and DB_CONNECTION not found in .env")
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

func (s *DbCreateStep) createWithRetry(ctx *types.ScaffoldContext, engine string, opts types.StepOptions) error {
	siteName := s.getPrefixOrSiteName(ctx)
	dbOpts := s.parseConnectionOptions()

	client, err := s.clientFactory(engine, dbOpts)
	if err != nil {
		return fmt.Errorf("creating database client: %w", err)
	}
	defer func() { _ = client.Close() }()

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
			dbName = fmt.Sprintf("%s_%s", words.SanitizeSiteName(siteName), suffix)
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
			if err := s.persistDbSuffix(ctx); err != nil {
				if opts.Verbose {
					fmt.Printf("  warning: failed to persist db_suffix: %v\n", err)
				}
			}
			return nil
		}

		if !IsDatabaseExistsError(err) {
			return fmt.Errorf("failed to create database: %w", err)
		}

		if opts.Verbose {
			fmt.Printf("  Database '%s' already exists, retrying...\n", dbName)
		}
		ctx.SetDbSuffix("")
		lastErr = err
	}

	return fmt.Errorf("failed to create database after %d attempts: %w", maxDbCreateRetries, lastErr)
}

func (s *DbCreateStep) persistDbSuffix(ctx *types.ScaffoldContext) error {
	suffix := ctx.GetDbSuffix()
	if suffix == "" {
		return nil
	}

	// Write to .arbor.local instead of arbor.yaml
	return config.WriteLocalState(ctx.WorktreePath, config.LocalState{DbSuffix: suffix})
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
}

func NewDbDestroyStep(cfg config.StepConfig) *DbDestroyStep {
	return &DbDestroyStep{
		name:          "db.destroy",
		args:          cfg.Args,
		dbType:        cfg.Type,
		clientFactory: DefaultDatabaseClientFactory,
	}
}

func NewDbDestroyStepWithFactory(cfg config.StepConfig, factory DatabaseClientFactory) *DbDestroyStep {
	return &DbDestroyStep{
		name:          "db.destroy",
		args:          cfg.Args,
		dbType:        cfg.Type,
		clientFactory: factory,
	}
}

func (s *DbDestroyStep) Name() string {
	return s.name
}

func (s *DbDestroyStep) Condition(ctx *types.ScaffoldContext) bool {
	return true
}

func (s *DbDestroyStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	suffix := ctx.GetDbSuffix()
	if suffix == "" {
		localState, err := config.ReadLocalState(ctx.WorktreePath)
		if err != nil {
			return nil
		}
		suffix = localState.DbSuffix
	}

	if suffix == "" {
		if opts.Verbose {
			fmt.Printf("  No database suffix found, skipping cleanup.\n")
		}
		return nil
	}

	ctx.SetDbSuffix(suffix)

	engine, err := s.detectEngine(ctx)
	if err != nil {
		if opts.Verbose {
			fmt.Printf("  %v\n", err)
		}
		return nil
	}

	if opts.Verbose {
		fmt.Printf("  Cleaning up databases matching suffix: %s\n", suffix)
	}

	if engine == "sqlite" {
		return nil
	}

	return s.destroyDatabases(engine, suffix, opts)
}

func (s *DbDestroyStep) detectEngine(ctx *types.ScaffoldContext) (string, error) {
	if s.dbType != "" {
		switch s.dbType {
		case "mysql", "pgsql", "sqlite":
			return s.dbType, nil
		default:
			return "", fmt.Errorf("unsupported database type: %s", s.dbType)
		}
	}

	env := utils.ReadEnvFile(ctx.WorktreePath, ".env")
	if conn := env["DB_CONNECTION"]; conn != "" {
		switch conn {
		case "mysql", "mariadb":
			return "mysql", nil
		case "pgsql", "postgres", "postgresql":
			return "pgsql", nil
		case "sqlite":
			return "sqlite", nil
		}
	}

	return "", fmt.Errorf("database type not specified and DB_CONNECTION not found in .env")
}

func (s *DbDestroyStep) parseConnectionOptions(engine string) DatabaseOptions {
	opts := DatabaseOptions{
		Host: "127.0.0.1",
	}

	if engine == "pgsql" {
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

func (s *DbDestroyStep) destroyDatabases(engine, suffix string, opts types.StepOptions) error {
	dbOpts := s.parseConnectionOptions(engine)

	client, err := s.clientFactory(engine, dbOpts)
	if err != nil {
		if opts.Verbose {
			fmt.Printf("  Could not create database client: %v\n", err)
		}
		return nil
	}
	defer func() { _ = client.Close() }()

	if err := client.Ping(); err != nil {
		if opts.Verbose {
			fmt.Printf("  Could not connect to %s database: %v\n", engine, err)
		}
		return nil
	}

	pattern := fmt.Sprintf("%%_%s", suffix)
	databases, err := client.ListDatabases(pattern)
	if err != nil {
		if opts.Verbose {
			fmt.Printf("  Failed to list databases: %v\n", err)
		}
		return nil
	}

	if len(databases) == 0 {
		if opts.Verbose {
			fmt.Printf("  No databases matching pattern found.\n")
		}
		return nil
	}

	for _, dbName := range databases {
		if opts.DryRun {
			if opts.Verbose {
				fmt.Printf("  Would drop database: %s\n", dbName)
			}
			continue
		}

		if err := client.DropDatabase(dbName); err != nil {
			if opts.Verbose {
				fmt.Printf("  Failed to drop database %s: %v\n", dbName, err)
			}
			continue
		}

		if opts.Verbose {
			fmt.Printf("  Dropped database: %s\n", dbName)
		}
	}

	return nil
}
