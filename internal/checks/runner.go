package checks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/lifecycle"
	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/workenv"
)

// CommandTarget is the resolved directory and environment of one command.
type CommandTarget struct {
	Dir string
	Env *workenv.Environment
}

// RunRequest describes one check run.
type RunRequest struct {
	Check        config.CheckConfig
	Worktree     WorktreeRef
	RuntimeDir   string
	RuntimeRoot  string
	Env          *workenv.Environment
	Resolve      func(config.CheckCommand) (CommandTarget, error)
	Output       io.Writer
	LockWait     time.Duration
	AnvilVersion string
	Unset        []string
	SecretEnv    []string
	Now          func() time.Time
	Reuse        bool
	ReuseMiss    func(string)
	// Producer describes this worktree (role, lease owner) on every result
	// it records; its ID and path default to Worktree's.
	Producer Producer
	// ReusePolicy restricts which producers' results --reuse returns.
	ReusePolicy ReusePolicy
	// CommonDir is the canonical Git common directory shared by the
	// project's worktrees. It enables the cross-worktree reuse index under
	// RuntimeRoot ("" limits reuse to this worktree).
	CommonDir string
	// Resources are the declared shared resources (see IdentityRequest).
	Resources []SharedResource
	// Warn receives nonfatal problems found while identifying the run.
	Warn func(string)
	// Slot gates expensive or slot-weighted checks machine-wide.
	Slot SlotOptions
}

// ErrAlreadyRunning reports a concurrent run of the same check in the same worktree.
var ErrAlreadyRunning = errors.New("this check is already running in this worktree")

func (r RunRequest) producer() *Producer {
	producer := r.Producer
	if producer.WorktreeID == "" {
		producer.WorktreeID = r.Worktree.ID
	}
	if producer.Path == "" {
		producer.Path = r.Worktree.Path
	}
	return &producer
}

func (r RunRequest) reuseIndex() ReuseIndex {
	return ReuseIndex{Root: r.RuntimeRoot, CommonDir: r.CommonDir}
}

func (r RunRequest) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Run executes the check. The returned result is persisted for every
// outcome that reached execution; errors before execution (busy locks,
// unreadable state) return a nil result.
func Run(ctx context.Context, req RunRequest) (result *Result, err error) {
	gate, err := lifecycle.AcquireShared(req.Worktree.Path)
	if err != nil {
		return nil, fmt.Errorf("locking check lifecycle: %w", err)
	}
	defer func() { err = errors.Join(err, gate.Release()) }()
	timeout, err := config.ParseOptionalDuration(req.Check.Timeout)
	if err != nil {
		return nil, fmt.Errorf("check timeout: %w", err)
	}
	store := Store{RuntimeDir: req.RuntimeDir}
	active, err := lockfile.TryAcquire(store.activeLockPath(req.Check.Name))
	if errors.Is(err, lockfile.ErrLocked) {
		return nil, ErrAlreadyRunning
	}
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, active.Release()) }()

	started := req.now().UTC()
	runID, err := newRunID(started)
	if err != nil {
		return nil, err
	}
	adm, err := admit(ctx, req, store, gate, active, runID)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, adm.release()) }()
	// Reuse hits (this worktree's or another's) execute nothing; a hit found
	// after queueing releases its slot immediately (on return).
	if reused := adm.assessment.Reused; reused != nil {
		if reused.Worktree.ID == req.Worktree.ID {
			reused.Reused, reused.ReusedFrom = true, adm.assessment.ReusedFrom
			return reused, nil
		}
		return recordReuse(req, store, adm.assessment.Identity, reused, adm.assessment.ReusedFrom, runID, started, adm.queued)
	}
	if req.Reuse && req.ReuseMiss != nil {
		req.ReuseMiss(adm.assessment.Reason)
	}
	started = started.Add(adm.queued) // timings exclude the slot queue
	guards := adm.guards(gate, active)
	targets, identity := adm.assessment.Targets, adm.assessment.Identity

	runDir := store.runDir(req.Check.Name, runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, err
	}
	runLock, err := lockfile.Acquire(runLockPath(runDir))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, runLock.Release()) }()
	guards = append(guards, runLock)

	result = &Result{
		Schema: ResultSchema, RunID: runID, Check: req.Check.Name, Status: StatusRunning,
		StartedAt: started, QueuedMS: adm.queued.Milliseconds(), Worktree: req.Worktree, Identity: identity,
		Commands: []CommandResult{}, Artifacts: []Artifact{}, Locks: append([]string{}, req.Check.Locks...),
		AnvilVersion: req.AnvilVersion, RunDir: runDir, Producer: req.producer(),
	}
	if err := store.save(result); err != nil {
		return nil, err
	}

	checkCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		checkCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	result.Status = StatusPassed
	for i, command := range req.Check.Commands {
		commandResult, status, runErr := runCommand(ctx, checkCtx, req, command, targets[i], guards, identity, runID, runDir, i)
		result.Commands = append(result.Commands, commandResult)
		if runErr != nil {
			result.Status, result.Error, result.ExitCode = StatusError, runErr.Error(), 1
			break
		}
		if status != StatusPassed {
			result.Status, result.ExitCode = status, commandResult.ExitCode
			break
		}
	}
	if result.Status == StatusPassed {
		if err := collectArtifacts(req, result); err != nil {
			result.Status, result.Error, result.ExitCode = StatusError, "recording artifacts: "+err.Error(), 1
		}
	}
	finished := req.now().UTC()
	result.FinishedAt = &finished
	result.DurationMS = finished.Sub(started).Milliseconds()
	if err := store.save(result); err != nil {
		return result, err
	}
	if req.Check.Reusable && result.Identity.ContentDigest != "" && result.Status == StatusPassed {
		if err := indexIfUnchanged(req, result, finished); err != nil && req.Warn != nil {
			req.Warn(fmt.Sprintf("check %s: not offered for reuse across worktrees: %v", req.Check.Name, err))
		}
	}
	return result, store.prune(req.Check.Name)
}

// indexIfUnchanged offers a passing result to other worktrees only when the
// checkout still has the state the identity recorded before the commands
// ran: a run that modified tracked or untracked files validated a state
// nobody else has, so it stays usable only in this worktree.
func indexIfUnchanged(req RunRequest, result *Result, finished time.Time) error {
	state, err := git.ReadWorkingState(req.Worktree.Path)
	if err != nil {
		return fmt.Errorf("re-reading the working tree: %w", err)
	}
	if state.Digest != result.Identity.Git.Digest || state.Dirty {
		return errors.New("the run changed the working tree")
	}
	return req.reuseIndex().record(result, finished)
}

// Run-specific variables give runners stable cache keys.
func checkVariables(check string, identity Identity, runID, runDir string) []workenv.Variable {
	dirty := "false"
	if identity.Git.Dirty {
		dirty = "true"
	}
	vars := []workenv.Variable{
		{Name: "ANVIL_CHECK_NAME", Value: check},
		{Name: "ANVIL_CHECK_RUN_ID", Value: runID},
		{Name: "ANVIL_CHECK_RUN_DIR", Value: runDir},
		{Name: "ANVIL_CHECK_IDENTITY", Value: identity.Digest},
		{Name: "ANVIL_GIT_HEAD", Value: identity.Git.Head},
		{Name: "ANVIL_GIT_STATE", Value: identity.Git.Digest},
		{Name: "ANVIL_GIT_DIRTY", Value: dirty},
	}
	for i := range vars {
		vars[i].Source = "check"
	}
	return vars
}

func runCommand(parent, ctx context.Context, req RunRequest, command config.CheckCommand, target CommandTarget, guards []*lockfile.Lock, identity Identity, runID, runDir string, index int) (CommandResult, string, error) {
	for _, variable := range checkVariables(req.Check.Name, identity, runID, runDir) {
		target.Env.Set(variable)
	}
	logPath := filepath.Join(runDir, fmt.Sprintf("command-%02d.log", index))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return CommandResult{Run: command.Run}, StatusError, err
	}
	result := CommandResult{Run: command.Run, Dir: target.Dir, Log: logPath}

	timeout, err := config.ParseOptionalDuration(command.Timeout)
	if err != nil {
		return result, StatusError, errors.Join(fmt.Errorf("command timeout: %w", err), logFile.Close())
	}
	commandCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		commandCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd, finish, err := supervisedCommand(commandCtx, command.Run, guards)
	if err != nil {
		return result, StatusError, errors.Join(err, logFile.Close())
	}
	cmd.Dir = target.Dir
	cmd.Env = target.Env.Environ(nil, nil, false)
	var output io.Writer = logFile
	if req.Output != nil {
		output = io.MultiWriter(logFile, req.Output)
	}
	cmd.Stdout, cmd.Stderr = output, output
	cmd.Stdin = nil

	start := time.Now()
	runErr := cmd.Run()
	finishErr := finish()
	result.DurationMS = time.Since(start).Milliseconds()
	if err := errors.Join(logFile.Close(), finishErr); err != nil {
		return result, StatusError, err
	}

	switch {
	case parent.Err() != nil:
		result.ExitCode = 130
		return result, StatusInterrupted, nil
	case commandCtx.Err() != nil:
		result.ExitCode, result.TimedOut = 124, true
		return result, StatusTimedOut, nil
	case runErr == nil:
		return result, StatusPassed, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		if result.ExitCode <= 0 {
			result.ExitCode = 1
		}
		return result, StatusFailed, nil
	}
	return result, StatusError, runErr
}

func collectArtifacts(req RunRequest, result *Result) error {
	paths, err := ResolvePaths(req.Worktree.Path, req.RuntimeDir, req.Check.Artifacts, req.Env.Map())
	if err != nil {
		return err
	}
	patterns := make([]string, 0, len(paths))
	for pattern := range paths {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	for _, pattern := range patterns {
		if len(paths[pattern]) == 0 {
			result.MissingArtifacts = append(result.MissingArtifacts, pattern)
		}
		for _, match := range paths[pattern] {
			info, err := os.Stat(match)
			if err != nil {
				return err
			}
			if info.IsDir() {
				return fmt.Errorf("directory artifacts are unsupported: %s; declare file patterns instead", match)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("unsupported artifact type: %s", match)
			}
			sum, size, err := fileDigest(match)
			if err != nil {
				return err
			}
			result.Artifacts = append(result.Artifacts, Artifact{Path: match, SHA256: sum, Size: size})
		}
	}
	return nil
}
