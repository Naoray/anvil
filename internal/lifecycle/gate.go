// Package lifecycle coordinates worktree users with destructive cleanup.
package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/dblifecycle"
	"github.com/naoray/anvil/internal/lockfile"
)

// Path is stable for a worktree across private Git-directory deletion. Gate
// files must never be removed: waiters must keep using the same inode.
func Path(worktreePath string) (string, error) {
	canonical, err := dblifecycle.Canonical(worktreePath)
	if err != nil {
		return "", err
	}
	common, err := dblifecycle.CommonDir(canonical)
	if err != nil {
		return config.WorktreeLockPath(canonical, "anvil-lifecycle.lock"), nil
	}
	sum := sha256.Sum256([]byte(canonical))
	return filepath.Join(common, "anvil-lifecycle", hex.EncodeToString(sum[:])+".lock"), nil
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
