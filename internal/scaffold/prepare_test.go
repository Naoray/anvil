package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/wtstate"
)

func prepareFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out, err := exec.Command("git", "init", dir).CombinedOutput()
	require.NoError(t, err, string(out))
	canonical, err := wtstate.CanonicalPath(dir)
	require.NoError(t, err)
	return canonical
}

// countingStep appends one line to counter file name each time it runs.
func countingStep(name string, extra func(*config.StepConfig)) config.StepConfig {
	cfg := config.StepConfig{Name: config.StepCommandRun, Command: "echo run >> " + name + ".count"}
	if extra != nil {
		extra(&cfg)
	}
	return cfg
}

func runCount(t *testing.T, dir, name string) int {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, name+".count"))
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	return strings.Count(string(content), "run")
}

func ensureRun(t *testing.T, dir string, steps []config.StepConfig) (*PrepareReport, error) {
	t.Helper()
	cfg := &config.Config{Scaffold: config.ScaffoldConfig{Override: true, Steps: steps}}
	return NewScaffoldManager().RunScaffoldWithOptions(ScaffoldRequest{
		WorktreePath:  dir,
		Branch:        "feature",
		Config:        cfg,
		Quiet:         true,
		SkipCompleted: true,
	})
}

func results(report *PrepareReport) []string {
	out := make([]string, len(report.Steps))
	for i, step := range report.Steps {
		out[i] = step.Result
	}
	return out
}

func TestEnsureSkipsCompletedStepsAndDetectsChangedInputs(t *testing.T) {
	dir := prepareFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deps.lock"), []byte("v1"), 0o644))
	steps := []config.StepConfig{
		countingStep("once", nil),
		countingStep("install", func(c *config.StepConfig) {
			c.Inputs = []string{"deps.lock"}
			c.Outputs = []string{"install.count"}
		}),
		countingStep("always", func(c *config.StepConfig) { c.Always = true }),
		countingStep("producer", func(c *config.StepConfig) { c.StoreAs = "Produced" }),
	}

	report, err := ensureRun(t, dir, steps)
	require.NoError(t, err)
	require.Equal(t, []string{OutcomeRan, OutcomeRan, OutcomeRan, OutcomeRan}, results(report))

	report, err = ensureRun(t, dir, steps)
	require.NoError(t, err)
	require.Equal(t, []string{OutcomeUpToDate, OutcomeUpToDate, OutcomeRan, OutcomeRan}, results(report),
		"store_as producers and always steps rerun; cached steps do not")
	require.Equal(t, 1, runCount(t, dir, "once"))
	require.Equal(t, 1, runCount(t, dir, "install"))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "deps.lock"), []byte("v2"), 0o644))
	report, err = ensureRun(t, dir, steps)
	require.NoError(t, err)
	require.Equal(t, OutcomeRan, report.Steps[1].Result)
	require.Equal(t, "inputs changed", report.Steps[1].Reason)
	require.Equal(t, 2, runCount(t, dir, "install"))

	require.NoError(t, os.Remove(filepath.Join(dir, "install.count")))
	report, err = ensureRun(t, dir, steps)
	require.NoError(t, err)
	require.Equal(t, "output install.count missing", report.Steps[1].Reason)

	state, err := wtstate.ReadPrepare(dir)
	require.NoError(t, err)
	require.Equal(t, wtstate.PrepareComplete, state.Status)
	require.Equal(t, report.PlanDigest, state.PlanDigest)
}

func TestEnsureChangedDefinitionReruns(t *testing.T) {
	dir := prepareFixture(t)
	_, err := ensureRun(t, dir, []config.StepConfig{countingStep("a", nil)})
	require.NoError(t, err)
	changed := countingStep("a", func(c *config.StepConfig) { c.Command += " # changed" })
	report, err := ensureRun(t, dir, []config.StepConfig{changed})
	require.NoError(t, err)
	require.Equal(t, []string{OutcomeRan}, results(report))
	require.Equal(t, 2, runCount(t, dir, "a"))
	state, err := wtstate.ReadPrepare(dir)
	require.NoError(t, err)
	require.Len(t, state.Steps, 1, "superseded definitions are pruned")
}

func TestEnsureResumesAfterFailure(t *testing.T) {
	dir := prepareFixture(t)
	steps := []config.StepConfig{
		countingStep("first", nil),
		{Name: config.StepCommandRun, Command: "test -f allow-second && echo run >> second.count"},
		countingStep("third", nil),
	}
	report, err := ensureRun(t, dir, steps)
	require.Error(t, err)
	require.Equal(t, []string{OutcomeRan, OutcomeFailed, OutcomeNotReached}, results(report))
	state, err := wtstate.ReadPrepare(dir)
	require.NoError(t, err)
	require.Equal(t, wtstate.PrepareFailed, state.Status)
	require.Equal(t, report.Steps[1].Key, state.FailedStep)
	require.NotContains(t, state.Error, "\n")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "allow-second"), nil, 0o644))
	report, err = ensureRun(t, dir, steps)
	require.NoError(t, err)
	require.Equal(t, []string{OutcomeUpToDate, OutcomeRan, OutcomeRan}, results(report))
	require.Equal(t, 1, runCount(t, dir, "first"), "completed step is not repeated on resume")
	require.Equal(t, 1, runCount(t, dir, "second"))
	require.Equal(t, 1, runCount(t, dir, "third"))
}

func TestEnsureResumesAfterInterruptedRun(t *testing.T) {
	dir := prepareFixture(t)
	steps := []config.StepConfig{countingStep("first", nil), countingStep("second", nil)}
	// Simulate a process killed after the first step completed.
	require.NoError(t, wtstate.BeginPrepare(dir, "plan", timeNow()))
	plan, _ := buildPlan(steps, nil)
	recorder := &prepareRecorder{worktreePath: dir, plan: plan, now: timeNow, reasons: map[int]string{}}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "first.count"), []byte("run\n"), 0o644))
	require.NoError(t, recorder.AfterStep(0, nil, nil))
	state, err := wtstate.ReadPrepare(dir)
	require.NoError(t, err)
	status, err := wtstate.EffectivePrepareStatus(dir, state)
	require.NoError(t, err)
	require.Equal(t, wtstate.PrepareInterrupted, status)

	report, err := ensureRun(t, dir, steps)
	require.NoError(t, err)
	require.Equal(t, []string{OutcomeUpToDate, OutcomeRan}, results(report))
	require.Equal(t, 1, runCount(t, dir, "first"))
}

func TestScaffoldWithoutSkipStillRunsEverythingButRecords(t *testing.T) {
	dir := prepareFixture(t)
	steps := []config.StepConfig{countingStep("a", nil)}
	cfg := &config.Config{Scaffold: config.ScaffoldConfig{Override: true, Steps: steps}}
	manager := NewScaffoldManager()
	for i := 0; i < 2; i++ {
		require.NoError(t, manager.RunScaffold(dir, "feature", "", "", "", nil, cfg, false, false, true))
	}
	require.Equal(t, 2, runCount(t, dir, "a"), "anvil scaffold semantics are unchanged")
	report, err := ensureRun(t, dir, steps)
	require.NoError(t, err)
	require.Equal(t, []string{OutcomeUpToDate}, results(report), "scaffold runs feed ensure")
}

func TestDryRunWritesNoPreparationState(t *testing.T) {
	dir := prepareFixture(t)
	cfg := &config.Config{Scaffold: config.ScaffoldConfig{Override: true, Steps: []config.StepConfig{countingStep("a", nil)}}}
	require.NoError(t, NewScaffoldManager().RunScaffold(dir, "feature", "", "", "", nil, cfg, true, false, true))
	require.NoFileExists(t, filepath.Join(dir, config.LocalStateFile))
}

func TestStepDirAndEnvApplyToCommandsAndConditions(t *testing.T) {
	dir := prepareFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "apps", "web"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "apps", "web", "package.json"), []byte("{}"), 0o644))
	steps := []config.StepConfig{
		{
			Name:            config.StepCommandRun,
			Command:         `printf '%s' "$GREETING" > out.txt`,
			Dir:             "apps/web",
			Env:             map[string]string{"GREETING": "hi-${ANVIL_TEST_NAME}"},
			ConditionHolder: config.ConditionHolder{Condition: map[string]any{"file_exists": "package.json"}},
		},
		{
			Name:            config.StepCommandRun,
			Command:         "touch root-marker",
			ConditionHolder: config.ConditionHolder{Condition: map[string]any{"file_exists": "package.json"}},
		},
	}
	cfg := &config.Config{Scaffold: config.ScaffoldConfig{Override: true, Steps: steps}}
	report, err := NewScaffoldManager().RunScaffoldWithOptions(ScaffoldRequest{
		WorktreePath: dir, Config: cfg, Quiet: true, Env: map[string]string{"ANVIL_TEST_NAME": "anvil"},
	})
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(dir, "apps", "web", "out.txt"))
	require.NoError(t, err)
	require.Equal(t, "hi-anvil", string(content))
	require.Equal(t, OutcomeSkipped, report.Steps[1].Result, "root has no package.json")
	require.NoFileExists(t, filepath.Join(dir, "root-marker"))
}

func TestStepPathValidationRejectsEscapes(t *testing.T) {
	for _, cfg := range []config.StepConfig{
		{Name: config.StepCommandRun, Command: "true", Dir: "../outside"},
		{Name: config.StepCommandRun, Command: "true", Dir: "/abs"},
		{Name: config.StepCommandRun, Command: "true", Inputs: []string{"../x"}},
		{Name: config.StepCommandRun, Command: "true", Env: map[string]string{"BAD-NAME": "x"}},
	} {
		dir := prepareFixture(t)
		_, err := ensureRun(t, dir, []config.StepConfig{cfg})
		require.Error(t, err)
		require.NoFileExists(t, filepath.Join(dir, config.LocalStateFile), "validation happens before effects")
	}
}

func TestInputsDigestCoversDirectoriesGlobsAndMissing(t *testing.T) {
	dir := t.TempDir()
	missing, err := InputsDigest(dir, []string{"*.lock"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.lock"), []byte("1"), 0o644))
	one, err := InputsDigest(dir, []string{"*.lock"})
	require.NoError(t, err)
	require.NotEqual(t, missing, one)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src", "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "nested", "f"), []byte("x"), 0o644))
	first, err := InputsDigest(dir, []string{"src"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "nested", "f"), []byte("y"), 0o644))
	second, err := InputsDigest(dir, []string{"src"})
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	empty, err := InputsDigest(dir, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}

func timeNow() time.Time { return time.Now() }
