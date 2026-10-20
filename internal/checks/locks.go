package checks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/naoray/anvil/internal/lockfile"
)

// LockHolder describes the run holding a resource lock (diagnostic only;
// the operating-system lock itself is authoritative).
type LockHolder struct {
	Lock       string    `json:"lock"`
	PID        int       `json:"pid"`
	WorktreeID string    `json:"worktree_id"`
	Worktree   string    `json:"worktree"`
	Check      string    `json:"check"`
	RunID      string    `json:"run_id"`
	Since      time.Time `json:"since"`
}

// BusyError reports a resource lock held by another live process.
type BusyError struct {
	Lock   string
	Holder *LockHolder
}

func (e *BusyError) Error() string {
	if e.Holder != nil {
		return fmt.Sprintf("resource lock %q is held by check %q in %s (pid %d) since %s",
			e.Lock, e.Holder.Check, e.Holder.Worktree, e.Holder.PID, e.Holder.Since.Format(time.RFC3339))
	}
	return fmt.Sprintf("resource lock %q is held by another process", e.Lock)
}

func lockPaths(root, name string) (string, string) {
	dir := filepath.Join(root, "locks")
	return filepath.Join(dir, name+".lock"), filepath.Join(dir, name+".holder.json")
}

// ReadHolder returns the recorded holder of a lock if the lock is held.
func ReadHolder(root, name string) (*LockHolder, error) {
	lockPath, holderPath := lockPaths(root, name)
	held, err := lockfile.IsHeld(lockPath)
	if err != nil || !held {
		return nil, err
	}
	content, err := os.ReadFile(holderPath)
	if err != nil {
		return nil, nil
	}
	var holder LockHolder
	if err := json.Unmarshal(content, &holder); err != nil {
		return nil, nil
	}
	return &holder, nil
}

// heldLocks are acquired resource locks.
type heldLocks []*lockfile.Lock

func (h heldLocks) release() error {
	var problems []error
	for i := len(h) - 1; i >= 0; i-- {
		problems = append(problems, h[i].Release())
	}
	return errors.Join(problems...)
}

// acquireResourceLocks takes every named lock in sorted order (avoiding
// lock-order deadlocks between checks). A held lock is waited for up to
// wait; locks are only ever released by their holder or by the operating
// system when the holder exits — never because time passed.
func acquireResourceLocks(root string, names []string, wait time.Duration, holder LockHolder) (heldLocks, error) {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	var held heldLocks
	for i, name := range sorted {
		if i > 0 && name == sorted[i-1] {
			continue
		}
		lockPath, holderPath := lockPaths(root, name)
		lock, err := lockfile.AcquireTimeout(lockPath, wait)
		if errors.Is(err, lockfile.ErrLocked) {
			current, holderErr := ReadHolder(root, name)
			return nil, errors.Join(&BusyError{Lock: name, Holder: current}, holderErr, held.release())
		}
		if err != nil {
			return nil, errors.Join(err, held.release())
		}
		held = append(held, lock)
		record := holder
		record.Lock = name
		if err := writeJSON(holderPath, record); err != nil {
			return nil, errors.Join(err, held.release())
		}
	}
	return held, nil
}
