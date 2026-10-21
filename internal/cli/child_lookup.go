package cli

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// lookPathIn resolves command like exec.LookPath, but against the PATH (and
// PATHEXT on Windows) the child will receive and the directory it will run
// in, so component-local tools such as node_modules/.bin win over global
// ones. Relative PATH entries are ignored, matching exec.LookPath's refusal
// to run programs found through the current directory.
func lookPathIn(command, dir string, env []string) (string, error) {
	if strings.ContainsAny(command, `/\`) || filepath.IsAbs(command) {
		candidate := command
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(dir, candidate)
		}
		return findExecutable(candidate, env)
	}
	var blocked error
	for _, entry := range filepath.SplitList(environValue(env, "PATH")) {
		if entry == "" || !filepath.IsAbs(entry) {
			continue
		}
		path, err := findExecutable(filepath.Join(entry, command), env)
		if err == nil {
			return path, nil
		}
		if errors.Is(err, fs.ErrPermission) && blocked == nil {
			blocked = err
		}
	}
	if blocked != nil {
		return "", blocked
	}
	return "", &exec.Error{Name: command, Err: exec.ErrNotFound}
}

// findExecutable returns candidate (or, on Windows, candidate plus a PATHEXT
// extension) when it is an executable file. A file that exists but cannot be
// executed reports fs.ErrPermission.
func findExecutable(candidate string, env []string) (string, error) {
	for _, path := range executableCandidates(candidate, env) {
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return "", err
		}
		if info.IsDir() {
			continue
		}
		if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			return "", &fs.PathError{Op: "exec", Path: path, Err: fs.ErrPermission}
		}
		return path, nil
	}
	return "", &exec.Error{Name: candidate, Err: exec.ErrNotFound}
}

func executableCandidates(candidate string, env []string) []string {
	if runtime.GOOS != "windows" {
		return []string{candidate}
	}
	pathext := environValue(env, "PATHEXT")
	if pathext == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}
	var exts []string
	for _, ext := range strings.Split(strings.ToLower(pathext), ";") {
		if ext != "" {
			exts = append(exts, ext)
		}
	}
	var out []string
	for _, ext := range exts {
		if strings.EqualFold(filepath.Ext(candidate), ext) {
			out = append(out, candidate)
			break
		}
	}
	for _, ext := range exts {
		out = append(out, candidate+ext)
	}
	return out
}

// environValue returns the last value of key in env (case-insensitively on
// Windows, where environment names are case-insensitive).
func environValue(env []string, key string) string {
	value := ""
	for _, entry := range env {
		name, v, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if name == key || (runtime.GOOS == "windows" && strings.EqualFold(name, key)) {
			value = v
		}
	}
	return value
}

// childLookupError maps a lookPathIn failure to the shell's exit statuses:
// 126 for a found but non-executable file, 127 for not found.
func childLookupError(command string, err error) error {
	if errors.Is(err, fs.ErrPermission) {
		return &ChildExitError{Code: 126, Message: "cannot execute " + command + ": " + err.Error()}
	}
	return &ChildExitError{Code: 127, Message: "command not found: " + command}
}
