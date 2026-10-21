//go:build windows

package checks

import (
	"os"
	"os/exec"
	"time"
)

// configureGroup relies on the default kill of the direct child on Windows;
// grandchildren are not tracked (documented limitation).
func configureGroup(cmd *exec.Cmd) {
	cmd.WaitDelay = 5 * time.Second
}

func killGroup(*exec.Cmd) {}

// inheritFiles is a no-op: Windows cannot pass extra handles through
// os/exec, so locks end with Anvil there (documented limitation).
func inheritFiles(*exec.Cmd, []*os.File) {}
