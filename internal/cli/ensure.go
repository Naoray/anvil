package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/capabilities"
	"github.com/naoray/anvil/internal/dblifecycle"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/scaffold"
	"github.com/naoray/anvil/internal/ui"
	"github.com/naoray/anvil/internal/wtstate"
)

// worktreeCreateLockFile serializes worktree creation within one repository
// so concurrent `anvil ensure` calls for the same branch converge on one
// worktree instead of racing `git worktree add`.
const worktreeCreateLockFile = "anvil-worktree-create.lock"

var ensureCmd = &cobra.Command{
	Use:   "ensure [WORKTREE]",
	Short: "Create a worktree if needed and prepare it idempotently",
	Long: `Ensure a worktree exists and is prepared.

WORKTREE is matched exactly against registered worktrees by folder name,
branch, path or Anvil worktree ID. When nothing matches, WORKTREE is treated
as a branch name and a worktree is created exactly as 'anvil work' would.
Without WORKTREE, the worktree containing the current directory is used.

Preparation runs the same scaffold steps as 'anvil scaffold', but skips steps
whose recorded completion still matches their definition, declared inputs and
outputs. Database, site-link and store_as steps always run under their own
safety policies. Each completed step is recorded immediately, so a failed or
interrupted run resumes where it stopped. Concurrent ensures of one worktree
wait for each other.

Mutating. Use --json for one versioned JSON document on stdout.`,
	Args:              func(cmd *cobra.Command, args []string) error { return invalidArgs(cobra.MaximumNArgs(1)(cmd, args)) },
	ValidArgsFunction: completeWorktreeNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAgentCommand(cmd, agentCommand{Name: "ensure", Mutating: true, Human: printEnsureHuman}, func() (agentResult, error) {
			return runEnsure(cmd, args)
		})
	},
}

type prepareSummary struct {
	Status     string                 `json:"status"`
	Ready      bool                   `json:"ready"`
	PlanDigest string                 `json:"plan_digest,omitempty"`
	Steps      []scaffold.StepOutcome `json:"steps"`
}

type ensureData struct {
	Worktree   worktreeSummary              `json:"worktree"`
	Created    bool                         `json:"created"`
	Prepare    prepareSummary               `json:"prepare"`
	Components []capabilities.ComponentPlan `json:"components"`
}

func runEnsure(cmd *cobra.Command, args []string) (agentResult, error) {
	jsonMode := mustGetBool(cmd, "json")
	verbose := mustGetBool(cmd, "verbose")
	quiet := mustGetBool(cmd, "quiet") || jsonMode

	pc, err := openAgentProject()
	if err != nil {
		return agentResult{}, err
	}
	target, created, err := ensureWorktreeExists(cmd, pc, args)
	if err != nil {
		return agentResult{}, err
	}

	data := ensureData{Components: []capabilities.ComponentPlan{}, Created: created, Prepare: prepareSummary{Status: wtstate.PrepareNone, Steps: []scaffold.StepOutcome{}}}
	if mustGetBool(cmd, "skip-prepare") {
		if _, _, err := wtstate.EnsureIdentity(target.Path); err != nil {
			return agentResult{}, agentapi.Wrap(agentapi.CodeInternal, err)
		}
		summary, warnings := summarizeWorktree(*target)
		data.Worktree = summary
		state, err := wtstate.ReadPrepare(target.Path)
		if err == nil {
			data.Prepare.Status, err = wtstate.EffectivePrepareStatus(target.Path, state)
		}
		if err != nil {
			return agentResult{Data: data, Warnings: warnings}, agentapi.Wrap(agentapi.CodeInternal, err)
		}
		data.Prepare.Ready = data.Prepare.Status == wtstate.PrepareComplete
		return agentResult{Data: data, Warnings: warnings}, nil
	}

	selected, err := cmd.Flags().GetStringArray("component")
	if err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	wenv, err := loadWorktreeEnv(pc, *target, worktreeEnvOptions{Selected: selected})
	if err != nil {
		return agentResult{}, err
	}
	data.Components = wenv.Components
	if wenv.ComponentErr != nil {
		summary, warnings := summarizeWorktree(*target)
		data.Worktree = summary
		return agentResult{Data: data, Warnings: warnings}, &agentapi.Error{
			Code:    agentapi.CodePrepareFailed,
			Message: fmt.Sprintf("component requirements not met (nothing was changed): %v", wenv.ComponentErr),
			Cause:   wenv.ComponentErr,
		}
	}
	if wenv.SharedErr != nil {
		summary, warnings := summarizeWorktree(*target)
		data.Worktree = summary
		return agentResult{Data: data, Warnings: warnings}, &agentapi.Error{
			Code:    agentapi.CodePrepareFailed,
			Message: fmt.Sprintf("%v (nothing was changed)", wenv.SharedErr),
			Cause:   wenv.SharedErr,
		}
	}
	targetPC := wenv.PC
	if err := applyDatabaseFlags(cmd, targetPC); err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	extraSteps := capabilities.Steps(wenv.Components)
	resolvedPreset := targetPC.ResolvePreset("", target.Path)
	request := scaffold.ScaffoldRequest{WorktreePath: target.Path, Config: targetPC.Config, DefaultSteps: resolvedPreset.DefaultSteps(), ExtraSteps: extraSteps}
	if err := targetPC.ScaffoldManager().Validate(request); err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeConfigInvalid, err)
	}
	// Identity is the first write; the exported environment includes it.
	if _, _, err := wtstate.EnsureIdentity(target.Path); err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	if wenv, err = loadWorktreeEnv(pc, *target, worktreeEnvOptions{Selected: selected}); err != nil {
		return agentResult{}, err
	}
	if err := provisionResources(wenv); err != nil {
		return agentResult{Data: data}, err
	}
	if wenv, err = loadWorktreeEnv(pc, *target, worktreeEnvOptions{Selected: selected}); err != nil {
		return agentResult{}, err
	}
	// Match the naming used by the command that would otherwise have run:
	// `anvil work` for new worktrees, `anvil scaffold` for existing ones.
	repoName := filepath.Base(targetPC.ProjectPath)
	if created {
		repoName = filepath.Base(filepath.Dir(target.Path))
	}
	report, runErr := targetPC.ScaffoldManager().RunScaffoldWithOptions(scaffold.ScaffoldRequest{
		WorktreePath:  target.Path,
		Branch:        target.Branch,
		RepoName:      repoName,
		SiteName:      worktreeSiteName(target.Path, target.Branch, targetPC.DefaultBranch, targetPC.Config.SiteName),
		Preset:        resolvedPreset.Name(),
		DefaultSteps:  resolvedPreset.DefaultSteps(),
		Config:        targetPC.Config,
		Verbose:       verbose,
		Quiet:         quiet,
		ExtraSteps:    extraSteps,
		Env:           wenv.Env.Map(),
		SkipCompleted: !mustGetBool(cmd, "refresh"),
	})

	summary, warnings := summarizeWorktree(*target)
	data.Worktree = summary
	if report != nil {
		data.Prepare.PlanDigest = report.PlanDigest
		data.Prepare.Steps = report.Steps
	}
	if state, err := wtstate.ReadPrepare(target.Path); err == nil && state != nil {
		data.Prepare.Status = state.Status
	}
	if runErr != nil {
		if report == nil {
			// Validation or pre-flight failed before any effect.
			return agentResult{Data: data, Warnings: warnings}, agentapi.Wrap(agentapi.CodeConfigInvalid, runErr)
		}
		data.Prepare.Status = wtstate.PrepareFailed
		return agentResult{Data: data, Warnings: warnings}, prepareFailure(report, runErr)
	}
	data.Prepare.Ready = data.Prepare.Status == wtstate.PrepareComplete
	return agentResult{Data: data, Warnings: warnings}, nil
}

// prepareFailure returns a JSON-safe error. Step errors embed the command's
// combined output, which may contain secrets, so only the failing step is
// named; the full diagnostics go to stderr.
func prepareFailure(report *scaffold.PrepareReport, runErr error) error {
	fmt.Fprintf(os.Stderr, "preparation failed:\n%v\n", runErr)
	step := "a step"
	for _, outcome := range report.Steps {
		if outcome.Result == scaffold.OutcomeFailed {
			step = fmt.Sprintf("step %d (%s)", outcome.Index+1, outcome.Name)
		}
	}
	return &agentapi.Error{
		Code:    agentapi.CodePrepareFailed,
		Message: fmt.Sprintf("preparation failed at %s; command output is on stderr", step),
		Details: map[string]any{"failed_step": failedKey(report)},
		Cause:   runErr,
	}
}

func failedKey(report *scaffold.PrepareReport) string {
	for _, outcome := range report.Steps {
		if outcome.Result == scaffold.OutcomeFailed {
			return outcome.Key
		}
	}
	return ""
}

// ensureWorktreeExists resolves the target, creating it under the
// repository's creation lock when no registered worktree matches.
func ensureWorktreeExists(cmd *cobra.Command, pc *ProjectContext, args []string) (target *git.Worktree, created bool, err error) {
	if len(args) == 0 {
		wt, err := selectAgentWorktree(pc, nil)
		return wt, false, err
	}
	query := args[0]
	worktrees, err := git.ListWorktrees(pc.GitDir)
	if err != nil {
		return nil, false, agentapi.Wrap(agentapi.CodeGitFailed, err)
	}
	if wt, err := resolveExactWorktree(worktrees, query); err != nil || wt != nil {
		return wt, false, err
	}

	commonDir, err := dblifecycle.CommonDir(pc.ProjectPath)
	if err != nil {
		return nil, false, agentapi.Wrap(agentapi.CodeGitFailed, err)
	}
	lock, err := lockfile.Acquire(filepath.Join(commonDir, worktreeCreateLockFile))
	if err != nil {
		return nil, false, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	defer func() { err = errors.Join(err, lock.Release()) }()

	// Another process may have created it while this one waited.
	worktrees, err = git.ListWorktrees(pc.GitDir)
	if err != nil {
		return nil, false, agentapi.Wrap(agentapi.CodeGitFailed, err)
	}
	if wt, err := resolveExactWorktree(worktrees, query); err != nil || wt != nil {
		return wt, false, err
	}

	selection, err := git.ResolveWorkBranch(pc.GitDir, query)
	if err != nil {
		return nil, false, agentapi.Wrap(agentapi.CodeGitFailed, err)
	}
	for _, wt := range worktrees {
		if wt.Branch == selection.Branch {
			copied := wt
			return &copied, false, nil
		}
	}
	baseBranch := mustGetString(cmd, "base")
	if baseBranch == "" {
		baseBranch = selection.Base
		if baseBranch == "" {
			baseBranch = pc.DefaultBranch
		}
	}
	absPath, err := filepath.Abs(pc.GetWorktreePath(selection.Branch))
	if err != nil {
		return nil, false, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	ui.PrintStep(fmt.Sprintf("Creating worktree for branch '%s' from '%s'", selection.Branch, baseBranch))
	if err := createBranchWorktree(pc, selection, absPath, baseBranch, mustGetBool(cmd, "no-track"), mustGetBool(cmd, "verbose")); err != nil {
		return nil, false, agentapi.Wrap(agentapi.CodeGitFailed, err)
	}
	worktrees, err = git.ListWorktrees(pc.GitDir)
	if err != nil {
		return nil, true, agentapi.Wrap(agentapi.CodeGitFailed, err)
	}
	for _, wt := range worktrees {
		if git.PathsEqual(wt.Path, absPath) {
			copied := wt
			return &copied, true, nil
		}
	}
	return nil, true, agentapi.Errorf(agentapi.CodeGitFailed, "created worktree %s is not registered", absPath)
}

func printEnsureHuman(result agentResult) error {
	data, ok := result.Data.(ensureData)
	if !ok {
		return nil
	}
	ran, upToDate := 0, 0
	for _, step := range data.Prepare.Steps {
		switch step.Result {
		case scaffold.OutcomeRan:
			ran++
		case scaffold.OutcomeUpToDate:
			upToDate++
		}
	}
	ui.PrintDone(fmt.Sprintf("Worktree ready at %s (%d steps ran, %d up to date)", data.Worktree.Path, ran, upToDate))
	return nil
}

func init() {
	addJSONFlag(ensureCmd)
	addDatabaseFlags(ensureCmd)
	ensureCmd.Flags().StringP("base", "b", "", "Base branch for a newly created worktree")
	ensureCmd.Flags().Bool("no-track", false, "Skip remote tracking for a newly created branch")
	ensureCmd.Flags().Bool("refresh", false, "Rerun every step, ignoring completion records")
	ensureCmd.Flags().StringArray("component", nil, "Also prepare this opt-in component (repeatable)")
	ensureCmd.Flags().Bool("skip-prepare", false, "Only create or resolve the worktree")
	rootCmd.AddCommand(ensureCmd)
}
