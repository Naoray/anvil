package scaffold

import (
	"fmt"
	"sort"
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
	ctx     types.ScaffoldContext
	opts    types.StepOptions
	results []ExecutionResult
	mu      sync.Mutex
	errMu   sync.Mutex
}

func NewStepExecutor(steps []types.ScaffoldStep, ctx types.ScaffoldContext, opts types.StepOptions) *StepExecutor {
	return &StepExecutor{
		steps: steps,
		ctx:   ctx,
		opts:  opts,
	}
}

func (e *StepExecutor) Execute() error {
	e.results = make([]ExecutionResult, 0, len(e.steps))

	sortedSteps := e.sortByPriority()

	groups := e.groupByPriority(sortedSteps)

	for _, group := range groups {
		if err := e.executeGroup(group); err != nil {
			return err
		}
	}

	return nil
}

func (e *StepExecutor) sortByPriority() []types.ScaffoldStep {
	sorted := make([]types.ScaffoldStep, len(e.steps))
	copy(sorted, e.steps)

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority() < sorted[j].Priority()
	})

	return sorted
}

func (e *StepExecutor) groupByPriority(steps []types.ScaffoldStep) [][]types.ScaffoldStep {
	if len(steps) == 0 {
		return nil
	}

	var groups [][]types.ScaffoldStep
	var currentGroup []types.ScaffoldStep
	currentPriority := steps[0].Priority()

	for _, step := range steps {
		if step.Priority() != currentPriority {
			groups = append(groups, currentGroup)
			currentGroup = []types.ScaffoldStep{}
			currentPriority = step.Priority()
		}
		currentGroup = append(currentGroup, step)
	}

	if len(currentGroup) > 0 {
		groups = append(groups, currentGroup)
	}

	return groups
}

func (e *StepExecutor) executeGroup(group []types.ScaffoldStep) error {
	if len(group) == 1 {
		return e.executeStep(group[0])
	}

	return e.executeGroupParallel(group)
}

func (e *StepExecutor) executeGroupParallel(group []types.ScaffoldStep) error {
	var wg sync.WaitGroup
	var firstErr error
	errChan := make(chan error, len(group))

	for _, step := range group {
		wg.Add(1)
		go func(s types.ScaffoldStep) {
			defer wg.Done()

			err := e.executeStep(s)
			if err != nil {
				e.errMu.Lock()
				if firstErr == nil {
					firstErr = err
					select {
					case errChan <- err:
					default:
					}
				}
				e.errMu.Unlock()
			}
		}(step)
	}

	wg.Wait()
	close(errChan)

	return firstErr
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
