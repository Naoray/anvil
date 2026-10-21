package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/procs"
	"github.com/naoray/anvil/internal/ui"
	"github.com/naoray/anvil/internal/utils"
	"github.com/naoray/anvil/internal/wtstate"
)

const defaultStopTimeout = 10 * time.Second

// processStatus is the JSON view of one managed process.
type processStatus struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	Result  string `json:"result,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Command string `json:"command,omitempty"`
	Log     string `json:"log,omitempty"`
}

type processesData struct {
	Worktree  worktreeSummary `json:"worktree"`
	Processes []processStatus `json:"processes"`
}

var startCmd = &cobra.Command{
	Use:   "start [WORKTREE]",
	Short: "Start a worktree's managed development processes",
	Long: `Start the processes declared under 'processes' in anvil.yaml.

Each process runs detached through 'sh -c' in its own process group with the
worktree environment (directories, ports, project and component env), stdin
from /dev/null and output appended to $ANVIL_LOG_DIR/<name>.log. The group is
led by a small Anvil supervisor that lives until every process in the group
has exited, so commands that background their work stay tracked. Anvil
records the leader PID together with its kernel start time so a recycled PID
is never mistaken for it. Already running processes are left alone. Directories and
ports are provisioned first, like 'anvil ensure'.

Supported on macOS and Linux. Mutating.`,
	Args:              func(cmd *cobra.Command, args []string) error { return invalidArgs(cobra.MaximumNArgs(1)(cmd, args)) },
	ValidArgsFunction: completeWorktreeNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAgentCommand(cmd, agentCommand{Name: "start", Mutating: true, Human: printProcessesHuman}, func() (agentResult, error) {
			return runStart(cmd, args)
		})
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop [WORKTREE]",
	Short: "Stop a worktree's managed development processes",
	Long: `Stop processes Anvil started for this worktree.

A process group is signaled (SIGTERM, then SIGKILL after the stop timeout)
only while its recorded leader PID still has the recorded start identity. A
PID that now belongs to another process, or a surviving group whose leader is
gone, is never signaled. Processes are never stopped because of a
port they listen on. Mutating.`,
	Args:              func(cmd *cobra.Command, args []string) error { return invalidArgs(cobra.MaximumNArgs(1)(cmd, args)) },
	ValidArgsFunction: completeWorktreeNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAgentCommand(cmd, agentCommand{Name: "stop", Mutating: true, DryRun: true, Human: printProcessesHuman}, func() (agentResult, error) {
			return runStop(cmd, args)
		})
	},
}

func selectedProcesses(cmd *cobra.Command, cfg *config.Config) ([]config.ProcessConfig, error) {
	names, err := cmd.Flags().GetStringArray("process")
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	if len(names) == 0 {
		return cfg.Processes, nil
	}
	var out []config.ProcessConfig
	for _, name := range names {
		process, ok := cfg.FindProcess(name)
		if !ok {
			return nil, agentapi.Errorf(agentapi.CodeProcessNotFound, "no process named %q is declared", name)
		}
		out = append(out, *process)
	}
	return out, nil
}

func runStart(cmd *cobra.Command, args []string) (agentResult, error) {
	if !procs.Supported {
		return agentResult{}, agentapi.Errorf(agentapi.CodeUnsupported, "managed processes are not supported on %s", runtime.GOOS)
	}
	pc, err := openAgentProject()
	if err != nil {
		return agentResult{}, err
	}
	wt, err := selectAgentWorktree(pc, args)
	if err != nil {
		return agentResult{}, err
	}
	if _, _, err := wtstate.EnsureIdentity(wt.Path); err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	wenv, err := loadWorktreeEnv(pc, *wt, worktreeEnvOptions{})
	if err != nil {
		return agentResult{}, err
	}
	selected, err := selectedProcesses(cmd, wenv.PC.Config)
	if err != nil {
		return agentResult{}, err
	}
	if err := provisionResources(wenv); err != nil {
		return agentResult{}, err
	}
	lock, err := wtstate.AcquireRunLock(wt.Path)
	if err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	result, err := startLocked(pc, wt, wenv, selected)
	if releaseErr := lock.Release(); releaseErr != nil && err == nil {
		return result, agentapi.Wrap(agentapi.CodeInternal, releaseErr)
	}
	return result, err
}

// startLocked launches the selected processes that are not already running.
// The caller holds the worktree run lock.
func startLocked(pc *ProjectContext, wt *git.Worktree, wenv *worktreeEnv, selected []config.ProcessConfig) (agentResult, error) {
	records, err := wtstate.ReadProcesses(wt.Path)
	if err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	running := map[string]wtstate.ProcessRecord{}
	for _, record := range records {
		if record.WorktreeID == wenv.Identity.ID && procs.Check(record.PID, record.Identity) == procs.StateRunning {
			running[record.Name] = record
		}
	}

	summary, warnings := summarizeWorktree(*wt)
	data := processesData{Worktree: summary, Processes: []processStatus{}}
	for _, process := range selected {
		if record, ok := running[process.Name]; ok {
			data.Processes = append(data.Processes, processStatus{Name: process.Name, State: string(procs.StateRunning), Result: "already_running", PID: record.PID, Command: record.Command, Log: record.LogPath})
			continue
		}
		status, err := startProcess(pc, wenv.Worktree, process, wenv.Identity.ID)
		if err != nil {
			return agentResult{Data: data, Warnings: warnings}, err
		}
		data.Processes = append(data.Processes, status)
	}
	return agentResult{Data: data, Warnings: append(warnings, wenv.Warnings...)}, nil
}

func startProcess(pc *ProjectContext, wt git.Worktree, process config.ProcessConfig, worktreeID string) (processStatus, error) {
	wenv, err := loadWorktreeEnv(pc, wt, worktreeEnvOptions{Component: process.Component})
	if err != nil {
		return processStatus{}, err
	}
	dir := wenv.Worktree.Path
	if wenv.Component != nil {
		dir = wenv.Component.AbsPath
	}
	if wenv.Directories == nil {
		return processStatus{}, agentapi.Errorf(agentapi.CodeInternal, "runtime directories are unavailable")
	}
	base := wenv.Env.Map()
	extra := make(map[string]string, len(process.Env))
	for key, value := range process.Env {
		extra[key] = utils.ExpandEnv(value, base)
	}
	environ := wenv.Env.Environ(os.Environ(), nil, false)
	for key, value := range extra {
		environ = append(environ, key+"="+value)
	}
	logPath := filepath.Join(wenv.Directories.Named["logs"], process.Name+".log")
	started, err := procs.Start(procs.Spec{Command: process.Command, Dir: dir, Env: environ, LogPath: logPath})
	if err != nil {
		return processStatus{}, agentapi.Wrap(agentapi.CodeInternal, fmt.Errorf("starting %s: %w", process.Name, err))
	}
	record := wtstate.ProcessRecord{
		Name: process.Name, Command: process.Command, Dir: dir, PID: started.PID, Identity: started.Identity,
		LogPath: logPath, WorktreeID: worktreeID, StartedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := wtstate.PutProcess(wt.Path, record); err != nil {
		// An unrecorded process could never be stopped safely.
		_, stopErr := procs.Stop(started.PID, started.Identity, time.Second)
		return processStatus{}, agentapi.Wrap(agentapi.CodeInternal, fmt.Errorf("recording %s: %w (stop: %v)", process.Name, err, stopErr))
	}
	return processStatus{Name: process.Name, State: string(procs.StateRunning), Result: "started", PID: started.PID, Command: process.Command, Log: logPath}, nil
}

// stopRecordedProcesses stops verified processes for a worktree, removing
// their records. It returns per-process outcomes.
func stopRecordedProcesses(worktreePath, worktreeID string, cfg *config.Config, names map[string]bool, dryRun bool) (statuses []processStatus, err error) {
	if !dryRun {
		lock, lockErr := wtstate.AcquireRunLock(worktreePath)
		if lockErr != nil {
			return nil, lockErr
		}
		defer func() { err = errors.Join(err, lock.Release()) }()
	}
	records, err := wtstate.ReadProcesses(worktreePath)
	if err != nil {
		return nil, err
	}
	statuses = []processStatus{}
	for _, record := range records {
		if names != nil && !names[record.Name] {
			continue
		}
		status := processStatus{Name: record.Name, PID: record.PID, Command: record.Command, Log: record.LogPath}
		if record.WorktreeID != worktreeID {
			status.State, status.Result = "foreign", "not_signaled_foreign_record"
			statuses = append(statuses, status)
			continue
		}
		status.State = string(procs.Check(record.PID, record.Identity))
		if dryRun {
			status.Result = "would_stop"
			statuses = append(statuses, status)
			continue
		}
		timeout := defaultStopTimeout
		if process, ok := cfg.FindProcess(record.Name); ok && process.StopTimeout != "" {
			if parsed, err := time.ParseDuration(process.StopTimeout); err == nil {
				timeout = parsed
			}
		}
		result, err := procs.Stop(record.PID, record.Identity, timeout)
		if err != nil {
			return statuses, fmt.Errorf("stopping %s: %w", record.Name, err)
		}
		status.Result = string(result)
		if result != procs.StopUnverifiable {
			if err := wtstate.RemoveProcess(worktreePath, record); err != nil {
				return statuses, err
			}
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func runStop(cmd *cobra.Command, args []string) (agentResult, error) {
	pc, err := openAgentProject()
	if err != nil {
		return agentResult{}, err
	}
	wt, err := selectAgentWorktree(pc, args)
	if err != nil {
		return agentResult{}, err
	}
	wenv, err := loadWorktreeEnv(pc, *wt, worktreeEnvOptions{})
	if err != nil {
		return agentResult{}, err
	}
	if wenv.Identity == nil || wenv.IdentityStatus == wtstate.IdentityCopied {
		return agentResult{}, agentapi.Errorf(agentapi.CodeInvalidArguments, "worktree has no verified identity; nothing to stop")
	}
	names, err := cmd.Flags().GetStringArray("process")
	if err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	var filter map[string]bool
	if len(names) > 0 {
		filter = map[string]bool{}
		for _, name := range names {
			filter[name] = true
		}
	}
	summary, warnings := summarizeWorktree(*wt)
	statuses, err := stopRecordedProcesses(wenv.Worktree.Path, wenv.Identity.ID, wenv.PC.Config, filter, mustGetBool(cmd, "dry-run"))
	data := processesData{Worktree: summary, Processes: statuses}
	if err != nil {
		return agentResult{Data: data, Warnings: warnings}, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	return agentResult{Data: data, Warnings: warnings}, nil
}

func printProcessesHuman(result agentResult) error {
	data, ok := result.Data.(processesData)
	if !ok {
		return nil
	}
	if len(data.Processes) == 0 {
		ui.PrintInfo("No managed processes")
	}
	for _, process := range data.Processes {
		ui.PrintInfo(fmt.Sprintf("%s: %s (pid %d) %s", process.Name, process.Result, process.PID, process.Log))
	}
	return nil
}

func init() {
	for _, command := range []*cobra.Command{startCmd, stopCmd} {
		addJSONFlag(command)
		command.Flags().StringArray("process", nil, "Limit to this declared process (repeatable)")
		rootCmd.AddCommand(command)
	}
}
