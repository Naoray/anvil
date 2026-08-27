package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/ui"
	"github.com/naoray/anvil/internal/utils"
)

// legacyExecHint is the locked fail-safe contract text for worktrees without a
// recorded testing database. exec never creates databases or touches
// .anvil.local, so the message spells out the safe remediation paths.
const legacyExecHint = `no testing database is recorded in .anvil.local — 'anvil exec' is available for
worktrees scaffolded with Anvil v1.8 or later.
Options:
  - create a new worktree ('anvil work <branch>') to get an isolated test database, or
  - configure a scaffold override containing ONLY a 'db.create' step with 'role: testing'
    and run that in this worktree.
anvil exec never creates or modifies databases or .anvil.local.
Do NOT rerun the full default Laravel scaffold here if this worktree's database
holds data you care about — it runs 'migrate:fresh' and would wipe it.`

const sharedDbExecHint = `this worktree appears to use a shared database; shared-database worktrees do not support 'anvil exec'.`

const sqliteExecHint = `SQLite databases are per-worktree files; the worktree is already isolated and 'anvil exec' is not needed.`

// These are the environment variables anvil exec owns: inherited values are
// dropped and replaced with the worktree's recorded databases.
const (
	execDatabaseEnvKey     = "DB_DATABASE"
	execAppDatabaseEnvKey  = "ANVIL_DB_DATABASE"
	execTestDatabaseEnvKey = "ANVIL_TEST_DB_DATABASE"
)

var execCmd = &cobra.Command{
	Use:   "exec [--] COMMAND [ARGS...]",
	Short: "Run a command with the worktree's test database exported",
	Long: `Run a command with the worktree's isolated test database exported.

Exports DB_DATABASE and ANVIL_TEST_DB_DATABASE set to the worktree's testing
database (and ANVIL_DB_DATABASE set to the application database when known),
then executes COMMAND. The child's exit code is passed through.

anvil exec is strictly read-only: it never creates or modifies databases or
.anvil.local.`,
	Args: func(cmd *cobra.Command, args []string) error {
		err := cobra.MinimumNArgs(1)(cmd, args)
		if err != nil {
			ui.PrintError(err.Error())
		}
		return err
	},
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			ui.PrintError(err.Error())
			return err
		}
		root, err := findWorktreeRoot(cwd)
		if err != nil {
			ui.PrintError(err.Error())
			return err
		}
		appDb, testDb, err := resolveExecDatabases(root)
		if err != nil {
			ui.PrintError(err.Error())
			return err
		}
		env := buildExecEnv(os.Environ(), appDb, testDb, runtime.GOOS == "windows")
		return runChild(args, env)
	},
}

func init() {
	// Child flags pass through without "--": anvil exec ./script.sh --parallel.
	execCmd.Flags().SetInterspersed(false)
	execCmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		ui.PrintError(err.Error())
		return err
	})
	rootCmd.AddCommand(execCmd)
}

// findWorktreeRoot walks up from start to the first directory containing
// .anvil.local. Stat errors other than not-exist are surfaced, never treated
// as not-found.
func findWorktreeRoot(start string) (string, error) {
	dir := start
	for {
		statePath := filepath.Join(dir, config.LocalStateFile)
		info, err := os.Stat(statePath)
		if err == nil {
			if !info.IsDir() {
				return dir, nil
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("checking %s: %w", statePath, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .anvil.local found in %s or any parent directory (run 'anvil scaffold' in the worktree first)", start)
		}
		dir = parent
	}
}

// resolveExecDatabases reads the worktree's owned-database records and returns
// the application (may be empty) and testing database names. It is strictly
// read-only; the diagnostic .env read only refines error messages.
func resolveExecDatabases(worktreeRoot string) (string, string, error) {
	state, err := config.ReadLocalState(worktreeRoot)
	if err != nil {
		return "", "", fmt.Errorf("reading %s: %w", config.LocalStateFile, err)
	}
	envValues := utils.ReadEnvFile(worktreeRoot, ".env")

	// SQLite worktrees are already isolated; this precise refusal takes
	// precedence over the generic record validator.
	sqliteRecorded := false
	for _, database := range state.Databases {
		if strings.EqualFold(database.Engine, "sqlite") {
			sqliteRecorded = true
			break
		}
	}
	if sqliteRecorded || strings.EqualFold(envValues["DB_CONNECTION"], "sqlite") {
		return "", "", errors.New(sqliteExecHint) //nolint:staticcheck // ST1005: locked user-facing CLI contract wording
	}

	if err := config.ValidateOwnedDatabases(state.Databases); err != nil {
		return "", "", err
	}

	appDb := ""
	testDb := ""
	for _, database := range state.Databases {
		switch database.Role {
		case config.DbRoleApplication:
			appDb = database.Name
		case config.DbRoleTesting:
			testDb = database.Name
		case config.DbRoleAuxiliary:
			// Auxiliary records are cleanup-only and never select exec databases.
		}
	}

	if testDb == "" {
		message := legacyExecHint
		if dbName := envValues["DB_DATABASE"]; dbName != "" && state.DbSuffix != "" && !strings.HasSuffix(dbName, "_"+state.DbSuffix) {
			message += "\n" + sharedDbExecHint
		}
		return "", "", errors.New(message) //nolint:staticcheck // ST1005: locked user-facing CLI contract wording
	}

	return appDb, testDb, nil
}

// buildExecEnv returns environ with the managed database variables replaced by
// the worktree's values. caseInsensitiveKeys matches Windows environment
// semantics, where DB_DATABASE and db_database are the same variable.
func buildExecEnv(environ []string, appDb, testDb string, caseInsensitiveKeys bool) []string {
	out := make([]string, 0, len(environ)+3)
	for _, entry := range environ {
		key, _, found := strings.Cut(entry, "=")
		if found && isManagedExecEnvKey(key, caseInsensitiveKeys) {
			continue
		}
		out = append(out, entry)
	}
	out = append(out, execDatabaseEnvKey+"="+testDb, execTestDatabaseEnvKey+"="+testDb)
	if appDb != "" {
		out = append(out, execAppDatabaseEnvKey+"="+appDb)
	}
	return out
}

func isManagedExecEnvKey(key string, caseInsensitive bool) bool {
	return key == execDatabaseEnvKey ||
		key == execAppDatabaseEnvKey ||
		key == execTestDatabaseEnvKey ||
		(caseInsensitive && (strings.EqualFold(key, execDatabaseEnvKey) ||
			strings.EqualFold(key, execAppDatabaseEnvKey) ||
			strings.EqualFold(key, execTestDatabaseEnvKey)))
}
