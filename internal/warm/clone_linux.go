package warm

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func cloneFile(source, dest string) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	dst, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.Join(err, src.Close())
	}
	cloneErr := unix.IoctlFileClone(int(dst.Fd()), int(src.Fd()))
	closeErr := errors.Join(src.Close(), dst.Close())
	if cloneErr != nil {
		if err := os.Remove(dest); err != nil {
			return errors.Join(cloneErr, closeErr, err)
		}
	}
	if closeErr != nil {
		return errors.Join(cloneErr, closeErr)
	}
	if errors.Is(cloneErr, unix.EOPNOTSUPP) || errors.Is(cloneErr, unix.EXDEV) || errors.Is(cloneErr, unix.ENOTTY) || errors.Is(cloneErr, unix.ENOSYS) || errors.Is(cloneErr, unix.EINVAL) {
		return errCloneUnsupported
	}
	return cloneErr
}

func publish(source, dest string) error {
	return unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, dest, unix.RENAME_NOREPLACE)
}
