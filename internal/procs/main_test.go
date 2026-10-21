package procs

import (
	"os"
	"testing"
)

// TestMain lets the test binary act as the process-group supervisor, the
// role the anvil binary plays in production.
func TestMain(m *testing.M) {
	if code, ok := Supervise(os.Args); ok {
		os.Exit(code)
	}
	os.Exit(m.Run())
}
