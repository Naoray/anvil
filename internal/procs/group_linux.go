//go:build linux

package procs

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// groupMembers returns the live PIDs whose process group is pgid.
func groupMembers(pgid int) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // not a process directory
		}
		content, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			continue // exited while scanning
		}
		if err != nil {
			return nil, err
		}
		text := string(content)
		end := strings.LastIndexByte(text, ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(text[end+1:])
		if len(fields) < 3 || fields[0] == "Z" || fields[0] == "X" {
			continue
		}
		if fields[2] == strconv.Itoa(pgid) {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}
