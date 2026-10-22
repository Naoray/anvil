package config

import (
	"fmt"
	"path"
)

// PruneConfig declares worktrees bulk pruning must never select. It may be
// set globally and per project; both lists apply.
type PruneConfig struct {
	// Protected holds path.Match glob patterns matched against a worktree's
	// branch and folder name.
	Protected []string `yaml:"protected" mapstructure:"protected"`
	// AllowIgnored holds path.Match glob patterns for gitignored paths
	// (relative to the worktree, or a parent directory of them) that pruning
	// may delete. Every other ignored path blocks pruning.
	AllowIgnored []string `yaml:"allow_ignored" mapstructure:"allow_ignored"`
}

// Validate rejects malformed glob patterns.
func (p PruneConfig) Validate() error {
	if err := ValidateProtectedPatterns("prune.protected", p.Protected); err != nil {
		return err
	}
	return ValidateProtectedPatterns("prune.allow_ignored", p.AllowIgnored)
}

// ValidateProtectedPatterns rejects empty or malformed glob patterns.
func ValidateProtectedPatterns(source string, patterns []string) error {
	for _, pattern := range patterns {
		if pattern == "" {
			return fmt.Errorf("%s: empty pattern", source)
		}
		if _, err := path.Match(pattern, ""); err != nil {
			return fmt.Errorf("%s: invalid pattern %q: %w", source, pattern, err)
		}
	}
	return nil
}
