package scaffold

import (
	"slices"
	"testing"

	"github.com/naoray/anvil/internal/config"
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

type cleanupRecordingRegistry struct {
	steps map[string]*mockStep
}

func (r *cleanupRecordingRegistry) Create(name string, _ config.StepConfig) (types.ScaffoldStep, error) {
	return r.steps[name], nil
}

func (r *cleanupRecordingRegistry) ListRegistered() []string {
	return nil
}

type cleanupRecordingPreset struct {
	cleanup []config.CleanupStep
}

func (p cleanupRecordingPreset) Name() string                       { return "recording" }
func (p cleanupRecordingPreset) Detect(string) bool                 { return false }
func (p cleanupRecordingPreset) DefaultSteps() []config.StepConfig  { return nil }
func (p cleanupRecordingPreset) CleanupSteps() []config.CleanupStep { return p.cleanup }

func TestRunCleanupWithOptions_SkipDatabaseCleanup(t *testing.T) {
	herdStep := &mockStep{name: "herd", conditionResult: true}
	databaseStep := &mockStep{name: config.StepDbDestroy, conditionResult: true}
	registry := &cleanupRecordingRegistry{steps: map[string]*mockStep{
		"herd":               herdStep,
		config.StepDbDestroy: databaseStep,
	}}
	manager := NewScaffoldManagerWithRegistry(registry)
	manager.RegisterPreset(cleanupRecordingPreset{cleanup: []config.CleanupStep{
		{Name: "herd"},
		{Name: config.StepDbDestroy},
	}})
	cfg := &config.Config{Preset: "recording"}

	err := manager.RunCleanupWithOptions(
		t.TempDir(), "branch", "repo", "site", "recording", cfg,
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
	if err := manager.RunCleanup(t.TempDir(), "branch", "repo", "site", "recording", cfg, false, false, true); err != nil {
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
	manager.RegisterPreset(cleanupRecordingPreset{cleanup: []config.CleanupStep{{Name: config.StepDbDestroy}}})

	err := manager.RunCleanupWithOptions(
		t.TempDir(), "branch", "repo", "site", "recording", &config.Config{Preset: "recording"},
		CleanupOptions{DryRun: true, Quiet: true},
	)

	if err != nil {
		t.Fatalf("RunCleanupWithOptions() error = %v", err)
	}
	if len(databaseStep.runOptions) != 1 || !databaseStep.runOptions[0].DryRun {
		t.Fatalf("db.destroy run options = %#v", databaseStep.runOptions)
	}
}

func TestPrepareDbSuffixSetsProvenance(t *testing.T) {
	t.Run("persisted suffix", func(t *testing.T) {
		dir := t.TempDir()
		if err := config.WriteLocalState(dir, config.LocalState{DbSuffix: "top_provider"}); err != nil {
			t.Fatalf("WriteLocalState() error = %v", err)
		}
		ctx := &types.ScaffoldContext{WorktreePath: dir}
		if err := prepareDbSuffix(ctx, dir, false); err != nil {
			t.Fatalf("prepareDbSuffix() error = %v", err)
		}
		if ctx.GetDbSuffix() != "top_provider" || !ctx.DbSuffixFromState() {
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
