package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveContainedPath resolves existing symlinks and permits missing suffixes,
// while requiring the result to remain inside the canonical worktree root.
func ResolveContainedPath(worktreePath, path string) (string, error) {
	root, err := filepath.EvalSymlinks(worktreePath)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := resolveMissingPath(absPath)
	if err != nil {
		return "", fmt.Errorf("resolving path %q: %w", path, err)
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q leaves the worktree", path)
	}
	return resolved, nil
}

func resolveMissingPath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	// An unresolved existing link is not a missing output directory.
	if _, statErr := os.Lstat(path); statErr == nil {
		return "", err
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	resolved, err = resolveMissingPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(path)), nil
}

// ValidateContainedPattern checks every existing glob prefix, including links
// whose remaining suffix has no matches yet.
func ValidateContainedPattern(worktreePath, root, pattern string) error {
	if err := ValidateRelativePath(pattern); err != nil {
		return err
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return err
	}
	if _, err := ResolveContainedPath(worktreePath, filepath.Join(root, pattern)); err != nil {
		return err
	}
	prefix := root
	for _, part := range strings.Split(filepath.Clean(pattern), string(filepath.Separator)) {
		prefix = filepath.Join(prefix, part)
		matches, err := filepath.Glob(prefix)
		if err != nil {
			return err
		}
		for _, match := range matches {
			if _, err := ResolveContainedPath(worktreePath, match); err != nil {
				return err
			}
		}
	}
	return nil
}
