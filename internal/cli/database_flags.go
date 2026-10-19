package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/naoray/anvil/internal/dblifecycle"

	"github.com/spf13/cobra"
)

func addDatabaseFlags(cmd *cobra.Command) {
	cmd.Flags().StringArray("db-action", nil, "Database action: application|testing|EXACT_NAME=skip|migrate (repeatable)")
	cmd.Flags().StringArray("reset-database", nil, "Reset and seed an existing exact owned database name (repeatable)")
	cmd.Flags().String("borrow-app-db", "", "Borrow the application database of a registered worktree; testing stays isolated")
}
func applyDatabaseFlags(cmd *cobra.Command, pc *ProjectContext) error {
	actions, err := cmd.Flags().GetStringArray("db-action")
	if err != nil {
		return err
	}
	merged := make(map[string]string, len(pc.Config.Scaffold.DatabaseActions)+len(actions))
	for k, v := range pc.Config.Scaffold.DatabaseActions {
		merged[k] = v
	}
	seen := map[string]bool{}
	for _, value := range actions {
		selector, action, ok := strings.Cut(value, "=")
		if !ok || selector == "" || (action != "skip" && action != "migrate") {
			return fmt.Errorf("invalid --db-action %q; use SELECTOR=skip|migrate", value)
		}
		if seen[selector] {
			return fmt.Errorf("duplicate --db-action selector %q", selector)
		}
		seen[selector] = true
		merged[selector] = action
	}
	pc.Config.Scaffold.DatabaseActions = merged
	pc.Config.Scaffold.ResetDatabases, err = cmd.Flags().GetStringArray("reset-database")
	if err != nil {
		return err
	}
	source, err := cmd.Flags().GetString("borrow-app-db")
	if err != nil {
		return err
	}
	if source != "" {
		if info, statErr := os.Stat(source); statErr == nil && info.IsDir() {
			source, err = dblifecycle.Canonical(source)
		} else {
			source, err = findWorktreePath(pc.GitDir, source)
		}
		if err != nil {
			return fmt.Errorf("resolving borrow source: %w", err)
		}
	}

	pc.Config.Scaffold.BorrowAppDB = source
	pc.Config.Scaffold.DatabaseDriver = string(pc.GlobalConfig.ResolveSiteDriver(exec.LookPath))
	return nil
}
