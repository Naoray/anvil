package steps

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/scaffold/template"
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

type BinaryStep struct {
	name      string
	binary    string
	args      []string
	priority  int
	condition map[string]interface{}
}

func NewBinaryStep(name, binary string, args []string, priority int) *BinaryStep {
	return &BinaryStep{
		name:      name,
		binary:    binary,
		args:      args,
		priority:  priority,
		condition: nil,
	}
}

func NewBinaryStepWithCondition(name string, cfg config.StepConfig, binary string, priority int) *BinaryStep {
	return &BinaryStep{
		name:      name,
		binary:    binary,
		args:      cfg.Args,
		priority:  priority,
		condition: cfg.Condition,
	}
}

func (s *BinaryStep) Name() string {
	return s.name
}

func (s *BinaryStep) Priority() int {
	return s.priority
}

func (s *BinaryStep) Condition(ctx *types.ScaffoldContext) bool {
	if len(s.condition) > 0 {
		result, err := ctx.EvaluateCondition(s.condition)
		if err != nil {
			return false
		}
		return result
	}

	binaries := strings.Fields(s.binary)
	if len(binaries) == 0 {
		return false
	}
	_, err := exec.LookPath(binaries[0])
	return err == nil
}

func (s *BinaryStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	allArgs := append(s.args, opts.Args...)
	allArgs = s.replaceTemplate(allArgs, ctx)
	if opts.Verbose {
		binaryParts := strings.Fields(s.binary)
		fullCmd := append(binaryParts, allArgs...)
		fmt.Printf("  Running: %s\n", strings.Join(fullCmd, " "))
	}
	cmd := exec.Command(strings.Fields(s.binary)[0], append(strings.Fields(s.binary)[1:], allArgs...)...)
	cmd.Dir = ctx.WorktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w\n%s", s.name, err, string(output))
	}
	return nil
}

func (s *BinaryStep) replaceTemplate(args []string, ctx *types.ScaffoldContext) []string {
	for i, arg := range args {
		replaced, err := template.ReplaceTemplateVars(arg, ctx)
		if err != nil {
			continue
		}
		args[i] = replaced
	}
	return args
}
