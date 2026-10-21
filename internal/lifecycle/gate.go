// Package lifecycle coordinates worktree users with destructive cleanup.
package lifecycle

import (
	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/lockfile"
)

// Path is stable across worktree and private Git-directory deletion. It lives
// outside both directories so Windows can remove them while the gate is held.
// Gate files must never be removed: waiters must keep using the same inode.
func Path(worktreePath string) (string, error) {
	return config.WorktreeExternalLockPath(worktreePath, "anvil-lifecycle.lock"), nil
}

// AcquireShared fails fast during cleanup. Hold it before startup state reads
// and mutations through the complete operation. Never upgrade it to exclusive.
func AcquireShared(worktreePath string) (*lockfile.Lock, error) {
	path, err := Path(worktreePath)
	if err != nil {
		return nil, err
	}
	return lockfile.TryAcquireShared(path)
}

// TryExclusive excludes startup and existing users through deletion and runtime
// release. Order: lifecycle → prepare → repository database lock → check active
// → sorted resource locks → run lock → .anvil.local state lock.
func TryExclusive(worktreePath string) (*lockfile.Lock, error) {
	path, err := Path(worktreePath)
	if err != nil {
		return nil, err
	}
	return lockfile.TryAcquire(path)
}
