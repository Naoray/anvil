package cli

import "fmt"

// ChildExitError carries a child process's exit code (and optional message)
// to cmd/anvil/main.go, which maps it to os.Exit — RunE never exits directly.
type ChildExitError struct {
	Code    int
	Message string
}

func (e *ChildExitError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("exit status %d", e.Code)
}
