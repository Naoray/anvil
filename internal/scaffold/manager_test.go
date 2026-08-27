package scaffold

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	scaffoldsteps "github.com/naoray/anvil/internal/scaffold/steps"
	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestCleanupConfigToStepConfig_PreservesArgsAndHerdDefault(t *testing.T) {
	manager := &ScaffoldManager{}

	databaseArgs := []string{"--host=127.0.0.1", "--port=15432"}
	database := manager.cleanupConfigToStepConfig(config.CleanupStep{
		Name: config.StepDbDestroy,
		Args: databaseArgs,
	})
	if !slices.Equal(database.Args, databaseArgs) {
		t.Fatalf("db.destroy args = %v, want %v", database.Args, databaseArgs)
	}

	herd := manager.cleanupConfigToStepConfig(config.CleanupStep{Name: "herd"})
	if !slices.Equal(herd.Args, []string{"unlink"}) {
		t.Fatalf("default herd args = %v, want [unlink]", herd.Args)
	}

	herdOverride := manager.cleanupConfigToStepConfig(config.CleanupStep{
		Name: "herd",
		Args: []string{"unlink", "custom.test"},
	})
	if !slices.Equal(herdOverride.Args, []string{"unlink", "custom.test"}) {
		t.Fatalf("explicit herd args = %v", herdOverride.Args)
	}
}

func TestGetCleanupSteps_UsesPassedPresetDefinition(t *testing.T) {
	herdStep := &mockStep{name: "herd"}
	manager := NewScaffoldManagerWithRegistry(&cleanupRecordingRegistry{steps: map[string]*mockStep{
		"herd": herdStep,
	}})

	steps, err := manager.GetCleanupSteps(
		&config.Config{}, t.TempDir(), "branch", "recording",
		[]config.CleanupStep{{Name: "herd"}},
	)

	if err != nil {
		t.Fatalf("GetCleanupSteps() error = %v", err)
	}
	if len(steps) != 1 || steps[0] != herdStep {
		t.Fatalf("GetCleanupSteps() = %#v, want the passed preset step", steps)
	}
}

type contextRecordingStep struct {
	preset string
}

func (s *contextRecordingStep) Name() string        { return "selected-step" }
func (s *contextRecordingStep) Description() string { return "selected step" }
func (s *contextRecordingStep) Condition(*types.ScaffoldContext) bool {
	return true
}
func (s *contextRecordingStep) Run(ctx *types.ScaffoldContext, _ types.StepOptions) error {
	s.preset = ctx.Preset
	return nil
}

type contextRecordingRegistry struct {
	step *contextRecordingStep
}

func (r *contextRecordingRegistry) Create(string, config.StepConfig) (types.ScaffoldStep, error) {
	return r.step, nil
}

func TestRunScaffold_UsesPassedPresetDefinition(t *testing.T) {
	step := &contextRecordingStep{}
	manager := NewScaffoldManagerWithRegistry(&contextRecordingRegistry{step: step})

	err := manager.RunScaffold(
		t.TempDir(), "branch", "repo", "site", "selected",
		[]config.StepConfig{{Name: "selected-step"}},
		&config.Config{}, false, false, true,
	)

	if err != nil {
		t.Fatalf("RunScaffold() error = %v", err)
	}
	if step.preset != "selected" {
		t.Fatalf("step context preset = %q, want selected", step.preset)
	}
}

type cleanupRecordingRegistry struct {
	steps map[string]*mockStep
}

func (r *cleanupRecordingRegistry) Create(name string, _ config.StepConfig) (types.ScaffoldStep, error) {
	return r.steps[name], nil
}

func TestRunCleanupWithOptions_SkipDatabaseCleanup(t *testing.T) {
	herdStep := &mockStep{name: "herd", conditionResult: true}
	databaseStep := &mockStep{name: config.StepDbDestroy, conditionResult: true}
	registry := &cleanupRecordingRegistry{steps: map[string]*mockStep{
		"herd":               herdStep,
		config.StepDbDestroy: databaseStep,
	}}
	manager := NewScaffoldManagerWithRegistry(registry)
	cleanupSteps := []config.CleanupStep{
		{Name: "herd"},
		{Name: config.StepDbDestroy},
	}
	cfg := &config.Config{Preset: "recording"}

	err := manager.RunCleanupWithOptions(
		t.TempDir(), "branch", "repo", "site", "recording", cleanupSteps, cfg,
		CleanupOptions{Quiet: true, SkipDatabaseCleanup: true},
	)

	if err != nil {
		t.Fatalf("RunCleanupWithOptions() error = %v", err)
	}
	if !herdStep.runCalled || databaseStep.runCalled {
		t.Fatalf("cleanup calls: herd=%v db.destroy=%v", herdStep.runCalled, databaseStep.runCalled)
	}

	herdStep.runCalled = false
	databaseStep.runCalled = false
	if err := manager.RunCleanup(t.TempDir(), "branch", "repo", "site", "recording", cleanupSteps, cfg, false, false, true); err != nil {
		t.Fatalf("RunCleanup() error = %v", err)
	}
	if !herdStep.runCalled || !databaseStep.runCalled {
		t.Fatalf("wrapper cleanup calls: herd=%v db.destroy=%v", herdStep.runCalled, databaseStep.runCalled)
	}
}

func TestRunCleanupWithOptions_DryRunInvokesDbDestroyWithDryRun(t *testing.T) {
	databaseStep := &mockStep{name: config.StepDbDestroy, conditionResult: true}
	manager := NewScaffoldManagerWithRegistry(&cleanupRecordingRegistry{steps: map[string]*mockStep{
		config.StepDbDestroy: databaseStep,
	}})

	err := manager.RunCleanupWithOptions(
		t.TempDir(), "branch", "repo", "site", "recording",
		[]config.CleanupStep{{Name: config.StepDbDestroy}},
		&config.Config{Preset: "recording"},
		CleanupOptions{DryRun: true, Quiet: true},
	)

	if err != nil {
		t.Fatalf("RunCleanupWithOptions() error = %v", err)
	}
	if len(databaseStep.runOptions) != 1 || !databaseStep.runOptions[0].DryRun {
		t.Fatalf("db.destroy run options = %#v", databaseStep.runOptions)
	}
}

func TestNewScaffoldManager_UsesFreshDefaultRegistries(t *testing.T) {
	first := NewScaffoldManager()
	second := NewScaffoldManager()

	firstRegistry, ok := first.registry.(*scaffoldsteps.Registry)
	require.True(t, ok, "default manager should own an explicit registry")
	secondRegistry, ok := second.registry.(*scaffoldsteps.Registry)
	require.True(t, ok, "default manager should own an explicit registry")
	require.NotSame(t, firstRegistry, secondRegistry)
	assert.Equal(t, firstRegistry.ListRegistered(), secondRegistry.ListRegistered())
	builtIns := secondRegistry.ListRegistered()

	firstRegistry.Register("custom.step", func(config.StepConfig) types.ScaffoldStep {
		return nil
	})

	_, err := secondRegistry.Create("custom.step", config.StepConfig{})
	assert.Error(t, err, "custom registrations must not leak between default managers")
	assert.Equal(t, builtIns, secondRegistry.ListRegistered())

	for _, registry := range []*scaffoldsteps.Registry{firstRegistry, secondRegistry} {
		step, err := registry.Create("php", config.StepConfig{})
		require.NoError(t, err)
		assert.Equal(t, "php", step.Name())
	}
}

func TestNewScaffoldManagerWithRegistry_RequiresExplicitRegistry(t *testing.T) {
	assert.PanicsWithValue(t, "scaffold manager requires an explicit step registry", func() {
		NewScaffoldManagerWithRegistry(nil)
	})
}

func TestPrepareDbSuffixSetsProvenance(t *testing.T) {
	t.Run("migrated legacy suffix", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte("db_suffix: top_provider\n"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		ctx := &types.ScaffoldContext{WorktreePath: dir}
		if err := prepareDbSuffix(ctx, dir, false); err != nil {
			t.Fatalf("prepareDbSuffix() error = %v", err)
		}
		if !ctx.DbSuffixFromState() || !ctx.DbSuffixFromLegacyState() {
			t.Fatalf("migrated suffix provenance = state:%v legacy:%v", ctx.DbSuffixFromState(), ctx.DbSuffixFromLegacyState())
		}
	})

	t.Run("persisted suffix", func(t *testing.T) {
		dir := t.TempDir()
		if err := config.WriteLocalState(dir, config.LocalState{DbSuffix: "top_provider"}); err != nil {
			t.Fatalf("WriteLocalState() error = %v", err)
		}
		ctx := &types.ScaffoldContext{WorktreePath: dir}
		if err := prepareDbSuffix(ctx, dir, false); err != nil {
			t.Fatalf("prepareDbSuffix() error = %v", err)
		}
		if ctx.GetDbSuffix() != "top_provider" || !ctx.DbSuffixFromState() || ctx.DbSuffixFromLegacyState() {
			t.Fatalf("persisted suffix state = %q, provenance=%v", ctx.GetDbSuffix(), ctx.DbSuffixFromState())
		}
	})

	t.Run("fresh suffix", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &types.ScaffoldContext{WorktreePath: dir}
		if err := prepareDbSuffix(ctx, dir, false); err != nil {
			t.Fatalf("prepareDbSuffix() error = %v", err)
		}
		if ctx.GetDbSuffix() == "" || ctx.DbSuffixFromState() {
			t.Fatalf("fresh suffix state = %q, provenance=%v", ctx.GetDbSuffix(), ctx.DbSuffixFromState())
		}
	})
}
