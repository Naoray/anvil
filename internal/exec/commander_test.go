package exec

import (
	"context"
	"errors"
	"testing"
)

func TestRealCommander_Run(t *testing.T) {
	commander := &RealCommander{}
	ctx := context.Background()

	// Test running a simple command that should succeed
	output, err := commander.Run(ctx, ".", "echo", "hello")
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if string(output) != "hello\n" {
		t.Errorf("expected 'hello\\n', got: %s", string(output))
	}
}

func TestRealCommander_Run_WithContextCancellation(t *testing.T) {
	commander := &RealCommander{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// This should fail because context is cancelled
	_, err := commander.Run(ctx, ".", "sleep", "1")
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestCommandExecutor_RunBinary(t *testing.T) {
	mock := NewMockCommander()
	mock.SetResponse("php", []string{"-v"}, []byte("PHP 8.0"), nil)

	executor := NewCommandExecutor(mock)
	ctx := context.Background()

	output, err := executor.RunBinary(ctx, "/worktree", "php", []string{"-v"})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if string(output) != "PHP 8.0" {
		t.Errorf("expected 'PHP 8.0', got: %s", string(output))
	}

	// Verify the call was recorded
	if mock.CallCount() != 1 {
		t.Errorf("expected 1 call, got %d", mock.CallCount())
	}

	call := mock.LastCall()
	if call == nil {
		t.Fatal("expected call to be recorded")
	}
	if call.Dir != "/worktree" {
		t.Errorf("expected dir '/worktree', got: %s", call.Dir)
	}
	if call.Command != "php" {
		t.Errorf("expected command 'php', got: %s", call.Command)
	}
}

func TestCommandExecutor_RunBinary_WithSpaces(t *testing.T) {
	mock := NewMockCommander()
	mock.SetResponse("php", []string{"artisan", "migrate"}, []byte("migrated"), nil)

	executor := NewCommandExecutor(mock)
	ctx := context.Background()

	output, err := executor.RunBinary(ctx, "/worktree", "php artisan", []string{"migrate"})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if string(output) != "migrated" {
		t.Errorf("expected 'migrated', got: %s", string(output))
	}

	call := mock.LastCall()
	if call.Command != "php" {
		t.Errorf("expected command 'php', got: %s", call.Command)
	}
	if len(call.Args) != 2 || call.Args[0] != "artisan" || call.Args[1] != "migrate" {
		t.Errorf("expected args ['artisan', 'migrate'], got: %v", call.Args)
	}
}

func TestCommandExecutor_RunBash(t *testing.T) {
	mock := NewMockCommander()
	mock.SetResponse("bash", []string{"-c", "echo hello"}, []byte("hello"), nil)

	executor := NewCommandExecutor(mock)
	ctx := context.Background()

	output, err := executor.RunBash(ctx, "/worktree", "echo hello")

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if string(output) != "hello" {
		t.Errorf("expected 'hello', got: %s", string(output))
	}

	call := mock.LastCall()
	if call.Command != "bash" {
		t.Errorf("expected command 'bash', got: %s", call.Command)
	}
	if len(call.Args) != 2 || call.Args[0] != "-c" || call.Args[1] != "echo hello" {
		t.Errorf("expected args ['-c', 'echo hello'], got: %v", call.Args)
	}
}

func TestCommandExecutor_RunShell(t *testing.T) {
	mock := NewMockCommander()
	mock.SetResponse("sh", []string{"-c", "ls -la"}, []byte("file.txt"), nil)

	executor := NewCommandExecutor(mock)
	ctx := context.Background()

	output, err := executor.RunShell(ctx, "/worktree", "ls -la")

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if string(output) != "file.txt" {
		t.Errorf("expected 'file.txt', got: %s", string(output))
	}

	call := mock.LastCall()
	if call.Command != "sh" {
		t.Errorf("expected command 'sh', got: %s", call.Command)
	}
}

func TestMockCommander_WasCalled(t *testing.T) {
	mock := NewMockCommander()

	// Run a command
	ctx := context.Background()
	mock.Run(ctx, "/worktree", "git", "status")

	// Check if it was called
	if !mock.WasCalled("git", "status") {
		t.Error("expected WasCalled to return true for 'git status'")
	}

	if mock.WasCalled("git", "log") {
		t.Error("expected WasCalled to return false for 'git log'")
	}
}

func TestMockCommander_Reset(t *testing.T) {
	mock := NewMockCommander()
	ctx := context.Background()

	// Run some commands
	mock.Run(ctx, ".", "echo", "hello")
	mock.Run(ctx, ".", "echo", "world")

	if mock.CallCount() != 2 {
		t.Errorf("expected 2 calls before reset, got %d", mock.CallCount())
	}

	// Reset
	mock.Reset()

	if mock.CallCount() != 0 {
		t.Errorf("expected 0 calls after reset, got %d", mock.CallCount())
	}

	if len(mock.Responses) != 0 {
		t.Error("expected responses to be cleared")
	}
}

func TestMockCommander_NoResponse(t *testing.T) {
	mock := NewMockCommander()
	ctx := context.Background()

	// Run without setting a response
	output, err := mock.Run(ctx, ".", "unknown", "cmd")

	if err != nil {
		t.Errorf("expected no error for unset response, got: %v", err)
	}
	if output != nil {
		t.Errorf("expected nil output for unset response, got: %v", output)
	}
}

func TestMockCommander_ErrorResponse(t *testing.T) {
	mock := NewMockCommander()
	expectedErr := errors.New("command failed")
	mock.SetResponse("failing", []string{"cmd"}, []byte("error output"), expectedErr)

	ctx := context.Background()
	output, err := mock.Run(ctx, ".", "failing", "cmd")

	if err != expectedErr {
		t.Errorf("expected error %v, got: %v", expectedErr, err)
	}
	if string(output) != "error output" {
		t.Errorf("expected 'error output', got: %s", string(output))
	}
}
