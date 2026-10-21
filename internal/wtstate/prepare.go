package wtstate

import (
	"fmt"
	"strings"
	"time"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/lockfile"
)

const prepareKey = "prepare"

// PrepareLockFile serializes preparation of one worktree across processes.
const PrepareLockFile = "anvil-prepare.lock"

// Preparation states. Interrupted is derived, never stored: a stored
// "running" record whose preparation lock is free belonged to a process that
// died before finishing.
const (
	PrepareRunning     = "running"
	PrepareComplete    = "complete"
	PrepareFailed      = "failed"
	PrepareInterrupted = "interrupted"
	PrepareNone        = "unprepared"
)

const maxRecordedError = 300

// PrepareState is the observed preparation progress of one worktree.
type PrepareState struct {
	Status     string                `yaml:"status" json:"status"`
	PlanDigest string                `yaml:"plan_digest,omitempty" json:"plan_digest,omitempty"`
	StartedAt  time.Time             `yaml:"started_at,omitempty" json:"started_at,omitempty"`
	FinishedAt *time.Time            `yaml:"finished_at,omitempty" json:"finished_at,omitempty"`
	FailedStep string                `yaml:"failed_step,omitempty" json:"failed_step,omitempty"`
	Error      string                `yaml:"error,omitempty" json:"error,omitempty"`
	Steps      map[string]StepRecord `yaml:"steps,omitempty" json:"-"`
}

// StepRecord attests that one step definition completed against an input identity.
type StepRecord struct {
	Name       string `yaml:"name" json:"name"`
	Definition string `yaml:"definition" json:"definition"`
	Inputs     string `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	// Env digests the declared environment the step received; raw values
	// are never stored.
	Env         string    `yaml:"env,omitempty" json:"env,omitempty"`
	CompletedAt time.Time `yaml:"completed_at" json:"completed_at"`
}

// PrepareLockPath returns the per-worktree preparation lock.
func PrepareLockPath(worktreePath string) string {
	return config.WorktreeExternalLockPath(worktreePath, PrepareLockFile)
}

// AcquirePrepareLock blocks until this process may prepare the worktree.
func AcquirePrepareLock(worktreePath string) (*lockfile.Lock, error) {
	return lockfile.Acquire(PrepareLockPath(worktreePath))
}

// ReadPrepare returns the stored preparation record, or nil when none exists.
func ReadPrepare(worktreePath string) (*PrepareState, error) {
	doc, err := config.ReadLocalStateMap(worktreePath)
	if err != nil {
		return nil, err
	}
	return decodePrepare(doc)
}

// EffectivePrepareStatus derives the reader-facing status, detecting
// interrupted runs through the preparation lock instead of timestamps.
func EffectivePrepareStatus(worktreePath string, state *PrepareState) (string, error) {
	if state == nil || state.Status == "" {
		return PrepareNone, nil
	}
	if state.Status != PrepareRunning {
		return state.Status, nil
	}
	held, err := lockfile.IsHeld(PrepareLockPath(worktreePath))
	if err != nil {
		return "", err
	}
	if held {
		return PrepareRunning, nil
	}
	return PrepareInterrupted, nil
}

func decodePrepare(doc map[string]any) (*PrepareState, error) {
	raw, ok := doc[prepareKey]
	if !ok {
		return nil, nil
	}
	var state PrepareState
	if err := config.DecodeSection(raw, &state); err != nil {
		return nil, fmt.Errorf("malformed prepare record: %w", err)
	}
	return &state, nil
}

func updatePrepare(worktreePath string, fn func(*PrepareState)) error {
	return config.UpdateLocalState(worktreePath, func(doc map[string]any) error {
		state, err := decodePrepare(doc)
		if err != nil {
			return err
		}
		if state == nil {
			state = &PrepareState{}
		}
		fn(state)
		encoded, err := config.EncodeSection(state)
		if err != nil {
			return err
		}
		doc[prepareKey] = encoded
		return nil
	})
}

// BeginPrepare marks preparation as running. Completed step records are kept
// so an interrupted or failed run resumes instead of restarting.
func BeginPrepare(worktreePath, planDigest string, now time.Time) error {
	return updatePrepare(worktreePath, func(state *PrepareState) {
		state.Status = PrepareRunning
		state.PlanDigest = planDigest
		state.StartedAt = now.UTC()
		state.FinishedAt = nil
		state.FailedStep = ""
		state.Error = ""
	})
}

// RecordStep persists one completed step immediately after it succeeds.
func RecordStep(worktreePath, key string, record StepRecord) error {
	return updatePrepare(worktreePath, func(state *PrepareState) {
		if state.Steps == nil {
			state.Steps = map[string]StepRecord{}
		}
		record.CompletedAt = record.CompletedAt.UTC()
		state.Steps[key] = record
	})
}

// ForgetStep drops a record whose step must run again (for example when its
// declared outputs disappeared).
func ForgetStep(worktreePath, key string) error {
	return updatePrepare(worktreePath, func(state *PrepareState) {
		delete(state.Steps, key)
	})
}

// FinishPrepare stores the terminal outcome. On success, records for steps
// that are no longer part of the plan are pruned.
func FinishPrepare(worktreePath string, now time.Time, failedStep string, failure error, planKeys []string) error {
	return updatePrepare(worktreePath, func(state *PrepareState) {
		finished := now.UTC()
		state.FinishedAt = &finished
		if failure != nil {
			state.Status = PrepareFailed
			state.FailedStep = failedStep
			state.Error = summarizeError(failure)
			return
		}
		state.Status = PrepareComplete
		keep := make(map[string]bool, len(planKeys))
		for _, key := range planKeys {
			keep[key] = true
		}
		for key := range state.Steps {
			if !keep[key] {
				delete(state.Steps, key)
			}
		}
	})
}

// summarizeError keeps only the first line: step errors may embed command
// output, which must not be persisted wholesale.
func summarizeError(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	if len(line) > maxRecordedError {
		line = line[:maxRecordedError] + "…"
	}
	return line
}
