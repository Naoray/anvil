package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/naoray/anvil/internal/lockfile"
)

// LocalStateLockFile is the lock that serializes every read-modify-write of
// .anvil.local. It lives in the worktree's private Git directory so the
// working tree is never polluted.
const LocalStateLockFile = "anvil-local-state.lock"

// WorktreeGitDir returns the worktree's private Git directory: .git itself
// for the main worktree, or the gitdir referenced by a linked worktree's .git
// file. It returns an error when the path is not a Git working tree.
func WorktreeGitDir(worktreePath string) (string, error) {
	gitPath := filepath.Join(worktreePath, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return gitPath, nil
	}
	content, err := os.ReadFile(gitPath)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(content))
	if !strings.HasPrefix(line, "gitdir: ") {
		return "", fmt.Errorf("malformed .git file in %s", worktreePath)
	}
	dir := strings.TrimPrefix(line, "gitdir: ")
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(worktreePath, dir)
	}
	return filepath.Clean(dir), nil
}

// WorktreeLockPath returns a lock path private to one worktree. Non-Git
// directories fall back to a per-path lock in a per-user directory under the
// system temp directory, so users sharing /tmp never block each other.
func WorktreeLockPath(worktreePath, name string) string {
	if dir, err := WorktreeGitDir(worktreePath); err == nil {
		return filepath.Join(dir, name)
	}
	return WorktreeExternalLockPath(worktreePath, name)
}

// WorktreeExternalLockPath returns a per-path lock outside the worktree and
// its Git directory, in a per-user directory under the system temp
// directory. Locks held while the worktree is removed must live here:
// Windows cannot delete a directory containing an open file.
func WorktreeExternalLockPath(worktreePath, name string) string {
	canonical := worktreePath
	if abs, err := filepath.Abs(worktreePath); err == nil {
		canonical = abs
	}
	if eval, err := filepath.EvalSymlinks(canonical); err == nil {
		canonical = eval
	}
	sum := sha256.Sum256([]byte(canonical))
	return filepath.Join(os.TempDir(), fmt.Sprintf("anvil-locks-%d", os.Getuid()), hex.EncodeToString(sum[:8])+"-"+name)
}

// ReadLocalStateMap returns the raw .anvil.local document. A missing file is
// an empty document.
func ReadLocalStateMap(worktreePath string) (map[string]any, error) {
	path := filepath.Join(worktreePath, LocalStateFile)
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading local state: %w", err)
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("parsing local state: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

// UpdateLocalState performs one atomic, cross-process read-modify-write of
// .anvil.local. fn receives the raw document and may mutate it; unknown fields
// are preserved because the whole document round-trips. Returning an error
// from fn leaves the file untouched. fn must not call UpdateLocalState.
func UpdateLocalState(worktreePath string, fn func(doc map[string]any) error) (err error) {
	lock, err := lockfile.Acquire(WorktreeLockPath(worktreePath, LocalStateLockFile))
	if err != nil {
		return fmt.Errorf("locking local state: %w", err)
	}
	defer func() { err = errors.Join(err, lock.Release()) }()

	path := filepath.Join(worktreePath, LocalStateFile)
	mode := os.FileMode(0o644)
	if info, statErr := os.Lstat(path); statErr == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("local state %s must be a regular file", path)
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("stating local state: %w", statErr)
	}

	doc, err := ReadLocalStateMap(worktreePath)
	if err != nil {
		return err
	}
	if err := fn(doc); err != nil {
		return err
	}
	content, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshaling local state: %w", err)
	}
	return writeFileAtomic(path, content, mode)
}

// writeFileAtomic writes content through a synced temporary file and rename,
// so readers observe either the previous or the new document, never a torn one.
func writeFileAtomic(path string, content []byte, mode os.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".anvil-local-")
	if err != nil {
		return fmt.Errorf("creating temporary local state: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			if removeErr := os.Remove(tmpPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, removeErr)
			}
		}
	}()
	if _, err = tmp.Write(content); err != nil {
		return errors.Join(fmt.Errorf("writing local state: %w", err), tmp.Close())
	}
	if err = tmp.Sync(); err != nil {
		return errors.Join(fmt.Errorf("syncing local state: %w", err), tmp.Close())
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("closing local state: %w", err)
	}
	if err = os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("setting local state mode: %w", err)
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replacing local state: %w", err)
	}
	return nil
}

// DecodeSection converts a raw document section into a typed value through
// YAML, so typed sections share the same field names as the file.
func DecodeSection(raw any, out any) error {
	if raw == nil {
		return nil
	}
	content, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(content, out)
}

// EncodeSection converts a typed value into a generic YAML value suitable for
// storing in a raw document.
func EncodeSection(value any) (any, error) {
	content, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out any
	if err := yaml.Unmarshal(content, &out); err != nil {
		return nil, err
	}
	return out, nil
}
