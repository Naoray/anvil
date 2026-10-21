//go:build windows

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runChild runs the child command with wired stdio and propagates its exit
// code as a typed error. Windows has no execve; anvil stays the parent
// process, so signal forwarding and job-object management are out of scope
// for v1.8 (documented limitation).
func runChild(argv []string, env []string) error {
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return childLookPathError(argv[0], err)
	}
	return startChildIn("", path, argv, env)
}

// runChildIn runs the child in dir ("" keeps the current directory).
func runChildIn(dir string, argv []string, env []string) error {
	path, err := lookPathIn(dir, argv[0], env)
	if err != nil {
		return err
	}
	return startChildIn(dir, path, argv, env)
}

func startChildIn(dir, path string, argv []string, env []string) error {
	cmd := exec.Command(path, argv[1:]...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return &ChildExitError{Code: exitErr.ExitCode()}
		}
		return &ChildExitError{Code: 126, Message: fmt.Sprintf("executing %s: %v", path, err)}
	}
	return nil
}

func lookPathIn(dir, name string, env []string) (string, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	extensions := []string{""}
	if filepath.Ext(name) == "" {
		extensions = nil
		pathExt := childEnvValue(env, "PATHEXT", true)
		if pathExt == "" {
			pathExt = ".COM;.EXE;.BAT;.CMD"
		}
		for _, ext := range strings.Split(pathExt, ";") {
			if ext != "" {
				extensions = append(extensions, strings.ToLower(ext))
			}
		}
	}
	dirs := []string{dir}
	if strings.ContainsAny(name, `/\:`) {
		dirs = []string{""}
	} else {
		for _, entry := range filepath.SplitList(childEnvValue(env, "PATH", true)) {
			dirs = append(dirs, entry)
		}
	}
	blocked := false
	for _, entry := range dirs {
		candidate := filepath.Join(entry, name)
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(dir, candidate)
		}
		for _, ext := range extensions {
			path := candidate + ext
			info, err := os.Stat(path)
			if err == nil && !info.IsDir() {
				return path, nil
			}
			blocked = blocked || pathExistsOrIsBlocked(path)
		}
	}
	code := 127
	if blocked {
		code = 126
	}
	return "", &ChildExitError{Code: code, Message: fmt.Sprintf("cannot resolve command: %s", name)}
}
