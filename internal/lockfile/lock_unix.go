//go:build !windows

package lockfile

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockFile(f *os.File, block, exclusive bool) error {
	how := unix.LOCK_EX
	if !exclusive {
		how = unix.LOCK_SH
	}
	if !block {
		how |= unix.LOCK_NB
	}
	for {
		err := unix.Flock(int(f.Fd()), how)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

func unlockFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }

func isContended(err error) bool { return errors.Is(err, unix.EWOULDBLOCK) }
