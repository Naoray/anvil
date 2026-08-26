package scaffold

import (
	"fmt"
	"strings"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
	"github.com/naoray/anvil/internal/ui"
)

type ExecutionResult struct {
	Step    types.ScaffoldStep
	Error   error
	Skipped bool
}

type ExecutorOptions struct {
	DelegateDryRunToSteps bool
}

type StepExecutor struct {
	steps        []types.ScaffoldStep
	ctx          *types.ScaffoldContext
	opts         types.StepOptions
	executorOpts ExecutorOptions
	results      []ExecutionResult
	completedCnt int
	skippedCnt   int
}

func NewStepExecutor(steps []types.ScaffoldStep, ctx *types.ScaffoldContext, opts types.StepOptions) *StepExecutor {
	return NewStepExecutorWithOptions(steps, ctx, opts, ExecutorOptions{})
}

func NewStepExecutorWithOptions(
	steps []types.ScaffoldStep,
	ctx *types.ScaffoldContext,
	opts types.StepOptions,
	executorOpts ExecutorOptions,
) *StepExecutor {
	return &StepExecutor{
		steps:        steps,
		ctx:          ctx,
		opts:         opts,
		executorOpts: executorOpts,
	}
}

// Execute runs each configured step sequentially. A StepExecutor has one
// owner; callers must not invoke Execute or Results concurrently.
func (e *StepExecutor) Execute() error {
	e.results = make([]ExecutionResult, 0, len(e.steps))
	e.completedCnt = 0
	e.skippedCnt = 0

	// Eligibility is evaluated as each step reaches its turn, so earlier steps
	// can change the context used by later conditions.
	totalSteps := len(e.steps)
	currentStep := 0

	// Execute steps sequentially in the order they were provided
	// Preset steps come first, followed by config steps
	for _, step := range e.steps {
		eligible, skipReason := e.evaluateEligibility(step)
		if !eligible {
			e.recordResult(step, nil, true)
			if e.opts.Verbose {
				fmt.Printf("Skipping step (%s): %s\n", skipReason, step.Name())
			}
			continue
		}

		// Increment current step counter
		currentStep++

		if err := e.executeStep(step, currentStep, totalSteps); err != nil {
			return err
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

func (e *StepExecutor) evaluateEligibility(step types.ScaffoldStep) (bool, string) {
	if stepConfig, ok := step.(interface{ IsEnabled() bool }); ok && !stepConfig.IsEnabled() {
		return false, "disabled"
	}
	if !step.Condition(e.ctx) {
		return false, "condition not met"
	}
	return true, ""
}

func (e *StepExecutor) executeStep(step types.ScaffoldStep, current, total int) error {
	err := e.presentStep(step, current, total)
	e.recordResult(step, err, false)
	if err != nil {
		return fmt.Errorf("step %s failed: %w", step.Name(), err)
	}
	return nil
}

func (e *StepExecutor) presentStep(step types.ScaffoldStep, current, total int) error {
	if e.opts.Verbose {
		return e.executeVerbose(step, current, total)
	}
	if !e.opts.Quiet {
		return e.executeSpinner(step, current, total)
	}
	return e.executeQuiet(step)
}

func (e *StepExecutor) executeVerbose(step types.ScaffoldStep, current, total int) error {
	fmt.Printf("[%d/%d] Executing step: %s\n", current, total, step.Name())

	if e.opts.DryRun && !e.executorOpts.DelegateDryRunToSteps {
		fmt.Printf("[DRY-RUN] Would execute: %s\n", step.Name())
		return nil
	}

	if err := step.Run(e.ctx, e.opts); err != nil {
		return err
	}
	if !e.opts.DryRun {
		fmt.Printf("✓ [%d/%d] %s completed\n", current, total, step.Name())
	}
	return nil
}

func (e *StepExecutor) executeSpinner(step types.ScaffoldStep, current, total int) error {
	if e.opts.DryRun {
		if e.executorOpts.DelegateDryRunToSteps {
			return step.Run(e.ctx, e.opts)
		}
		desc := getStepDescription(step)
		fmt.Printf("[DRY-RUN] [%d/%d] Would execute: %s\n", current, total, desc)
		return nil
	}
	return e.executeWithSpinner(step, current, total)
}

func (e *StepExecutor) executeQuiet(step types.ScaffoldStep) error {
	if !e.opts.DryRun || e.executorOpts.DelegateDryRunToSteps {
		return step.Run(e.ctx, e.opts)
	}
	return nil
}

func (e *StepExecutor) recordResult(step types.ScaffoldStep, err error, skipped bool) {
	e.results = append(e.results, ExecutionResult{
		Step:    step,
		Error:   err,
		Skipped: skipped,
	})
	if skipped {
		e.skippedCnt++
	} else if err == nil {
		e.completedCnt++
	}
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
		config.StepFileCopy:    "Copying files",
		"file.template":        "Processing template files",
		config.StepEnvRead:     "Reading environment variables",
		config.StepEnvWrite:    "Writing environment variables",
		config.StepDbCreate:    "Creating database",
		config.StepDbDestroy:   "Destroying database",
		config.StepBashRun:     "Running bash command",
		config.StepCommandRun:  "Running command",
		"herd":                 "Managing Herd",
		"yerd":                 "Managing Yerd",
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

	// For local site driver commands, try to extract the subcommand.
	if stepName == "herd" || stepName == "yerd" {
		if argGetter, ok := step.(interface{ GetArgs() []string }); ok {
			args := argGetter.GetArgs()
			if len(args) > 0 {
				baseDesc = fmt.Sprintf("Running %s %s", stepName, args[0])
			}
		}
	}

	// If no description found, use the step name
	if baseDesc == "" {
		baseDesc = fmt.Sprintf("Running %s", stepName)
	}

	return fmt.Sprintf("%s (%s)", baseDesc, stepName)
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
