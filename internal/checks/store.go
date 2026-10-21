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
	"strings"
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

// ArtifactDirectory marks a directory artifact, whose SHA256 digests its
// whole tree and Size totals its files.
const ArtifactDirectory = "directory"

// Artifact records one produced file or directory.
type Artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Kind   string `json:"kind,omitempty"`
}

// WorktreeRef identifies where a check ran.
type WorktreeRef struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
}

// Result is the persisted provenance of one check run.
type Result struct {
	Schema           string          `json:"schema"`
	RunID            string          `json:"run_id"`
	Check            string          `json:"check"`
	Status           string          `json:"status"`
	ExitCode         int             `json:"exit_code"`
	Error            string          `json:"error,omitempty"`
	StartedAt        time.Time       `json:"started_at"`
	FinishedAt       *time.Time      `json:"finished_at,omitempty"`
	DurationMS       int64           `json:"duration_ms"`
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
	return now.UTC().Format(runIDTime) + "-" + hex.EncodeToString(buf), nil
}

// runIDTime orders run IDs chronologically to the nanosecond; earlier
// versions used whole seconds (legacyRunIDTime).
const (
	runIDTime       = "20060102T150405.000000000Z"
	legacyRunIDTime = "20060102T150405Z"
)

// runTime parses the start time encoded in a run ID.
func runTime(runID string) (time.Time, bool) {
	stamp, _, _ := strings.Cut(runID, "-")
	for _, layout := range []string{runIDTime, legacyRunIDTime} {
		if parsed, err := time.Parse(layout, stamp); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
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

// prune keeps the most recent run directories by start time (ties broken
// by ID), and never removes keep or the run latest.json refers to.
func (s Store) prune(check, keep string) error {
	runs, err := os.ReadDir(filepath.Join(s.checkDir(check), "runs"))
	if err != nil {
		return err
	}
	protected := map[string]bool{keep: true}
	if latest, err := s.Latest(check); err == nil && latest != nil {
		protected[latest.RunID] = true
	}
	var candidates []string
	kept := 0
	for _, run := range runs {
		if !run.IsDir() {
			continue
		}
		if protected[run.Name()] {
			kept++
			continue
		}
		candidates = append(candidates, run.Name())
	}
	// Oldest first; unparseable names sort before every dated run.
	sort.Slice(candidates, func(i, j int) bool {
		ti, okI := runTime(candidates[i])
		tj, okJ := runTime(candidates[j])
		if okI != okJ {
			return !okI
		}
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return candidates[i] < candidates[j]
	})
	var problems []error
	for len(candidates)+kept > retainedRuns && len(candidates) > 0 {
		if err := os.RemoveAll(s.runDir(check, candidates[0])); err != nil {
			problems = append(problems, err)
		}
		candidates = candidates[1:]
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
