package cli

import (
	"fmt"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/capabilities"
	"github.com/naoray/anvil/internal/checks"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/procs"
	"github.com/naoray/anvil/internal/resources"
	"github.com/naoray/anvil/internal/ui"
	"github.com/naoray/anvil/internal/workenv"
	"github.com/naoray/anvil/internal/wtstate"
)

var contextCmd = &cobra.Command{
	Use:   "context [WORKTREE]",
	Short: "Describe a worktree's environment and capabilities (read-only)",
	Long: `Describe everything an agent needs to work in a worktree without knowing
its stack: identity, role and task metadata, lease, exported environment
variables (secret values redacted), components and their status, runtime
directories, ports, shared resources, declared processes and checks.

Read-only: never prepares, allocates or writes state.`,
	Args:              func(cmd *cobra.Command, args []string) error { return invalidArgs(cobra.MaximumNArgs(1)(cmd, args)) },
	ValidArgsFunction: completeWorktreeNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAgentCommand(cmd, agentCommand{Name: "context", Human: printContextHuman}, func() (agentResult, error) {
			return runContext(cmd, args)
		})
	},
}

var statusCmd = &cobra.Command{
	Use:   "status [WORKTREE]",
	Short: "Report a worktree's readiness, resources, lease and cleanup preview (read-only)",
	Long: `Report whether a worktree is ready and what it owns: preparation status,
exact Git state (and, for review worktrees, whether HEAD still matches the
pinned commit), lease, managed processes with verified state, port
reservations, latest check results with currency, and a cleanup preview of
what 'anvil remove' would stop, release and delete — or what blocks it.

--all summarizes every worktree of the project. Read-only.`,
	Args:              func(cmd *cobra.Command, args []string) error { return invalidArgs(cobra.MaximumNArgs(1)(cmd, args)) },
	ValidArgsFunction: completeWorktreeNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAgentCommand(cmd, agentCommand{Name: "status", Human: printStatusHuman}, func() (agentResult, error) {
			if mustGetBool(cmd, "all") {
				return runStatusAll()
			}
			return runStatus(cmd, args)
		})
	},
}

type resourcesView struct {
	RuntimeDir  string                   `json:"runtime_dir,omitempty"`
	Provisioned bool                     `json:"provisioned"`
	Directories map[string]string        `json:"directories"`
	Ports       []resources.PortState    `json:"ports"`
	Shared      []resources.SharedStatus `json:"shared"`
}

type declaredProcess struct {
	Name      string `json:"name"`
	Command   string `json:"command"`
	Component string `json:"component,omitempty"`
}

type declaredCheck struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Expensive   bool     `json:"expensive"`
	Reusable    bool     `json:"reusable"`
	Locks       []string `json:"locks"`
}

type capabilitiesView struct {
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	ManagedProcesses bool   `json:"managed_processes"`
	ExecDatabase     bool   `json:"exec_test_database"`
}

type contextData struct {
	Worktree     worktreeSummary              `json:"worktree"`
	Metadata     wtstate.Metadata             `json:"metadata"`
	Lease        *leaseView                   `json:"lease"`
	Environment  []workenv.Variable           `json:"environment"`
	EnvUnset     []string                     `json:"env_unset"`
	Components   []capabilities.ComponentPlan `json:"components"`
	Resources    resourcesView                `json:"resources"`
	Processes    []declaredProcess            `json:"processes"`
	Checks       []declaredCheck              `json:"checks"`
	Capabilities capabilitiesView             `json:"capabilities"`
}

func inspectTarget(args []string) (*worktreeEnv, error) {
	pc, err := openAgentProject()
	if err != nil {
		return nil, err
	}
	wt, err := selectAgentWorktree(pc, args)
	if err != nil {
		return nil, err
	}
	return loadWorktreeEnv(pc, *wt, worktreeEnvOptions{})
}

func resourcesOf(wenv *worktreeEnv) resourcesView {
	view := resourcesView{Directories: map[string]string{}, Ports: []resources.PortState{}, Shared: []resources.SharedStatus{}}
	if wenv.Directories != nil {
		view.RuntimeDir, view.Provisioned, view.Directories = wenv.Directories.Root, wenv.Directories.Exists, wenv.Directories.Named
	}
	if wenv.Ports != nil {
		view.Ports = wenv.Ports
	}
	if wenv.Shared != nil {
		view.Shared = wenv.Shared
	}
	return view
}

func runContext(_ *cobra.Command, args []string) (agentResult, error) {
	wenv, err := inspectTarget(args)
	if err != nil {
		return agentResult{}, err
	}
	summary, warnings := summarizeWorktree(wenv.Worktree)
	warnings = append(warnings, wenv.Warnings...)
	if wenv.ComponentErr != nil {
		warnings = append(warnings, wenv.ComponentErr.Error())
	}
	cfg := wenv.PC.Config
	data := contextData{
		Worktree: summary, Metadata: wenv.Metadata, Lease: newLeaseView(wenv.Lease, time.Now(), ""),
		Environment: wenv.Env.Redacted(), EnvUnset: append([]string{}, wenv.Unset...),
		Components: wenv.Components, Resources: resourcesOf(wenv),
		Processes: []declaredProcess{}, Checks: []declaredCheck{},
		Capabilities: capabilitiesView{OS: runtime.GOOS, Arch: runtime.GOARCH, ManagedProcesses: procs.Supported},
	}
	if data.Components == nil {
		data.Components = []capabilities.ComponentPlan{}
	}
	for _, process := range cfg.Processes {
		data.Processes = append(data.Processes, declaredProcess{Name: process.Name, Command: process.Command, Component: process.Component})
	}
	for _, check := range cfg.Checks {
		data.Checks = append(data.Checks, declaredCheck{Name: check.Name, Description: check.Description, Expensive: check.Expensive, Reusable: check.Reusable, Locks: append([]string{}, check.Locks...)})
	}
	if _, _, err := resolveExecDatabases(wenv.Worktree.Path); err == nil {
		data.Capabilities.ExecDatabase = true
	}
	return agentResult{Data: data, Warnings: warnings}, nil
}

type statusGit struct {
	git.WorkingState
	// PinnedCommitMatches is set for review worktrees.
	PinnedCommitMatches *bool `json:"pinned_commit_matches,omitempty"`
}

type statusPrepare struct {
	Status     string     `json:"status"`
	Ready      bool       `json:"ready"`
	FailedStep string     `json:"failed_step,omitempty"`
	Error      string     `json:"error,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type statusCheck struct {
	Name              string   `json:"name"`
	Status            string   `json:"status"`
	RunID             string   `json:"run_id,omitempty"`
	Current           bool     `json:"current"`
	PassingAndCurrent bool     `json:"passing_and_current"`
	StaleReasons      []string `json:"stale_reasons"`
}

type cleanupPreview struct {
	Allowed         bool                    `json:"allowed"`
	BlockedBy       []string                `json:"blocked_by"`
	Warnings        []string                `json:"warnings"`
	StopProcesses   []processStatus         `json:"stop_processes"`
	ReleasePorts    []resources.Reservation `json:"release_ports"`
	RemoveDirectory string                  `json:"remove_runtime_dir,omitempty"`
	OwnedDatabases  []string                `json:"owned_databases"`
}

type statusData struct {
	Worktree  worktreeSummary       `json:"worktree"`
	Ready     bool                  `json:"ready"`
	Metadata  wtstate.Metadata      `json:"metadata"`
	Lease     *leaseView            `json:"lease"`
	Git       *statusGit            `json:"git"`
	Prepare   statusPrepare         `json:"prepare"`
	Processes []processStatus       `json:"processes"`
	Ports     []resources.PortState `json:"ports"`
	Checks    []statusCheck         `json:"checks"`
	Cleanup   cleanupPreview        `json:"cleanup"`
}

func runStatus(cmd *cobra.Command, args []string) (agentResult, error) {
	wenv, err := inspectTarget(args)
	if err != nil {
		return agentResult{}, err
	}
	summary, warnings := summarizeWorktree(wenv.Worktree)
	warnings = append(warnings, wenv.Warnings...)
	now := time.Now()
	data := statusData{Worktree: summary, Metadata: wenv.Metadata, Lease: newLeaseView(wenv.Lease, now, ""),
		Processes: []processStatus{}, Ports: resourcesOf(wenv).Ports, Checks: []statusCheck{}}

	if state, err := git.ReadWorkingState(wenv.Worktree.Path); err != nil {
		warnings = append(warnings, err.Error())
	} else {
		data.Git = &statusGit{WorkingState: state}
		if wenv.Metadata.PinnedCommit != "" {
			matches := state.Head == wenv.Metadata.PinnedCommit
			data.Git.PinnedCommitMatches = &matches
			if !matches {
				warnings = append(warnings, "review worktree HEAD no longer matches its pinned commit "+wenv.Metadata.PinnedCommit)
			}
		}
	}

	prepare, err := wtstate.ReadPrepare(wenv.Worktree.Path)
	if err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	data.Prepare.Status, err = wtstate.EffectivePrepareStatus(wenv.Worktree.Path, prepare)
	if err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	if prepare != nil {
		data.Prepare.FailedStep, data.Prepare.Error, data.Prepare.FinishedAt = prepare.FailedStep, prepare.Error, prepare.FinishedAt
	}
	data.Prepare.Ready = data.Prepare.Status == wtstate.PrepareComplete
	data.Ready = data.Prepare.Ready && wenv.ComponentErr == nil && wenv.SharedErr == nil

	records, err := wtstate.ReadProcesses(wenv.Worktree.Path)
	if err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	for _, record := range records {
		state := string(procs.Check(record.PID, record.Identity))
		if wenv.Identity == nil || record.WorktreeID != wenv.Identity.ID {
			state = "foreign"
		}
		data.Processes = append(data.Processes, processStatus{Name: record.Name, State: state, PID: record.PID, Command: record.Command, Log: record.LogPath})
	}

	if wenv.Directories != nil {
		store := checks.Store{RuntimeDir: wenv.Directories.Root}
		for _, check := range wenv.PC.Config.Checks {
			entry := statusCheck{Name: check.Name, Status: "never_run", StaleReasons: []string{}}
			latest, err := store.Latest(check.Name)
			if err != nil {
				warnings = append(warnings, err.Error())
			}
			if latest != nil {
				entry.Status, entry.RunID = latest.Status, latest.RunID
				if identity, err := checks.ComputeIdentity(cmd.Context(), checkIdentityRequest(wenv, check)); err == nil {
					assessment := checks.Assess(latest, identity)
					entry.Current, entry.PassingAndCurrent, entry.StaleReasons = assessment.Current, assessment.PassingAndCurrent, assessment.Reasons
				} else {
					warnings = append(warnings, err.Error())
				}
			}
			data.Checks = append(data.Checks, entry)
		}
	}

	data.Cleanup = previewCleanup(wenv, now, &warnings)
	return agentResult{Data: data, Warnings: warnings}, nil
}

// previewCleanup reports what removal would do without doing it.
func previewCleanup(wenv *worktreeEnv, now time.Time, warnings *[]string) cleanupPreview {
	preview := cleanupPreview{BlockedBy: []string{}, Warnings: []string{}, StopProcesses: []processStatus{}, ReleasePorts: []resources.Reservation{}, OwnedDatabases: []string{}}
	plan, err := planRuntimeCleanup(wenv.Worktree.Path)
	if err != nil {
		preview.BlockedBy = append(preview.BlockedBy, "cannot inspect runtime resources: "+err.Error())
	} else {
		if blocked := plan.blocked(); blocked != nil {
			preview.BlockedBy = append(preview.BlockedBy, blocked.Error())
		}
		if plan.Skipped != "" {
			preview.Warnings = append(preview.Warnings, plan.Skipped)
		}
		for _, record := range plan.Processes {
			preview.StopProcesses = append(preview.StopProcesses, processStatus{Name: record.Name, State: string(procs.Check(record.PID, record.Identity)), PID: record.PID, Command: record.Command})
		}
		for _, port := range plan.Ports {
			preview.ReleasePorts = append(preview.ReleasePorts, port.Reservation)
		}
		preview.RemoveDirectory = plan.RuntimeDir
	}
	switch wenv.Lease.State(now) {
	case wtstate.LeaseActive:
		preview.BlockedBy = append(preview.BlockedBy, fmt.Sprintf("active lease by %q (task %q); release it or use --ignore-lease", wenv.Lease.Owner, wenv.Lease.TaskID))
	case wtstate.LeaseStale:
		preview.Warnings = append(preview.Warnings, fmt.Sprintf("stale lease by %q", wenv.Lease.Owner))
	}
	if wenv.State != nil {
		for _, database := range wenv.State.Databases {
			preview.OwnedDatabases = append(preview.OwnedDatabases, database.Name)
		}
	}
	if len(preview.OwnedDatabases) > 0 {
		preview.Warnings = append(preview.Warnings, "owned databases are dropped unless --keep-db; 'anvil remove --dry-run' also enumerates parallel-worker databases and borrower blockers")
	}
	preview.Allowed = len(preview.BlockedBy) == 0
	return preview
}

type statusAllEntry struct {
	Worktree worktreeSummary `json:"worktree"`
	Role     string          `json:"role,omitempty"`
	Task     wtstate.Task    `json:"task"`
	Lease    string          `json:"lease"`
	Prepare  string          `json:"prepare"`
	Dirty    *bool           `json:"dirty,omitempty"`
}

func runStatusAll() (agentResult, error) {
	pc, err := openAgentProject()
	if err != nil {
		return agentResult{}, err
	}
	worktrees, err := git.ListWorktrees(pc.GitDir)
	if err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeGitFailed, err)
	}
	var warnings []string
	entries := []statusAllEntry{}
	now := time.Now()
	for _, wt := range worktrees {
		if wt.Bare {
			continue
		}
		summary, summaryWarnings := summarizeWorktree(wt)
		warnings = append(warnings, summaryWarnings...)
		entry := statusAllEntry{Worktree: summary, Lease: wtstate.LeaseNone, Prepare: wtstate.PrepareNone}
		if metadata, err := wtstate.ReadMetadata(wt.Path); err == nil {
			entry.Role, entry.Task = metadata.Role, metadata.Task
		}
		if lease, err := wtstate.ReadLease(wt.Path); err == nil {
			entry.Lease = lease.State(now)
		} else {
			entry.Lease = "unknown"
			warnings = append(warnings, fmt.Sprintf("%s: reading lease: %v", wt.Path, err))
		}
		if prepare, err := wtstate.ReadPrepare(wt.Path); err == nil {
			if status, err := wtstate.EffectivePrepareStatus(wt.Path, prepare); err == nil {
				entry.Prepare = status
			}
		}
		if state, err := git.ReadWorkingState(wt.Path); err == nil {
			dirty := state.Dirty
			entry.Dirty = &dirty
		}
		entries = append(entries, entry)
	}
	return agentResult{Data: map[string]any{"worktrees": entries}, Warnings: warnings}, nil
}

func printContextHuman(result agentResult) error {
	data, ok := result.Data.(contextData)
	if !ok {
		return nil
	}
	fmt.Printf("%s (%s)\n", data.Worktree.Path, data.Worktree.ID)
	for _, variable := range data.Environment {
		value := variable.Value
		if variable.Secret {
			value = "<redacted>"
		}
		fmt.Printf("  %s=%s\n", variable.Name, value)
	}
	for _, component := range data.Components {
		fmt.Printf("component %s: %s %s\n", component.Name, component.Status, component.Reason)
	}
	return nil
}

func printStatusHuman(result agentResult) error {
	switch data := result.Data.(type) {
	case statusData:
		ui.PrintInfo(fmt.Sprintf("%s: prepare %s, ready=%t, lease %s", data.Worktree.Name, data.Prepare.Status, data.Ready, data.Lease.State))
		for _, check := range data.Checks {
			ui.PrintInfo(fmt.Sprintf("check %s: %s (current=%t)", check.Name, check.Status, check.Current))
		}
		if !data.Cleanup.Allowed {
			ui.PrintWarning(fmt.Sprintf("removal blocked: %v", data.Cleanup.BlockedBy))
		}
	case map[string]any:
		for _, entry := range data["worktrees"].([]statusAllEntry) {
			fmt.Printf("%-32s role=%-14s prepare=%-11s lease=%s\n", entry.Worktree.Name, entry.Role, entry.Prepare, entry.Lease)
		}
	}
	return nil
}

func init() {
	addJSONFlag(contextCmd)
	addJSONFlag(statusCmd)
	statusCmd.Flags().Bool("all", false, "Summarize every worktree of the project")
	rootCmd.AddCommand(contextCmd, statusCmd)
}
