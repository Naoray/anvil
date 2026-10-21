// Package resources manages runtime resources owned by worktrees: writable
// directories outside the working tree, stable development ports and
// declared shared resources. Ownership is keyed by the worktree identity and
// verified before anything is released or deleted. This is development
// resource isolation, not a security sandbox.
package resources

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/workenv"
)

// RootEnv overrides the machine-wide runtime root.
const RootEnv = "ANVIL_RUNTIME_ROOT"

// StandardDirs are created for every prepared worktree.
var StandardDirs = []struct{ Name, Env string }{
	{"state", "ANVIL_STATE_DIR"},
	{"tmp", "ANVIL_TMP_DIR"},
	{"cache", "ANVIL_CACHE_DIR"},
	{"logs", "ANVIL_LOG_DIR"},
	{"artifacts", "ANVIL_ARTIFACTS_DIR"},
}

const ownerFile = "owner.json"

// Root returns Anvil's machine-wide runtime directory.
func Root() (string, error) {
	if root := os.Getenv(RootEnv); root != "" {
		return filepath.Abs(root)
	}
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "anvil"), nil
		}
	}
	if state := os.Getenv("XDG_STATE_HOME"); state != "" {
		return filepath.Join(state, "anvil"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "anvil"), nil
}

// Owner identifies the worktree a runtime directory belongs to.
type Owner struct {
	WorktreeID   string    `json:"worktree_id"`
	WorktreePath string    `json:"worktree_path"`
	CreatedAt    time.Time `json:"created_at"`
}

// WorktreeDir returns the runtime directory for a worktree identity. It is
// deterministic so read-only commands can report it without creating it.
func WorktreeDir(root, worktreeID string) (string, error) {
	if !validID(worktreeID) {
		return "", fmt.Errorf("invalid worktree id %q", worktreeID)
	}
	return filepath.Join(root, "worktrees", worktreeID), nil
}

func validID(id string) bool {
	if !strings.HasPrefix(id, "wt_") || len(id) < 4 {
		return false
	}
	for _, r := range id[3:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// Directories describes a worktree's managed directories.
type Directories struct {
	Root  string            `json:"root"`
	Named map[string]string `json:"named"`
	// Exists reports whether the runtime directory has been provisioned.
	Exists bool `json:"exists"`
}

// DescribeDirectories computes directory locations without creating them.
func DescribeDirectories(root, worktreeID string, cfg *config.Config) (Directories, []workenv.Variable, error) {
	dir, err := WorktreeDir(root, worktreeID)
	if err != nil {
		return Directories{}, nil, err
	}
	result := Directories{Root: dir, Named: map[string]string{}}
	if _, err := os.Stat(dir); err == nil {
		result.Exists = true
	}
	vars := []workenv.Variable{{Name: "ANVIL_RUNTIME_DIR", Value: dir, Source: workenv.SourceResource}}
	for _, standard := range StandardDirs {
		path := filepath.Join(dir, standard.Name)
		result.Named[standard.Name] = path
		vars = append(vars, workenv.Variable{Name: standard.Env, Value: path, Source: workenv.SourceResource})
	}
	if cfg != nil {
		for _, name := range cfg.Resources.Directories {
			path := filepath.Join(dir, "dirs", name)
			result.Named[name] = path
			vars = append(vars, workenv.Variable{Name: "ANVIL_DIR_" + config.ResourceEnvSuffix(name), Value: path, Source: workenv.SourceResource})
		}
	}
	return result, vars, nil
}

// ProvisionDirectories creates the managed directories and records the owner.
// It is idempotent and refuses a directory owned by another worktree.
func ProvisionDirectories(root, worktreeID, worktreePath string, cfg *config.Config) (dirs Directories, vars []workenv.Variable, err error) {
	dirs, vars, err = DescribeDirectories(root, worktreeID, cfg)
	if err != nil {
		return dirs, nil, err
	}
	if err := os.MkdirAll(dirs.Root, 0o700); err != nil {
		return dirs, nil, fmt.Errorf("creating runtime directory: %w", err)
	}
	// Concurrent preparations (for example parallel `anvil ensure`) must not
	// read and replace the owner record at the same time: Windows refuses to
	// rename over a file another process has open.
	lock, err := lockfile.Acquire(filepath.Join(root, "locks", "runtime-"+worktreeID+".lock"))
	if err != nil {
		return dirs, nil, fmt.Errorf("locking runtime directory: %w", err)
	}
	defer func() { err = errors.Join(err, lock.Release()) }()
	owner, err := ReadOwner(dirs.Root)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := writeOwner(dirs.Root, Owner{WorktreeID: worktreeID, WorktreePath: worktreePath, CreatedAt: time.Now().UTC().Truncate(time.Second)}); err != nil {
			return dirs, nil, err
		}
	case err != nil:
		return dirs, nil, err
	case owner.WorktreeID != worktreeID:
		return dirs, nil, fmt.Errorf("runtime directory %s belongs to %s", dirs.Root, owner.WorktreeID)
	case owner.WorktreePath != worktreePath:
		owner.WorktreePath = worktreePath
		if err := writeOwner(dirs.Root, owner); err != nil {
			return dirs, nil, err
		}
	}
	for _, path := range dirs.Named {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return dirs, nil, fmt.Errorf("creating %s: %w", path, err)
		}
	}
	dirs.Exists = true
	return dirs, vars, nil
}

// ReadOwner reads a runtime directory's owner record.
func ReadOwner(dir string) (Owner, error) {
	content, err := os.ReadFile(filepath.Join(dir, ownerFile))
	if err != nil {
		return Owner{}, err
	}
	var owner Owner
	if err := json.Unmarshal(content, &owner); err != nil {
		return Owner{}, fmt.Errorf("malformed runtime owner record: %w", err)
	}
	return owner, nil
}

func writeOwner(dir string, owner Owner) error {
	content, err := json.MarshalIndent(owner, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, ownerFile), content)
}

func writeAtomic(path string, content []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".anvil-")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if removeErr := os.Remove(tmp.Name()); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, removeErr)
			}
		}
	}()
	if _, err = tmp.Write(content); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err = tmp.Sync(); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// RemoveDirectories deletes a worktree's runtime directory only when its
// owner record is still exactly expected, the record the caller validated
// against the worktree's verified identity before removing the worktree. A
// moved worktree keeps its ID, so the recorded path may be its old location.
// Shared resources and anything outside the runtime directory are never
// touched.
func RemoveDirectories(root string, expected Owner) (bool, error) {
	dir, err := WorktreeDir(root, expected.WorktreeID)
	if err != nil {
		return false, err
	}
	owner, err := ReadOwner(dir)
	if errors.Is(err, os.ErrNotExist) {
		if _, statErr := os.Stat(dir); errors.Is(statErr, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("runtime directory %s has no owner record; leaving it in place", dir)
	}
	if err != nil {
		return false, err
	}
	if owner.WorktreeID != expected.WorktreeID || owner.WorktreePath != expected.WorktreePath || !owner.CreatedAt.Equal(expected.CreatedAt) {
		return false, fmt.Errorf("runtime directory %s is owned by %s at %s; leaving it in place", dir, owner.WorktreeID, owner.WorktreePath)
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, fmt.Errorf("removing runtime directory: %w", err)
	}
	return true, nil
}
