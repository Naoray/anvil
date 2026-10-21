package scaffold

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/steps"
	"github.com/naoray/anvil/internal/scaffold/types"
	"github.com/naoray/anvil/internal/wtstate"
)

// Step outcomes reported by preparation.
const (
	OutcomeRan        = "ran"
	OutcomeUpToDate   = "up_to_date"
	OutcomeSkipped    = "skipped"
	OutcomeFailed     = "failed"
	OutcomeNotReached = "not_reached"
)

// alwaysRunSteps own their idempotency and safety policy (database ownership,
// migration authorization, site links); a completion record never bypasses them.
var alwaysRunSteps = map[string]bool{
	config.StepDbCreate:  true,
	config.StepDbDestroy: true,
	"db.migrate":         true,
	"herd":               true,
	"yerd":               true,
}

// contextProducers set template variables without store_as; skipping them
// would starve later steps that consume those variables.
var contextProducers = map[string]bool{
	config.StepEnvRead: true,
}

// PrepareReport describes one preparation run.
type PrepareReport struct {
	PlanDigest string        `json:"plan_digest"`
	Steps      []StepOutcome `json:"steps"`
}

// StepOutcome is the per-step result of a preparation run.
type StepOutcome struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Key    string `json:"key"`
	Dir    string `json:"dir,omitempty"`
	Result string `json:"result"`
	Reason string `json:"reason,omitempty"`
}

type planEntry struct {
	key        string
	definition string
	cfg        config.StepConfig
	cacheable  bool
	// env digests the declared environment the step receives.
	env string
}

// definitionDigest identifies a step definition independent of its position.
func definitionDigest(cfg config.StepConfig) string {
	encoded, err := json.Marshal(cfg)
	if err != nil {
		// StepConfig contains only JSON-encodable values (strings, slices, maps).
		panic(fmt.Sprintf("encoding step definition: %v", err))
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func buildPlan(configs []config.StepConfig) ([]planEntry, string) {
	plan := make([]planEntry, len(configs))
	occurrences := map[string]int{}
	planHash := sha256.New()
	for i, cfg := range configs {
		definition := definitionDigest(cfg)
		occurrences[definition]++
		plan[i] = planEntry{
			key:        fmt.Sprintf("%s#%s#%d", cfg.Name, definition[:16], occurrences[definition]),
			definition: definition,
			cfg:        cfg,
			cacheable:  !cfg.Always && cfg.StoreAs == "" && !alwaysRunSteps[cfg.Name] && !contextProducers[cfg.Name],
		}
		writeHashString(planHash, definition)
	}
	return plan, hex.EncodeToString(planHash.Sum(nil))
}

// applyEnvDigests records, per step, a digest of the declared environment
// it receives: the project env keys and the step's own keys, with the
// values after expansion. PATH is left out so differences in the caller's
// shell PATH do not invalidate preparation; its declared prefix is part of
// the step definition.
func applyEnvDigests(plan []planEntry, base map[string]string, projectKeys []string) {
	for i := range plan {
		resolved := steps.ResolveEnv(base, plan[i].cfg.Env)
		keys := map[string]bool{}
		for _, key := range projectKeys {
			keys[key] = true
		}
		for key := range plan[i].cfg.Env {
			keys[key] = true
		}
		delete(keys, "PATH")
		if len(keys) == 0 {
			continue
		}
		names := make([]string, 0, len(keys))
		for key := range keys {
			names = append(names, key)
		}
		sort.Strings(names)
		h := sha256.New()
		for _, name := range names {
			writeHashString(h, name)
			value, ok := resolved[name]
			writeHashString(h, fmt.Sprintf("%t", ok))
			writeHashString(h, value)
		}
		plan[i].env = hex.EncodeToString(h.Sum(nil))
	}
}

func writeHashString(w io.Writer, value string) {
	// Hash writers never fail; length-prefixing keeps field boundaries unambiguous.
	if _, err := fmt.Fprintf(w, "%d:%s\n", len(value), value); err != nil {
		panic(err)
	}
}

func stepRoot(worktreePath string, cfg config.StepConfig) string {
	if cfg.Dir == "" {
		return worktreePath
	}
	return filepath.Join(worktreePath, cfg.Dir)
}

// InputsDigest content-addresses the step's declared inputs. Missing inputs
// are part of the identity, so creating one later invalidates the record.
func InputsDigest(root string, patterns []string) (string, error) {
	if len(patterns) == 0 {
		return "", nil
	}
	h := sha256.New()
	for _, pattern := range patterns {
		writeHashString(h, "pattern")
		writeHashString(h, pattern)
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return "", fmt.Errorf("input pattern %q: %w", pattern, err)
		}
		sort.Strings(matches)
		if len(matches) == 0 {
			writeHashString(h, "missing")
			continue
		}
		for _, match := range matches {
			if err := config.CheckResolvedContained(root, match, pattern); err != nil {
				return "", err
			}
			if err := hashTree(h, root, match); err != nil {
				return "", err
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashTree(h io.Writer, root, start string) error {
	return filepath.WalkDir(start, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		writeHashString(h, filepath.ToSlash(rel))
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			writeHashString(h, "link:"+target)
		case entry.IsDir():
			writeHashString(h, "dir")
		case entry.Type().IsRegular():
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			sum := sha256.New()
			_, copyErr := io.Copy(sum, file)
			if err := errors.Join(copyErr, file.Close()); err != nil {
				return err
			}
			writeHashString(h, "file:"+hex.EncodeToString(sum.Sum(nil)))
		default:
			writeHashString(h, "special")
		}
		return nil
	})
}

// OutputsPresent reports whether every declared output pattern matches.
func OutputsPresent(root string, patterns []string) (bool, string, error) {
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return false, "", fmt.Errorf("output pattern %q: %w", pattern, err)
		}
		if len(matches) == 0 {
			return false, pattern, nil
		}
		for _, match := range matches {
			if err := config.CheckResolvedContained(root, match, pattern); err != nil {
				return false, "", err
			}
		}
	}
	return true, "", nil
}

// prepareRecorder persists step completions and, when skipCompleted is set,
// skips steps whose completion record is still valid.
type prepareRecorder struct {
	worktreePath  string
	plan          []planEntry
	records       map[string]wtstate.StepRecord
	skipCompleted bool
	now           func() time.Time
	reasons       map[int]string
}

func (r *prepareRecorder) BeforeStep(index int, _ types.ScaffoldStep) (bool, string, error) {
	entry := r.plan[index]
	if !entry.cacheable {
		return false, "", nil
	}
	record, ok := r.records[entry.key]
	if !ok {
		return false, "", nil
	}
	if !r.skipCompleted || record.Definition != entry.definition {
		return false, "", r.invalidate(entry.key)
	}
	current, reason, err := recordCurrent(r.worktreePath, entry, record)
	if err != nil {
		return false, "", err
	}
	if !current {
		r.reasons[index] = reason
		return false, "", r.invalidate(entry.key)
	}
	return true, SkipUpToDate, nil
}

// recordCurrent reports whether a completion record with a matching
// definition still describes the step's environment, inputs and outputs.
func recordCurrent(worktreePath string, entry planEntry, record wtstate.StepRecord) (bool, string, error) {
	if record.Env != entry.env {
		return false, "environment changed", nil
	}
	root := stepRoot(worktreePath, entry.cfg)
	inputs, err := InputsDigest(root, entry.cfg.Inputs)
	if err != nil {
		return false, "", err
	}
	if inputs != record.Inputs {
		return false, "inputs changed", nil
	}
	present, missing, err := OutputsPresent(root, entry.cfg.Outputs)
	if err != nil {
		return false, "", err
	}
	if !present {
		return false, fmt.Sprintf("output %s missing", missing), nil
	}
	return true, "", nil
}

// invalidate durably drops a completion record before its step reruns, so
// a failed or killed rerun can never leave the earlier success in place.
// Only a later successful run records completion again.
func (r *prepareRecorder) invalidate(key string) error {
	if err := wtstate.ForgetStep(r.worktreePath, key); err != nil {
		return fmt.Errorf("invalidating completion record: %w", err)
	}
	delete(r.records, key)
	return nil
}

func (r *prepareRecorder) AfterStep(index int, _ types.ScaffoldStep, stepErr error) error {
	if stepErr != nil {
		return nil
	}
	entry := r.plan[index]
	inputs, err := InputsDigest(stepRoot(r.worktreePath, entry.cfg), entry.cfg.Inputs)
	if err != nil {
		return fmt.Errorf("recording step inputs: %w", err)
	}
	return wtstate.RecordStep(r.worktreePath, entry.key, wtstate.StepRecord{
		Name:        entry.cfg.Name,
		Definition:  entry.definition,
		Inputs:      inputs,
		Env:         entry.env,
		CompletedAt: r.now(),
	})
}

func (r *prepareRecorder) planKeys() []string {
	keys := make([]string, len(r.plan))
	for i, entry := range r.plan {
		keys[i] = entry.key
	}
	return keys
}

// buildReport merges executor results with the plan. Steps after a failure
// are reported as not reached.
func buildReport(plan []planEntry, planDigest string, results []ExecutionResult, reasons map[int]string) *PrepareReport {
	report := &PrepareReport{PlanDigest: planDigest, Steps: make([]StepOutcome, len(plan))}
	for i, entry := range plan {
		report.Steps[i] = StepOutcome{Index: i, Name: entry.cfg.Name, Key: entry.key, Dir: entry.cfg.Dir, Result: OutcomeNotReached}
	}
	for _, result := range results {
		outcome := &report.Steps[result.Index]
		switch {
		case result.Error != nil:
			outcome.Result = OutcomeFailed
		case result.Skipped && result.SkipReason == SkipUpToDate:
			outcome.Result = OutcomeUpToDate
		case result.Skipped:
			outcome.Result = OutcomeSkipped
			outcome.Reason = result.SkipReason
		default:
			outcome.Result = OutcomeRan
			outcome.Reason = reasons[result.Index]
		}
	}
	return report
}

// ReadinessRequest describes a subset of preparation steps to verify, for
// example one component's.
type ReadinessRequest struct {
	WorktreePath string
	Branch       string
	// Env is the worktree environment ensure exports to steps.
	Env    map[string]string
	Config *config.Config
	Steps  []config.StepConfig
}

// StepsPrepared reports whether every applicable step has a completion
// record for its current definition that still matches its environment,
// inputs and outputs. Steps whose conditions are not met are ignored. It
// never writes state.
func (m *ScaffoldManager) StepsPrepared(request ReadinessRequest) (bool, string, error) {
	state, err := wtstate.ReadPrepare(request.WorktreePath)
	if err != nil {
		return false, "", err
	}
	byDefinition := map[string]wtstate.StepRecord{}
	if state != nil {
		for _, record := range state.Steps {
			byDefinition[record.Definition] = record
		}
	}
	stepsList, err := m.stepsFromConfig(request.Steps)
	if err != nil {
		return false, "", fmt.Errorf("getting scaffold steps: %w", err)
	}
	ctx := m.newScaffoldContext(request.WorktreePath, request.Branch, "", "", "")
	for key, value := range request.Env {
		ctx.Env[key] = value
	}
	var projectKeys []string
	if request.Config != nil {
		projectKeys = sortedEnvKeys(request.Config.Env)
	}
	plan, _ := buildPlan(request.Steps)
	applyEnvDigests(plan, ctx.Env, projectKeys)
	for i, entry := range plan {
		applicable, err := stepsList[i].Condition(&ctx)
		if err != nil {
			return false, "", fmt.Errorf("step %d (%s) condition: %w", i+1, entry.cfg.Name, err)
		}
		if !applicable {
			continue
		}
		label := fmt.Sprintf("step %d (%s)", i+1, entry.cfg.Name)
		if entry.cfg.Command != "" {
			label = fmt.Sprintf("step %d (%s)", i+1, entry.cfg.Command)
		}
		record, ok := byDefinition[entry.definition]
		if !ok {
			return false, label + " has not run", nil
		}
		if !entry.cacheable {
			continue
		}
		current, reason, err := recordCurrent(request.WorktreePath, entry, record)
		if err != nil {
			return false, "", err
		}
		if !current {
			return false, label + ": " + reason, nil
		}
	}
	return true, "", nil
}
