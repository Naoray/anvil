package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/checks"
	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/dblifecycle"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/ui"
	"github.com/naoray/anvil/internal/utils"
	"github.com/naoray/anvil/internal/wtstate"
)

var checkCmd = &cobra.Command{
	Use:   "check [CHECK...]",
	Short: "Run named validation workflows and record their provenance",
	Long: `Run checks declared under 'checks' in anvil.yaml and record trustworthy results.

Each result records the commit, whether the tree was dirty (and a digest of
the exact uncommitted contents), the check definition, toolchain probe output,
an environment digest, declared inputs, the identity of declared shared
'resources', exit status, duration, logs and produced artifacts. 'anvil
check --list' reports, for the latest result of each check, whether it is
still current for the checkout and why not (for example 'shared resource
"models" changed').

Without CHECK, every check not marked expensive runs in declaration order.
Expensive checks require naming them and --allow-expensive. Checks holding
'locks' take exclusive machine-wide resource locks; a lock held by another
live process fails fast unless --lock-wait is given. Locks are released only
by their holder or when it exits, never because time passed.

With --reuse, a check declared 'reusable' returns a current passing result
instead of running: this worktree's latest, or a clean passing result another
worktree of the project recorded at the same content identity (HEAD's tree,
check definition, inputs, toolchain, platform, declared shared resources and
declared environment; inherited variables count only for a short toolchain
list (PATH, GOFLAGS, NODE_ENV, RUSTFLAGS, ...) or when named in the check's
'identity_env', and worktree paths, IDs, ports and database names are
normalized). Files Git does not track are not part of it: declare lockfiles
and manifests as 'inputs'. Every result records its producer (worktree, role, lease owner);
a reused one reports assessment.reused_from and is recorded in this worktree.
--reuse-trust (same-worktree, same-role or any) and repeatable
--reuse-exclude-role restrict producers; they default to
roles.<role>.reuse_trust and reuse_exclude_roles, else any. Dirty checkouts,
missing or changed artifacts and unregistered producers are never reused.

Child output streams to stdout (stderr with --json). With --json the
envelope carries the results; the exit status is the first failing
command's (124 timed out, 130 interrupted).`,
	ValidArgsFunction: completeCheckNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		list := mustGetBool(cmd, "list")
		return runAgentCommand(cmd, agentCommand{Name: "check", Mutating: !list, Human: printCheckHuman}, func() (agentResult, error) {
			if list {
				return runCheckList(cmd, args)
			}
			return runChecks(cmd, args)
		})
	},
}

type checkSummary struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Expensive   bool                 `json:"expensive"`
	Reusable    bool                 `json:"reusable"`
	Locks       []string             `json:"locks"`
	Commands    []string             `json:"commands"`
	Latest      *checks.Result       `json:"latest"`
	Assessment  checks.Assessment    `json:"assessment"`
	LockHolders []*checks.LockHolder `json:"lock_holders"`
}

type checkListData struct {
	Worktree worktreeSummary `json:"worktree"`
	Checks   []checkSummary  `json:"checks"`
}

type checkRunEntry struct {
	*checks.Result
	Reused     bool              `json:"reused"`
	Assessment checks.Assessment `json:"assessment"`
}

type checkRunData struct {
	Worktree worktreeSummary `json:"worktree"`
	Results  []checkRunEntry `json:"results"`
}

// checkContext loads the worktree, its identity and runtime directory.
func checkContext(cmd *cobra.Command, mutate bool) (wenv *worktreeEnv, held *lockfile.Lock, err error) {
	pc, err := openAgentProject()
	if err != nil {
		return nil, nil, err
	}
	var query []string
	if wt := mustGetString(cmd, "worktree"); wt != "" {
		query = []string{wt}
	}
	wt, err := selectAgentWorktree(pc, query)
	if err != nil {
		return nil, nil, err
	}
	gate, err := acquireWorktreeUse(pc, *wt)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, gate.Release())
		}
	}()

	if mutate {
		if _, _, err := wtstate.EnsureIdentity(wt.Path); err != nil {
			return nil, nil, agentapi.Wrap(agentapi.CodeInternal, err)
		}
	}
	wenv, err = loadWorktreeEnv(pc, *wt, worktreeEnvOptions{})
	if err != nil {
		return nil, nil, err
	}
	if mutate {
		if err := provisionResources(wenv); err != nil {
			return nil, nil, err
		}
		if wenv, err = loadWorktreeEnv(pc, *wt, worktreeEnvOptions{}); err != nil {
			return nil, nil, err
		}
	}
	return wenv, gate, nil
}

// checkIdentityRequest builds a read-only identity request: the directory
// identity cache is read but never written, and memo shares resource
// identities between the checks of one invocation.
func checkIdentityRequest(wenv *worktreeEnv, check config.CheckConfig, memo *checks.ResourceMemo) checks.IdentityRequest {
	request := checks.IdentityRequest{WorktreePath: wenv.Worktree.Path, Check: check, Env: wenv.Env, Unset: wenv.Unset, SecretEnv: wenv.PC.Config.SecretEnv,
		Resolve: func(command config.CheckCommand) (checks.CommandTarget, error) {
			return resolveCheckCommand(wenv, command)
		}, Resources: checkSharedResources(wenv), RuntimeRoot: wenv.RuntimeRoot, Memo: memo}
	if wenv.Directories != nil {
		request.RuntimeDir = wenv.Directories.Root
	}
	return request
}

// checkSharedResources pairs declared shared resources with their resolved
// paths. A path that could not be resolved stays empty and fails identity
// computation for checks that declare the resource.
func checkSharedResources(wenv *worktreeEnv) []checks.SharedResource {
	paths := map[string]string{}
	for _, status := range wenv.Shared {
		paths[status.Name] = status.Path
	}
	out := make([]checks.SharedResource, 0, len(wenv.PC.Config.Resources.Shared))
	for _, shared := range wenv.PC.Config.Resources.Shared {
		resource := checks.SharedResource{Name: shared.Name, Path: paths[shared.Name], Required: shared.Required}
		if shared.Identity != nil {
			resource.Identity = *shared.Identity
		}
		out = append(out, resource)
	}
	return out
}

func runCheckList(cmd *cobra.Command, args []string) (result agentResult, runErr error) {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	wenv, gate, err := checkContext(cmd, false)
	if err != nil {
		return agentResult{}, err
	}
	defer func() { runErr = errors.Join(runErr, gate.Release()) }()
	summary, warnings := summarizeWorktree(wenv.Worktree)
	data := checkListData{Worktree: summary, Checks: []checkSummary{}}
	selected, err := selectChecks(wenv.PC.Config, args, true)
	if err != nil {
		return agentResult{Data: data}, err
	}
	memo := checks.NewResourceMemo()
	for _, check := range selected {
		entry := checkSummary{Name: check.Name, Description: check.Description, Expensive: check.Expensive, Reusable: check.Reusable,
			Locks: append([]string{}, check.Locks...), Commands: []string{}, LockHolders: []*checks.LockHolder{}}
		for _, command := range check.Commands {
			entry.Commands = append(entry.Commands, command.Run)
		}
		if wenv.Directories != nil {
			entry.Latest, err = checks.Store{RuntimeDir: wenv.Directories.Root}.Latest(check.Name)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("check %s: %v", check.Name, err))
			}
		}
		if entry.Latest == nil {
			entry.Assessment = checks.Assess(nil, checks.Identity{})
		} else if identity, err := checks.ComputeIdentity(ctx, checkIdentityRequest(wenv, check, memo)); err != nil {
			if ctx.Err() != nil {
				return agentResult{Data: data, Warnings: warnings}, &agentapi.Error{Code: agentapi.CodeCheckFailed, Exit: 130, Message: "check inspection interrupted", Cause: ctx.Err()}
			}
			warnings = append(warnings, fmt.Sprintf("check %s: %v", check.Name, err))
			entry.Assessment = checks.Assessment{Reasons: []string{"identity unavailable: " + err.Error()}}
		} else {
			entry.Assessment = checks.Assess(entry.Latest, identity)
		}
		if wenv.RuntimeRoot != "" {
			for _, lock := range check.Locks {
				holder, err := checks.ReadHolder(wenv.RuntimeRoot, lock)
				if err != nil {
					warnings = append(warnings, err.Error())
				} else if holder != nil {
					entry.LockHolders = append(entry.LockHolders, holder)
				}
			}
		}
		data.Checks = append(data.Checks, entry)
	}
	return agentResult{Data: data, Warnings: append(warnings, wenv.Warnings...)}, nil
}

// selectChecks resolves names; without names it returns every check (list)
// or every non-expensive check (run).
func selectChecks(cfg *config.Config, names []string, includeExpensive bool) ([]config.CheckConfig, error) {
	if len(names) == 0 {
		var out []config.CheckConfig
		for _, check := range cfg.Checks {
			if includeExpensive || !check.Expensive {
				out = append(out, check)
			}
		}
		return out, nil
	}
	var out []config.CheckConfig
	for _, name := range names {
		check, ok := cfg.FindCheck(name)
		if !ok {
			return nil, agentapi.Errorf(agentapi.CodeCheckNotFound, "no check named %q is declared", name)
		}
		out = append(out, *check)
	}
	return out, nil
}

func runChecks(cmd *cobra.Command, args []string) (result agentResult, runErr error) {
	wenv, gate, err := checkContext(cmd, true)
	if err != nil {
		return agentResult{}, err
	}
	defer func() { runErr = errors.Join(runErr, gate.Release()) }()
	selected, err := selectChecks(wenv.PC.Config, args, false)
	if err != nil {
		return agentResult{}, err
	}
	allowExpensive := mustGetBool(cmd, "allow-expensive")
	for _, check := range selected {
		if check.Expensive && !allowExpensive {
			return agentResult{}, agentapi.Errorf(agentapi.CodeExpensiveCheck, "check %q is expensive; rerun with --allow-expensive to opt in", check.Name)
		}
	}
	if len(selected) == 0 {
		return agentResult{}, agentapi.Errorf(agentapi.CodeCheckNotFound, "no checks to run (expensive checks must be named)")
	}
	lockWait, err := cmd.Flags().GetDuration("lock-wait")
	if err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	policy, err := checkReusePolicy(cmd, wenv)
	if err != nil {
		return agentResult{}, err
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	summary, warnings := summarizeWorktree(wenv.Worktree)
	warnings = append(warnings, wenv.Warnings...)
	commonDir, err := dblifecycle.CommonDir(wenv.Worktree.Path)
	if err != nil {
		warnings = append(warnings, "reuse across worktrees unavailable: "+err.Error())
		commonDir = ""
	}
	run := checkRunOptions{lockWait: lockWait, policy: policy, commonDir: commonDir, producer: checkProducer(wenv)}
	data := checkRunData{Worktree: summary, Results: []checkRunEntry{}}
	var failure error
	for _, check := range selected {
		entry, warning, err := runOneCheck(ctx, cmd, wenv, check, run)
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if err != nil {
			return agentResult{Data: data, Warnings: warnings}, err
		}
		data.Results = append(data.Results, entry)
		if failure == nil && entry.Status != checks.StatusPassed {
			failure = checkFailure(entry.Result)
		}
		if ctx.Err() != nil {
			break
		}
	}
	return agentResult{Data: data, Warnings: warnings}, failure
}

func checkFailure(result *checks.Result) error {
	exit := result.ExitCode
	if exit <= 0 || exit > 255 {
		exit = agentapi.ExitGeneral
	}
	return &agentapi.Error{Code: agentapi.CodeCheckFailed, Exit: exit, Message: fmt.Sprintf("check %s %s (exit %d)", result.Check, result.Status, result.ExitCode),
		Details: map[string]any{"check": result.Check, "status": result.Status, "run_id": result.RunID}}
}

// checkRunOptions are the per-invocation settings shared by every check run.
type checkRunOptions struct {
	lockWait  time.Duration
	policy    checks.ReusePolicy
	commonDir string
	producer  checks.Producer
}

// checkReusePolicy combines roles.<role>.reuse_trust and reuse_exclude_roles
// for this worktree's role with the flags: --reuse-trust replaces the
// configured trust level, --reuse-exclude-role adds to the configured
// exclusions. The default trusts any worktree of the project.
func checkReusePolicy(cmd *cobra.Command, wenv *worktreeEnv) (checks.ReusePolicy, error) {
	declared := wenv.PC.Config.Roles.Reuse(wenv.Metadata.Role)
	policy := checks.ReusePolicy{Trust: declared.ReuseTrust, ExcludeRoles: append([]string{}, declared.ReuseExcludeRoles...)}
	if trust := mustGetString(cmd, "reuse-trust"); trust != "" {
		if !config.ValidReuseTrust(trust) {
			return checks.ReusePolicy{}, agentapi.Errorf(agentapi.CodeInvalidArguments, "invalid --reuse-trust %q (use same-worktree, same-role or any)", trust)
		}
		policy.Trust = trust
	}
	excluded, err := cmd.Flags().GetStringArray("reuse-exclude-role")
	if err != nil {
		return checks.ReusePolicy{}, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	for _, role := range excluded {
		if !wtstate.ValidRole(role) {
			return checks.ReusePolicy{}, agentapi.Errorf(agentapi.CodeInvalidArguments, "invalid --reuse-exclude-role %q (use implementation or review)", role)
		}
		policy.ExcludeRoles = append(policy.ExcludeRoles, role)
	}
	if policy.Trust == "" {
		policy.Trust = checks.TrustAny
	}
	return policy, nil
}

// checkProducer describes this worktree on the results it records.
func checkProducer(wenv *worktreeEnv) checks.Producer {
	producer := checks.Producer{WorktreeID: wenv.Identity.ID, Path: wenv.Worktree.Path, Role: wenv.Metadata.Role}
	if wenv.Lease.State(time.Now()) == wtstate.LeaseActive {
		producer.LeaseOwner = wenv.Lease.Owner
	}
	return producer
}

func runOneCheck(ctx context.Context, cmd *cobra.Command, wenv *worktreeEnv, check config.CheckConfig, run checkRunOptions) (checkRunEntry, string, error) {
	warning := ""
	var cacheWarnings []string
	result, err := checks.Run(ctx, checks.RunRequest{
		Check:       check,
		Reuse:       mustGetBool(cmd, "reuse"),
		ReuseMiss:   func(reason string) { warning = fmt.Sprintf("check %s not reused: %s", check.Name, reason) },
		Producer:    run.producer,
		ReusePolicy: run.policy,
		CommonDir:   run.commonDir,
		Worktree:    checks.WorktreeRef{ID: wenv.Identity.ID, Path: wenv.Worktree.Path, Branch: wenv.Worktree.Branch},
		RuntimeDir:  wenv.Directories.Root,
		RuntimeRoot: wenv.RuntimeRoot,
		Env:         wenv.Env,
		Resolve: func(command config.CheckCommand) (checks.CommandTarget, error) {
			return resolveCheckCommand(wenv, command)
		},
		Output:       checkOutput(cmd),
		LockWait:     run.lockWait,
		AnvilVersion: Version,
		Unset:        wenv.Unset,
		SecretEnv:    wenv.PC.Config.SecretEnv,
		Resources:    checkSharedResources(wenv),
		Warn:         func(message string) { cacheWarnings = append(cacheWarnings, message) },
	})
	if len(cacheWarnings) > 0 {
		warning = strings.Join(append(nonEmpty(warning), cacheWarnings...), "; ")
	}
	var busy *checks.BusyError
	var resourceErr *checks.ResourceIdentityError
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return checkRunEntry{}, warning, &agentapi.Error{Code: agentapi.CodeCheckFailed, Exit: 130, Message: "check interrupted before execution", Cause: err}
	case errors.As(err, &busy):
		details := map[string]any{"lock": busy.Lock}
		if busy.Holder != nil {
			details["holder"] = busy.Holder
		}
		return checkRunEntry{}, warning, (&agentapi.Error{Code: agentapi.CodeResourceBusy, Message: busy.Error(), Cause: err}).WithDetails(details)
	case errors.As(err, &resourceErr):
		return checkRunEntry{}, warning, (&agentapi.Error{Code: agentapi.CodeCheckFailed, Message: fmt.Sprintf("check %s not run: identity unavailable: %v", check.Name, resourceErr), Cause: err}).
			WithDetails(map[string]any{"check": check.Name, "reason": "identity_unavailable", "resource": resourceErr.Resource})
	case errors.Is(err, checks.ErrAlreadyRunning):
		return checkRunEntry{}, warning, agentapi.Errorf(agentapi.CodeResourceBusy, "check %s: %v", check.Name, err)
	case err != nil && result == nil:
		return checkRunEntry{}, warning, agentapi.Wrap(agentapi.CodeInternal, err)
	case err != nil:
		warning = fmt.Sprintf("check %s: %v", check.Name, err)
	}
	return checkRunEntry{Result: result, Reused: result.Reused, Assessment: checks.Assess(result, result.Identity)}, warning, nil
}

func nonEmptyOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func nonEmpty(values ...string) []string {
	var out []string
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func resolveCheckCommand(wenv *worktreeEnv, command config.CheckCommand) (checks.CommandTarget, error) {
	target, err := loadWorktreeEnv(wenv.PC, wenv.Worktree, worktreeEnvOptions{Component: command.Component})
	if err != nil {
		return checks.CommandTarget{}, err
	}
	dir := target.Worktree.Path
	if target.Component != nil {
		dir = target.Component.AbsPath
	}
	if command.Dir != "" {
		dir = filepath.Join(dir, command.Dir)
	}
	base := target.Env.Map()
	for _, key := range sortedKeys(command.Env) {
		target.Env.Set(variableFor(key, utils.ExpandEnv(command.Env[key], base), target.PC.Config))
	}
	return checks.CommandTarget{Dir: dir, Env: target.Env}, nil
}

func checkOutput(cmd *cobra.Command) io.Writer {
	if mustGetBool(cmd, "quiet") {
		return nil
	}
	// In --json mode os.Stdout is redirected to stderr by runAgentCommand.
	return os.Stdout
}

func printCheckHuman(result agentResult) error {
	switch data := result.Data.(type) {
	case checkListData:
		if len(data.Checks) == 0 {
			ui.PrintInfo("No checks declared")
		}
		for _, check := range data.Checks {
			state := "never run"
			if check.Latest != nil {
				state = check.Latest.Status
				if check.Assessment.Current && check.Assessment.ReusedFrom != nil {
					state += ", current (reused from " + check.Assessment.ReusedFrom.Path + ")"
				} else if check.Assessment.Current {
					state += ", current"
				} else {
					state += fmt.Sprintf(", stale: %v", check.Assessment.Reasons)
				}
			}
			flags := ""
			if check.Expensive {
				flags = " [expensive]"
			}
			fmt.Printf("%-16s %s%s\n", check.Name, state, flags)
		}
	case checkRunData:
		for _, entry := range data.Results {
			label := entry.Status
			if from := entry.Assessment.ReusedFrom; entry.Reused && from != nil && from.WorktreeID != entry.Worktree.ID {
				label += fmt.Sprintf(" (reused %s from %s, %s)", from.RunID, from.Path, nonEmptyOr(from.Role, "no role"))
			} else if entry.Reused {
				label += " (reused " + entry.RunID + ")"
			}
			ui.PrintInfo(fmt.Sprintf("%s: %s in %dms", entry.Check, label, entry.DurationMS))
		}
	}
	return nil
}

func completeCheckNames(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	pc, err := OpenProjectFromCWD()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	cwd, err := os.Getwd()
	if err == nil {
		if worktrees, err := git.ListWorktrees(pc.GitDir); err == nil {
			if wt, err := selectWorktreeByContainment(worktrees, cwd); err == nil && wt != nil {
				if target, err := pc.ForWorktree(wt.Path); err == nil {
					pc = target
				}
			}
		}
	}
	names := make([]string, 0, len(pc.Config.Checks))
	for _, check := range pc.Config.Checks {
		names = append(names, check.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func init() {
	addJSONFlag(checkCmd, agentCommand{Name: "check", Mutating: true, Human: printCheckHuman})
	checkCmd.Flags().Bool("list", false, "List checks with their latest result and whether it is current (read-only)")
	checkCmd.Flags().String("worktree", "", "Worktree to check (exact name, branch, path or ID; default: current)")
	checkCmd.Flags().Bool("allow-expensive", false, "Allow running checks marked expensive")
	checkCmd.Flags().Bool("reuse", false, "Return a current passing result of a reusable check (this worktree's, or another worktree's at the same content) instead of rerunning")
	checkCmd.Flags().String("reuse-trust", "", "Producers whose results --reuse accepts: same-worktree, same-role or any (default: roles.<role>.reuse_trust, else any)")
	checkCmd.Flags().StringArray("reuse-exclude-role", nil, "Refuse results produced by other worktrees with this role (repeatable; adds to roles.<role>.reuse_exclude_roles)")
	checkCmd.Flags().Duration("lock-wait", 0, "Wait this long for busy resource locks (default: fail immediately)")
	if err := checkCmd.RegisterFlagCompletionFunc("worktree", completeWorktreeNames); err != nil {
		panic(err)
	}
	rootCmd.AddCommand(checkCmd)
}
