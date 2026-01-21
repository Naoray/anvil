package ui

import (
	"context"
	"errors"
	"io"

	"github.com/charmbracelet/huh"
)

// ErrUserAborted is returned when the user aborts an interactive prompt.
// This sentinel error normalizes various abort signals (Ctrl+C, Esc, Ctrl+D, etc.)
// into a single error that can be handled centrally.
var ErrUserAborted = errors.New("user aborted")

// NormalizeAbort converts known abort-like errors to ErrUserAborted.
// This includes huh.ErrUserAborted (Esc/Ctrl+C in huh prompts),
// io.EOF (Ctrl+D/closed stdin), and context.Canceled.
func NormalizeAbort(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, huh.ErrUserAborted) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, context.Canceled) {
		return ErrUserAborted
	}
	return err
}

// IsAbort returns true if the error represents a user abort.
func IsAbort(err error) bool {
	return errors.Is(err, ErrUserAborted)
}
