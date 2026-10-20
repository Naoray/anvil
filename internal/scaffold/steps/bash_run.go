package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/naoray/anvil/internal/config"
	anvil_exec "github.com/naoray/anvil/internal/exec"
	"github.com/naoray/anvil/internal/scaffold/template"
	"github.com/naoray/anvil/internal/scaffold/types"
)

type BashRunStep struct {
	target   execTarget
	command  string
	storeAs  string
	executor *anvil_exec.CommandExecutor
}

// NewBashRunStep creates a bash step with the default command executor.
func NewBashRunStep(command string, storeAs string) *BashRunStep {
	return NewBashRunStepWithExecutor(command, storeAs, nil)
}

// NewBashRunStepWithExecutor creates a bash step with a custom command executor.
// This is useful for testing with mock executors.
func NewBashRunStepWithExecutor(command string, storeAs string, executor *anvil_exec.CommandExecutor) *BashRunStep {
	if executor == nil {
		executor = anvil_exec.NewCommandExecutor(nil)
	}
	return &BashRunStep{
		command:  command,
		storeAs:  storeAs,
		executor: executor,
	}
}

func (s *BashRunStep) Name() string {
	return "bash.run"
}

func (s *BashRunStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	if opts.DryRun {
		return nil
	}
	command, err := template.ReplaceTemplateVars(s.command, ctx)
	if err != nil {
		return fmt.Errorf("template replacement failed: %w", err)
	}

	dir, env, err := s.target.resolve(ctx)
	if err != nil {
		return fmt.Errorf("bash.run: %w", err)
	}
	// Use the command executor for testability
	var output []byte
	if env == nil {
		output, err = s.executor.RunBash(context.Background(), dir, command)
	} else {
		output, err = s.executor.RunBashWithEnv(context.Background(), dir, env, command)
	}
	if err != nil {
		return fmt.Errorf("bash.run failed: %w\n%s", err, string(output))
	}

	if s.storeAs != "" {
		ctx.SetVar(s.storeAs, strings.TrimSpace(string(output)))
		if opts.Verbose {
			fmt.Printf("  Stored output as %s\n", s.storeAs)
		}
	}

	return nil
}

func (s *BashRunStep) Condition(ctx *types.ScaffoldContext) (bool, error) {
	return true, nil
}

// WithTarget sets the step's working directory and extra environment.
func (s *BashRunStep) WithTarget(cfg config.StepConfig) *BashRunStep {
	s.target = newExecTarget(cfg)
	return s
}
