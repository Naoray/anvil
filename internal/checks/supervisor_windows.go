//go:build windows

package checks

import (
	"context"
	"errors"
	"os/exec"

	"github.com/naoray/anvil/internal/lockfile"
)

// Fail closed until job-object supervision can retain exclusion through the
// complete descendant lifetime on Windows.
func supervisedCommand(context.Context, string, []*lockfile.Lock) (*exec.Cmd, func() error, error) {
	return nil, nil, errors.New("check commands and toolchain probes are unsupported on Windows: complete process-lifetime supervision requires job objects")
}
