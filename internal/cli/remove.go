package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/checks"
	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/dblifecycle"
	anvilerrors "github.com/naoray/anvil/internal/errors"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/presets"
	"github.com/naoray/anvil/internal/scaffold"
	"github.com/naoray/anvil/internal/ui"
	"github.com/naoray/anvil/internal/wtstate"
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
	// IgnoreLease explicitly authorizes removing a worktree another agent
	// holds an active lease on.
	IgnoreLease bool
}

type removeLifecycleDependencies struct {
	lifecycleLock   func(string) (func() error, error)
	readLocalState  func(string) (*config.LocalState, error)
	scaffoldManager func(*ProjectContext) *scaffold.ScaffoldManager
	resolvePreset   func(*ProjectContext, string, string) presets.ResolvedPreset
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
) (runErr error) {
	target, err := pc.ForWorktree(worktree.Path)
	if err != nil {
		return err
	}
	pc = target
	if deps.lifecycleLock != nil {
		release, err := deps.lifecycleLock(worktree.Path)
		if err != nil {
			return err
		}
		defer func() { runErr = errors.Join(runErr, release()) }()
		cleanupOpts.DatabaseLockHeld = true
		if !cleanupOpts.SkipDatabaseCleanup {
			if err := dblifecycle.CheckCleanup(worktree.Path); err != nil {
				return err
			}
		}
	}
	cleanupOpts.DryRun = options.DryRun
	cleanupOpts.Verbose = options.Verbose
	cleanupOpts.Quiet = options.Quiet
	for _, message := range cleanupMessages {
		deps.printInfoMessage(message)
	}

	// The exclusive check gate is held from this safety check until the
	// worktree is gone: running checks (and commands they left behind) hold
	// it shared, and no check can start meanwhile.
	gate, err := lockfile.TryAcquire(checks.GatePath(worktree.Path))
	if errors.Is(err, lockfile.ErrLocked) {
		return agentapi.Errorf(agentapi.CodeCleanupBlocked, "refusing to remove %s: checks are running in this worktree; wait for them or stop them first", worktree.Path)
	}
	if err != nil {
		return fmt.Errorf("locking checks for removal: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, gate.Release()) }()

	runtimePlan, err := planRuntimeCleanup(worktree.Path)
	if err != nil {
		return fmt.Errorf("inspecting runtime resources: %w", err)
	}
	if err := runtimePlan.blocked(); err != nil {
		return agentapi.Wrap(agentapi.CodeCleanupBlocked, fmt.Errorf("refusing to remove %s: %w", worktree.Path, err))
	}
	// Held from the lease check until the worktree is gone, so no lease can
	// be acquired or renewed after the check (lease commands take it too).
	lifecycle, err := wtstate.LockLifecycle(worktree.Path)
	if err != nil {
		return fmt.Errorf("locking worktree lifecycle: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, lifecycle.Release()) }()
	if err := checkRemovalLease(worktree.Path, options.IgnoreLease, deps.printInfoMessage); err != nil {
		return err
	}
	if options.DryRun {
		for _, line := range runtimePlan.describe("[DRY RUN] Would ") {
			deps.printInfoMessage(line)
		}
	} else {
		stopped, err := runtimePlan.stopProcesses(pc.Config)
		for _, status := range stopped {
			deps.printInfoMessage(fmt.Sprintf("Process %s: %s", status.Name, status.Result))
		}
		if err != nil {
			return fmt.Errorf("stopping managed processes (worktree kept): %w", err)
		}
	}

	resolvedPreset := deps.resolvePreset(pc, "", worktree.Path)

	if options.Verbose && resolvedPreset.Name() != "" {
		deps.printInfoMessage(fmt.Sprintf("Running cleanup for preset: %s", resolvedPreset.Name()))
	}

	if resolvedPreset.Name() != "" || len(pc.Config.Cleanup.Steps) != 0 {
		siteName := worktreeSiteName(worktree.Path, worktree.Branch, pc.DefaultBranch, pc.Config.SiteName)
		if err := deps.scaffoldManager(pc).RunCleanupWithOptions(
			worktree.Path,
			worktree.Branch,
			"",
			siteName,
			resolvedPreset.Name(),
			resolvedPreset.CleanupSteps(),
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
	for _, line := range runtimePlan.describe("Released: ") {
		if options.Verbose {
			deps.printInfoMessage(line)
		}
	}
	if err := runtimePlan.release(); err != nil {
		return fmt.Errorf("worktree removed, but runtime cleanup failed: %w", err)
	}
	return nil
}

type removeCommandDependencies struct {
	lifecycleLock    func(string) (func() error, error)
	openProject      func() (*ProjectContext, error)
	getwd            func() (string, error)
	getDefaultBranch func(string) (string, error)
	listWorktrees    func(string, string, string) ([]git.Worktree, error)
	branchExists     func(string, string) bool
	removeWorktree   func(string, string, bool) error
	deleteBranch     func(string, string, bool) error
	readLocalState   func(string) (*config.LocalState, error)
	scaffoldManager  func(*ProjectContext) *scaffold.ScaffoldManager
	resolvePreset    func(*ProjectContext, string, string) presets.ResolvedPreset
	printInfo        func(string)
}

func defaultRemoveCommandDependencies() removeCommandDependencies {
	return removeCommandDependencies{
		lifecycleLock:    dblifecycle.Lock,
		openProject:      OpenProjectFromCWD,
		getwd:            os.Getwd,
		getDefaultBranch: git.GetDefaultBranch,
		listWorktrees:    git.ListWorktreesDetailed,
		branchExists:     git.BranchExists,
		removeWorktree:   git.RemoveWorktree,
		deleteBranch:     git.DeleteBranch,
		readLocalState:   config.ReadLocalState,
		scaffoldManager:  func(pc *ProjectContext) *scaffold.ScaffoldManager { return pc.ScaffoldManager() },
		resolvePreset: func(pc *ProjectContext, explicit, path string) presets.ResolvedPreset {
			return pc.ResolvePreset(explicit, path)
		},
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
	command.Flags().Bool("ignore-lease", false, "Remove even if another agent holds an active lease (explicit authorization)")
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
	if targetWorktree.Bare {
		return fmt.Errorf("cannot remove bare worktree")
	}
	if targetWorktree.Locked {
		if targetWorktree.LockReason == "" {
			return fmt.Errorf("cannot remove locked worktree")
		}
		return fmt.Errorf("cannot remove locked worktree: %s", targetWorktree.LockReason)
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

	ui.PrintInfo(fmt.Sprintf("Removing %s at %s", worktreeBranchLabel(*targetWorktree), targetWorktree.Path))

	deleteBranch := false
	if !force {
		if !ui.IsInteractive() {
			return fmt.Errorf("worktree removal requires confirmation (use --force to skip)")
		}

		ui.PrintInfo("This will run cleanup steps.")
		confirmed, err := ui.Confirm(fmt.Sprintf("Remove worktree '%s'?", worktreeBranchLabel(*targetWorktree)))
		if err != nil {
			return fmt.Errorf("confirmation: %w", err)
		}
		if !confirmed {
			ui.PrintInfo("Cancelled.")
			return nil
		}

		if !targetWorktree.Detached && targetWorktree.Branch != "" && deps.branchExists(pc.GitDir, targetWorktree.Branch) {
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
		removeLifecycleOptions{DryRun: dryRun, Verbose: verbose, Quiet: quiet, IgnoreLease: mustGetBool(cmd, "ignore-lease")},
		cleanupOpts,
		cleanupMessages,
		deps.lifecycleDependencies(),
	); err != nil {
		return err
	}

	if !dryRun {
		ui.PrintSuccessPath("Removed", targetWorktree.Path)
		if deleteBranch && !targetWorktree.Detached && targetWorktree.Branch != "" && deps.branchExists(pc.GitDir, targetWorktree.Branch) {
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
	} else if deleteBranch && !targetWorktree.Detached && targetWorktree.Branch != "" {
		ui.PrintInfo("[DRY RUN] Would delete branch")
	}

	if dryRun {
		ui.PrintInfo("[DRY RUN] Worktree removal planned")
	} else {
		ui.PrintDone("Worktree removed")
	}
	return nil
}

// checkRemovalLease refuses to remove a worktree with an active lease unless
// explicitly authorized; a stale lease only warns.
func checkRemovalLease(path string, ignore bool, info func(string)) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	lease, err := wtstate.ReadLease(path)
	if err != nil {
		return fmt.Errorf("reading lease: %w", err)
	}
	now := time.Now()
	switch lease.State(now) {
	case wtstate.LeaseActive:
		message := fmt.Sprintf("worktree is leased by %q (task %q) until %s", lease.Owner, lease.TaskID, lease.ExpiresAt().UTC().Format(time.RFC3339))
		if !ignore {
			return (&agentapi.Error{Code: agentapi.CodeLeaseHeld, Message: "refusing to remove " + path + ": " + message + "; release the lease or pass --ignore-lease"}).
				WithDetails(map[string]any{"lease": newLeaseView(lease, now, "")})
		}
		info("WARNING: " + message + "; removing because --ignore-lease was given")
	case wtstate.LeaseStale:
		info(fmt.Sprintf("WARNING: stale lease by %q (task %q) expired at %s", lease.Owner, lease.TaskID, lease.ExpiresAt().UTC().Format(time.RFC3339)))
	}
	return nil
}

func (deps removeCommandDependencies) lifecycleDependencies() removeLifecycleDependencies {
	return removeLifecycleDependencies{
		lifecycleLock:   deps.lifecycleLock,
		readLocalState:  deps.readLocalState,
		scaffoldManager: deps.scaffoldManager,
		resolvePreset:   deps.resolvePreset,
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
