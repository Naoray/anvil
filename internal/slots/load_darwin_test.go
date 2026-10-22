//go:build darwin

package slots

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadAverageParsesAlignedDarwinStruct(t *testing.T) {
	raw := make([]byte, 24)
	binary.LittleEndian.PutUint32(raw[0:4], 4096)
	binary.LittleEndian.PutUint32(raw[4:8], 5120)
	binary.LittleEndian.PutUint32(raw[8:12], 3072)
	binary.LittleEndian.PutUint64(raw[16:24], 2048)

	got, err := loadAverageFromRaw(raw)
	require.NoError(t, err)
	require.Equal(t, 2.0, got)
}

func TestLoadAverageRejectsTruncatedDarwinStruct(t *testing.T) {
	_, err := loadAverageFromRaw(make([]byte, 16))
	require.ErrorIs(t, err, ErrLoadUnsupported)
}
