//go:build darwin

package slots

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"
)

// LoadAverage returns the 1-minute load average from sysctl vm.loadavg
// (struct loadavg { fixpt_t ldavg[3]; long fscale; }).
func LoadAverage() (float64, error) {
	raw, err := unix.SysctlRaw("vm.loadavg")
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrLoadUnsupported, err)
	}
	return loadAverageFromRaw(raw)
}

func loadAverageFromRaw(raw []byte) (float64, error) {
	// On 64-bit Darwin, three uint32 fixpt_t values are followed by padding
	// before the 64-bit long fscale field.
	if len(raw) < 24 {
		return 0, fmt.Errorf("%w: short vm.loadavg (%d bytes)", ErrLoadUnsupported, len(raw))
	}
	load := binary.LittleEndian.Uint32(raw[0:4])
	scale := binary.LittleEndian.Uint64(raw[16:24])
	if scale == 0 {
		return 0, fmt.Errorf("%w: vm.loadavg has no scale", ErrLoadUnsupported)
	}
	return float64(load) / float64(scale), nil
}
