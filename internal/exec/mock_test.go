package exec

import (
	"context"
	"fmt"
	"strings"
)

// MockCommander is a test double that records command calls and returns preset responses.
// Use this in tests to verify commands are executed correctly without actually running them.
type MockCommander struct {
	// Responses maps command keys to their preset responses.
	// The key is formatted as: "command arg1 arg2 ..."
	Responses map[string]CommandResponse

	// Calls records all commands that were executed.
	// Each entry contains the full details of a command invocation.
	Calls []CommandCall
}

// CommandCall records details of a single command execution.
type CommandCall struct {
	// Dir is the working directory where the command was executed.
	Dir string

	// Command is the executable that was called.
	Command string

	// Args contains all arguments passed to the command.
	Args []string
}

// CommandResponse defines the response for a specific command.
type CommandResponse struct {
	// Output is the byte slice to return as the command output.
	Output []byte

	// Err is the error to return (nil for successful execution).
	Err error
}

// NewMockCommander creates a new MockCommander with empty responses and calls.
func NewMockCommander() *MockCommander {
	return &MockCommander{
		Responses: make(map[string]CommandResponse),
		Calls:     make([]CommandCall, 0),
	}
}

// Run records the command call and returns the preset response if one exists.
// The command key is constructed as "command arg1 arg2 ...".
// If no response is found for the key, it returns nil, nil.
func (m *MockCommander) Run(ctx context.Context, dir string, command string, args ...string) ([]byte, error) {
	call := CommandCall{
		Dir:     dir,
		Command: command,
		Args:    args,
	}
	m.Calls = append(m.Calls, call)

	key := buildCommandKey(command, args)
	if resp, ok := m.Responses[key]; ok {
		return resp.Output, resp.Err
	}

	// No preset response found - return success by default
	return nil, nil
}

// SetResponse configures a preset response for a specific command.
// The command key is automatically built from the command and args.
func (m *MockCommander) SetResponse(command string, args []string, output []byte, err error) {
	key := buildCommandKey(command, args)
	m.Responses[key] = CommandResponse{
		Output: output,
		Err:    err,
	}
}

// GetCall returns the nth command call (0-indexed).
// Returns nil if n is out of range.
func (m *MockCommander) GetCall(n int) *CommandCall {
	if n < 0 || n >= len(m.Calls) {
		return nil
	}
	return &m.Calls[n]
}

// LastCall returns the most recent command call.
// Returns nil if no commands have been executed.
func (m *MockCommander) LastCall() *CommandCall {
	if len(m.Calls) == 0 {
		return nil
	}
	return &m.Calls[len(m.Calls)-1]
}

// CallCount returns the number of commands that have been executed.
func (m *MockCommander) CallCount() int {
	return len(m.Calls)
}

// WasCalled checks if a command with the given arguments was ever executed.
// The command key must match exactly.
func (m *MockCommander) WasCalled(command string, args ...string) bool {
	key := buildCommandKey(command, args)
	for _, call := range m.Calls {
		callKey := buildCommandKey(call.Command, call.Args)
		if callKey == key {
			return true
		}
	}
	return false
}

// Reset clears all recorded calls and responses.
func (m *MockCommander) Reset() {
	m.Calls = make([]CommandCall, 0)
	m.Responses = make(map[string]CommandResponse)
}

// buildCommandKey constructs a command key from command and args.
func buildCommandKey(command string, args []string) string {
	if len(args) == 0 {
		return command
	}
	return fmt.Sprintf("%s %s", command, strings.Join(args, " "))
}
