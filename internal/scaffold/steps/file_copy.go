package steps

import (
	"fmt"
	"os"
	"path/filepath"

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

func (s *FileCopyStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	fromPath := filepath.Join(ctx.WorktreePath, s.from)
	toPath := filepath.Join(ctx.WorktreePath, s.to)

	if opts.Verbose {
		fmt.Printf("  Copying %s to %s\n", s.from, s.to)
	}

	data, err := os.ReadFile(fromPath)
	if err != nil {
		return fmt.Errorf("reading source file %s: %w", fromPath, err)
	}

	if err := os.WriteFile(toPath, data, 0644); err != nil {
		return fmt.Errorf("writing destination file %s: %w", toPath, err)
	}

	return nil
}

func (s *FileCopyStep) Priority() int {
	return s.priority
}

func (s *FileCopyStep) Condition(ctx *types.ScaffoldContext) bool {
	fromPath := filepath.Join(ctx.WorktreePath, s.from)
	_, err := os.Stat(fromPath)
	return err == nil
}
