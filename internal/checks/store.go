package checks

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/naoray/anvil/internal/lockfile"
)

// ResultSchema versions the persisted result document.
const ResultSchema = "anvil.check-result/v1"

// Result statuses. Only StatusPassed is a success; a run that did not finish
// (killed Anvil, reboot) is reported as StatusInterrupted, never passed.
const (
	StatusRunning     = "running"
	StatusPassed      = "passed"
	StatusFailed      = "failed"
	StatusTimedOut    = "timed_out"
	StatusInterrupted = "interrupted"
	StatusError       = "error"
)

const retainedRuns = 20

// CommandResult records one executed command.
type CommandResult struct {
	Run        string `json:"run"`
	Dir        string `json:"dir"`
	ExitCode   int    `json:"exit_code"`
	TimedOut   bool   `json:"timed_out,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Log        string `json:"log"`
}

// Artifact records one produced file.
type Artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// WorktreeRef identifies where a check ran.
type WorktreeRef struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
}

// Result is the persisted provenance of one check run.
type Result struct {
	Reused     bool       `json:"-"`
	Schema     string     `json:"schema"`
	RunID      string     `json:"run_id"`
	Check      string     `json:"check"`
	Status     string     `json:"status"`
	ExitCode   int        `json:"exit_code"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	DurationMS int64      `json:"duration_ms"`
	// QueuedMS is the time spent waiting for a machine-wide check slot,
	// excluded from StartedAt and DurationMS.
	QueuedMS         int64           `json:"queued_ms"`
	Worktree         WorktreeRef     `json:"worktree"`
	Identity         Identity        `json:"identity"`
	Commands         []CommandResult `json:"commands"`
	Artifacts        []Artifact      `json:"artifacts"`
	MissingArtifacts []string        `json:"missing_artifacts,omitempty"`
	Locks            []string        `json:"locks"`
	AnvilVersion     string          `json:"anvil_version"`
	RunDir           string          `json:"run_dir"`
}

// Store persists results in a worktree's runtime directory.
type Store struct {
	RuntimeDir string
}

func (s Store) checkDir(check string) string { return filepath.Join(s.RuntimeDir, "checks", check) }

func (s Store) runDir(check, runID string) string {
	return filepath.Join(s.checkDir(check), "runs", runID)
}

func newRunID(now time.Time) (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return now.UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(buf), nil
}

func runLockPath(runDir string) string { return filepath.Join(runDir, "run.lock") }

// activeLockPath serializes runs of one check within one worktree.
func (s Store) activeLockPath(check string) string {
	return filepath.Join(s.checkDir(check), "active.lock")
}

func writeJSON(path string, value any) (err error) {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".result-")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if removeErr := os.Remove(tmp.Name()); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, removeErr)
			}
		}
	}()
	if _, err = tmp.Write(content); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err = tmp.Sync(); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// save writes the run's result and points latest.json at it.
func (s Store) save(result *Result) error {
	if err := writeJSON(filepath.Join(result.RunDir, "result.json"), result); err != nil {
		return err
	}
	return writeJSON(filepath.Join(s.checkDir(result.Check), "latest.json"), result)
}

// Latest returns the newest result for a check (nil when none), resolving a
// stored "running" status to "interrupted" when no process holds the run.
func (s Store) Latest(check string) (*Result, error) {
	content, err := os.ReadFile(filepath.Join(s.checkDir(check), "latest.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result Result
	if err := json.Unmarshal(content, &result); err != nil {
		return nil, fmt.Errorf("malformed check result: %w", err)
	}
	if result.Status == StatusRunning {
		held, err := lockfile.IsHeld(runLockPath(result.RunDir))
		if err != nil {
			return nil, err
		}
		if !held {
			result.Status = StatusInterrupted
			result.Error = "the run ended without recording an outcome (Anvil was killed or the machine stopped)"
		}
	}
	return &result, nil
}

// prune keeps the most recent run directories.
func (s Store) prune(check string) error {
	runs, err := os.ReadDir(filepath.Join(s.checkDir(check), "runs"))
	if err != nil {
		return err
	}
	latest, err := s.Latest(check)
	if err != nil {
		return err
	}
	protected := ""
	if latest != nil {
		protected = latest.RunID
	}
	type savedRun struct {
		name    string
		started time.Time
	}
	var ordered []savedRun
	for _, run := range runs {
		if !run.IsDir() {
			continue
		}
		info, err := run.Info()
		if err != nil {
			return err
		}
		started := info.ModTime()
		data, err := os.ReadFile(filepath.Join(s.runDir(check, run.Name()), "result.json"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			var result Result
			if err := json.Unmarshal(data, &result); err != nil {
				return fmt.Errorf("reading retention chronology for %s: %w", run.Name(), err)
			}
			if !result.StartedAt.IsZero() {
				started = result.StartedAt
			}
		}
		ordered = append(ordered, savedRun{name: run.Name(), started: started})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].started.Equal(ordered[j].started) {
			return ordered[i].name < ordered[j].name
		}
		return ordered[i].started.Before(ordered[j].started)
	})
	remaining := len(ordered)
	var problems []error
	for _, run := range ordered {
		if remaining <= retainedRuns {
			break
		}
		if run.name == protected {
			continue
		}
		held, err := lockfile.IsHeld(runLockPath(s.runDir(check, run.name)))
		if err != nil {
			return errors.Join(append(problems, err)...)
		}
		if held {
			continue
		}
		if err := os.RemoveAll(s.runDir(check, run.name)); err != nil {
			problems = append(problems, err)
		} else {
			remaining--
		}
	}

	return errors.Join(problems...)
}

// RunningChecks lists checks a live process is currently running in the
// runtime directory.
func RunningChecks(runtimeDir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(runtimeDir, "checks"))
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	running := []string{}
	store := Store{RuntimeDir: runtimeDir}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		held, err := lockfile.IsHeld(store.activeLockPath(entry.Name()))
		if err != nil {
			return nil, err
		}
		if held {
			running = append(running, entry.Name())
		}
	}
	return running, nil
}
