// Package lockfile provides exclusive advisory file locks shared by Anvil
// processes. Locks are bound to an open file description, so the operating
// system releases them when the holder exits for any reason. Recovery never
// depends on elapsed time or on deleting another holder's lock file.
package lockfile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrLocked reports that another holder owns the lock.
var ErrLocked = errors.New("lock is held by another process")

// pollInterval bounds how often a timed acquisition retries.
const pollInterval = 50 * time.Millisecond

// Lock is an acquired exclusive lock. A Lock has one owner; Release must be
// called exactly once.
type Lock struct {
	file *os.File
	path string
}

// Path returns the lock file path.
func (l *Lock) Path() string { return l.path }

// File exposes the acquired handle only for exec inheritance. Callers must not
// close or unlock it; Release remains the owner's responsibility.
func (l *Lock) File() *os.File { return l.file }

// Acquire blocks until the exclusive lock at path is held.
func Acquire(path string) (*Lock, error) {
	file, err := open(path)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file, true, false); err != nil {
		return nil, errors.Join(fmt.Errorf("locking %s: %w", path, err), file.Close())
	}
	return &Lock{file: file, path: path}, nil
}

// TryAcquire attempts the lock once. It returns ErrLocked when another holder
// owns it.
func TryAcquire(path string) (*Lock, error) { return tryAcquire(path, false) }

// TryAcquireShared allows concurrent readers and excludes an exclusive owner.
func TryAcquireShared(path string) (*Lock, error) { return tryAcquire(path, true) }

func tryAcquire(path string, shared bool) (*Lock, error) {
	file, err := open(path)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file, false, shared); err != nil {
		closeErr := file.Close()
		if isContended(err) {
			return nil, errors.Join(ErrLocked, closeErr)
		}
		return nil, errors.Join(fmt.Errorf("locking %s: %w", path, err), closeErr)
	}
	return &Lock{file: file, path: path}, nil
}

// AcquireTimeout retries TryAcquire until timeout elapses. A zero timeout
// tries exactly once; a negative timeout blocks like Acquire.
func AcquireTimeout(path string, timeout time.Duration) (*Lock, error) {
	return AcquireContext(context.Background(), path, timeout)
}

// AcquireContext retries without hiding cancellation. Negative timeouts wait
// indefinitely, zero tries once; cancellation never starts a later acquisition.
func AcquireContext(ctx context.Context, path string, timeout time.Duration) (*Lock, error) {
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lock, err := TryAcquire(path)
		if err == nil {
			if err := ctx.Err(); err != nil {
				return nil, errors.Join(err, lock.Release())
			}
			return lock, nil
		}
		if !errors.Is(err, ErrLocked) {
			return nil, err
		}
		if timeout >= 0 && !time.Now().Before(deadline) {
			return nil, err
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// IsHeld reports whether another holder currently owns the lock. It acquires
// and immediately releases the lock when free, so it never blocks.
func IsHeld(path string) (bool, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	lock, err := TryAcquire(path)
	if errors.Is(err, ErrLocked) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, lock.Release()
}

// Release unlocks and closes the lock file. The file itself is retained so
// concurrent waiters keep contending on the same inode.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := errors.Join(unlockFile(l.file), l.file.Close())
	l.file = nil
	return err
}

func open(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening lock %s: %w", path, err)
	}
	return file, nil
}
