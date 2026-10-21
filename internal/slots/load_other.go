//go:build !linux && !darwin

package slots

// LoadAverage is unavailable here; load gating is skipped with a warning.
func LoadAverage() (float64, error) { return 0, ErrLoadUnsupported }
