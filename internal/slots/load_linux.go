//go:build linux

package slots

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// LoadAverage returns the 1-minute load average from /proc/loadavg.
func LoadAverage() (float64, error) {
	content, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrLoadUnsupported, err)
	}
	fields := strings.Fields(string(content))
	if len(fields) == 0 {
		return 0, fmt.Errorf("%w: empty /proc/loadavg", ErrLoadUnsupported)
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("%w: malformed /proc/loadavg: %w", ErrLoadUnsupported, err)
	}
	return value, nil
}
