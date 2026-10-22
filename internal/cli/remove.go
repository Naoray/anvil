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
	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/dblifecycle"
	anvilerrors "github.com/naoray/anvil/internal/errors"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/lifecycle"
	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/presets"
	"github.com/naoray/anvil/internal/procs"
	"github.com/naoray/anvil/internal/resources"
	"github.com/naoray/anvil/internal/scaffold"
	"github.com/naoray/anvil/internal/scaffold/types"
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
	// Report, when set, collects the plan and warnings for `remove --json`.
	Report *removeReport
}

var removeAgentCommand = agentCommand{Name: "remove", Mutating: true, DryRun: true}

// removeData is the `remove --json` result: what removal did or, with
// --dry-run, would do.
type removeData struct {
	Worktree        worktreeSummary         `json:"worktree"`
	DryRun          bool                    `json:"dry_run"`
	Removed         bool                    `json:"removed"`
	StopProcesses   []processStatus         `json:"stop_processes"`
	ReleasePorts    []resources.Reservation `json:"release_ports"`
	RemoveDirectory string                  `json:"remove_runtime_dir,omitempty"`
	Databases       removeDatabases         `json:"databases"`
	Branch          removeBranch            `json:"branch"`
}

type removeDatabases struct {
	// Action is drop (a db.destroy cleanup step drops them), preserve
	// (--keep-db) or untouched (no db.destroy step, or records unreadable
	// and --force given).
	Action string   `json:"action"`
	Names  []string `json:"names"`
	// Targets are the databases db.destroy dropped or, in a dry run, would
	// drop, including enumerated parallel-worker databases. Empty when the
	// step did not run or its service was not running during a dry run.
	Targets []string `json:"targets"`
	// Suffix identifies a legacy worktree's databases when names are unknown:
	// present only with a db_suffix, no exact records and action drop.
	Suffix string `json:"suffix,omitempty"`
}

type removeBranch struct {
	Name    string `json:"name,omitempty"`
	Delete  bool   `json:"delete"`
	Deleted bool   `json:"deleted"`
}

type removeReport struct {
	data     *removeData
	warnings []string
	// legacySuffix is a db_suffix without exact records; it is reported only
	// when db.destroy acts on it.
	legacySuffix string
}

// note prints a lifecycle message; WARNING-prefixed messages are also
// collected as JSON warnings. A nil report only prints.
func (r *removeReport) note(deps removeLifecycleDependencies, message string) {
	deps.printInfoMessage(message)
	if r != nil {
		if warning, ok := strings.CutPrefix(message, "WARNING: "); ok {
			r.warnings = append(r.warnings, warning)
		}
	}
}

// removeRefusal classifies a refusal for `remove --json` without changing
// human output or exit status (it is not an agentapi.Error).
type removeRefusal struct {
	code string
	err  error
}

func (e removeRefusal) Error() string { return e.err.Error() }

func (e removeRefusal) Unwrap() error { return e.err }

func classifyRemoveError(err error) error {
	if err == nil {
		return nil
	}
	var classified *agentapi.Error
	if errors.As(err, &classified) {
		return err
	}
	var refusal removeRefusal
	if errors.As(err, &refusal) {
		return &agentapi.Error{Code: refusal.code, Message: err.Error(), Cause: err}
	}
	if errors.Is(err, anvilerrors.ErrWorktreeNotFound) {
		return &agentapi.Error{Code: agentapi.CodeWorktreeNotFound, Message: err.Error(), Cause: err}
	}
	return err
}

type removeLifecycleDependencies struct {
	worktreeGate    func(string) (*lockfile.Lock, error)
	lifecycleLock   func(string) (func() error, error)
	readLocalState  func(string) (*config.LocalState, error)
	scaffoldManager func(*ProjectContext) *scaffold.ScaffoldManager
	resolvePreset   func(*ProjectContext, string, string) presets.ResolvedPreset
	removeWorktree  func(string, string, bool) error
	printInfo       func(string)
	printWarning    func(string)
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
	if deps.worktreeGate != nil {
		gate, err := deps.worktreeGate(worktree.Path)
		if errors.Is(err, lockfile.ErrLocked) {
			return agentapi.Errorf(agentapi.CodeCleanupBlocked, "checks are running, preparation is active, or another lifecycle operation owns this worktree; wait before cleanup")
		}
		if err != nil {
			return err
		}
		defer func() { runErr = errors.Join(runErr, gate.Release()) }()
		// Retain the preparation lock through deletion, also excluding older callers
		// which do not yet participate in the shared lifecycle gate.
		prepare, err := lockfile.TryAcquire(wtstate.PrepareLockPath(worktree.Path))
		if errors.Is(err, lockfile.ErrLocked) {
			return agentapi.Errorf(agentapi.CodeCleanupBlocked, "worktree preparation is currently in progress; wait for it to complete")
		}
		if err != nil {
			return err
		}
		defer func() { runErr = errors.Join(runErr, prepare.Release()) }()
	}
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
			warn := func(message string) {
				deps.printWarningMessage(message)
				options.Report.addWarning(message)
			}
			if err := dblifecycle.CheckCleanupWithWarnings(worktree.Path, warn); err != nil {
				return removeRefusal{code: agentapi.CodeCleanupBlocked, err: err}
			}
		}
	}
	cleanupOpts.DryRun = options.DryRun
	cleanupOpts.Verbose = options.Verbose
	cleanupOpts.Quiet = options.Quiet
	for _, message := range cleanupMessages {
		options.Report.note(deps, message)
	}

	runtimePlan, err := planRuntimeCleanup(worktree.Path)
	if err != nil {
		return fmt.Errorf("inspecting runtime resources: %w", err)
	}
	if err := runtimePlan.blocked(); err != nil {
		return agentapi.Wrap(agentapi.CodeCleanupBlocked, fmt.Errorf("refusing to remove %s: %w", worktree.Path, err))
	}
	if err := checkRemovalLease(worktree.Path, options.IgnoreLease, func(message string) { options.Report.note(deps, message) }); err != nil {
		return err
	}
	if options.Report != nil {
		options.Report.recordRuntime(runtimePlan)
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
	if options.Report != nil && options.Report.data.Databases.Action == "" {
		options.Report.data.Databases.Action = "untouched"
		if !cleanupOpts.SkipDatabaseCleanup && hasCleanupStep(config.StepDbDestroy, resolvedPreset.CleanupSteps(), pc.Config.Cleanup.Steps) {
			options.Report.data.Databases.Action = "drop"
			options.Report.data.Databases.Suffix = options.Report.legacySuffix
		}
	}

	if options.Verbose && resolvedPreset.Name() != "" {
		deps.printInfoMessage(fmt.Sprintf("Running cleanup for preset: %s", resolvedPreset.Name()))
	}

	if resolvedPreset.Name() != "" || len(pc.Config.Cleanup.Steps) != 0 {
		siteName := worktreeSiteName(worktree.Path, worktree.Branch, pc.DefaultBranch, pc.Config.SiteName)
		if options.Report != nil {
			cleanupOpts.Report = &types.CleanupReport{}
		}
		cleanupErr := deps.scaffoldManager(pc).RunCleanupWithOptions(
			worktree.Path,
			worktree.Branch,
			"",
			siteName,
			resolvedPreset.Name(),
			resolvedPreset.CleanupSteps(),
			pc.Config,
			cleanupOpts,
		)
		if options.Report != nil {
			options.Report.data.Databases.Targets = append(options.Report.data.Databases.Targets, cleanupOpts.Report.Databases()...)
			options.Report.warnings = append(options.Report.warnings, cleanupOpts.Report.Warnings()...)
		}
		if cleanupErr != nil {
			return fmt.Errorf("cleanup for worktree %q: %w", worktree.Branch, cleanupErr)
		}
	}

	if options.DryRun {
		deps.printInfoMessage(fmt.Sprintf("[DRY RUN] Would remove %s at %s", worktree.Branch, worktree.Path))
		return nil
	}

	if err := deps.removeWorktree(pc.GitDir, worktree.Path, true); err != nil {
		return fmt.Errorf("removing worktree: %w", err)
	}
	if options.Report != nil {
		// Later failures must not hide that the worktree is gone.
		options.Report.data.Removed = true
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

// recordRuntime adds the runtime part of the removal plan.
func (r *removeReport) recordRuntime(plan *runtimeCleanupPlan) {
	if plan.Skipped != "" {
		r.warnings = append(r.warnings, plan.Skipped)
		return
	}
	for _, record := range plan.Processes {
		r.data.StopProcesses = append(r.data.StopProcesses, processStatus{Name: record.Name, State: string(procs.Check(record.PID, record.Identity)), PID: record.PID, Command: record.Command, Log: record.LogPath})
	}
	for _, port := range plan.Ports {
		r.data.ReleasePorts = append(r.data.ReleasePorts, port.Reservation)
	}
	r.data.RemoveDirectory = plan.RuntimeDir
}

func hasCleanupStep(name string, groups ...[]config.CleanupStep) bool {
	for _, steps := range groups {
		for _, step := range steps {
			if step.Name == name {
				return true
			}
		}
	}
	return false
}

type removeCommandDependencies struct {
	worktreeGate     func(string) (*lockfile.Lock, error)
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
		worktreeGate:     lifecycle.TryExclusive,
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
  - Database cleanup prompts

--dry-run previews the removal without prompting and without --force: it
changes no worktree, branch, .anvil.local, runtime directory, port, process
or database (db.destroy only lists what it would drop, and never starts a
database service). Coordination lock files are still taken.

--json writes one anvil.agent/v1 document describing the plan (worktree,
processes to stop, ports to release, runtime directory, databases, branch)
and never prompts: FOLDER is required (exact folder, branch, path or worktree
ID), and removal without --dry-run requires --force.

An unlocked registered worktree whose directory was deleted does not block
removal of another worktree; it is skipped with a warning (run 'git worktree
prune', or 'git worktree repair' if it moved). A locked one that is absent
(e.g. unmounted removable media) blocks database cleanup.`,
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
	addJSONFlag(command, removeAgentCommand)
	// Human mode keeps Cobra's error printing, flag errors and exit statuses;
	// JSON mode silences them in RunE and agentValidationError covers flags.
	command.SilenceErrors = false
	command.SetFlagErrorFunc(nil)
	return command
}

func runRemove(cmd *cobra.Command, args []string, deps removeCommandDependencies) error {
	if !mustGetBool(cmd, "json") {
		return removeWorktree(cmd, args, deps, nil)
	}
	cmd.SilenceErrors = true
	return runAgentCommand(cmd, removeAgentCommand, func() (agentResult, error) {
		report := &removeReport{}
		err := removeWorktree(cmd, args, deps, report)
		result := agentResult{Warnings: report.warnings}
		if report.data != nil {
			result.Data = report.data
		}
		return result, classifyRemoveError(err)
	})
}

// removeWorktree implements remove; report is non-nil in --json mode, which
// never prompts.
func removeWorktree(cmd *cobra.Command, args []string, deps removeCommandDependencies, report *removeReport) error {
	force := mustGetBool(cmd, "force")
	dryRun := mustGetBool(cmd, "dry-run")
	if report == nil && dryRun && len(args) == 0 {
		return fmt.Errorf("--dry-run requires a worktree argument because dry runs never prompt")
	}
	if report != nil {
		if len(args) == 0 {
			return agentapi.Errorf(agentapi.CodeInvalidArguments, "worktree argument required with --json (--json never prompts)")
		}
		if !force && !dryRun {
			return agentapi.Errorf(agentapi.CodeInvalidArguments, "removal with --json requires --force (--json never prompts); preview with --dry-run")
		}
	}

	pc, err := deps.openProject()
	if err != nil {
		if report != nil {
			return agentapi.Wrap(agentapi.CodeProjectNotFound, err)
		}
		return err
	}

	verbose := mustGetBool(cmd, "verbose")
	quiet := mustGetBool(cmd, "quiet")
	keepDB := mustGetBool(cmd, "keep-db")

	currentWorktreePath, err := deps.getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	defaultBranch, err := deps.getDefaultBranch(pc.GitDir)
	if err != nil {
		return removeRefusal{code: agentapi.CodeGitFailed, err: fmt.Errorf("getting default branch: %w", err)}
	}

	worktrees, err := deps.listWorktrees(pc.GitDir, currentWorktreePath, defaultBranch)
	if err != nil {
		return removeRefusal{code: agentapi.CodeGitFailed, err: fmt.Errorf("listing worktrees: %w", err)}
	}

	var targetWorktree *git.Worktree

	if report != nil {
		// Agent commands resolve exactly by folder, branch, path or ID.
		targetWorktree, err = resolveExactWorktree(worktrees, args[0])
		if err != nil {
			return err
		}
		if targetWorktree == nil {
			return agentapi.Errorf(agentapi.CodeWorktreeNotFound, "no worktree named %q (exact folder, branch, path or worktree ID required)", args[0]).
				WithDetails(map[string]any{"query": args[0]})
		}
	} else if len(args) > 0 {
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
		return removeRefusal{code: agentapi.CodeInvalidArguments, err: fmt.Errorf("cannot remove main worktree")}
	}
	if targetWorktree.Bare {
		return removeRefusal{code: agentapi.CodeInvalidArguments, err: fmt.Errorf("cannot remove bare worktree")}
	}
	if targetWorktree.Locked {
		if targetWorktree.LockReason == "" {
			return removeRefusal{code: agentapi.CodeCleanupBlocked, err: fmt.Errorf("cannot remove locked worktree")}
		}
		return removeRefusal{code: agentapi.CodeCleanupBlocked, err: fmt.Errorf("cannot remove locked worktree: %s", targetWorktree.LockReason)}
	}

	if report != nil {
		summary, warnings := summarizeWorktree(*targetWorktree)
		report.data = &removeData{Worktree: summary, DryRun: dryRun, StopProcesses: []processStatus{}, ReleasePorts: []resources.Reservation{}, Databases: removeDatabases{Names: []string{}, Targets: []string{}}}
		report.warnings = append(report.warnings, warnings...)
		if !targetWorktree.Detached {
			report.data.Branch.Name = targetWorktree.Branch
		}
	}

	cleanupOpts, cleanupMessages, err := planWorktreeCleanup(
		targetWorktree.Path,
		keepDB,
		dryRun,
		force,
		deps.readLocalState,
	)
	if err != nil {
		return removeRefusal{code: agentapi.CodeCleanupBlocked, err: err}
	}
	if report != nil {
		report.recordDatabases(targetWorktree.Path, keepDB, deps.readLocalState)
	}

	ui.PrintInfo(fmt.Sprintf("Removing %s at %s", worktreeBranchLabel(*targetWorktree), targetWorktree.Path))

	deleteBranch := false
	if !force && !dryRun {
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
		removeLifecycleOptions{DryRun: dryRun, Verbose: verbose, Quiet: quiet, IgnoreLease: mustGetBool(cmd, "ignore-lease"), Report: report},
		cleanupOpts,
		cleanupMessages,
		deps.lifecycleDependencies(),
	); err != nil {
		return err
	}

	if report != nil {
		report.data.Branch.Delete = deleteBranch && !targetWorktree.Detached && targetWorktree.Branch != ""
	}
	if !dryRun {
		ui.PrintSuccessPath("Removed", targetWorktree.Path)
		if deleteBranch && !targetWorktree.Detached && targetWorktree.Branch != "" && deps.branchExists(pc.GitDir, targetWorktree.Branch) {
			if err := deps.deleteBranch(pc.GitDir, targetWorktree.Branch, true); err != nil {
				ui.PrintErrorWithHint("Failed to delete branch", err.Error())
				report.addWarning("failed to delete branch " + targetWorktree.Branch + ": " + err.Error())
			} else {
				ui.PrintSuccess(fmt.Sprintf("Deleted branch '%s'", targetWorktree.Branch))
				if report != nil {
					report.data.Branch.Deleted = true
				}
			}
		}

		parentDir := filepath.Dir(targetWorktree.Path)
		entries, err := os.ReadDir(parentDir)
		if err == nil && len(entries) == 0 {
			if err := os.Remove(parentDir); err != nil {
				ui.PrintErrorWithHint(fmt.Sprintf("Could not remove empty directory %s", parentDir), err.Error())
				report.addWarning(fmt.Sprintf("could not remove empty directory %s: %v", parentDir, err))
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

func (r *removeReport) addWarning(message string) {
	if r != nil {
		r.warnings = append(r.warnings, message)
	}
}

// recordDatabases lists the owned database records removal acts on. Its
// action is decided by the lifecycle unless --keep-db preserves them.
func (r *removeReport) recordDatabases(path string, keepDB bool, read func(string) (*config.LocalState, error)) {
	state, err := read(path)
	if err != nil || state == nil {
		// planRemoveCleanup already refused or warned (with --force).
		return
	}
	for _, database := range state.Databases {
		r.data.Databases.Names = append(r.data.Databases.Names, database.Name)
	}
	if len(state.Databases) == 0 {
		r.legacySuffix = state.DbSuffix
	}
	if keepDB {
		r.data.Databases.Action = "preserve"
	}
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
		worktreeGate:    deps.worktreeGate,
		lifecycleLock:   deps.lifecycleLock,
		readLocalState:  deps.readLocalState,
		scaffoldManager: deps.scaffoldManager,
		resolvePreset:   deps.resolvePreset,
		removeWorktree:  deps.removeWorktree,
		printInfo:       deps.printInfo,
	}
}

func (deps removeLifecycleDependencies) printWarningMessage(message string) {
	if deps.printWarning != nil {
		deps.printWarning(message)
		return
	}
	if deps.printInfo != nil {
		deps.printInfo("WARNING: " + message)
		return
	}
	ui.PrintWarning(message)
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
