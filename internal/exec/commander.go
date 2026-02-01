// Package exec provides interfaces and implementations for command execution.
// This abstraction allows for dependency injection and testing of steps that
// execute external commands.
package exec

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Commander defines the interface for executing commands.
// Implementations can provide real command execution or mock behavior for testing.
type Commander interface {
	// Run executes a command in the specified directory with the given arguments.
	// Returns the combined stdout and stderr output, and any execution error.
	Run(ctx context.Context, dir string, command string, args ...string) ([]byte, error)
}

// RealCommander executes commands using the real operating system.
// This is the production implementation that actually runs commands.
type RealCommander struct{}

// Run executes the command using exec.CommandContext.
// The command is executed in the specified directory with the provided arguments.
func (c *RealCommander) Run(ctx context.Context, dir string, command string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// CommandExecutor provides a higher-level interface for common execution patterns.
// It wraps a Commander and provides convenience methods.
type CommandExecutor struct {
	commander Commander
}

// NewCommandExecutor creates a new CommandExecutor with the given Commander.
// If commander is nil, a RealCommander is used.
func NewCommandExecutor(commander Commander) *CommandExecutor {
	if commander == nil {
		commander = &RealCommander{}
	}
	return &CommandExecutor{commander: commander}
}

// RunBinary executes a binary command with arguments.
// The binary can contain spaces (e.g., "php artisan") and will be properly split.
func (e *CommandExecutor) RunBinary(ctx context.Context, dir string, binary string, args []string) ([]byte, error) {
	binaryParts := strings.Fields(binary)
	if len(binaryParts) == 0 {
		return nil, fmt.Errorf("empty binary command")
	}

	command := binaryParts[0]
	allArgs := append(binaryParts[1:], args...)

	return e.commander.Run(ctx, dir, command, allArgs...)
}

// RunBash executes a command through bash -c.
// This is useful for complex commands that require bash features.
func (e *CommandExecutor) RunBash(ctx context.Context, dir string, command string) ([]byte, error) {
	return e.commander.Run(ctx, dir, "bash", "-c", command)
}

// RunShell executes a command through sh -c.
// This is more portable than bash but has fewer features.
func (e *CommandExecutor) RunShell(ctx context.Context, dir string, command string) ([]byte, error) {
	return e.commander.Run(ctx, dir, "sh", "-c", command)
}

// DefaultExecutor is a package-level default executor using RealCommander.
// It can be used when you don't need to inject a custom Commander.
var DefaultExecutor = NewCommandExecutor(nil)
