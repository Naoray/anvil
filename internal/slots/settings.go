package slots

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"time"

	"github.com/naoray/anvil/internal/config"
)

// Environment overrides of the global `checks:` configuration (tests and
// one-off tuning). They take precedence over ~/.config/anvil/anvil.yaml.
const (
	EnvSlots    = "ANVIL_CHECK_SLOTS"
	EnvMaxLoad  = "ANVIL_CHECK_MAX_LOAD"
	EnvLoadWait = "ANVIL_CHECK_LOAD_WAIT"
)

// DefaultLoadWait bounds load gating when max_load is set without load_wait.
const DefaultLoadWait = 2 * time.Minute

// Settings are the machine-wide admission parameters.
type Settings struct {
	// Capacity is the total slot weight that may run at once.
	Capacity int
	// MaxLoad gates admission on the 1-minute load average; 0 disables it.
	MaxLoad float64
	// LoadWait bounds the load wait, after which admission proceeds.
	LoadWait time.Duration
}

// DefaultCapacity is one slot per four logical CPUs, at least two. Expensive
// checks (benchmarks, full builds, end-to-end suites) usually parallelize
// internally across every core; a quarter of the cores bounds
// oversubscription, and the floor of two keeps one long check from
// serializing every other agent on small machines.
func DefaultCapacity() int {
	return max(2, runtime.NumCPU()/4)
}

// ResolveSettings combines the global configuration, environment overrides
// and defaults, rejecting invalid values instead of silently ignoring them.
func ResolveSettings(cfg config.GlobalChecksConfig, getenv func(string) string) (Settings, error) {
	settings := Settings{Capacity: cfg.Slots, MaxLoad: cfg.MaxLoad}
	var problems []error
	if cfg.Slots < 0 {
		problems = append(problems, fmt.Errorf("checks.slots must be at least 1, got %d", cfg.Slots))
	}
	if cfg.MaxLoad < 0 {
		problems = append(problems, fmt.Errorf("checks.max_load must not be negative, got %g", cfg.MaxLoad))
	}
	if cfg.LoadWait != "" {
		wait, err := config.ParseOptionalDuration(cfg.LoadWait)
		if err != nil {
			problems = append(problems, fmt.Errorf("checks.load_wait: %w", err))
		}
		settings.LoadWait = wait
	}
	if value := getenv(EnvSlots); value != "" {
		slots, err := strconv.Atoi(value)
		if err != nil || slots < 1 {
			problems = append(problems, fmt.Errorf("%s must be a positive integer, got %q", EnvSlots, value))
		}
		settings.Capacity = slots
	}
	if value := getenv(EnvMaxLoad); value != "" {
		load, err := strconv.ParseFloat(value, 64)
		if err != nil || load < 0 {
			problems = append(problems, fmt.Errorf("%s must be a non-negative number, got %q", EnvMaxLoad, value))
		}
		settings.MaxLoad = load
	}
	if value := getenv(EnvLoadWait); value != "" {
		wait, err := config.ParseOptionalDuration(value)
		if err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", EnvLoadWait, err))
		}
		settings.LoadWait = wait
	}
	if err := errors.Join(problems...); err != nil {
		return Settings{}, err
	}
	if settings.Capacity == 0 {
		settings.Capacity = DefaultCapacity()
	}
	if settings.MaxLoad > 0 && settings.LoadWait == 0 {
		settings.LoadWait = DefaultLoadWait
	}
	return settings, nil
}
