// Package agentapi defines Anvil's versioned machine-readable response
// envelope and its stable error codes. Every --json command writes exactly
// one Envelope to stdout; diagnostics and child process output go to stderr.
package agentapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// SchemaVersion identifies the envelope contract. Additive fields keep the
// version; renaming or removing a field, or changing a field's meaning,
// requires a new version.
const SchemaVersion = "anvil.agent/v1"

// Exit codes shared by agent commands (documented in AGENTS.md).
const (
	ExitGeneral       = 1
	ExitInvalid       = 2
	ExitNotFound      = 3
	ExitGit           = 4
	ExitConfiguration = 5
	ExitPrepare       = 6
	ExitUnavailable   = 7
)

// Stable error codes. Codes are never reused for a different meaning.
const (
	CodeInternal          = "internal_error"
	CodeInvalidArguments  = "invalid_arguments"
	CodeProjectNotFound   = "project_not_found"
	CodeWorktreeNotFound  = "worktree_not_found"
	CodeAmbiguous         = "ambiguous_worktree"
	CodeGitFailed         = "git_failed"
	CodeConfigInvalid     = "config_invalid"
	CodePrepareFailed     = "prepare_failed"
	CodeNotPrepared       = "not_prepared"
	CodeResourceBusy      = "resource_busy"
	CodeLeaseHeld         = "lease_held"
	CodeLeaseStale        = "lease_stale"
	CodeLeaseNotOwned     = "lease_not_owned"
	CodeUnsupported       = "unsupported_platform"
	CodeCheckNotFound     = "check_not_found"
	CodeCheckFailed       = "check_failed"
	CodeExpensiveCheck    = "expensive_check_requires_opt_in"
	CodeProcessNotFound   = "process_not_found"
	CodeCleanupBlocked    = "cleanup_blocked"
	CodeComponentNotFound = "component_not_found"
)

var exitCodes = map[string]int{
	CodeInternal:          ExitGeneral,
	CodeInvalidArguments:  ExitInvalid,
	CodeProjectNotFound:   ExitConfiguration,
	CodeWorktreeNotFound:  ExitNotFound,
	CodeAmbiguous:         ExitInvalid,
	CodeGitFailed:         ExitGit,
	CodeConfigInvalid:     ExitConfiguration,
	CodePrepareFailed:     ExitPrepare,
	CodeNotPrepared:       ExitPrepare,
	CodeResourceBusy:      ExitUnavailable,
	CodeLeaseHeld:         ExitUnavailable,
	CodeLeaseStale:        ExitUnavailable,
	CodeLeaseNotOwned:     ExitUnavailable,
	CodeUnsupported:       ExitGeneral,
	CodeCheckNotFound:     ExitNotFound,
	CodeCheckFailed:       ExitGeneral,
	CodeExpensiveCheck:    ExitInvalid,
	CodeProcessNotFound:   ExitNotFound,
	CodeCleanupBlocked:    ExitUnavailable,
	CodeComponentNotFound: ExitNotFound,
}

// Codes returns every stable error code with its exit status.
func Codes() map[string]int {
	out := make(map[string]int, len(exitCodes))
	for code, exit := range exitCodes {
		out[code] = exit
	}
	return out
}

// Error is a classified failure. Message is safe to print; it must never
// contain secrets.
type Error struct {
	Code    string
	Message string
	Details map[string]any
	// Exit overrides the code's default exit status (for example to pass a
	// failing check's child exit status through).
	Exit  int
	Cause error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.Cause }

// ExitCode returns the process exit status for the error.
func (e *Error) ExitCode() int {
	if e.Exit != 0 {
		return e.Exit
	}
	if code, ok := exitCodes[e.Code]; ok {
		return code
	}
	return ExitGeneral
}

// Errorf creates a classified error.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap classifies err, keeping an existing classification.
func Wrap(code string, err error) *Error {
	var existing *Error
	if errors.As(err, &existing) {
		return existing
	}
	return &Error{Code: code, Message: err.Error(), Cause: err}
}

// WithDetails attaches structured, nonsecret details.
func (e *Error) WithDetails(details map[string]any) *Error {
	e.Details = details
	return e
}

// ErrorBody is the serialized error.
type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Envelope is the single JSON document a --json command writes to stdout.
// Data may be present on failure to describe partial progress (for example
// which preparation step failed); ok is false whenever the operation did not
// fully succeed.
type Envelope struct {
	Schema   string     `json:"schema"`
	Command  string     `json:"command"`
	OK       bool       `json:"ok"`
	Mutating bool       `json:"mutating"`
	Data     any        `json:"data,omitempty"`
	Error    *ErrorBody `json:"error,omitempty"`
	Warnings []string   `json:"warnings"`
}

// NewEnvelope builds an envelope from a result and an error.
func NewEnvelope(command string, mutating bool, data any, warnings []string, err error) Envelope {
	if warnings == nil {
		warnings = []string{}
	}
	envelope := Envelope{Schema: SchemaVersion, Command: command, OK: err == nil, Mutating: mutating, Data: data, Warnings: warnings}
	if err != nil {
		classified := Wrap(CodeInternal, err)
		envelope.Error = &ErrorBody{Code: classified.Code, Message: classified.Message, Details: classified.Details}
	}
	return envelope
}

// Write encodes the envelope as indented JSON. HTML escaping is disabled so
// paths and commands appear verbatim.
func (e Envelope) Write(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(e)
}
