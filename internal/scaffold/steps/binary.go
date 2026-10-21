package steps

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/naoray/anvil/internal/config"
	anvil_exec "github.com/naoray/anvil/internal/exec"
	"github.com/naoray/anvil/internal/scaffold/template"
	"github.com/naoray/anvil/internal/scaffold/types"
	"github.com/naoray/anvil/internal/warm"
)

// ansiPattern matches ANSI escape sequences (colors, cursor movement, etc.)
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

type BinaryStep struct {
	target    execTarget
	name      string
	binary    string
	args      []string
	condition map[string]any
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
	return NewBinaryStepWithConditionAndExecutor(name, cfg, binary, nil)
}

// NewBinaryStepWithConditionAndExecutor creates a conditional binary step with a custom command executor.
// This is useful for testing conditional command execution without launching an operating-system process.
func NewBinaryStepWithConditionAndExecutor(name string, cfg config.StepConfig, binary string, executor *anvil_exec.CommandExecutor) *BinaryStep {
	if executor == nil {
		executor = anvil_exec.NewCommandExecutor(nil)
	}

	return &BinaryStep{
		name:      name,
		binary:    binary,
		args:      cfg.Args,
		condition: cfg.Condition,
		storeAs:   cfg.StoreAs,
		executor:  executor,
	}
}

func (s *BinaryStep) Name() string {
	return s.name
}

func (s *BinaryStep) GetArgs() []string {
	return s.args
}

func (s *BinaryStep) Condition(ctx *types.ScaffoldContext) (bool, error) {
	if len(s.condition) > 0 {
		result, err := ctx.EvaluateCondition(s.condition)
		if err != nil || !result {
			return false, err
		}
	}

	binaries := strings.Fields(s.binary)
	if len(binaries) == 0 {
		return false, nil
	}
	_, err := exec.LookPath(binaries[0])
	return err == nil, nil
}

func (s *BinaryStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	if opts.DryRun {
		return nil
	}
	allArgs := append(s.args, opts.Args...)
	allArgs = s.replaceTemplate(allArgs, ctx)

	var install *warm.Install
	if s.target.dir == "" {
		policy := warm.Policy{TrustInstallScripts: opts.WarmNodeModules.TrustInstallScripts, Rebuild: opts.WarmNodeModules.Rebuild}
		install = warm.PrepareWithPolicy(ctx.WorktreePath, s.name, s.binary, allArgs, policy)
	}
	if install != nil && s.storeAs == "" && opts.WarmSource != "" {
		mode := opts.CopyMode
		if mode == "" {
			mode = "auto"
		}
		result, err := install.Reuse(opts.WarmSource, mode)
		if err != nil {
			return fmt.Errorf("warming %s: %w", install.Tree(), err)
		}
		if result != warm.Cold && install.Rebuild() != "" {
			// The rebuild mutates the warm tree: it must still be detached
			// from the source and certifiable. Otherwise no tree or
			// provenance is left behind, so a retry installs cold, and the
			// warm hit is never reported.
			err := s.rebuildWarm(ctx, install, opts)
			if err == nil {
				err = install.RecordWarm()
			}
			if err != nil {
				if discardErr := install.Discard(); discardErr != nil {
					err = errors.Join(err, fmt.Errorf("removing failed warm %s: %w", install.Tree(), discardErr))
				}
				return fmt.Errorf("warming %s: %w", install.Tree(), err)
			}
			if !opts.Quiet {
				fmt.Printf("  %s: %s\n", install.Tree(), result)
			}
			return nil
		}
		if !opts.Quiet {
			fmt.Printf("  %s: %s\n", install.Tree(), result)
		}
		if result != warm.Cold {
			if install.Tree() == "vendor" {
				args := []string{"dump-autoload", "--no-interaction", "--no-plugins", "--no-scripts"}
				if opts.Verbose {
					fmt.Printf("  Running: %s %s\n", s.binary, strings.Join(args, " "))
				}
				output, err := s.executor.RunBinary(context.Background(), ctx.WorktreePath, s.binary, args)
				if err != nil {
					return fmt.Errorf("refreshing warm Composer autoload: %w\n%s", err, output)
				}
			}
			return install.Record()
		}
	} else if opts.WarmSource != "" && len(allArgs) > 0 && (s.name == "node.npm" && allArgs[0] == "ci" || s.name == "php.composer" && allArgs[0] == "install") && !opts.Quiet {
		fmt.Printf("  %s: cold (install inputs are not reusable)\n", s.name)
	}

	if opts.Verbose {
		binaryParts := strings.Fields(s.binary)
		fullCmd := append(binaryParts, allArgs...)
		fmt.Printf("  Running: %s\n", strings.Join(fullCmd, " "))
	}

	dir, env, err := s.target.resolve(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", s.name, err)
	}
	// Use the command executor for testability
	var output []byte
	if env == nil {
		output, err = s.executor.RunBinary(context.Background(), dir, s.binary, allArgs)
	} else {
		output, err = s.executor.RunBinaryWithEnv(context.Background(), dir, env, s.binary, allArgs)
	}
	if err != nil {
		return fmt.Errorf("%s failed: %w\n%s", s.name, err, string(output))
	}

	if install != nil {
		if err := install.Record(); err != nil {
			return fmt.Errorf("recording dependency install: %w", err)
		}
	}

	if s.storeAs != "" {
		cleaned := ansiPattern.ReplaceAllString(string(output), "")
		ctx.SetVar(s.storeAs, strings.TrimSpace(cleaned))
		if opts.Verbose {
			fmt.Printf("  Stored output as %s\n", s.storeAs)
		}
	}

	return nil
}

// rebuildWarm runs the declared post-warm command in the destination with the
// step's environment, then refuses a tree still referencing the source.
func (s *BinaryStep) rebuildWarm(ctx *types.ScaffoldContext, install *warm.Install, opts types.StepOptions) error {
	command := install.Rebuild()
	if command == "" {
		return nil
	}
	if opts.Verbose {
		fmt.Printf("  Running: %s\n", command)
	}
	dir, env, err := s.target.resolve(ctx)
	if err != nil {
		return err
	}
	output, err := s.executor.RunShellWithEnv(context.Background(), dir, env, command)
	if err != nil {
		return fmt.Errorf("post-warm rebuild %q failed: %w\n%s", command, err, output)
	}
	if opts.Verbose && len(output) > 0 {
		fmt.Print(string(output))
	}
	return install.CheckDetached()
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

// WithTarget sets the step's working directory and extra environment. Warm
// dependency reuse only applies to worktree-root installs.
func (s *BinaryStep) WithTarget(cfg config.StepConfig) *BinaryStep {
	s.target = newExecTarget(cfg)
	return s
}
