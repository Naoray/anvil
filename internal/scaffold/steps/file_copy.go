package steps

import (
	"fmt"
	"path/filepath"

	"github.com/artisanexperiences/arbor/internal/fs"
	"github.com/artisanexperiences/arbor/internal/scaffold/types"
)

type FileCopyStep struct {
	from string
	to   string
	fs   fs.FS
}

// NewFileCopyStep creates a file copy step with the default file system.
func NewFileCopyStep(from, to string) *FileCopyStep {
	return NewFileCopyStepWithFS(from, to, nil)
}

// NewFileCopyStepWithFS creates a file copy step with a custom file system.
// This is useful for testing with mock file systems.
func NewFileCopyStepWithFS(from, to string, filesystem fs.FS) *FileCopyStep {
	if filesystem == nil {
		filesystem = fs.Default
	}
	return &FileCopyStep{from: from, to: to, fs: filesystem}
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

	// Use the file system interface for testability
	data, err := s.fs.ReadFile(fromPath)
	if err != nil {
		return fmt.Errorf("reading source file %s: %w", fromPath, err)
	}

	if err := s.fs.WriteFile(toPath, data, 0644); err != nil {
		return fmt.Errorf("writing destination file %s: %w", toPath, err)
	}

	return nil
}

func (s *FileCopyStep) Condition(ctx *types.ScaffoldContext) bool {
	fromPath := filepath.Join(ctx.WorktreePath, s.from)
	_, err := s.fs.Stat(fromPath)
	return err == nil
}
