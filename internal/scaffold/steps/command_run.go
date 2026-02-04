package steps

import (
	"context"
	"fmt"
	"strings"

	arbor_exec "github.com/artisanexperiences/arbor/internal/exec"
	"github.com/artisanexperiences/arbor/internal/scaffold/types"
)

type CommandRunStep struct {
	command  string
	storeAs  string
	executor *arbor_exec.CommandExecutor
}

// NewCommandRunStep creates a command step with the default command executor.
func NewCommandRunStep(command string, storeAs string) *CommandRunStep {
	return NewCommandRunStepWithExecutor(command, storeAs, nil)
}

// NewCommandRunStepWithExecutor creates a command step with a custom command executor.
// This is useful for testing with mock executors.
func NewCommandRunStepWithExecutor(command string, storeAs string, executor *arbor_exec.CommandExecutor) *CommandRunStep {
	if executor == nil {
		executor = arbor_exec.NewCommandExecutor(nil)
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
	// Use the command executor for testability
	output, err := s.executor.RunShell(context.Background(), ctx.WorktreePath, s.command)
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

func (s *CommandRunStep) Condition(ctx *types.ScaffoldContext) bool {
	return true
}
