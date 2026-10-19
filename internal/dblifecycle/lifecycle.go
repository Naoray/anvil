// Package dblifecycle serializes database binding and worktree deletion within a repository.
package dblifecycle

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/naoray/anvil/internal/config"
)

func Canonical(path string) (string, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

func CommonDir(path string) (string, error) {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", fmt.Errorf("resolving common Git directory: %w", err)
	}
	return Canonical(strings.TrimSpace(string(out)))
}

func RegisteredPaths(path string) ([]string, error) {
	out, err := exec.Command("git", "-C", path, "worktree", "list", "--porcelain", "-z").Output()
	if err != nil {
		return nil, fmt.Errorf("listing registered worktrees: %w", err)
	}
	var paths []string
	for _, field := range strings.Split(string(out), "\x00") {
		if strings.HasPrefix(field, "worktree ") {
			paths = append(paths, strings.TrimPrefix(field, "worktree "))
		}
	}
	return paths, nil
}

// Lock is held by one top-level caller. Nested scaffold/cleanup calls receive the held flag.
func Lock(path string) (release func() error, err error) {
	dir, err := CommonDir(path)
	if err != nil {
		// Standalone db steps without a Git registry cannot have registered borrowers.
		if _, statErr := os.Stat(filepath.Join(path, ".git")); !os.IsNotExist(statErr) {
			return nil, err
		}
		dir, err = Canonical(path)
		if err != nil {
			return nil, err
		}
	}
	file, err := os.OpenFile(filepath.Join(dir, "anvil-database-lifecycle.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(file); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return func() error { return errors.Join(unlockFile(file), file.Close()) }, nil
}

// CheckCleanup must run under Lock before ANY side effect, and again only under the same holder.
func CheckCleanup(path string) error {
	state, err := config.ReadLocalState(path)
	if err != nil {
		return err
	}
	if err = config.ValidateOwnedDatabases(state.Databases); err != nil {
		return err
	}
	for _, db := range state.Databases {
		if db.Endpoint == nil {
			return fmt.Errorf("database %q has unknown legacy endpoint identity; use --keep-db", db.Name)
		}
	}

	paths, err := RegisteredPaths(path)
	if err != nil {
		if _, statErr := os.Stat(filepath.Join(path, ".git")); os.IsNotExist(statErr) {
			return nil
		}
		return err
	}
	ownPath, err := Canonical(path)
	if err != nil {
		return err
	}
	for _, sibling := range paths {
		siblingPath, err := Canonical(sibling)
		if err != nil {
			return fmt.Errorf("cannot inspect sibling %q: %w", sibling, err)
		}
		if siblingPath == ownPath {
			continue
		}
		other, err := config.ReadLocalState(siblingPath)
		if err != nil {
			return fmt.Errorf("cannot inspect sibling %q: %w", siblingPath, err)
		}
		if err = config.ValidateOwnedDatabases(other.Databases); err != nil {
			return fmt.Errorf("invalid sibling %q: %w", siblingPath, err)
		}
		if other.BorrowedApplication == nil {
			continue
		}
		if err = config.ValidateBorrowedApplication(other.BorrowedApplication); err != nil {
			return fmt.Errorf("invalid sibling %q: %w", siblingPath, err)
		}
		borrowed := other.BorrowedApplication
		for _, owned := range state.Databases {
			if borrowed.SourcePath == ownPath || borrowed.Name == owned.Name || strings.HasPrefix(borrowed.Name, owned.Name+"_test_") {
				return fmt.Errorf("database %q is referenced by borrower %q; remove borrower first or use --keep-db", borrowed.Name, siblingPath)
			}
		}
		if borrowed.SourcePath == ownPath {
			return fmt.Errorf("borrower %q references this worktree; use --keep-db", siblingPath)
		}
	}
	return nil
}
