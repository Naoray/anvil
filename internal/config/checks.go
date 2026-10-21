package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// CheckConfig declares one named validation workflow. Anvil runs existing
// project commands and records provenance; it never replaces test runners.
type CheckConfig struct {
	Name        string         `yaml:"-" json:"name"`
	Description string         `yaml:"description" json:"description,omitempty"`
	Commands    []CheckCommand `yaml:"commands" json:"commands"`
	// Timeout bounds the whole check (Go duration, e.g. 30m).
	Timeout string `yaml:"timeout" json:"timeout,omitempty"`
	// Expensive checks never run implicitly and require --allow-expensive.
	Expensive bool `yaml:"expensive" json:"expensive"`
	// Reusable asserts the check is deterministic for its recorded identity,
	// allowing `anvil check --reuse` to return a current passing result.
	Reusable bool `yaml:"reusable" json:"reusable"`
	// Locks are exclusive machine-wide resources held while the check runs.
	Locks []string `yaml:"locks" json:"locks,omitempty"`
	// Slots is the check's weight in the machine-wide slot semaphore shared
	// by expensive checks across projects and worktrees (0: 1 when
	// Expensive, otherwise ungated).
	Slots int `yaml:"slots" json:"slots,omitempty"`
	// Artifacts are produced paths (globs; worktree-relative or under
	// ${ANVIL_RUNTIME_DIR}) recorded with content digests.
	Artifacts []string `yaml:"artifacts" json:"artifacts,omitempty"`
	// Inputs are extra identity inputs beyond the Git working tree, such as
	// ignored fixtures or earlier recordings (same path rules as Artifacts).
	Inputs []string `yaml:"inputs" json:"inputs,omitempty"`
	// Toolchain commands (e.g. "cargo --version") whose output is recorded
	// and becomes part of the result identity.
	Toolchain []string `yaml:"toolchain" json:"toolchain,omitempty"`
	// Resources names shared resources (resources.shared) whose identity
	// becomes part of the result identity. Undeclared resources never do.
	Resources []string `yaml:"resources" json:"resources,omitempty"`
}

// CheckCommand is one ordered command of a check. A plain string is
// shorthand for {run: ...}.
type CheckCommand struct {
	Run       string            `yaml:"run" json:"run"`
	Component string            `yaml:"component" json:"component,omitempty"`
	Dir       string            `yaml:"dir" json:"dir,omitempty"`
	Env       map[string]string `yaml:"env" json:"env,omitempty"`
	Timeout   string            `yaml:"timeout" json:"timeout,omitempty"`
}

// UnmarshalYAML accepts a string or a mapping.
func (c *CheckCommand) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		c.Run = node.Value
		return nil
	}
	type plain CheckCommand
	return node.Decode((*plain)(c))
}

// Checks preserves declaration order of the checks mapping.
type Checks []CheckConfig

// UnmarshalYAML decodes an ordered mapping of check name → definition.
func (c *Checks) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("checks must be a mapping of name to definition")
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		var check CheckConfig
		if err := node.Content[i+1].Decode(&check); err != nil {
			return fmt.Errorf("check %q: %w", node.Content[i].Value, err)
		}
		check.Name = node.Content[i].Value
		*c = append(*c, check)
	}
	return nil
}

// FindCheck returns the named check.
func (c *Config) FindCheck(name string) (*CheckConfig, bool) {
	for i := range c.Checks {
		if c.Checks[i].Name == name {
			return &c.Checks[i], true
		}
	}
	return nil, false
}

func (c *Config) findShared(name string) (*SharedResource, bool) {
	for i := range c.Resources.Shared {
		if c.Resources.Shared[i].Name == name {
			return &c.Resources.Shared[i], true
		}
	}
	return nil, false
}

// ParseOptionalDuration parses an empty or Go-syntax duration.
func ParseOptionalDuration(value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if duration <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}
	return duration, nil
}

func validateCheckPath(path string) error {
	if strings.HasPrefix(path, "${ANVIL_RUNTIME_DIR}") || strings.HasPrefix(path, "${ANVIL_ARTIFACTS_DIR}") ||
		strings.HasPrefix(path, "${ANVIL_CACHE_DIR}") || strings.HasPrefix(path, "${ANVIL_STATE_DIR}") {
		return nil
	}
	return ValidateRelativePath(path)
}

func (c *Config) validateChecks() error {
	var problems []error
	seen := map[string]bool{}
	for _, check := range c.Checks {
		label := fmt.Sprintf("check %q", check.Name)
		if !resourceNamePattern.MatchString(check.Name) {
			problems = append(problems, fmt.Errorf("%s: invalid name (use lowercase letters, digits, '_' or '-')", label))
		}
		if seen[check.Name] {
			problems = append(problems, fmt.Errorf("%s: duplicate name", label))
		}
		seen[check.Name] = true
		if len(check.Commands) == 0 {
			problems = append(problems, fmt.Errorf("%s: at least one command is required", label))
		}
		if _, err := ParseOptionalDuration(check.Timeout); err != nil {
			problems = append(problems, fmt.Errorf("%s: timeout: %w", label, err))
		}
		if check.Slots < 0 {
			problems = append(problems, fmt.Errorf("%s: slots must be at least 1 when set", label))
		}
		for _, lock := range check.Locks {
			if !resourceNamePattern.MatchString(lock) {
				problems = append(problems, fmt.Errorf("%s: invalid lock name %q", label, lock))
			}
		}
		seenResources := map[string]bool{}
		for _, name := range check.Resources {
			if seenResources[name] {
				problems = append(problems, fmt.Errorf("%s: duplicate resource %q", label, name))
			}
			seenResources[name] = true
			shared, ok := c.findShared(name)
			switch {
			case !ok:
				problems = append(problems, fmt.Errorf("%s: unknown shared resource %q (declare it under resources.shared)", label, name))
			case shared.Identity == nil:
				problems = append(problems, fmt.Errorf("%s: shared resource %q declares no identity (set resources.shared identity file, command or directory)", label, name))
			}
		}
		for _, path := range append(append([]string(nil), check.Artifacts...), check.Inputs...) {
			if err := validateCheckPath(path); err != nil {
				problems = append(problems, fmt.Errorf("%s: %w", label, err))
			}
		}
		for i, command := range check.Commands {
			commandLabel := fmt.Sprintf("%s commands[%d]", label, i)
			if strings.TrimSpace(command.Run) == "" {
				problems = append(problems, fmt.Errorf("%s: run is required", commandLabel))
			}
			if command.Component != "" {
				if _, ok := c.FindComponent(command.Component); !ok {
					problems = append(problems, fmt.Errorf("%s: unknown component %q", commandLabel, command.Component))
				}
			}
			if command.Dir != "" {
				if err := ValidateRelativePath(command.Dir); err != nil {
					problems = append(problems, fmt.Errorf("%s: dir: %w", commandLabel, err))
				}
			}
			if err := validateEnvMap(command.Env); err != nil {
				problems = append(problems, fmt.Errorf("%s: env: %w", commandLabel, err))
			}
			if _, err := ParseOptionalDuration(command.Timeout); err != nil {
				problems = append(problems, fmt.Errorf("%s: timeout: %w", commandLabel, err))
			}
		}
	}
	return errors.Join(problems...)
}
