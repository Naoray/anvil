package steps

import (
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

type FileCopyStep struct {
	from     string
	to       string
	priority int
}

func NewFileCopyStep(from, to string, priority ...int) *FileCopyStep {
	p := 15
	if len(priority) > 0 {
		p = priority[0]
	}
	return &FileCopyStep{from: from, to: to, priority: p}
}

func (s *FileCopyStep) Name() string {
	return "file.copy"
}

func (s *FileCopyStep) Run(ctx types.ScaffoldContext, opts types.StepOptions) error {
	return nil
}

func (s *FileCopyStep) Priority() int {
	return s.priority
}

func (s *FileCopyStep) Condition(ctx types.ScaffoldContext) bool {
	return true
}
