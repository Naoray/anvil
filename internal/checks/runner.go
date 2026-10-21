package checks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/naoray/anvil/internal/config"
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
	// Gate is the caller's shared hold on the worktree's check gate
	// (GatePath). It is passed to commands with the run's other locks so
	// cleanup stays blocked while any command survives, even if Anvil dies.
	Gate *lockfile.Lock
	Now  func() time.Time
}

// GatePath is the per-worktree lock that check runs hold shared for their
// whole lifetime and worktree removal takes exclusively before cleanup.
func GatePath(worktreePath string) string {
	return config.WorktreeLockPath(worktreePath, "checks-gate.lock")
}

// ErrAlreadyRunning reports a concurrent run of the same check in the same worktree.
var ErrAlreadyRunning = errors.New("this check is already running in this worktree")

// ErrInterrupted reports a run cancelled before any command started, for
// example while waiting for a resource lock.
var ErrInterrupted = errors.New("interrupted before any command started")

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
	// Resource locks come first: the identity must describe the state the
	// commands run against, not the state before a possibly long wait.
	locks, err := acquireResourceLocks(ctx, req.RuntimeRoot, req.Check.Locks, req.LockWait, LockHolder{
		PID: os.Getpid(), WorktreeID: req.Worktree.ID, Worktree: req.Worktree.Path, Check: req.Check.Name, RunID: runID, Since: started,
	})
	if ctx.Err() != nil {
		return nil, errors.Join(fmt.Errorf("%w: %w", ErrInterrupted, ctx.Err()), locks.release())
	}
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, locks.release()) }()

	identity, err := ComputeIdentity(ctx, IdentityRequest{
		WorktreePath: req.Worktree.Path, RuntimeDir: req.RuntimeDir, Check: req.Check, Env: req.Env,
		Resolve: req.Resolve, Unset: req.Unset, SecretEnv: req.SecretEnv,
	})
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%w: %w", ErrInterrupted, ctx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("computing check identity: %w", err)
	}

	runDir := store.runDir(req.Check.Name, runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, err
	}
	runLock, err := lockfile.Acquire(runLockPath(runDir))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, runLock.Release()) }()

	// Commands inherit every lock of the run, so exclusivity, "running"
	// status and cleanup protection last as long as any command does.
	inherited := []*os.File{active.File(), runLock.File()}
	if req.Gate != nil {
		inherited = append(inherited, req.Gate.File())
	}
	for _, lock := range locks {
		inherited = append(inherited, lock.File())
	}

	result = &Result{
		Schema: ResultSchema, RunID: runID, Check: req.Check.Name, Status: StatusRunning,
		StartedAt: started, Worktree: req.Worktree, Identity: identity,
		Commands: []CommandResult{}, Artifacts: []Artifact{}, Locks: append([]string{}, req.Check.Locks...),
		AnvilVersion: req.AnvilVersion, RunDir: runDir,
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
		commandResult, status, runErr := runCommand(ctx, checkCtx, req, command, identity, runID, runDir, i, inherited)
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
	return result, store.prune(req.Check.Name, runID)
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

func runCommand(parent, ctx context.Context, req RunRequest, command config.CheckCommand, identity Identity, runID, runDir string, index int, inherited []*os.File) (CommandResult, string, error) {
	target, err := req.Resolve(command)
	if err != nil {
		return CommandResult{Run: command.Run}, StatusError, err
	}
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
	cmd := exec.CommandContext(commandCtx, "sh", "-c", command.Run)
	cmd.Dir = target.Dir
	cmd.Env = target.Env.Environ(os.Environ(), req.Unset, false)
	var output io.Writer = logFile
	if req.Output != nil {
		output = io.MultiWriter(logFile, req.Output)
	}
	cmd.Stdout, cmd.Stderr = output, output
	cmd.Stdin = nil
	configureGroup(cmd)
	inheritFiles(cmd, inherited)

	start := time.Now()
	runErr := cmd.Run()
	killGroup(cmd)
	result.DurationMS = time.Since(start).Milliseconds()
	if err := logFile.Close(); err != nil && runErr == nil {
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

// treeDigest content-addresses a directory artifact: every entry's relative
// path and each file's content, so tampering, deletion and added files all
// change it. Links must stay inside the directory.
func treeDigest(dir string) (string, int64, error) {
	h := sha256.New()
	writeField(h, "anvil-artifact-tree-v1")
	var total int64
	err := walkContained(dir, "", dir, func(label, path string, info fs.FileInfo) error {
		writeField(h, label)
		if !info.Mode().IsRegular() {
			return nil
		}
		sum, size, err := fileDigest(path)
		if err != nil {
			return err
		}
		total += size
		writeField(h, sum)
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), total, nil
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
				sum, size, err := treeDigest(match)
				if err != nil {
					return err
				}
				result.Artifacts = append(result.Artifacts, Artifact{Path: match, SHA256: sum, Size: size, Kind: ArtifactDirectory})
				continue
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
