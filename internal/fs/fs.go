// Package fs provides a file system abstraction for testing.
// This allows steps that perform file I/O to be unit tested without
// touching the real file system.
package fs

import (
	"os"
)

// FS defines the interface for file system operations.
// Implementations can provide real file system access or in-memory
// mocking for testing.
type FS interface {
	// ReadFile reads the entire file at path and returns its contents.
	ReadFile(path string) ([]byte, error)

	// WriteFile writes data to the file at path with the given permissions.
	WriteFile(path string, data []byte, perm os.FileMode) error

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

	// CreateTemp creates a temporary file in dir with the given pattern.
	// Note: MockFS cannot fully support this as it returns *os.File.
	// Code using CreateTemp should use the useRealFS pattern for testing.
	CreateTemp(dir, pattern string) (*os.File, error)
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

// CreateTemp creates a temporary file in dir with the given pattern.
func (r *RealFS) CreateTemp(dir, pattern string) (*os.File, error) {
	return os.CreateTemp(dir, pattern)
}

// Default is the default RealFS instance for convenience.
var Default = &RealFS{}
