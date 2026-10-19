//go:build !darwin && !linux

package warm

import "os"

func cloneFile(source, dest string) error { return errCloneUnsupported }

func publish(source, dest string) error { return os.Rename(source, dest) }
