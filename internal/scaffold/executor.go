package scaffold

import (
	"fmt"
	"sync"

	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

type ExecutionResult struct {
	Step    types.ScaffoldStep
	Error   error
	Skipped bool
}

type StepExecutor struct {
	steps   []types.ScaffoldStep
	ctx     *types.ScaffoldContext
	opts    types.StepOptions
	results []ExecutionResult
	mu      sync.Mutex
	errMu   sync.Mutex
}

func NewStepExecutor(steps []types.ScaffoldStep, ctx *types.ScaffoldContext, opts types.StepOptions) *StepExecutor {
	return &StepExecutor{
		steps: steps,
		ctx:   ctx,
		opts:  opts,
	}
}

func (e *StepExecutor) Execute() error {
	e.results = make([]ExecutionResult, 0, len(e.steps))

	// Execute steps sequentially in the order they were provided
	// Preset steps come first, followed by config steps
	for _, step := range e.steps {
		if err := e.executeStep(step); err != nil {
			return err
		}
	}

	return nil
}

func (e *StepExecutor) executeStep(step types.ScaffoldStep) error {
	enabled := true

	stepConfig, ok := step.(interface{ IsEnabled() bool })
	if ok {
		enabled = stepConfig.IsEnabled()
	}

	if !enabled {
		e.mu.Lock()
		e.results = append(e.results, ExecutionResult{
			Step:    step,
			Skipped: true,
		})
		e.mu.Unlock()
		if e.opts.Verbose {
			fmt.Printf("Skipping step (disabled): %s\n", step.Name())
		}
		return nil
	}

	if step.Condition(e.ctx) {
		if e.opts.Verbose {
			fmt.Printf("Executing step: %s\n", step.Name())
		}

		if e.opts.DryRun {
			if e.opts.Verbose {
				fmt.Printf("[DRY-RUN] Would execute: %s\n", step.Name())
			}
			e.mu.Lock()
			e.results = append(e.results, ExecutionResult{
				Step: step,
			})
			e.mu.Unlock()
			return nil
		}

		if err := step.Run(e.ctx, e.opts); err != nil {
			e.mu.Lock()
			e.results = append(e.results, ExecutionResult{
				Step:  step,
				Error: err,
			})
			e.mu.Unlock()
			return fmt.Errorf("step %s failed: %w", step.Name(), err)
		}
		e.mu.Lock()
		e.results = append(e.results, ExecutionResult{
			Step: step,
		})
		e.mu.Unlock()
	} else {
		if e.opts.Verbose {
			fmt.Printf("Skipping step (condition not met): %s\n", step.Name())
		}
		e.mu.Lock()
		e.results = append(e.results, ExecutionResult{
			Step:    step,
			Skipped: true,
		})
		e.mu.Unlock()
	}

	return nil
}

func (e *StepExecutor) Results() []ExecutionResult {
	return e.results
}
