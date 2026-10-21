//go:build linux

package procs

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ScanOpenFiles records, for every process this user can inspect, the paths
// it references through /proc/<pid>/{cwd,root,exe,fd/*,maps}. This process
// is excluded. A process that exits during the scan is ignored.
func ScanOpenFiles() (Snapshot, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: reading /proc: %v", ErrHoldersUnsupported, err)
	}
	snapshot := Snapshot{Method: MethodProcfs, OtherUsersCounted: true}
	self := os.Getpid()
	euid := os.Geteuid()
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == self {
			continue
		}
		process, uid, err := inspectProcess(pid)
		switch {
		case err == nil:
			snapshot.processes = append(snapshot.processes, process)
		case gone(err):
		case denied(err):
			if uid == euid {
				snapshot.UninspectableOwn = append(snapshot.UninspectableOwn, Process{PID: pid, Command: commandName(pid)})
			} else {
				snapshot.SkippedOtherUsers++
			}
		default:
			return Snapshot{}, fmt.Errorf("inspecting process %d: %w", pid, err)
		}
	}
	return snapshot, nil
}

// commandName is best effort: comm stays readable for most processes whose
// other entries are not.
func commandName(pid int) string {
	comm, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(comm))
}

func gone(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

func denied(err error) bool {
	return errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM)
}

// inspectProcess returns the process paths and its owner. A missing link
// (kernel threads have no exe; zombies no cwd) is not an error.
func inspectProcess(pid int) (processPaths, int, error) {
	base := "/proc/" + strconv.Itoa(pid)
	info, err := os.Stat(base)
	if err != nil {
		return processPaths{}, -1, err
	}
	uid := -1
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		uid = int(stat.Uid)
	}
	process := processPaths{pid: pid, paths: map[string][]string{}}
	process.command = commandName(pid)
	for _, link := range []string{"cwd", "root", "exe"} {
		target, err := os.Readlink(base + "/" + link)
		if err != nil {
			if gone(err) {
				continue
			}
			return processPaths{}, uid, err
		}
		if link == "root" && target == "/" {
			continue // every unconfined process has / as its root
		}
		process.add(target, link)
	}
	fds, err := os.ReadDir(base + "/fd")
	if err != nil && !gone(err) {
		return processPaths{}, uid, err
	}
	for _, fd := range fds {
		target, err := os.Readlink(filepath.Join(base, "fd", fd.Name()))
		if err != nil {
			if gone(err) {
				continue // closed during the scan
			}
			return processPaths{}, uid, err
		}
		process.add(target, "fd")
	}
	maps, err := os.ReadFile(base + "/maps")
	if err != nil && !gone(err) {
		return processPaths{}, uid, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(maps))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		// address perms offset dev inode pathname (pathname may hold spaces)
		fields := strings.SplitN(scanner.Text(), " ", 6)
		if len(fields) == 6 {
			process.add(strings.TrimSpace(fields[5]), "maps")
		}
	}
	if err := scanner.Err(); err != nil {
		return processPaths{}, uid, fmt.Errorf("reading %s/maps: %w", base, err)
	}
	return process, uid, nil
}
