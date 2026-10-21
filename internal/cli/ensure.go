package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/capabilities"
	"github.com/naoray/anvil/internal/config"
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

// LeaseTokenEnv supplies a lease token without exposing it in process
// arguments.
const LeaseTokenEnv = "ANVIL_LEASE_TOKEN"

var ensureCmd = &cobra.Command{
	Use:   "ensure [WORKTREE]",
	Short: "Create a worktree if needed and prepare it idempotently",
	Long: `Ensure a worktree exists and is prepared.

WORKTREE is matched exactly against registered worktrees by folder name,
branch, path or Anvil worktree ID. When nothing matches, WORKTREE is treated
as a branch name and a worktree is created exactly as 'anvil work' would.
Without WORKTREE, the worktree containing the current directory is used.

--review COMMIT creates (or reuses) an independent review worktree detached
at that exact commit: it never touches branches, so implementation workers
and their uncommitted changes are unaffected. WORKTREE names it; otherwise a
unique review-<sha>-<suffix> name is generated, so several reviewers can
inspect the same commit independently. Review worktrees prepare components
only (no preset/scaffold steps such as databases or .env copies) unless
--full-prepare or roles.review.scaffold is set. A review worktree is not a
security boundary.

Preparation runs the same scaffold steps as 'anvil scaffold', but skips steps
whose recorded completion still matches their definition, declared inputs and
outputs. Database, site-link and store_as steps always run under their own
safety policies. Each completed step is recorded immediately, so a failed or
interrupted run resumes where it stopped. Concurrent ensures of one worktree
wait for each other.

--lease-owner acquires (or, with the same token, renews) an advisory lease
before preparation; the new token is returned once in the JSON output.

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
	Metadata   wtstate.Metadata             `json:"metadata"`
	Lease      *leaseView                   `json:"lease"`
	Prepare    prepareSummary               `json:"prepare"`
	Components []capabilities.ComponentPlan `json:"components"`
}

// ensureOptions are the parsed role, task and lease flags.
type ensureOptions struct {
	ReviewCommit string
	Role         string
	Task         wtstate.Task
	LeaseOwner   string
	LeaseToken   string
	LeaseTTL     time.Duration
	SessionID    string
	Takeover     bool
}

func parseEnsureOptions(cmd *cobra.Command, pc *ProjectContext) (ensureOptions, error) {
	opts := ensureOptions{
		Role:       mustGetString(cmd, "role"),
		Task:       wtstate.Task{ID: mustGetString(cmd, "task-id"), URL: mustGetString(cmd, "task-url"), PR: mustGetString(cmd, "pr")},
		LeaseOwner: mustGetString(cmd, "lease-owner"),
		LeaseToken: os.Getenv(LeaseTokenEnv),
		SessionID:  mustGetString(cmd, "session-id"),
		Takeover:   mustGetBool(cmd, "takeover-stale"),
	}
	ttl, err := cmd.Flags().GetDuration("lease-ttl")
	if err != nil {
		return opts, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	opts.LeaseTTL = ttl
	if opts.Role != "" && !wtstate.ValidRole(opts.Role) {
		return opts, agentapi.Errorf(agentapi.CodeInvalidArguments, "--role must be implementation or review")
	}
	if review := mustGetString(cmd, "review"); review != "" {
		if opts.Role == wtstate.RoleImplementation {
			return opts, agentapi.Errorf(agentapi.CodeInvalidArguments, "--review creates review worktrees; drop --role implementation")
		}
		sha, err := git.ResolveCommit(pc.ProjectPath, review)
		if err != nil {
			return opts, agentapi.Wrap(agentapi.CodeGitFailed, err)
		}
		opts.ReviewCommit, opts.Role = sha, wtstate.RoleReview
	} else if opts.Role == wtstate.RoleReview {
		return opts, agentapi.Errorf(agentapi.CodeInvalidArguments, "review worktrees are pinned to a commit: use --review COMMIT")
	}
	if opts.Takeover && opts.LeaseOwner == "" {
		return opts, agentapi.Errorf(agentapi.CodeInvalidArguments, "--takeover-stale requires --lease-owner")
	}
	return opts, nil
}

func runEnsure(cmd *cobra.Command, args []string) (agentResult, error) {
	jsonMode := mustGetBool(cmd, "json")
	verbose := mustGetBool(cmd, "verbose")
	quiet := mustGetBool(cmd, "quiet") || jsonMode

	pc, err := openAgentProject()
	if err != nil {
		return agentResult{}, err
	}
	opts, err := parseEnsureOptions(cmd, pc)
	if err != nil {
		return agentResult{}, err
	}
	var target *git.Worktree
	var created bool
	if opts.ReviewCommit != "" {
		target, created, err = ensureReviewWorktree(pc, args, opts.ReviewCommit)
	} else {
		target, created, err = ensureWorktreeExists(cmd, pc, args)
	}
	if err != nil {
		return agentResult{}, err
	}
	if err := checkRoleCompatible(target.Path, opts); err != nil {
		return agentResult{}, err
	}
	metadata, err := wtstate.ReadMetadata(target.Path)
	if err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	effectiveRole := opts.Role
	if effectiveRole == "" {
		effectiveRole = metadata.Role
	}

	data := ensureData{Components: []capabilities.ComponentPlan{}, Created: created, Prepare: prepareSummary{Status: wtstate.PrepareNone, Steps: []scaffold.StepOutcome{}}}
	var warnings []string
	if mustGetBool(cmd, "skip-prepare") {
		if err := recordEnsureClaims(target, opts, &data, &warnings); err != nil {
			return agentResult{Data: data, Warnings: warnings}, err
		}
		summary, summaryWarnings := summarizeWorktree(*target)
		data.Worktree = summary
		warnings = append(warnings, summaryWarnings...)
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
	reviewMinimal := effectiveRole == wtstate.RoleReview && !mustGetBool(cmd, "full-prepare") && !wenv.PC.Config.Roles.Review.Scaffold
	components := reviewComponents(wenv, effectiveRole)
	data.Components = wenv.Components
	if failure := environmentPreconditions(wenv); failure != nil {
		summary, summaryWarnings := summarizeWorktree(*target)
		data.Worktree = summary
		return agentResult{Data: data, Warnings: summaryWarnings}, failure
	}
	targetPC := wenv.PC
	if err := applyDatabaseFlags(cmd, targetPC); err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	extraSteps := capabilities.Steps(components)
	resolvedPreset := targetPC.ResolvePreset("", target.Path)
	prepareConfig, defaultSteps := targetPC.Config, resolvedPreset.DefaultSteps()
	presetName := resolvedPreset.Name()
	if reviewMinimal {
		// Review worktrees run component preparation only: no databases,
		// site links or .env copies from preset/scaffold steps.
		minimal := *targetPC.Config
		minimal.Scaffold = config.ScaffoldConfig{Override: true}
		prepareConfig, defaultSteps, presetName = &minimal, nil, ""
	}
	request := scaffold.ScaffoldRequest{Config: prepareConfig, DefaultSteps: defaultSteps, ExtraSteps: extraSteps}
	if err := targetPC.ScaffoldManager().Validate(request); err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeConfigInvalid, err)
	}

	// First writes: identity, then role/task metadata and the lease, so an
	// agent's claim exists before preparation starts.
	if err := recordEnsureClaims(target, opts, &data, &warnings); err != nil {
		return agentResult{Data: data, Warnings: warnings}, err
	}
	if wenv, err = loadWorktreeEnv(pc, *target, worktreeEnvOptions{Selected: selected}); err != nil {
		return agentResult{}, err
	}
	if err := provisionResources(wenv); err != nil {
		return agentResult{Data: data, Warnings: warnings}, err
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
		Preset:        presetName,
		DefaultSteps:  defaultSteps,
		Config:        prepareConfig,
		Verbose:       verbose,
		Quiet:         quiet,
		ExtraSteps:    extraSteps,
		Env:           wenv.Env.Map(),
		SkipCompleted: !mustGetBool(cmd, "refresh"),
	})

	summary, summaryWarnings := summarizeWorktree(*target)
	data.Worktree = summary
	warnings = append(warnings, summaryWarnings...)
	if report != nil {
		data.Prepare.PlanDigest = report.PlanDigest
		data.Prepare.Steps = report.Steps
	}
	if state, err := wtstate.ReadPrepare(target.Path); err == nil && state != nil {
		data.Prepare.Status = state.Status
	}
	if runErr != nil {
		if report == nil {
			// Validation or pre-flight failed before any step ran.
			return agentResult{Data: data, Warnings: warnings}, agentapi.Wrap(agentapi.CodeConfigInvalid, runErr)
		}
		data.Prepare.Status = wtstate.PrepareFailed
		return agentResult{Data: data, Warnings: warnings}, &agentapi.Error{
			Code:    agentapi.CodePrepareFailed,
			Message: fmt.Sprintf("preparation failed: %v", runErr),
			Cause:   runErr,
		}
	}
	data.Prepare.Ready = data.Prepare.Status == wtstate.PrepareComplete
	return agentResult{Data: data, Warnings: warnings}, nil
}

// environmentPreconditions refuses preparation before any effect when
// components or required shared resources are unavailable.
func environmentPreconditions(wenv *worktreeEnv) error {
	if wenv.ComponentErr != nil {
		return &agentapi.Error{
			Code:    agentapi.CodePrepareFailed,
			Message: fmt.Sprintf("component requirements not met (nothing was changed): %v", wenv.ComponentErr),
			Cause:   wenv.ComponentErr,
		}
	}
	if wenv.SharedErr != nil {
		return &agentapi.Error{
			Code:    agentapi.CodePrepareFailed,
			Message: fmt.Sprintf("%v (nothing was changed)", wenv.SharedErr),
			Cause:   wenv.SharedErr,
		}
	}
	return nil
}

// reviewComponents applies roles.review.components to the plan.
func reviewComponents(wenv *worktreeEnv, role string) []capabilities.ComponentPlan {
	allowed := wenv.PC.Config.Roles.Review.Components
	if role != wtstate.RoleReview || len(allowed) == 0 {
		return wenv.Components
	}
	keep := map[string]bool{}
	for _, name := range allowed {
		keep[name] = true
	}
	var out []capabilities.ComponentPlan
	for _, plan := range wenv.Components {
		if keep[plan.Name] {
			out = append(out, plan)
		}
	}
	return out
}

// checkRoleCompatible refuses before any write when flags contradict the
// recorded role or pin.
func checkRoleCompatible(path string, opts ensureOptions) error {
	metadata, err := wtstate.ReadMetadata(path)
	if err != nil {
		return agentapi.Wrap(agentapi.CodeInternal, err)
	}
	if opts.Role != "" && metadata.Role != "" && metadata.Role != opts.Role {
		return agentapi.Errorf(agentapi.CodeInvalidArguments, "worktree %s has role %q, not %q", path, metadata.Role, opts.Role)
	}
	if opts.ReviewCommit != "" && metadata.PinnedCommit != "" && metadata.PinnedCommit != opts.ReviewCommit {
		return agentapi.Errorf(agentapi.CodeInvalidArguments, "review worktree %s is pinned to %s, not %s", path, metadata.PinnedCommit, opts.ReviewCommit)
	}
	if opts.ReviewCommit != "" && metadata.Role == "" {
		// An existing worktree named for a review must be one Anvil created.
		head, err := git.HeadCommit(path)
		if err != nil || head != opts.ReviewCommit {
			return agentapi.Errorf(agentapi.CodeInvalidArguments, "worktree %s exists and is not a review worktree of %s", path, opts.ReviewCommit)
		}
	}
	return nil
}

// recordEnsureClaims assigns identity, records metadata and acquires the
// requested lease.
func recordEnsureClaims(target *git.Worktree, opts ensureOptions, data *ensureData, warnings *[]string) error {
	identity, _, err := wtstate.EnsureIdentity(target.Path)
	if err != nil {
		return agentapi.Wrap(agentapi.CodeInternal, err)
	}
	now := time.Now()
	if opts.Role != "" || opts.ReviewCommit != "" || opts.Task != (wtstate.Task{}) {
		metadata, err := wtstate.UpdateMetadata(target.Path, wtstate.MetadataUpdate{Role: opts.Role, PinnedCommit: opts.ReviewCommit, Task: opts.Task}, now)
		if err != nil {
			return agentapi.Wrap(agentapi.CodeInvalidArguments, err)
		}
		data.Metadata = metadata
	} else if data.Metadata, err = wtstate.ReadMetadata(target.Path); err != nil {
		return agentapi.Wrap(agentapi.CodeInternal, err)
	}
	if opts.LeaseOwner == "" {
		lease, err := wtstate.ReadLease(target.Path)
		if err != nil {
			return agentapi.Wrap(agentapi.CodeInternal, err)
		}
		data.Lease = newLeaseView(lease, now, "")
		if lease.State(now) == wtstate.LeaseActive {
			*warnings = append(*warnings, fmt.Sprintf("worktree is leased by %q (task %q); leases are advisory", lease.Owner, lease.TaskID))
		}
		return nil
	}
	lease, token, err := wtstate.AcquireLease(target.Path, wtstate.AcquireRequest{
		WorktreeID: identity.ID, Token: opts.LeaseToken, Owner: opts.LeaseOwner, TaskID: opts.Task.ID,
		SessionID: opts.SessionID, TTL: opts.LeaseTTL, TakeoverStale: opts.Takeover, Now: now,
	})
	if err != nil {
		return leaseAPIError(err)
	}
	issued := ""
	if token != opts.LeaseToken {
		issued = token
	}
	data.Lease = newLeaseView(lease, now, issued)
	return nil
}

// ensureWorktreeExists resolves the target, creating it under the
// repository's creation lock when no registered worktree matches.
func ensureWorktreeExists(cmd *cobra.Command, pc *ProjectContext, args []string) (target *git.Worktree, created bool, err error) {
	if len(args) == 0 {
		wt, err := selectAgentWorktree(pc, nil)
		return wt, false, err
	}
	query := args[0]
	if wt, err := lookupWorktree(pc, query); err != nil || wt != nil {
		return wt, false, err
	}
	err = withCreateLock(pc, func() error {
		// Another process may have created it while this one waited.
		if target, err = lookupWorktree(pc, query); err != nil || target != nil {
			return err
		}
		selection, err := git.ResolveWorkBranch(pc.GitDir, query)
		if err != nil {
			return agentapi.Wrap(agentapi.CodeGitFailed, err)
		}
		worktrees, err := git.ListWorktrees(pc.GitDir)
		if err != nil {
			return agentapi.Wrap(agentapi.CodeGitFailed, err)
		}
		for _, wt := range worktrees {
			if wt.Branch == selection.Branch {
				copied := wt
				target = &copied
				return nil
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
			return agentapi.Wrap(agentapi.CodeInternal, err)
		}
		ui.PrintStep(fmt.Sprintf("Creating worktree for branch '%s' from '%s'", selection.Branch, baseBranch))
		if err := createBranchWorktree(pc, selection, absPath, baseBranch, mustGetBool(cmd, "no-track"), mustGetBool(cmd, "verbose")); err != nil {
			return agentapi.Wrap(agentapi.CodeGitFailed, err)
		}
		created = true
		target, err = registeredAt(pc, absPath)
		return err
	})
	return target, created, err
}

// ensureReviewWorktree creates or reuses a detached worktree at commit.
func ensureReviewWorktree(pc *ProjectContext, args []string, commit string) (target *git.Worktree, created bool, err error) {
	name := ""
	if len(args) > 0 {
		name = args[0]
		if wt, err := lookupWorktree(pc, name); err != nil || wt != nil {
			return wt, false, err
		}
	}
	err = withCreateLock(pc, func() error {
		if name != "" {
			if target, err = lookupWorktree(pc, name); err != nil || target != nil {
				return err
			}
		} else {
			suffix, err := wtstate.NewID()
			if err != nil {
				return agentapi.Wrap(agentapi.CodeInternal, err)
			}
			name = fmt.Sprintf("review-%s-%s", commit[:12], suffix[len(suffix)-4:])
		}
		absPath, err := filepath.Abs(pc.GetWorktreePath(name))
		if err != nil {
			return agentapi.Wrap(agentapi.CodeInternal, err)
		}
		if _, statErr := os.Stat(absPath); statErr == nil {
			return agentapi.Errorf(agentapi.CodeInvalidArguments, "%s already exists and is not a registered worktree", absPath)
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return agentapi.Wrap(agentapi.CodeInternal, err)
		}
		ui.PrintStep(fmt.Sprintf("Creating review worktree %s at %s", name, commit[:12]))
		if err := git.CreateDetachedWorktree(pc.ProjectPath, absPath, commit); err != nil {
			return agentapi.Wrap(agentapi.CodeGitFailed, err)
		}
		created = true
		target, err = registeredAt(pc, absPath)
		return err
	})
	return target, created, err
}

func lookupWorktree(pc *ProjectContext, query string) (*git.Worktree, error) {
	worktrees, err := git.ListWorktrees(pc.GitDir)
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeGitFailed, err)
	}
	return resolveExactWorktree(worktrees, query)
}

func registeredAt(pc *ProjectContext, absPath string) (*git.Worktree, error) {
	worktrees, err := git.ListWorktrees(pc.GitDir)
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeGitFailed, err)
	}
	for _, wt := range worktrees {
		if git.PathsEqual(wt.Path, absPath) {
			copied := wt
			return &copied, nil
		}
	}
	return nil, agentapi.Errorf(agentapi.CodeGitFailed, "created worktree %s is not registered", absPath)
}

func withCreateLock(pc *ProjectContext, fn func() error) (err error) {
	commonDir, err := dblifecycle.CommonDir(pc.ProjectPath)
	if err != nil {
		return agentapi.Wrap(agentapi.CodeGitFailed, err)
	}
	lock, err := lockfile.Acquire(filepath.Join(commonDir, worktreeCreateLockFile))
	if err != nil {
		return agentapi.Wrap(agentapi.CodeInternal, err)
	}
	defer func() { err = errors.Join(err, lock.Release()) }()
	return fn()
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
	if data.Lease != nil && data.Lease.Token != "" {
		fmt.Printf("Lease token (shown once; export %s to renew/release): %s\n", LeaseTokenEnv, data.Lease.Token)
	}
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
	ensureCmd.Flags().String("review", "", "Create or reuse a review worktree detached at this exact commit")
	ensureCmd.Flags().Bool("full-prepare", false, "Run preset/scaffold steps in review worktrees too")
	ensureCmd.Flags().String("role", "", "Record the worktree role: implementation or review")
	ensureCmd.Flags().String("task-id", "", "External task identifier (metadata only)")
	ensureCmd.Flags().String("task-url", "", "External task URL (metadata only)")
	ensureCmd.Flags().String("pr", "", "Pull request number or URL (metadata only)")
	addLeaseFlags(ensureCmd)
	rootCmd.AddCommand(ensureCmd)
}
