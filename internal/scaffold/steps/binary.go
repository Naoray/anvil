package steps

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/naoray/anvil/internal/config"
	anvil_exec "github.com/naoray/anvil/internal/exec"
	"github.com/naoray/anvil/internal/scaffold/template"
	"github.com/naoray/anvil/internal/scaffold/types"
)

type BinaryStep struct {
	name      string
	binary    string
	args      []string
	condition map[string]interface{}
	storeAs   string
	executor  *anvil_exec.CommandExecutor
}

// NewBinaryStep creates a binary step with the default command executor.
func NewBinaryStep(name, binary string, args []string, storeAs string) *BinaryStep {
	return NewBinaryStepWithExecutor(name, binary, args, storeAs, nil)
}

// NewBinaryStepWithExecutor creates a binary step with a custom command executor.
// This is useful for testing with mock executors.
func NewBinaryStepWithExecutor(name, binary string, args []string, storeAs string, executor *anvil_exec.CommandExecutor) *BinaryStep {
	if executor == nil {
		executor = anvil_exec.NewCommandExecutor(nil)
	}
	return &BinaryStep{
		name:      name,
		binary:    binary,
		args:      args,
		condition: nil,
		storeAs:   storeAs,
		executor:  executor,
	}
}

// NewBinaryStepWithCondition creates a binary step with condition evaluation.
// This is the factory function used by the registry.
func NewBinaryStepWithCondition(name string, cfg config.StepConfig, binary string) *BinaryStep {
	return &BinaryStep{
		name:      name,
		binary:    binary,
		args:      cfg.Args,
		condition: cfg.Condition,
		storeAs:   cfg.StoreAs,
		executor:  anvil_exec.NewCommandExecutor(nil),
	}
}

func (s *BinaryStep) Name() string {
	return s.name
}

func (s *BinaryStep) GetArgs() []string {
	return s.args
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

	// Use the command executor for testability
	output, err := s.executor.RunBinary(context.Background(), ctx.WorktreePath, s.binary, allArgs)
	if err != nil {
		return fmt.Errorf("%s failed: %w\n%s", s.name, err, string(output))
	}

	if s.storeAs != "" {
		ctx.SetVar(s.storeAs, strings.TrimSpace(string(output)))
		if opts.Verbose {
			fmt.Printf("  Stored output as %s\n", s.storeAs)
		}
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
