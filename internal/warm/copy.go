package warm

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

var errCloneUnsupported = errors.New("filesystem clone unsupported")
var errSourceChanged = errors.New("dependency source changed during copy")

func copyFile(source, dest string, mode os.FileMode, copyMode string) (string, error) {
	return copyWithClone(source, dest, mode, copyMode, cloneFile)
}
func copyWithClone(source, dest string, mode os.FileMode, copyMode string, clone func(string, string) error) (string, error) {
	if copyMode == "auto" {
		err := clone(source, dest)
		if err == nil {
			info, err := os.Lstat(dest)
			if err != nil {
				return "", err
			}
			if !info.Mode().IsRegular() {
				return "", errSourceChanged
			}
			return "cloned", os.Chmod(dest, mode.Perm())
		}
		if !errors.Is(err, errCloneUnsupported) {
			return "", err
		}
	}
	src, err := os.Open(source)
	if err != nil {
		return "", err
	}
	dst, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", errors.Join(err, src.Close())
	}
	_, copyErr := io.Copy(dst, src)
	err = errors.Join(copyErr, src.Close(), dst.Close())
	if err != nil {
		return "", err
	}
	return "copied", os.Chmod(dest, mode.Perm())
}
func copyTree(source, dest, mode string, copyOne fileCopier) (string, error) {
	result := "copied"
	cloned, ordinary := false, false
	var directories []struct {
		path string
		mode os.FileMode
	}
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dest, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().Type() != entry.Type() {
			return errSourceChanged
		}
		switch {
		case info.IsDir():
			if err := os.Mkdir(out, 0700); err != nil {
				return err
			}
			directories = append(directories, struct {
				path string
				mode os.FileMode
			}{out, info.Mode()})
			return nil
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, out)
		case info.Mode().IsRegular():
			r, err := copyOne(path, out, info.Mode(), mode)
			if r == "cloned" {
				cloned = true
			} else {
				ordinary = true
			}
			return err
		default:
			return fmt.Errorf("special dependency file: %s", rel)
		}
	})
	if err != nil {
		return "", err
	}
	for n := len(directories) - 1; n >= 0; n-- {
		d := directories[n]
		if err := os.Chmod(d.path, d.mode.Perm()); err != nil {
			return "", err
		}
	}
	if cloned && !ordinary {
		result = "cloned"
	}
	return result, nil
}

// Stages may already carry read-only source modes when verification fails.
// Restore permissions only inside the private stage before discarding it.
func removeStage(stage string) error {
	walkErr := filepath.WalkDir(stage, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.Chmod(path, 0700)
		}
		return nil
	})
	return errors.Join(walkErr, os.RemoveAll(stage))
}
