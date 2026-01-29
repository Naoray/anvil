package steps

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
	"github.com/michaeldyrynda/arbor/internal/scaffold/words"
	"github.com/michaeldyrynda/arbor/internal/utils"
)

type DbCreateStep struct {
	name     string
	args     []string
	priority int
	dbType   string
}

func NewDbCreateStep(cfg config.StepConfig, priority int) *DbCreateStep {
	return &DbCreateStep{
		name:     "db.create",
		args:     cfg.Args,
		priority: priority,
		dbType:   cfg.Type,
	}
}

func (s *DbCreateStep) Name() string {
	return s.name
}

func (s *DbCreateStep) Priority() int {
	return s.priority
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

const maxDbCreateRetries = 5

func (s *DbCreateStep) createWithRetry(ctx *types.ScaffoldContext, engine string, opts types.StepOptions) error {
	siteName := ctx.SiteName
	if siteName == "" {
		env := utils.ReadEnvFile(ctx.WorktreePath, ".env")
		siteName = env["APP_NAME"]
	}
	if siteName == "" {
		siteName = "app"
	}

	var lastErr error
	for attempt := 0; attempt < maxDbCreateRetries; attempt++ {
		dbName := words.GenerateDatabaseName(siteName, 0)
		suffix := words.ExtractSuffix(dbName)
		ctx.SetDbSuffix(suffix)

		if opts.Verbose {
			fmt.Printf("  Generated database name: %s (attempt %d/%d)\n", dbName, attempt+1, maxDbCreateRetries)
		}

		err := s.createDatabase(ctx, engine, dbName, opts)
		if err == nil {
			if err := s.persistDbSuffix(ctx); err != nil {
				if opts.Verbose {
					fmt.Printf("  warning: failed to persist db_suffix: %v\n", err)
				}
			}
			return nil
		}

		if !isDatabaseExistsError(err) {
			return fmt.Errorf("failed to create database: %w", err)
		}

		lastErr = err
	}

	return fmt.Errorf("failed to create database after %d attempts: %w", maxDbCreateRetries, lastErr)
}

func (s *DbCreateStep) persistDbSuffix(ctx *types.ScaffoldContext) error {
	suffix := ctx.GetDbSuffix()
	if suffix == "" {
		return nil
	}

	if err := config.WriteWorktreeConfig(ctx.WorktreePath, map[string]string{
		"db_suffix": suffix,
	}); err != nil {
		return fmt.Errorf("writing worktree config: %w", err)
	}

	return nil
}

func isDatabaseExistsError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "already exists") ||
		strings.Contains(errStr, "database exists") ||
		strings.Contains(errStr, "1007")
}

func (s *DbCreateStep) createDatabase(ctx *types.ScaffoldContext, engine, dbName string, opts types.StepOptions) error {
	dbUser := "root"
	dbPass := ""
	dbHost := "127.0.0.1"
	dbPort := ""

	for i, arg := range s.args {
		if arg == "--username" && i+1 < len(s.args) {
			dbUser = s.args[i+1]
		}
		if arg == "--password" && i+1 < len(s.args) {
			dbPass = s.args[i+1]
		}
		if arg == "--host" && i+1 < len(s.args) {
			dbHost = s.args[i+1]
		}
		if arg == "--port" && i+1 < len(s.args) {
			dbPort = s.args[i+1]
		}
	}

	if dbPort == "" && engine == "mysql" {
		dbPort = "3306"
	} else if dbPort == "" && engine == "pgsql" {
		dbPort = "5432"
	}

	var createCmd *exec.Cmd
	if engine == "mysql" {
		if _, err := exec.LookPath("mysql"); err == nil {
			createCmd = exec.Command("mysql", "-u", dbUser, "-h", dbHost, "-P", dbPort)
			if dbPass != "" {
				createCmd.Args = append(createCmd.Args, fmt.Sprintf("-p%s", dbPass))
			}
			createCmd.Args = append(createCmd.Args, "-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", dbName))
		}
	} else if engine == "pgsql" {
		if _, err := exec.LookPath("psql"); err == nil {
			env := os.Environ()
			if dbPass != "" {
				env = append(env, fmt.Sprintf("PGPASSWORD=%s", dbPass))
			}
			createCmd = exec.Command("psql", "-U", dbUser, "-h", dbHost, "-p", dbPort, "-c", fmt.Sprintf("CREATE DATABASE \"%s\"", dbName))
			createCmd.Env = env
		}
	}

	if createCmd != nil {
		if opts.Verbose {
			fmt.Printf("  Creating database with: %s\n", createCmd.Path)
		}
		output, err := createCmd.CombinedOutput()
		if err != nil {
			if opts.Verbose {
				fmt.Printf("  Database creation output: %s\n", string(output))
			}
			return fmt.Errorf("could not create database: %w", err)
		}

		if opts.Verbose {
			fmt.Printf("  Database '%s' created successfully.\n", dbName)
		}
	} else {
		if opts.Verbose {
			fmt.Printf("  No %s client found.\n", engine)
		}
		return fmt.Errorf("%s client not found", engine)
	}

	return nil
}

func (s *DbCreateStep) createSqlite(ctx *types.ScaffoldContext, dbName string, opts types.StepOptions) error {
	dbFile := filepath.Join(ctx.WorktreePath, dbName)
	dbDir := filepath.Dir(dbFile)

	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("creating database directory: %w", err)
	}

	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		if opts.Verbose {
			fmt.Printf("  Creating SQLite database: %s\n", dbName)
		}

		file, err := os.Create(dbFile)
		if err != nil {
			return fmt.Errorf("creating SQLite database file: %w", err)
		}
		file.Close()
	} else {
		if opts.Verbose {
			fmt.Printf("  SQLite database already exists: %s\n", dbName)
		}
	}

	return nil
}

type DbDestroyStep struct {
	name   string
	args   []string
	dbType string
}

func NewDbDestroyStep(cfg config.StepConfig) *DbDestroyStep {
	return &DbDestroyStep{
		name:   "db.destroy",
		args:   cfg.Args,
		dbType: cfg.Type,
	}
}

func (s *DbDestroyStep) Name() string {
	return s.name
}

func (s *DbDestroyStep) Priority() int {
	return 0
}

func (s *DbDestroyStep) Condition(ctx *types.ScaffoldContext) bool {
	return true
}

func (s *DbDestroyStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	suffix := ctx.GetDbSuffix()
	if suffix == "" {
		cfg, err := config.ReadWorktreeConfig(ctx.WorktreePath)
		if err != nil {
			return nil
		}
		suffix = cfg.DbSuffix
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

	pattern := fmt.Sprintf("%%_%s", suffix)
	switch engine {
	case "mysql":
		return s.destroyMysqlDatabases(pattern, opts)
	case "pgsql":
		return s.destroyPgsqlDatabases(pattern, opts)
	}

	return nil
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

func (s *DbDestroyStep) destroyMysqlDatabases(pattern string, opts types.StepOptions) error {
	if _, err := exec.LookPath("mysql"); err != nil {
		if opts.Verbose {
			fmt.Printf("  MySQL client not found, skipping database cleanup.\n")
		}
		return nil
	}

	dbUser := "root"
	dbPass := ""
	dbHost := "127.0.0.1"
	dbPort := "3306"

	for i, arg := range s.args {
		if arg == "--username" && i+1 < len(s.args) {
			dbUser = s.args[i+1]
		}
		if arg == "--password" && i+1 < len(s.args) {
			dbPass = s.args[i+1]
		}
		if arg == "--host" && i+1 < len(s.args) {
			dbHost = s.args[i+1]
		}
		if arg == "--port" && i+1 < len(s.args) {
			dbPort = s.args[i+1]
		}
	}

	listCmd := exec.Command("mysql", "-u", dbUser, "-h", dbHost, "-P", dbPort, "-e", fmt.Sprintf("SHOW DATABASES LIKE '%s'", pattern))
	if dbPass != "" {
		listCmd.Args = append(listCmd.Args, fmt.Sprintf("-p%s", dbPass))
	}

	output, err := listCmd.CombinedOutput()
	if err != nil {
		if opts.Verbose {
			fmt.Printf("  Failed to list databases: %v\n", err)
		}
		return nil
	}

	lines := strings.Split(string(output), "\n")
	databases := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && line != pattern {
			databases = append(databases, line)
		}
	}

	if len(databases) == 0 {
		if opts.Verbose {
			fmt.Printf("  No databases matching pattern found.\n")
		}
		return nil
	}

	for _, dbName := range databases {
		dropCmd := exec.Command("mysql", "-u", dbUser, "-h", dbHost, "-P", dbPort, "-e", fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))
		if dbPass != "" {
			dropCmd.Args = append(dropCmd.Args, fmt.Sprintf("-p%s", dbPass))
		}

		if err := dropCmd.Run(); err != nil {
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

func (s *DbDestroyStep) destroyPgsqlDatabases(pattern string, opts types.StepOptions) error {
	if _, err := exec.LookPath("psql"); err != nil {
		if opts.Verbose {
			fmt.Printf("  PostgreSQL client not found, skipping database cleanup.\n")
		}
		return nil
	}

	dbUser := "postgres"
	dbPass := ""
	dbHost := "127.0.0.1"
	dbPort := "5432"

	for i, arg := range s.args {
		if arg == "--username" && i+1 < len(s.args) {
			dbUser = s.args[i+1]
		}
		if arg == "--password" && i+1 < len(s.args) {
			dbPass = s.args[i+1]
		}
		if arg == "--host" && i+1 < len(s.args) {
			dbHost = s.args[i+1]
		}
		if arg == "--port" && i+1 < len(s.args) {
			dbPort = s.args[i+1]
		}
	}

	env := os.Environ()
	if dbPass != "" {
		env = append(env, fmt.Sprintf("PGPASSWORD=%s", dbPass))
	}

	query := fmt.Sprintf("SELECT datname FROM pg_database WHERE datname LIKE '%s' AND datistemplate = false", pattern)
	listCmd := exec.Command("psql", "-U", dbUser, "-h", dbHost, "-p", dbPort, "-t", "-c", query)
	listCmd.Env = env

	output, err := listCmd.CombinedOutput()
	if err != nil {
		if opts.Verbose {
			fmt.Printf("  Failed to list databases: %v\n", err)
		}
		return nil
	}

	lines := strings.Split(string(output), "\n")
	databases := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			databases = append(databases, line)
		}
	}

	if len(databases) == 0 {
		if opts.Verbose {
			fmt.Printf("  No databases matching pattern found.\n")
		}
		return nil
	}

	for _, dbName := range databases {
		dropCmd := exec.Command("psql", "-U", dbUser, "-h", dbHost, "-p", dbPort, "-c", fmt.Sprintf("DROP DATABASE IF EXISTS \"%s\"", dbName))
		dropCmd.Env = env

		if err := dropCmd.Run(); err != nil {
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
