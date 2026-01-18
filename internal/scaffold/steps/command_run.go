package steps

import (
	"os/exec"

	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

type CommandRunStep struct {
	command string
}

func NewCommandRunStep(command string) *CommandRunStep {
	return &CommandRunStep{command: command}
}

func (s *CommandRunStep) Name() string {
	return "command.run"
}

func (s *CommandRunStep) Run(ctx types.ScaffoldContext, opts types.StepOptions) error {
	cmd := exec.Command("sh", "-c", s.command)
	cmd.Dir = ctx.WorktreePath
	return cmd.Run()
}

func (s *CommandRunStep) Priority() int {
	return 100
}

func (s *CommandRunStep) Condition(ctx types.ScaffoldContext) bool {
	return true
}
