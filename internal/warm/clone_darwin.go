package warm

import (
	"errors"

	"golang.org/x/sys/unix"
)

func cloneFile(source, dest string) error {
	err := unix.Clonefile(source, dest, unix.CLONE_NOFOLLOW)
	if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EXDEV) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
		return errCloneUnsupported
	}
	return err
}

func publish(source, dest string) error { return unix.RenamexNp(source, dest, unix.RENAME_EXCL) }
