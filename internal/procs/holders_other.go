//go:build !linux

package procs

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// ScanOpenFiles uses lsof on macOS. Elsewhere open files cannot be
// inspected and ErrHoldersUnsupported is returned.
func ScanOpenFiles() (Snapshot, error) {
	if runtime.GOOS == "darwin" {
		return scanLsof()
	}
	return Snapshot{}, fmt.Errorf("%w (%s)", ErrHoldersUnsupported, runtime.GOOS)
}

// scanLsof runs lsof over every process and parses its field output.
func scanLsof() (Snapshot, error) {
	lsof, err := exec.LookPath("lsof")
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: lsof not found", ErrHoldersUnsupported)
	}
	var stderr bytes.Buffer
	cmd := exec.Command(lsof, "-n", "-P", "-Fpcn")
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return Snapshot{}, fmt.Errorf("running lsof: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parseLsof(out)
}
