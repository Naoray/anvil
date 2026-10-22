//go:build !windows

package checks

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTreeDigestRefusesSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a"), "a")
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "fifo"), 0o644))
	_, err := newTreeDigester("", false, nil).digest(context.Background(), dir)
	require.ErrorContains(t, err, "unsupported file type")
}
