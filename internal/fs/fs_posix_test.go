//go:build !windows

package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRealFS_AtomicWriteFile_PreservesPOSIXPermissions(t *testing.T) {
	filesystem := &RealFS{}
	target := filepath.Join(t.TempDir(), ".env")

	if err := os.WriteFile(target, []byte("old data"), 0600); err != nil {
		t.Fatalf("failed to create target: %v", err)
	}

	if err := filesystem.AtomicWriteFile(target, []byte("new data"), 0640); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("failed to stat target: %v", err)
	}
	if got := info.Mode().Perm(); got != 0640 {
		t.Errorf("expected permissions 0640, got %04o", got)
	}
}
