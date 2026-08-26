// Package fs provides a file system abstraction for testing.
// This allows steps that perform file I/O to be unit tested without
// touching the real file system.
package fs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// FS defines the interface for file system operations.
// Implementations can provide real file system access or in-memory
// mocking for testing.
type FS interface {
	// ReadFile reads the entire file at path and returns its contents.
	ReadFile(path string) ([]byte, error)

	// WriteFile writes data to the file at path with the given permissions.
	WriteFile(path string, data []byte, perm os.FileMode) error

	// AtomicWriteFile replaces the file at path with data and the given permissions.
	// Implementations must leave an existing destination unchanged when preparation fails.
	AtomicWriteFile(path string, data []byte, perm os.FileMode) error

	// MkdirAll creates all directories in the path.
	MkdirAll(path string, perm os.FileMode) error

	// Stat returns file info for the given path.
	Stat(path string) (os.FileInfo, error)

	// Exists returns true if the path exists (file or directory).
	Exists(path string) bool

	// Remove removes the file or directory at path.
	Remove(path string) error

	// Rename renames oldpath to newpath.
	Rename(oldpath, newpath string) error

	// Chmod changes the mode of the named file.
	Chmod(path string, mode os.FileMode) error
}

// RealFS implements FS using the actual operating system.
// This is the production implementation.
type RealFS struct{}

// ReadFile reads the entire file at path.
func (r *RealFS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// WriteFile writes data to the file at path with permissions.
func (r *RealFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

// AtomicWriteFile writes data to a temporary file beside path before replacing path.
func (r *RealFS) AtomicWriteFile(path string, data []byte, perm os.FileMode) (retErr error) {
	tmpFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if retErr == nil {
			return
		}
		if cleanupErr := os.Remove(tmpPath); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			retErr = fmt.Errorf("%w; removing temporary file: %v", retErr, cleanupErr)
		}
	}()

	remaining := data
	for len(remaining) > 0 {
		written, writeErr := tmpFile.Write(remaining)
		if writeErr != nil {
			return closeTemporaryFileOnError(tmpFile, fmt.Errorf("writing temporary file: %w", writeErr))
		}
		if written <= 0 || written > len(remaining) {
			return closeTemporaryFileOnError(tmpFile, fmt.Errorf("writing temporary file: %w", io.ErrShortWrite))
		}
		remaining = remaining[written:]
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing temporary file: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("setting temporary file permissions: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming temporary file: %w", err)
	}

	return nil
}

func closeTemporaryFileOnError(file *os.File, operationErr error) error {
	if err := file.Close(); err != nil {
		return fmt.Errorf("%w (closing temporary file: %v)", operationErr, err)
	}
	return operationErr
}

// MkdirAll creates all directories in the path.
func (r *RealFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// Stat returns file info for the given path.
func (r *RealFS) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// Exists returns true if the path exists.
func (r *RealFS) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Remove removes the file or directory at path.
func (r *RealFS) Remove(path string) error {
	return os.Remove(path)
}

// Rename renames oldpath to newpath.
func (r *RealFS) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

// Chmod changes the mode of the named file.
func (r *RealFS) Chmod(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}

// Default is the default RealFS instance for convenience.
var Default = &RealFS{}
