//go:build windows

package checks

import (
	"os/exec"
	"time"
)

// configureGroup relies on the default kill of the direct child on Windows;
// grandchildren are not tracked (documented limitation).
func configureGroup(cmd *exec.Cmd) {
	cmd.WaitDelay = 5 * time.Second
}

func killGroup(*exec.Cmd) {}
