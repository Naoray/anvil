package fs

import (
	"testing"
)

func TestMockFS_ReadFile(t *testing.T) {
	m := NewMockFS()
	m.AddFile("/test/file.txt", []byte("hello world"), 0644)

	data, err := m.ReadFile("/test/file.txt")
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("expected 'hello world', got: %s", string(data))
	}
}

func TestMockFS_ReadFile_NotFound(t *testing.T) {
	m := NewMockFS()

	_, err := m.ReadFile("/nonexistent/file.txt")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestMockFS_WriteFile(t *testing.T) {
	m := NewMockFS()

	err := m.WriteFile("/test/output.txt", []byte("test data"), 0644)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	data, err := m.ReadFile("/test/output.txt")
	if err != nil {
		t.Errorf("expected to read written file, got error: %v", err)
	}
	if string(data) != "test data" {
		t.Errorf("expected 'test data', got: %s", string(data))
	}
}

func TestMockFS_MkdirAll(t *testing.T) {
	m := NewMockFS()

	err := m.MkdirAll("/a/b/c/d", 0755)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !m.DirExists("/a") {
		t.Error("expected /a to exist")
	}
	if !m.DirExists("/a/b") {
		t.Error("expected /a/b to exist")
	}
	if !m.DirExists("/a/b/c") {
		t.Error("expected /a/b/c to exist")
	}
	if !m.DirExists("/a/b/c/d") {
		t.Error("expected /a/b/c/d to exist")
	}
}

func TestMockFS_Stat_File(t *testing.T) {
	m := NewMockFS()
	m.AddFile("/test.txt", []byte("content"), 0644)

	info, err := m.Stat("/test.txt")
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if info.IsDir() {
		t.Error("expected file, got directory")
	}
	if info.Name() != "test.txt" {
		t.Errorf("expected name 'test.txt', got: %s", info.Name())
	}
	if info.Size() != 7 {
		t.Errorf("expected size 7, got: %d", info.Size())
	}
}

func TestMockFS_Stat_Directory(t *testing.T) {
	m := NewMockFS()
	m.AddDir("/mydir")

	info, err := m.Stat("/mydir")
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory, got file")
	}
}

func TestMockFS_Stat_NotFound(t *testing.T) {
	m := NewMockFS()

	_, err := m.Stat("/nonexistent")
	if err == nil {
		t.Error("expected error for non-existent path, got nil")
	}
}

func TestMockFS_Remove(t *testing.T) {
	m := NewMockFS()
	m.AddFile("/delete.me", []byte("data"), 0644)

	err := m.Remove("/delete.me")
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if m.FileExists("/delete.me") {
		t.Error("expected file to be removed")
	}
}

func TestMockFS_Rename(t *testing.T) {
	m := NewMockFS()
	m.AddFile("/old.txt", []byte("content"), 0644)

	err := m.Rename("/old.txt", "/new.txt")
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if m.FileExists("/old.txt") {
		t.Error("expected old file to not exist")
	}
	if !m.FileExists("/new.txt") {
		t.Error("expected new file to exist")
	}

	data, _ := m.ReadFile("/new.txt")
	if string(data) != "content" {
		t.Errorf("expected content 'content', got: %s", string(data))
	}
}

func TestMockFS_Rename_NotFound(t *testing.T) {
	m := NewMockFS()

	err := m.Rename("/nonexistent", "/new")
	if err == nil {
		t.Error("expected error for non-existent source, got nil")
	}
}

func TestMockFS_Chmod(t *testing.T) {
	m := NewMockFS()
	m.AddFile("/test.txt", []byte("data"), 0644)

	err := m.Chmod("/test.txt", 0755)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	info, _ := m.Stat("/test.txt")
	if info.Mode().Perm() != 0755 {
		t.Errorf("expected permissions 0755, got: %o", info.Mode().Perm())
	}
}

func TestMockFS_Chmod_NotFound(t *testing.T) {
	m := NewMockFS()

	err := m.Chmod("/nonexistent", 0755)
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestMockFS_Reset(t *testing.T) {
	m := NewMockFS()
	m.AddFile("/file.txt", []byte("data"), 0644)
	m.AddDir("/dir")

	m.Reset()

	if m.FileExists("/file.txt") {
		t.Error("expected files to be cleared")
	}
	if m.DirExists("/dir") {
		t.Error("expected directories to be cleared")
	}
}

func TestMockFS_FileExists(t *testing.T) {
	m := NewMockFS()
	m.AddFile("/exists.txt", []byte("data"), 0644)

	if !m.FileExists("/exists.txt") {
		t.Error("expected FileExists to return true for existing file")
	}
	if m.FileExists("/notexists.txt") {
		t.Error("expected FileExists to return false for non-existing file")
	}
}

func TestMockFS_DirExists(t *testing.T) {
	m := NewMockFS()
	m.AddDir("/mydir")

	if !m.DirExists("/mydir") {
		t.Error("expected DirExists to return true for existing directory")
	}
	if m.DirExists("/notexists") {
		t.Error("expected DirExists to return false for non-existing directory")
	}
}

func TestMockFS_Exists(t *testing.T) {
	m := NewMockFS()
	m.AddFile("/test/file.txt", []byte("content"), 0644)
	m.AddDir("/test/dir")

	// File exists
	if !m.Exists("/test/file.txt") {
		t.Error("expected Exists to return true for existing file")
	}

	// Directory exists
	if !m.Exists("/test/dir") {
		t.Error("expected Exists to return true for existing directory")
	}

	// Non-existent path
	if m.Exists("/nonexistent") {
		t.Error("expected Exists to return false for non-existent path")
	}
}

func TestRealFS_Exists(t *testing.T) {
	fs := &RealFS{}

	// Create a temp file
	tmpFile, err := fs.CreateTemp("", "exists-test-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer fs.Remove(tmpPath)

	// File exists
	if !fs.Exists(tmpPath) {
		t.Error("expected Exists to return true for existing file")
	}

	// Non-existent path
	if fs.Exists("/this/path/should/not/exist/ever") {
		t.Error("expected Exists to return false for non-existent path")
	}
}

func TestRealFS(t *testing.T) {
	// Skip if not running on a real filesystem
	fs := &RealFS{}

	// Create a temp file
	tmpFile, err := fs.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer fs.Remove(tmpPath)

	// Write to it
	testData := []byte("test content")
	err = fs.WriteFile(tmpPath, testData, 0644)
	if err != nil {
		t.Errorf("failed to write file: %v", err)
	}

	// Read it back
	data, err := fs.ReadFile(tmpPath)
	if err != nil {
		t.Errorf("failed to read file: %v", err)
	}
	if string(data) != string(testData) {
		t.Errorf("read data doesn't match: got %s, want %s", string(data), string(testData))
	}

	// Stat it
	info, err := fs.Stat(tmpPath)
	if err != nil {
		t.Errorf("failed to stat file: %v", err)
	}
	if info.Size() != int64(len(testData)) {
		t.Errorf("wrong file size: got %d, want %d", info.Size(), len(testData))
	}
}
