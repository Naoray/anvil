package fs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MockFileInfo implements os.FileInfo for mock files.
type MockFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (m *MockFileInfo) Name() string       { return m.name }
func (m *MockFileInfo) Size() int64        { return m.size }
func (m *MockFileInfo) Mode() os.FileMode  { return m.mode }
func (m *MockFileInfo) ModTime() time.Time { return m.modTime }
func (m *MockFileInfo) IsDir() bool        { return m.isDir }
func (m *MockFileInfo) Sys() interface{}   { return nil }

// MockFS implements FS using an in-memory file system for testing.
type MockFS struct {
	mu     sync.RWMutex
	files  map[string][]byte
	perms  map[string]os.FileMode
	dirs   map[string]bool
	tempID int
}

// NewMockFS creates a new MockFS with empty storage.
func NewMockFS() *MockFS {
	return &MockFS{
		files: make(map[string][]byte),
		perms: make(map[string]os.FileMode),
		dirs:  make(map[string]bool),
	}
}

// ReadFile reads the file at path from memory.
func (m *MockFS) ReadFile(path string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cleanPath := filepath.Clean(path)
	data, ok := m.files[cleanPath]
	if !ok {
		return nil, &os.PathError{Op: "read", Path: path, Err: errors.New("file not found")}
	}
	// Return a copy to prevent external modification
	result := make([]byte, len(data))
	copy(result, data)
	return result, nil
}

// WriteFile writes data to the file at path in memory.
func (m *MockFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanPath := filepath.Clean(path)

	// Ensure parent directory exists
	dir := filepath.Dir(cleanPath)
	if dir != "." && dir != "/" {
		if !m.dirs[dir] {
			// Auto-create parent directories for convenience
			m.dirs[dir] = true
		}
	}

	// Store a copy of the data
	m.files[cleanPath] = make([]byte, len(data))
	copy(m.files[cleanPath], data)
	m.perms[cleanPath] = perm

	return nil
}

// MkdirAll creates all directories in the path.
func (m *MockFS) MkdirAll(path string, perm os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanPath := filepath.Clean(path)

	// Handle absolute paths by storing them as-is
	if filepath.IsAbs(cleanPath) {
		m.dirs[cleanPath] = true
		// Also store all parent directories
		dir := cleanPath
		for dir != "/" && dir != string(filepath.Separator) {
			dir = filepath.Dir(dir)
			if dir != "/" && dir != "." {
				m.dirs[dir] = true
			}
		}
		m.dirs["/"] = true
	} else {
		// Relative path - build up component by component
		parts := strings.Split(cleanPath, string(filepath.Separator))
		current := ""
		for _, part := range parts {
			if part == "" || part == "." {
				continue
			}
			if current == "" {
				current = part
			} else {
				current = filepath.Join(current, part)
			}
			m.dirs[current] = true
		}
	}

	return nil
}

// Stat returns file info for the given path.
func (m *MockFS) Stat(path string) (os.FileInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cleanPath := filepath.Clean(path)

	// Check if it's a file
	if data, ok := m.files[cleanPath]; ok {
		perm := m.perms[cleanPath]
		if perm == 0 {
			perm = 0644
		}
		return &MockFileInfo{
			name:    filepath.Base(cleanPath),
			size:    int64(len(data)),
			mode:    perm,
			modTime: time.Now(),
			isDir:   false,
		}, nil
	}

	// Check if it's a directory
	if m.dirs[cleanPath] || cleanPath == "." {
		return &MockFileInfo{
			name:    filepath.Base(cleanPath),
			size:    0,
			mode:    0755 | os.ModeDir,
			modTime: time.Now(),
			isDir:   true,
		}, nil
	}

	return nil, &os.PathError{Op: "stat", Path: path, Err: errors.New("file not found")}
}

// Exists returns true if the path exists (file or directory).
func (m *MockFS) Exists(path string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cleanPath := filepath.Clean(path)
	if _, ok := m.files[cleanPath]; ok {
		return true
	}
	if m.dirs[cleanPath] || cleanPath == "." {
		return true
	}
	return false
}

// Remove removes the file or directory at path.
func (m *MockFS) Remove(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanPath := filepath.Clean(path)
	delete(m.files, cleanPath)
	delete(m.perms, cleanPath)
	delete(m.dirs, cleanPath)
	return nil
}

// Rename renames oldpath to newpath.
func (m *MockFS) Rename(oldpath, newpath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanOld := filepath.Clean(oldpath)
	cleanNew := filepath.Clean(newpath)

	// Check if source exists
	data, ok := m.files[cleanOld]
	if !ok {
		return &os.PathError{Op: "rename", Path: oldpath, Err: errors.New("file not found")}
	}

	// Move the file
	perm := m.perms[cleanOld]
	m.files[cleanNew] = data
	m.perms[cleanNew] = perm
	delete(m.files, cleanOld)
	delete(m.perms, cleanOld)

	return nil
}

// Chmod changes the mode of the named file.
func (m *MockFS) Chmod(path string, mode os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanPath := filepath.Clean(path)
	if _, ok := m.files[cleanPath]; ok {
		m.perms[cleanPath] = mode
		return nil
	}
	return &os.PathError{Op: "chmod", Path: path, Err: errors.New("file not found")}
}

// CreateTemp creates a temporary file in memory.
func (m *MockFS) CreateTemp(dir, pattern string) (*os.File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.tempID++
	base := strings.ReplaceAll(pattern, "*", "")
	tempPath := filepath.Join(dir, base+".tmp."+string(rune('0'+m.tempID%10)))

	// Create empty file
	m.files[tempPath] = []byte{}
	m.perms[tempPath] = 0600

	// We can't easily return a real *os.File for in-memory storage,
	// so this is a limitation of MockFS. For testing, steps that use
	// CreateTemp will need special handling or the step should be
	// refactored to work better with the FS interface.
	// For now, return an error to indicate this isn't fully supported.
	return nil, errors.New("MockFS.CreateTemp not fully supported - use real FS or refactor step")
}

// AddFile adds a file with content to the mock FS for testing.
func (m *MockFS) AddFile(path string, content []byte, perm os.FileMode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cleanPath := filepath.Clean(path)
	m.files[cleanPath] = make([]byte, len(content))
	copy(m.files[cleanPath], content)
	m.perms[cleanPath] = perm
}

// AddDir adds a directory to the mock FS for testing.
func (m *MockFS) AddDir(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cleanPath := filepath.Clean(path)
	m.dirs[cleanPath] = true
}

// FileExists checks if a file exists in the mock FS.
func (m *MockFS) FileExists(path string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cleanPath := filepath.Clean(path)
	_, ok := m.files[cleanPath]
	return ok
}

// DirExists checks if a directory exists in the mock FS.
func (m *MockFS) DirExists(path string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cleanPath := filepath.Clean(path)
	return m.dirs[cleanPath]
}

// Reset clears all files and directories from the mock FS.
func (m *MockFS) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files = make(map[string][]byte)
	m.perms = make(map[string]os.FileMode)
	m.dirs = make(map[string]bool)
	m.tempID = 0
}
