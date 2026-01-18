package steps

import (
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

type FileCopyStep struct {
	from string
	to   string
}

func NewFileCopyStep(from, to string) *FileCopyStep {
	return &FileCopyStep{from: from, to: to}
}

func (s *FileCopyStep) Name() string {
	return "file.copy"
}

func (s *FileCopyStep) Run(ctx types.ScaffoldContext, opts types.StepOptions) error {
	return nil
}

func (s *FileCopyStep) Priority() int {
	return 50
}

func (s *FileCopyStep) Condition(ctx types.ScaffoldContext) bool {
	return true
}
