package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/naoray/anvil/internal/config"
	anvil_exec "github.com/naoray/anvil/internal/exec"
	"github.com/naoray/anvil/internal/scaffold/types"
)

type CommandRunStep struct {
	target   execTarget
	command  string
	storeAs  string
	executor *anvil_exec.CommandExecutor
}

// NewCommandRunStep creates a command step with the default command executor.
func NewCommandRunStep(command string, storeAs string) *CommandRunStep {
	return NewCommandRunStepWithExecutor(command, storeAs, nil)
}

// NewCommandRunStepWithExecutor creates a command step with a custom command executor.
// This is useful for testing with mock executors.
func NewCommandRunStepWithExecutor(command string, storeAs string, executor *anvil_exec.CommandExecutor) *CommandRunStep {
	if executor == nil {
		executor = anvil_exec.NewCommandExecutor(nil)
	}
	return &CommandRunStep{
		command:  command,
		storeAs:  storeAs,
		executor: executor,
	}
}

func (s *CommandRunStep) Name() string {
	return "command.run"
}

func (s *CommandRunStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	if opts.DryRun {
		return nil
	}
	dir, env, err := s.target.resolve(ctx)
	if err != nil {
		return fmt.Errorf("command.run: %w", err)
	}
	// Use the command executor for testability
	var output []byte
	if env == nil {
		output, err = s.executor.RunShell(context.Background(), dir, s.command)
	} else {
		output, err = s.executor.RunShellWithEnv(context.Background(), dir, env, s.command)
	}
	if err != nil {
		return fmt.Errorf("command.run failed: %w\n%s", err, string(output))
	}

	if s.storeAs != "" {
		ctx.SetVar(s.storeAs, strings.TrimSpace(string(output)))
		if opts.Verbose {
			fmt.Printf("  Stored output as %s\n", s.storeAs)
		}
	}

	return nil
}

func (s *CommandRunStep) Condition(ctx *types.ScaffoldContext) (bool, error) {
	return true, nil
}

// WithTarget sets the step's working directory and extra environment.
func (s *CommandRunStep) WithTarget(cfg config.StepConfig) *CommandRunStep {
	s.target = newExecTarget(cfg)
	return s
}
