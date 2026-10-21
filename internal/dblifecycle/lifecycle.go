// Package dblifecycle serializes database binding and worktree deletion within a repository.
package dblifecycle

import (
	"errors"
	"fmt"
	"io/fs"
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
	worktrees, err := RegisteredWorktrees(path)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(worktrees))
	for _, worktree := range worktrees {
		paths = append(paths, worktree.Path)
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
// Warnings about skipped siblings are discarded; top-level callers use
// CheckCleanupWithWarnings to report them.
func CheckCleanup(path string) error {
	return CheckCleanupWithWarnings(path, nil)
}

// CheckCleanupWithWarnings is CheckCleanup reporting nonfatal findings to warn
// (nil discards them). An unlocked registered sibling whose location provably
// no longer exists is skipped with a warning: it cannot hold a borrow record
// (an unlocked worktree on an unmounted volume is treated the same way). A
// locked one is expected to return and fails closed, as does any other
// failure to inspect a sibling.
func CheckCleanupWithWarnings(path string, warn func(string)) error {
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

	siblings, err := RegisteredWorktrees(path)
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
	for _, registered := range siblings {
		sibling := registered.Path
		siblingPath, err := Canonical(sibling)
		if err != nil {
			missing, missingErr := PathMissing(sibling)
			if missingErr != nil || !missing {
				return fmt.Errorf("cannot inspect sibling %q: %w", sibling, errors.Join(err, missingErr))
			}
			warning, err := SkipAbsentWorktree(sibling, registered.Locked, registered.LockReason)
			if err != nil {
				return err
			}
			if warn != nil {
				warn(warning)
			}
			continue
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

// SkipAbsentWorktree decides a registered worktree whose path provably no
// longer exists: an unlocked one is skipped with the returned warning, while
// a locked one is expected back (Git never prunes it) and fails closed.
func SkipAbsentWorktree(path string, locked bool, lockReason string) (string, error) {
	if locked {
		reason := ""
		if lockReason != "" {
			reason = " (" + lockReason + ")"
		}
		return "", fmt.Errorf("registered worktree %s is locked%s but absent; reconnect it, or unlock and prune it", path, reason)
	}
	return fmt.Sprintf("registered worktree %s no longer exists; skipping it (run 'git worktree prune' to unregister it, or 'git worktree repair' if it was moved)", path), nil
}

// PathMissing reports whether path provably does not exist: Lstat reports
// ErrNotExist and its nearest existing ancestor is a resolvable directory.
// A dangling symlink at path or at an ancestor (for example an unmounted
// volume) is not proof of absence, nor is any other Lstat error.
func PathMissing(path string) (bool, error) {
	path = filepath.Clean(path)
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		_, err := os.Lstat(dir)
		if errors.Is(err, fs.ErrNotExist) {
			if parent := filepath.Dir(dir); parent != dir {
				continue
			}
		}
		if err != nil {
			return false, err
		}
		info, err := os.Stat(dir)
		if err != nil {
			return false, err
		}
		if !info.IsDir() {
			return false, fmt.Errorf("%s is not a directory", dir)
		}
		return true, nil
	}
}

// RegisteredWorktree is one entry of `git worktree list`.
type RegisteredWorktree struct {
	Path       string
	Locked     bool
	LockReason string
}

// RegisteredWorktrees parses `git worktree list --porcelain -z`, including
// lock state: Git never prunes a locked entry whose directory is absent.
func RegisteredWorktrees(path string) ([]RegisteredWorktree, error) {
	out, err := exec.Command("git", "-C", path, "worktree", "list", "--porcelain", "-z").Output()
	if err != nil {
		return nil, fmt.Errorf("listing registered worktrees: %w", err)
	}
	var worktrees []RegisteredWorktree
	for _, field := range strings.Split(string(out), "\x00") {
		switch {
		case strings.HasPrefix(field, "worktree "):
			worktrees = append(worktrees, RegisteredWorktree{Path: strings.TrimPrefix(field, "worktree ")})
		case len(worktrees) == 0:
		case field == "locked":
			worktrees[len(worktrees)-1].Locked = true
		case strings.HasPrefix(field, "locked "):
			worktrees[len(worktrees)-1].Locked = true
			worktrees[len(worktrees)-1].LockReason = strings.TrimPrefix(field, "locked ")
		}
	}
	return worktrees, nil
}
