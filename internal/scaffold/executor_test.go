package scaffold

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/naoray/anvil/internal/scaffold/types"
)

type mockStep struct {
	name            string
	conditionResult bool
	runError        error
	runCalled       bool
}

func (s *mockStep) Name() string {
	return s.name
}

func (s *mockStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	s.runCalled = true
	return s.runError
}

func (s *mockStep) Condition(ctx *types.ScaffoldContext) bool {
	return s.conditionResult
}

func TestStepExecutor_Execute_AllStepsPass(t *testing.T) {
	ctx := &types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	step1 := &mockStep{name: "step1", conditionResult: true}
	step2 := &mockStep{name: "step2", conditionResult: true}

	executor := NewStepExecutor([]types.ScaffoldStep{step1, step2}, ctx, types.StepOptions{
		DryRun:  false,
		Verbose: false,
	})

	err := executor.Execute()

	assert.NoError(t, err)
	assert.True(t, step1.runCalled)
	assert.True(t, step2.runCalled)
}

func TestStepExecutor_Execute_ConditionFalse(t *testing.T) {
	ctx := &types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	step1 := &mockStep{name: "step1", conditionResult: true}
	step2 := &mockStep{name: "step2", conditionResult: false}

	executor := NewStepExecutor([]types.ScaffoldStep{step1, step2}, ctx, types.StepOptions{
		DryRun:  false,
		Verbose: false,
	})

	err := executor.Execute()

	assert.NoError(t, err)
	assert.True(t, step1.runCalled)
	assert.False(t, step2.runCalled)
}

func TestStepExecutor_Execute_StepFails(t *testing.T) {
	ctx := &types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	step1 := &mockStep{name: "step1", conditionResult: true}
	step2 := &mockStep{name: "step2", conditionResult: true, runError: assert.AnError}

	executor := NewStepExecutor([]types.ScaffoldStep{step1, step2}, ctx, types.StepOptions{
		DryRun:  false,
		Verbose: false,
	})

	err := executor.Execute()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "step2 failed")
}

func TestStepExecutor_Execute_DryRun(t *testing.T) {
	ctx := &types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	step1 := &mockStep{name: "step1", conditionResult: true}

	executor := NewStepExecutor([]types.ScaffoldStep{step1}, ctx, types.StepOptions{
		DryRun:  true,
		Verbose: false,
	})

	err := executor.Execute()

	assert.NoError(t, err)
	assert.False(t, step1.runCalled)
}

func TestStepExecutor_Results(t *testing.T) {
	ctx := &types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	step1 := &mockStep{name: "step1", conditionResult: true}
	step2 := &mockStep{name: "step2", conditionResult: false}

	t.Logf("Before execution - step1 called: %v", step1.runCalled)
	t.Logf("Before execution - step2 called: %v", step2.runCalled)

	executor := NewStepExecutor([]types.ScaffoldStep{step1, step2}, ctx, types.StepOptions{
		DryRun:  false,
		Verbose: false,
	})

	err := executor.Execute()
	t.Logf("Execute error: %v", err)

	t.Logf("After execution - step1 called: %v", step1.runCalled)
	t.Logf("After execution - step2 called: %v", step2.runCalled)

	results := executor.Results()

	t.Logf("Number of results: %d", len(results))
	for i, r := range results {
		t.Logf("Result %d: %s, Skipped: %v", i, r.Step.Name(), r.Skipped)
	}

	assert.NoError(t, err)
	assert.True(t, step1.runCalled, "step1 should have been called")
	assert.False(t, step2.runCalled, "step2 should not have been called")
	assert.Len(t, results, 2)
	assert.Equal(t, "step1", results[0].Step.Name())
	assert.False(t, results[0].Skipped)
	assert.Equal(t, "step2", results[1].Step.Name())
	assert.True(t, results[1].Skipped)
}

func TestStepExecutor_ParallelExecution_SamePriority(t *testing.T) {
	ctx := &types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	step1 := &mockStep{name: "step1", conditionResult: true}
	step2 := &mockStep{name: "step2", conditionResult: true}
	step3 := &mockStep{name: "step3", conditionResult: true}

	executor := NewStepExecutor([]types.ScaffoldStep{step1, step2, step3}, ctx, types.StepOptions{
		DryRun:  false,
		Verbose: false,
	})

	err := executor.Execute()

	assert.NoError(t, err)
	assert.True(t, step1.runCalled)
	assert.True(t, step2.runCalled)
	assert.True(t, step3.runCalled)

	// Note: Steps now run sequentially in order, not in parallel
	// This test is kept for backwards compatibility but behavior has changed
}

func TestStepExecutor_ParallelExecution_ErrorPropagation(t *testing.T) {
	ctx := &types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	step1 := &mockStep{name: "step1", conditionResult: true}
	step2 := &mockStep{name: "step2", conditionResult: true, runError: assert.AnError}
	step3 := &mockStep{name: "step3", conditionResult: true}

	executor := NewStepExecutor([]types.ScaffoldStep{step1, step2, step3}, ctx, types.StepOptions{
		DryRun:  false,
		Verbose: false,
	})

	err := executor.Execute()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "step2 failed")
	assert.True(t, step1.runCalled)
	assert.True(t, step2.runCalled)
	// step3 should NOT run because step2 failed and we now execute sequentially
	assert.False(t, step3.runCalled)
}

func TestStepExecutor_SequentialExecution(t *testing.T) {
	ctx := &types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	step1 := &mockStep{name: "step1", conditionResult: true}
	step2 := &mockStep{name: "step2", conditionResult: true}
	step3 := &mockStep{name: "step3", conditionResult: true}

	executor := NewStepExecutor([]types.ScaffoldStep{step1, step2, step3}, ctx, types.StepOptions{
		DryRun:  false,
		Verbose: false,
	})

	err := executor.Execute()

	assert.NoError(t, err)
	assert.True(t, step1.runCalled)
	assert.True(t, step2.runCalled)
	assert.True(t, step3.runCalled)
}

func TestStepExecutor_LaravelPresetStepOrdering(t *testing.T) {
	ctx := &types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	// Simulate Laravel preset steps in the order they should execute
	composerInstall := &mockStep{name: "php.composer install", conditionResult: true}
	fileCopy := &mockStep{name: "file.copy .env", conditionResult: true}
	dbCreate := &mockStep{name: "db.create", conditionResult: true}
	npmCi := &mockStep{name: "node.npm ci", conditionResult: true}
	keyGenerate := &mockStep{name: "php.laravel key:generate", conditionResult: true}
	migrateFresh := &mockStep{name: "php.laravel migrate:fresh", conditionResult: true}
	npmBuild := &mockStep{name: "node.npm build", conditionResult: true}
	storageLink := &mockStep{name: "php.laravel storage:link", conditionResult: true}
	herd := &mockStep{name: "herd", conditionResult: true}

	// Steps provided in the correct execution order (as they come from preset)
	steps := []types.ScaffoldStep{
		composerInstall,
		fileCopy,
		dbCreate,
		npmCi,
		keyGenerate,
		migrateFresh,
		npmBuild,
		storageLink,
		herd,
	}

	executor := NewStepExecutor(steps, ctx, types.StepOptions{
		DryRun:  false,
		Verbose: false,
	})

	err := executor.Execute()

	// All steps should have been called
	assert.NoError(t, err)
	assert.True(t, composerInstall.runCalled)
	assert.True(t, fileCopy.runCalled)
	assert.True(t, dbCreate.runCalled)
	assert.True(t, npmCi.runCalled)
	assert.True(t, keyGenerate.runCalled)
	assert.True(t, migrateFresh.runCalled)
	assert.True(t, npmBuild.runCalled)
	assert.True(t, storageLink.runCalled)
	assert.True(t, herd.runCalled)

	// Verify execution order through results - they run in the order provided
	results := executor.Results()
	assert.Len(t, results, 9)

	// Verify steps ran in the order they were provided
	assert.Equal(t, "php.composer install", results[0].Step.Name())
	assert.Equal(t, "file.copy .env", results[1].Step.Name())
	assert.Equal(t, "db.create", results[2].Step.Name())
	assert.Equal(t, "node.npm ci", results[3].Step.Name())
	assert.Equal(t, "php.laravel key:generate", results[4].Step.Name())
	assert.Equal(t, "php.laravel migrate:fresh", results[5].Step.Name())
	assert.Equal(t, "node.npm build", results[6].Step.Name())
	assert.Equal(t, "php.laravel storage:link", results[7].Step.Name())
	assert.Equal(t, "herd", results[8].Step.Name())
}
