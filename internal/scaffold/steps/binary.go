package steps

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

type BinaryStep struct {
	name     string
	binary   string
	args     []string
	priority int
}

func NewBinaryStep(name, binary string, args []string, priority int) *BinaryStep {
	return &BinaryStep{
		name:     name,
		binary:   binary,
		args:     args,
		priority: priority,
	}
}

func (s *BinaryStep) Name() string {
	return s.name
}

func (s *BinaryStep) Run(ctx types.ScaffoldContext, opts types.StepOptions) error {
	allArgs := append(s.args, opts.Args...)
	if opts.Verbose {
		fullCmd := append([]string{s.binary}, allArgs...)
		fmt.Printf("  Running: %s\n", strings.Join(fullCmd, " "))
	}
	cmd := exec.Command(s.binary, allArgs...)
	cmd.Dir = ctx.WorktreePath
	return cmd.Run()
}

func (s *BinaryStep) Priority() int {
	return s.priority
}

func (s *BinaryStep) Condition(ctx types.ScaffoldContext) bool {
	_, err := exec.LookPath(s.binary)
	return err == nil
}
