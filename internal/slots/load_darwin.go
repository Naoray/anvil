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
	if len(raw) < 16 {
		return 0, fmt.Errorf("%w: short vm.loadavg (%d bytes)", ErrLoadUnsupported, len(raw))
	}
	load := binary.LittleEndian.Uint32(raw[0:4])
	scale := binary.LittleEndian.Uint64(raw[8:16])
	if scale == 0 {
		return 0, fmt.Errorf("%w: vm.loadavg has no scale", ErrLoadUnsupported)
	}
	return float64(load) / float64(scale), nil
}
