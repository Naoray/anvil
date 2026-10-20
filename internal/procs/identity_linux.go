//go:build linux

package procs

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// StartIdentity returns "linux:<boot id>:<start ticks>" for a live process.
func StartIdentity(pid int) (string, error) {
	if pid <= 0 {
		return "", ErrNoProcess
	}
	content, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	// A process being reaped while its stat file is read yields ESRCH
	// rather than ENOENT; both mean it no longer exists.
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return "", ErrNoProcess
	}
	if err != nil {
		return "", err
	}
	if len(content) == 0 {
		return "", ErrNoProcess // exited while being read
	}
	// The command name may contain spaces and parentheses; fields resume
	// after the last ')'.
	text := string(content)
	end := strings.LastIndexByte(text, ')')
	if end < 0 {
		return "", fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(text[end+1:])
	if len(fields) < 20 {
		return "", fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	if fields[0] == "Z" || fields[0] == "X" {
		return "", ErrNoProcess
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return "linux:" + strings.TrimSpace(string(boot)) + ":" + fields[19], nil
}
