package errors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTypedErrors(t *testing.T) {
	assert.True(t, errors.Is(ErrWorktreeNotFound, ErrWorktreeNotFound))
	assert.False(t, errors.Is(ErrWorktreeNotFound, ErrConfigNotFound))
	assert.False(t, errors.Is(ErrConfigNotFound, ErrGitOperationFailed))

	wrapped := fmt.Errorf("context: %w", ErrWorktreeNotFound)
	assert.True(t, errors.Is(wrapped, ErrWorktreeNotFound))

	wrappedConfig := fmt.Errorf("config error: %w", ErrConfigNotFound)
	assert.True(t, errors.Is(wrappedConfig, ErrConfigNotFound))
	assert.False(t, errors.Is(wrappedConfig, ErrWorktreeNotFound))
}

func TestWrappedErrors_Chain(t *testing.T) {
	original := fmt.Errorf("original: %w", ErrGitOperationFailed)
	wrapped := fmt.Errorf("wrapped: %w", original)

	assert.True(t, errors.Is(wrapped, ErrGitOperationFailed))
	assert.True(t, errors.Is(wrapped, original))
}

func TestErrorMessages(t *testing.T) {
	assert.Equal(t, "worktree not found", ErrWorktreeNotFound.Error())
	assert.Equal(t, "configuration not found", ErrConfigNotFound.Error())
	assert.Equal(t, "git operation failed", ErrGitOperationFailed.Error())
}
