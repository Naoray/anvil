package steps

import (
	"fmt"
	"os/exec"

	"github.com/michaeldyrynda/arbor/internal/scaffold/template"
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

func (s *BashRunStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	command, err := template.ReplaceTemplateVars(s.command, ctx)
	if err != nil {
		return fmt.Errorf("template replacement failed: %w", err)
	}

	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = ctx.WorktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bash.run failed: %w\n%s", err, string(output))
	}
	return nil
}

func (s *BashRunStep) Condition(ctx *types.ScaffoldContext) bool {
	return true
}
