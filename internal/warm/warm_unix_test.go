//go:build darwin || linux

package warm

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestSpecialFileIsNeverCopied(t *testing.T) {
	src, install := fixture(t)
	require.NoError(t, unix.Mkfifo(filepath.Join(src, "node_modules", "pipe"), 0600))
	require.NoError(t, install.Record())
	result, err := target(t, src).Reuse(src, "copy")
	require.NoError(t, err)
	require.Equal(t, Cold, result)
}
