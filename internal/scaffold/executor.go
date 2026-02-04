package scaffold

import (
	"fmt"
	"strings"
	"sync"

	"github.com/artisanexperiences/arbor/internal/scaffold/types"
	"github.com/artisanexperiences/arbor/internal/ui"
)

type ExecutionResult struct {
	Step    types.ScaffoldStep
	Error   error
	Skipped bool
}

type StepExecutor struct {
	steps        []types.ScaffoldStep
	ctx          *types.ScaffoldContext
	opts         types.StepOptions
	results      []ExecutionResult
	mu           sync.Mutex
	completedCnt int
	skippedCnt   int
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
	e.completedCnt = 0
	e.skippedCnt = 0

	// Count active steps for progress tracking
	activeSteps := e.countActiveSteps()
	currentStep := 0

	// Execute steps sequentially in the order they were provided
	// Preset steps come first, followed by config steps
	for _, step := range e.steps {
		// Check if step is enabled
		enabled := true
		if stepConfig, ok := step.(interface{ IsEnabled() bool }); ok {
			enabled = stepConfig.IsEnabled()
		}

		if !enabled {
			e.mu.Lock()
			e.results = append(e.results, ExecutionResult{
				Step:    step,
				Skipped: true,
			})
			e.skippedCnt++
			e.mu.Unlock()
			if e.opts.Verbose {
				fmt.Printf("Skipping step (disabled): %s\n", step.Name())
			}
			continue
		}

		// Check condition
		if !step.Condition(e.ctx) {
			e.mu.Lock()
			e.results = append(e.results, ExecutionResult{
				Step:    step,
				Skipped: true,
			})
			e.skippedCnt++
			e.mu.Unlock()
			if e.opts.Verbose {
				fmt.Printf("Skipping step (condition not met): %s\n", step.Name())
			}
			continue
		}

		// Increment current step counter
		currentStep++

		// Execute the step based on mode
		if e.opts.Verbose {
			// Verbose mode: print detailed output
			fmt.Printf("[%d/%d] Executing step: %s\n", currentStep, activeSteps, step.Name())

			if e.opts.DryRun {
				fmt.Printf("[DRY-RUN] Would execute: %s\n", step.Name())
				e.mu.Lock()
				e.results = append(e.results, ExecutionResult{
					Step: step,
				})
				e.completedCnt++
				e.mu.Unlock()
			} else {
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
				e.completedCnt++
				e.mu.Unlock()
				fmt.Printf("✓ [%d/%d] %s completed\n", currentStep, activeSteps, step.Name())
			}
		} else if !e.opts.Quiet {
			// Normal mode: use spinner
			if e.opts.DryRun {
				desc := getStepDescription(step)
				fmt.Printf("[DRY-RUN] [%d/%d] Would execute: %s\n", currentStep, activeSteps, desc)
				e.mu.Lock()
				e.results = append(e.results, ExecutionResult{
					Step: step,
				})
				e.completedCnt++
				e.mu.Unlock()
			} else {
				if err := e.executeWithSpinner(step, currentStep, activeSteps); err != nil {
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
				e.completedCnt++
				e.mu.Unlock()
			}
		} else {
			// Quiet mode: silent execution
			if !e.opts.DryRun {
				if err := step.Run(e.ctx, e.opts); err != nil {
					e.mu.Lock()
					e.results = append(e.results, ExecutionResult{
						Step:  step,
						Error: err,
					})
					e.mu.Unlock()
					return fmt.Errorf("step %s failed: %w", step.Name(), err)
				}
			}
			e.mu.Lock()
			e.results = append(e.results, ExecutionResult{
				Step: step,
			})
			e.completedCnt++
			e.mu.Unlock()
		}
	}

	// Print summary if not in quiet mode
	if !e.opts.Quiet {
		e.printSummary()
	}

	return nil
}

func (e *StepExecutor) Results() []ExecutionResult {
	return e.results
}

// getStepDescription returns a friendly description for a step
func getStepDescription(step types.ScaffoldStep) string {
	stepName := step.Name()

	// Map common steps to friendly descriptions
	descriptions := map[string]string{
		"php.composer.install": "Installing composer dependencies",
		"php.composer.update":  "Updating composer dependencies",
		"node.npm.install":     "Installing npm packages",
		"node.npm.run":         "Running npm script",
		"node.yarn.install":    "Installing yarn packages",
		"node.pnpm.install":    "Installing pnpm packages",
		"node.bun":             "Running bun",
		"file.copy":            "Copying files",
		"file.template":        "Processing template files",
		"env.read":             "Reading environment variables",
		"env.write":            "Writing environment variables",
		"db.create":            "Creating database",
		"db.destroy":           "Destroying database",
		"bash.run":             "Running bash command",
		"command.run":          "Running command",
		"herd":                 "Managing Herd",
	}

	baseDesc := descriptions[stepName]

	// For Laravel artisan commands, try to extract the command name
	if stepName == "php.laravel" {
		// Try to get the args from the step
		if argGetter, ok := step.(interface{ GetArgs() []string }); ok {
			args := argGetter.GetArgs()
			if len(args) > 0 {
				// Extract the command (first part before any arguments)
				cmdPart := strings.Split(args[0], " ")[0]
				baseDesc = fmt.Sprintf("Running artisan %s", cmdPart)
			}
		}
		if baseDesc == "" {
			baseDesc = "Running artisan command"
		}
	}

	// For npm run, try to extract the script name
	if stepName == "node.npm.run" {
		if argGetter, ok := step.(interface{ GetArgs() []string }); ok {
			args := argGetter.GetArgs()
			if len(args) > 0 {
				baseDesc = fmt.Sprintf("Running npm %s", args[0])
			}
		}
	}

	// For herd commands, try to extract the subcommand
	if stepName == "herd" {
		if argGetter, ok := step.(interface{ GetArgs() []string }); ok {
			args := argGetter.GetArgs()
			if len(args) > 0 {
				baseDesc = fmt.Sprintf("Running herd %s", args[0])
			}
		}
	}

	// If no description found, use the step name
	if baseDesc == "" {
		baseDesc = fmt.Sprintf("Running %s", stepName)
	}

	return fmt.Sprintf("%s (%s)", baseDesc, stepName)
}

// countActiveSteps counts steps that will actually run (not skipped)
func (e *StepExecutor) countActiveSteps() int {
	count := 0
	for _, step := range e.steps {
		enabled := true
		if stepConfig, ok := step.(interface{ IsEnabled() bool }); ok {
			enabled = stepConfig.IsEnabled()
		}

		if enabled && step.Condition(e.ctx) {
			count++
		}
	}
	return count
}

// executeWithSpinner runs a step with a spinner showing progress
func (e *StepExecutor) executeWithSpinner(step types.ScaffoldStep, current, total int) error {
	desc := getStepDescription(step)
	title := fmt.Sprintf("[%d/%d] %s", current, total, desc)

	var stepErr error
	spinnerErr := ui.RunWithSpinner(title, func() error {
		stepErr = step.Run(e.ctx, e.opts)
		return stepErr
	})

	if spinnerErr != nil {
		return spinnerErr
	}

	return stepErr
}

// printSummary prints a summary of execution results
func (e *StepExecutor) printSummary() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.completedCnt > 0 || e.skippedCnt > 0 {
		summary := fmt.Sprintf("%d step", e.completedCnt)
		if e.completedCnt != 1 {
			summary += "s"
		}
		summary += " completed"

		if e.skippedCnt > 0 {
			summary += fmt.Sprintf(", %d skipped", e.skippedCnt)
		}

		ui.PrintSuccess(summary)
	}
}
