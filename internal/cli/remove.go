package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/config"
	anvilerrors "github.com/naoray/anvil/internal/errors"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/scaffold"
	"github.com/naoray/anvil/internal/ui"
)

func planRemoveCleanup(
	state *config.LocalState,
	stateErr error,
	keepDB, dryRun, force bool,
) (scaffold.CleanupOptions, []string, error) {
	opts := scaffold.CleanupOptions{DryRun: dryRun}
	if stateErr != nil {
		if !force {
			return opts, nil, fmt.Errorf(
				"cannot read .anvil.local (%v) — it is the only record of this worktree's databases; fix the file, or rerun with --force to remove the worktree anyway (databases will be left untouched and unrecorded)",
				stateErr,
			)
		}
		opts.SkipDatabaseCleanup = true
		return opts, []string{fmt.Sprintf(
			"WARNING: cannot read .anvil.local (%v); proceeding because --force was set; databases will be left untouched and unrecorded",
			stateErr,
		)}, nil
	}
	if state == nil {
		state = &config.LocalState{}
	}
	if err := config.ValidateOwnedDatabases(state.Databases); err != nil {
		if !force {
			return opts, nil, fmt.Errorf(
				"invalid database records in .anvil.local (%v) — no databases were dropped; fix the records, or rerun with --force to remove the worktree anyway (databases will be left untouched and unrecorded)",
				err,
			)
		}
		opts.SkipDatabaseCleanup = true
		return opts, []string{fmt.Sprintf(
			"WARNING: invalid database records in .anvil.local (%v); proceeding because --force was set; databases will be left untouched and unrecorded",
			err,
		)}, nil
	}
	if !keepDB {
		return opts, nil, nil
	}

	opts.SkipDatabaseCleanup = true
	messages := make([]string, 0, 2)
	if len(state.Databases) > 0 {
		names := make([]string, 0, len(state.Databases))
		for _, database := range state.Databases {
			names = append(names, database.Name)
		}
		messages = append(messages, fmt.Sprintf(
			"Preserving databases: %s (parallel worker databases are kept too; drop manually when done)",
			strings.Join(names, ", "),
		))
	} else if state.DbSuffix != "" {
		messages = append(messages, fmt.Sprintf(
			"Preserving databases matching suffix '%s' (legacy worktree — exact names unknown)",
			state.DbSuffix,
		))
	}
	if dryRun {
		messages = append(messages, "[DRY RUN] database cleanup would be skipped (--keep-db)")
	}
	return opts, messages, nil
}

type removeLifecycleOptions struct {
	DryRun  bool
	Verbose bool
	Quiet   bool
}

type removeLifecycleDependencies struct {
	readLocalState  func(string) (*config.LocalState, error)
	scaffoldManager func(*ProjectContext) *scaffold.ScaffoldManager
	detectPreset    func(*ProjectContext, string) string
	removeWorktree  func(string, string, bool) error
	printInfo       func(string)
}

func planWorktreeCleanup(
	worktreePath string,
	keepDB, dryRun, force bool,
	readLocalState func(string) (*config.LocalState, error),
) (scaffold.CleanupOptions, []string, error) {
	state, stateErr := readLocalState(worktreePath)
	return planRemoveCleanup(state, stateErr, keepDB, dryRun, force)
}

func runRemoveLifecycle(
	pc *ProjectContext,
	worktree git.Worktree,
	options removeLifecycleOptions,
	cleanupOpts scaffold.CleanupOptions,
	cleanupMessages []string,
	deps removeLifecycleDependencies,
) error {
	cleanupOpts.DryRun = options.DryRun
	cleanupOpts.Verbose = options.Verbose
	cleanupOpts.Quiet = options.Quiet
	for _, message := range cleanupMessages {
		deps.printInfoMessage(message)
	}

	preset := pc.Config.Preset
	if preset == "" {
		preset = deps.detectPreset(pc, worktree.Path)
	}

	if options.Verbose && preset != "" {
		deps.printInfoMessage(fmt.Sprintf("Running cleanup for preset: %s", preset))
	}

	if preset != "" {
		siteName := worktreeSiteName(worktree.Path, worktree.Branch, pc.DefaultBranch, pc.Config.SiteName)
		if err := deps.scaffoldManager(pc).RunCleanupWithOptions(
			worktree.Path,
			worktree.Branch,
			"",
			siteName,
			preset,
			pc.Config,
			cleanupOpts,
		); err != nil {
			return fmt.Errorf("cleanup for worktree %q: %w", worktree.Branch, err)
		}
	}

	if options.DryRun {
		deps.printInfoMessage(fmt.Sprintf("[DRY RUN] Would remove %s at %s", worktree.Branch, worktree.Path))
		return nil
	}

	if err := deps.removeWorktree(pc.GitDir, worktree.Path, true); err != nil {
		return fmt.Errorf("removing worktree: %w", err)
	}
	return nil
}

type removeCommandDependencies struct {
	openProject      func() (*ProjectContext, error)
	getwd            func() (string, error)
	getDefaultBranch func(string) (string, error)
	listWorktrees    func(string, string, string) ([]git.Worktree, error)
	branchExists     func(string, string) bool
	removeWorktree   func(string, string, bool) error
	deleteBranch     func(string, string, bool) error
	readLocalState   func(string) (*config.LocalState, error)
	scaffoldManager  func(*ProjectContext) *scaffold.ScaffoldManager
	detectPreset     func(*ProjectContext, string) string
	printInfo        func(string)
}

func defaultRemoveCommandDependencies() removeCommandDependencies {
	return removeCommandDependencies{
		openProject:      OpenProjectFromCWD,
		getwd:            os.Getwd,
		getDefaultBranch: git.GetDefaultBranch,
		listWorktrees:    git.ListWorktreesDetailed,
		branchExists:     git.BranchExists,
		removeWorktree:   git.RemoveWorktree,
		deleteBranch:     git.DeleteBranch,
		readLocalState:   config.ReadLocalState,
		scaffoldManager:  func(pc *ProjectContext) *scaffold.ScaffoldManager { return pc.ScaffoldManager() },
		detectPreset:     func(pc *ProjectContext, path string) string { return pc.PresetManager().Detect(path) },
	}
}

var removeCmd = newRemoveCommand(defaultRemoveCommandDependencies())

func newRemoveCommand(deps removeCommandDependencies) *cobra.Command {
	command := &cobra.Command{
		Use:   "remove [FOLDER]",
		Short: "Remove a worktree with cleanup",
		Long: `Removes a worktree and runs preset-defined cleanup steps.

Arguments:
  FOLDER  Name of the worktree folder to remove (e.g., feature-test-change)

Cleanup steps may include:
  - Removing local site links
  - Database cleanup prompts`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeWorktreeNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(cmd, args, deps)
		},
	}
	command.Flags().BoolP("force", "f", false, "Skip confirmation and cleanup prompts")
	command.Flags().Bool("delete-branch", false, "Also delete the branch after removing worktree")
	command.Flags().Bool("keep-db", false, "Keep owned databases and parallel-worker databases")
	return command
}

func runRemove(cmd *cobra.Command, args []string, deps removeCommandDependencies) error {
	pc, err := deps.openProject()
	if err != nil {
		return err
	}

	force := mustGetBool(cmd, "force")
	dryRun := mustGetBool(cmd, "dry-run")
	verbose := mustGetBool(cmd, "verbose")
	quiet := mustGetBool(cmd, "quiet")
	keepDB := mustGetBool(cmd, "keep-db")

	currentWorktreePath, err := deps.getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	defaultBranch, err := deps.getDefaultBranch(pc.GitDir)
	if err != nil {
		return fmt.Errorf("getting default branch: %w", err)
	}

	worktrees, err := deps.listWorktrees(pc.GitDir, currentWorktreePath, defaultBranch)
	if err != nil {
		return fmt.Errorf("listing worktrees: %w", err)
	}

	var targetWorktree *git.Worktree

	if len(args) > 0 {
		folderName := args[0]
		for _, wt := range worktrees {
			if filepath.Base(wt.Path) == folderName {
				targetWorktree = &wt
				break
			}
		}
		if targetWorktree == nil {
			return fmt.Errorf("worktree '%s' not found: %w", folderName, anvilerrors.ErrWorktreeNotFound)
		}
	} else if ui.IsInteractive() {
		selected, err := ui.SelectWorktreeToRemove(worktrees)
		if err != nil {
			return fmt.Errorf("selecting worktree: %w", err)
		}
		targetWorktree = selected
	} else {
		return fmt.Errorf("worktree folder name required (run interactively or use --force to skip prompts)")
	}

	if targetWorktree.IsMain {
		return fmt.Errorf("cannot remove main worktree")
	}

	cleanupOpts, cleanupMessages, err := planWorktreeCleanup(
		targetWorktree.Path,
		keepDB,
		dryRun,
		force,
		deps.readLocalState,
	)
	if err != nil {
		return err
	}

	ui.PrintInfo(fmt.Sprintf("Removing %s at %s", targetWorktree.Branch, targetWorktree.Path))

	deleteBranch := false
	if !force {
		if !ui.IsInteractive() {
			return fmt.Errorf("worktree removal requires confirmation (use --force to skip)")
		}

		ui.PrintInfo("This will run cleanup steps.")
		confirmed, err := ui.Confirm(fmt.Sprintf("Remove worktree '%s'?", targetWorktree.Branch))
		if err != nil {
			return fmt.Errorf("confirmation: %w", err)
		}
		if !confirmed {
			ui.PrintInfo("Cancelled.")
			return nil
		}

		if deps.branchExists(pc.GitDir, targetWorktree.Branch) {
			deleteBranch, err = ui.Confirm(fmt.Sprintf("Also delete branch '%s'?", targetWorktree.Branch))
			if err != nil {
				return fmt.Errorf("branch deletion confirmation: %w", err)
			}
		}
	} else {
		deleteBranch = mustGetBool(cmd, "delete-branch")
	}

	ui.PrintStep("Removing worktree")
	if err := runRemoveLifecycle(
		pc,
		*targetWorktree,
		removeLifecycleOptions{DryRun: dryRun, Verbose: verbose, Quiet: quiet},
		cleanupOpts,
		cleanupMessages,
		deps.lifecycleDependencies(),
	); err != nil {
		return err
	}

	if !dryRun {
		ui.PrintSuccessPath("Removed", targetWorktree.Path)
		if deleteBranch && deps.branchExists(pc.GitDir, targetWorktree.Branch) {
			if err := deps.deleteBranch(pc.GitDir, targetWorktree.Branch, true); err != nil {
				ui.PrintErrorWithHint("Failed to delete branch", err.Error())
			} else {
				ui.PrintSuccess(fmt.Sprintf("Deleted branch '%s'", targetWorktree.Branch))
			}
		}

		parentDir := filepath.Dir(targetWorktree.Path)
		entries, err := os.ReadDir(parentDir)
		if err == nil && len(entries) == 0 {
			if err := os.Remove(parentDir); err != nil {
				ui.PrintErrorWithHint(fmt.Sprintf("Could not remove empty directory %s", parentDir), err.Error())
			}
		}
	} else if deleteBranch {
		ui.PrintInfo("[DRY RUN] Would delete branch")
	}

	if dryRun {
		ui.PrintInfo("[DRY RUN] Worktree removal planned")
	} else {
		ui.PrintDone("Worktree removed")
	}
	return nil
}

func (deps removeCommandDependencies) lifecycleDependencies() removeLifecycleDependencies {
	return removeLifecycleDependencies{
		readLocalState:  deps.readLocalState,
		scaffoldManager: deps.scaffoldManager,
		detectPreset:    deps.detectPreset,
		removeWorktree:  deps.removeWorktree,
		printInfo:       deps.printInfo,
	}
}

func (deps removeLifecycleDependencies) printInfoMessage(message string) {
	if deps.printInfo != nil {
		deps.printInfo(message)
		return
	}
	ui.PrintInfo(message)
}

func init() {
	rootCmd.AddCommand(removeCmd)
}
