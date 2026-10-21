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

// procfsSource is what a procfs scan reads; tests inject it.
type procfsSource struct {
	self, euid    int
	selfNamespace string
	pids          func() ([]int, error)
	inspect       func(pid int) (processPaths, int, error)
	namespace     func(pid int) (string, error)
	command       func(pid int) string
	kernelThread  func(pid int) bool
}

// ScanOpenFiles records, for every process this user can inspect, the paths
// it references through /proc/<pid>/{cwd,root,exe,fd/*,maps}. This process
// is excluded. A process that exits during the scan is ignored. Paths of a
// process in another mount namespace are namespace-local and cannot be
// mapped; such processes are reported instead (ForeignNamespaceOwn, or
// counted as skipped for other users).
func ScanOpenFiles() (Snapshot, error) {
	selfNamespace, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: reading /proc/self/ns/mnt: %v", ErrHoldersUnsupported, err)
	}
	source := procfsSource{
		self: os.Getpid(), euid: os.Geteuid(), selfNamespace: selfNamespace,
		pids: listPIDs, inspect: inspectProcess, command: commandName, kernelThread: isKernelThread,
		namespace: func(pid int) (string, error) { return os.Readlink("/proc/" + strconv.Itoa(pid) + "/ns/mnt") },
	}
	return source.scan()
}

// pfKThread is PF_KTHREAD in the flags field of /proc/<pid>/stat.
const pfKThread = 0x00200000

// isKernelThread reports kernel threads: they hold no user files and live in
// the initial mount namespace. A process that cannot be classified is not
// treated as a kernel thread.
func isKernelThread(pid int) bool {
	content, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	text := string(content)
	end := strings.LastIndexByte(text, ')')
	if end < 0 {
		return false
	}
	fields := strings.Fields(text[end+1:])
	// state ppid pgrp session tty_nr tpgid flags
	if len(fields) < 7 {
		return false
	}
	flags, err := strconv.ParseUint(fields[6], 10, 64)
	return err == nil && flags&pfKThread != 0
}

func listPIDs() ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("%w: reading /proc: %v", ErrHoldersUnsupported, err)
	}
	var pids []int
	for _, entry := range entries {
		if pid, err := strconv.Atoi(entry.Name()); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

func (s procfsSource) scan() (Snapshot, error) {
	pids, err := s.pids()
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{Method: MethodProcfs, OtherUsersCounted: true}
	uninspectable := func(pid, uid int) {
		if uid == s.euid {
			snapshot.UninspectableOwn = append(snapshot.UninspectableOwn, Process{PID: pid, Command: s.command(pid)})
		} else {
			snapshot.SkippedOtherUsers++
		}
	}
	for _, pid := range pids {
		if pid == s.self || s.kernelThread(pid) {
			continue
		}
		process, uid, err := s.inspect(pid)
		switch {
		case err == nil:
		case gone(err):
			continue
		case denied(err):
			uninspectable(pid, uid)
			continue
		default:
			return Snapshot{}, fmt.Errorf("inspecting process %d: %w", pid, err)
		}
		namespace, err := s.namespace(pid)
		switch {
		case err == nil && namespace == s.selfNamespace:
			snapshot.processes = append(snapshot.processes, process)
		case err == nil && uid == s.euid:
			snapshot.ForeignNamespaceOwn = append(snapshot.ForeignNamespaceOwn, Process{PID: pid, Command: process.command})
		case err == nil:
			snapshot.SkippedOtherUsers++
		case gone(err):
		default:
			// An own process whose namespace is unknown is missing evidence.
			uninspectable(pid, uid)
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
