package steps

import (
	"os/exec"

	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

type BashRunStep struct {
	command string
}

func NewBashRunStep(command string) *BashRunStep {
	return &BashRunStep{command: command}
}

func (s *BashRunStep) Name() string {
	return "bash.run"
}

func (s *BashRunStep) Run(ctx types.ScaffoldContext, opts types.StepOptions) error {
	cmd := exec.Command("bash", "-c", s.command)
	cmd.Dir = ctx.WorktreePath
	return cmd.Run()
}

func (s *BashRunStep) Priority() int {
	return 100
}

func (s *BashRunStep) Condition(ctx types.ScaffoldContext) bool {
	return true
}
