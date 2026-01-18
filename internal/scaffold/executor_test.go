package scaffold

import (
	"testing"

	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
	"github.com/stretchr/testify/assert"
)

type mockStep struct {
	name            string
	priority        int
	conditionResult bool
	runError        error
	runCalled       bool
}

func (s *mockStep) Name() string {
	return s.name
}

func (s *mockStep) Run(ctx types.ScaffoldContext, opts types.StepOptions) error {
	s.runCalled = true
	return s.runError
}

func (s *mockStep) Priority() int {
	return s.priority
}

func (s *mockStep) Condition(ctx types.ScaffoldContext) bool {
	return s.conditionResult
}

func TestStepExecutor_SortByPriority(t *testing.T) {
	ctx := types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	steps := []types.ScaffoldStep{
		&mockStep{name: "step3", priority: 30},
		&mockStep{name: "step1", priority: 10},
		&mockStep{name: "step2", priority: 20},
	}

	executor := &StepExecutor{
		steps: steps,
		ctx:   ctx,
	}

	sorted := executor.sortByPriority()

	assert.Equal(t, "step1", sorted[0].Name())
	assert.Equal(t, "step2", sorted[1].Name())
	assert.Equal(t, "step3", sorted[2].Name())
}

func TestStepExecutor_GroupByPriority(t *testing.T) {
	ctx := types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	steps := []types.ScaffoldStep{
		&mockStep{name: "step1", priority: 10},
		&mockStep{name: "step2", priority: 10},
		&mockStep{name: "step3", priority: 20},
		&mockStep{name: "step4", priority: 30},
		&mockStep{name: "step5", priority: 30},
	}

	executor := &StepExecutor{
		steps: steps,
		ctx:   ctx,
	}

	sorted := executor.sortByPriority()
	groups := executor.groupByPriority(sorted)

	assert.Len(t, groups, 3)
	assert.Len(t, groups[0], 2)
	assert.Len(t, groups[1], 1)
	assert.Len(t, groups[2], 2)
}

func TestStepExecutor_Execute_AllStepsPass(t *testing.T) {
	ctx := types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	step1 := &mockStep{name: "step1", priority: 10, conditionResult: true}
	step2 := &mockStep{name: "step2", priority: 20, conditionResult: true}

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
	ctx := types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	step1 := &mockStep{name: "step1", priority: 10, conditionResult: true}
	step2 := &mockStep{name: "step2", priority: 20, conditionResult: false}

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
	ctx := types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	step1 := &mockStep{name: "step1", priority: 10, conditionResult: true}
	step2 := &mockStep{name: "step2", priority: 20, conditionResult: true, runError: assert.AnError}

	executor := NewStepExecutor([]types.ScaffoldStep{step1, step2}, ctx, types.StepOptions{
		DryRun:  false,
		Verbose: false,
	})

	err := executor.Execute()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "step2 failed")
}

func TestStepExecutor_Execute_DryRun(t *testing.T) {
	ctx := types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	step1 := &mockStep{name: "step1", priority: 10, conditionResult: true}

	executor := NewStepExecutor([]types.ScaffoldStep{step1}, ctx, types.StepOptions{
		DryRun:  true,
		Verbose: false,
	})

	err := executor.Execute()

	assert.NoError(t, err)
	assert.False(t, step1.runCalled)
}

func TestStepExecutor_Results(t *testing.T) {
	ctx := types.ScaffoldContext{
		WorktreePath: "/tmp",
		Branch:       "test",
	}

	step1 := &mockStep{name: "step1", priority: 10, conditionResult: true}
	step2 := &mockStep{name: "step2", priority: 30, conditionResult: false}

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
