package procs

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Open-file scanning methods.
const (
	// MethodProcfs inspects /proc/<pid>/{cwd,root,exe,fd/*,maps} (Linux).
	MethodProcfs = "procfs"
	// MethodLsof parses `lsof -Fpcn` (macOS).
	MethodLsof = "lsof"
)

// ErrHoldersUnsupported means open files cannot be inspected here; callers
// must treat it as missing evidence, never as "nothing is open".
var ErrHoldersUnsupported = errors.New("inspecting open files is not supported on this platform")

// Holder is a process referencing a path beneath a directory: its working
// directory, root, executable, an open file descriptor or a memory mapping.
type Holder struct {
	PID     int      `json:"pid"`
	Command string   `json:"command,omitempty"`
	Via     []string `json:"via"`
	Path    string   `json:"path"`
}

// Process names a process that could not be inspected.
type Process struct {
	PID     int    `json:"pid"`
	Command string `json:"command,omitempty"`
}

type processPaths struct {
	pid     int
	command string
	// paths maps each referenced absolute path to how it is referenced.
	paths map[string][]string
}

// Snapshot is the set of paths processes referenced at one moment.
type Snapshot struct {
	Method string
	// SkippedOtherUsers counts other users' processes that could not be
	// inspected (permission denied). They are not visible to this scan.
	SkippedOtherUsers int
	// OtherUsersCounted is false where other users' processes are invisible
	// without being counted (lsof); SkippedOtherUsers is then unknown.
	OtherUsersCounted bool
	// UninspectableOwn lists this user's processes that could not be
	// inspected (for example non-dumpable agents); their open files are
	// unknown.
	UninspectableOwn []Process
	// ForeignNamespaceOwn lists this user's processes in another mount
	// namespace (containers, sandboxed apps): their paths cannot be mapped to
	// this filesystem view, so what they hold open is unknown.
	ForeignNamespaceOwn []Process
	processes           []processPaths
}

// Holders returns the processes referencing dir or anything beneath it,
// sorted by PID. dir must be canonical (symlinks resolved).
func (s Snapshot) Holders(dir string) []Holder {
	dir = filepath.Clean(dir)
	holders := []Holder{}
	for _, process := range s.processes {
		var matched []string
		via := map[string]bool{}
		for path, kinds := range process.paths {
			if path == dir || strings.HasPrefix(path, dir+string(filepath.Separator)) {
				matched = append(matched, path)
				for _, kind := range kinds {
					via[kind] = true
				}
			}
		}
		if len(matched) == 0 {
			continue
		}
		sort.Strings(matched)
		kinds := make([]string, 0, len(via))
		for kind := range via {
			kinds = append(kinds, kind)
		}
		sort.Strings(kinds)
		holders = append(holders, Holder{PID: process.pid, Command: process.command, Via: kinds, Path: matched[0]})
	}
	sort.Slice(holders, func(i, j int) bool { return holders[i].PID < holders[j].PID })
	return holders
}

func (p *processPaths) add(path, kind string) {
	path = strings.TrimSuffix(path, " (deleted)")
	if !filepath.IsAbs(path) {
		return // sockets, pipes, anon inodes
	}
	path = filepath.Clean(path)
	for _, existing := range p.paths[path] {
		if existing == kind {
			return
		}
	}
	p.paths[path] = append(p.paths[path], kind)
}

// parseLsof parses `lsof -Fpcn` output (p<pid>, c<command>, n<name> lines).
// Malformed output fails closed.
func parseLsof(output []byte, self int) (Snapshot, error) {
	snapshot := Snapshot{Method: MethodLsof}
	var current *processPaths
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		value := line[1:]
		switch line[0] {
		case 'p':
			pid, err := strconv.Atoi(value)
			if err != nil {
				return Snapshot{}, fmt.Errorf("malformed lsof output %q: %w", line, err)
			}
			snapshot.processes = append(snapshot.processes, processPaths{pid: pid, paths: map[string][]string{}})
			current = &snapshot.processes[len(snapshot.processes)-1]
		case 'c':
			if current != nil {
				current.command = value
			}
		case 'n':
			if current == nil {
				return Snapshot{}, fmt.Errorf("malformed lsof output: file before process")
			}
			current.add(value, "lsof")
		}
	}
	if err := scanner.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("reading lsof output: %w", err)
	}
	kept := snapshot.processes[:0]
	for _, process := range snapshot.processes {
		if process.pid != self {
			kept = append(kept, process)
		}
	}
	snapshot.processes = kept
	return snapshot, nil
}
