package config

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Component is one independently prepared part of a repository, such as a
// Node frontend, a Rust crate or a Swift helper. Components are declared in
// anvil.yaml; their observed preparation lives in .anvil.local.
type Component struct {
	Name string `yaml:"name"`
	// Path is the worktree-relative component directory ("." by default).
	Path string `yaml:"path"`
	// Uses lists stack capabilities (node, rust, python, go, php, tauri,
	// swift) that contribute requirements, default preparation and
	// environment.
	Uses []string `yaml:"uses"`
	// Requires lists commands that must be on PATH.
	Requires []string `yaml:"requires"`
	// Platforms restricts the component to GOOS values (darwin, linux, ...).
	Platforms []string `yaml:"platforms"`
	// OptIn components are only prepared when requested explicitly.
	OptIn bool `yaml:"opt_in"`
	// OnMissing is "fail" (default) or "skip" when requirements are missing.
	OnMissing string `yaml:"on_missing"`
	// When is a scaffold condition evaluated relative to Path.
	When map[string]any `yaml:"when"`
	// Env is exported to this component's preparation and to
	// `anvil run --component`.
	Env map[string]string `yaml:"env"`
	// Prepare commands run after capability defaults (or instead of them
	// when Override is true).
	Prepare  []PrepareCommand `yaml:"prepare"`
	Override bool             `yaml:"override"`
}

// PrepareCommand is one declarative preparation command.
type PrepareCommand struct {
	Run       string            `yaml:"run"`
	Inputs    []string          `yaml:"inputs"`
	Outputs   []string          `yaml:"outputs"`
	Always    bool              `yaml:"always"`
	Env       map[string]string `yaml:"env"`
	When      map[string]any    `yaml:"when"`
	Platforms []string          `yaml:"platforms"`
}

// Missing-requirement policies.
const (
	OnMissingFail = "fail"
	OnMissingSkip = "skip"
)

// KnownCapabilities lists stack capabilities Anvil understands.
var KnownCapabilities = []string{"node", "rust", "python", "go", "php", "tauri", "swift"}

var knownPlatforms = map[string]bool{
	"darwin": true, "linux": true, "windows": true, "freebsd": true, "openbsd": true, "netbsd": true,
}

var componentNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// ValidComponentName reports whether name is usable as a component name.
func ValidComponentName(name string) bool {
	return componentNamePattern.MatchString(name)
}

// ComponentDir returns the cleaned worktree-relative directory ("" for root).
func (c Component) ComponentDir() string {
	if c.Path == "" || c.Path == "." {
		return ""
	}
	return strings.TrimSuffix(c.Path, "/")
}

// FindComponent returns the named component.
func (c *Config) FindComponent(name string) (*Component, bool) {
	for i := range c.Components {
		if c.Components[i].Name == name {
			return &c.Components[i], true
		}
	}
	return nil, false
}

func validatePlatforms(platforms []string) error {
	for _, platform := range platforms {
		if !knownPlatforms[platform] {
			return fmt.Errorf("unknown platform %q", platform)
		}
	}
	return nil
}

func validateEnvMap(env map[string]string) error {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !IsValidEnvName(key) {
			return fmt.Errorf("invalid environment variable name %q", key)
		}
	}
	return nil
}

// ValidateEnvironment checks the declarative environment schema. Conditions
// are validated later, before any effect, by the step registry.
func (c *Config) ValidateEnvironment() error {
	var problems []error
	if err := validateEnvMap(c.Env); err != nil {
		problems = append(problems, fmt.Errorf("env: %w", err))
	}
	for _, name := range c.SecretEnv {
		if !IsValidEnvName(name) {
			problems = append(problems, fmt.Errorf("secret_env: invalid variable name %q", name))
		}
	}
	seen := map[string]bool{}
	known := map[string]bool{}
	for _, capability := range KnownCapabilities {
		known[capability] = true
	}
	for i, component := range c.Components {
		label := fmt.Sprintf("components[%d]", i)
		if !ValidComponentName(component.Name) {
			problems = append(problems, fmt.Errorf("%s: invalid name %q (use lowercase letters, digits, '.', '_' or '-')", label, component.Name))
		} else {
			label = fmt.Sprintf("component %q", component.Name)
		}
		if seen[component.Name] {
			problems = append(problems, fmt.Errorf("%s: duplicate name", label))
		}
		seen[component.Name] = true
		if component.Path != "" {
			if err := ValidateRelativePath(component.Path); err != nil {
				problems = append(problems, fmt.Errorf("%s: path: %w", label, err))
			}
		}
		for _, capability := range component.Uses {
			if !known[capability] {
				problems = append(problems, fmt.Errorf("%s: unknown capability %q (known: %s)", label, capability, strings.Join(KnownCapabilities, ", ")))
			}
		}
		if err := validatePlatforms(component.Platforms); err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", label, err))
		}
		switch component.OnMissing {
		case "", OnMissingFail, OnMissingSkip:
		default:
			problems = append(problems, fmt.Errorf("%s: on_missing must be fail or skip", label))
		}
		if err := validateEnvMap(component.Env); err != nil {
			problems = append(problems, fmt.Errorf("%s: env: %w", label, err))
		}
		for j, command := range component.Prepare {
			commandLabel := fmt.Sprintf("%s prepare[%d]", label, j)
			if strings.TrimSpace(command.Run) == "" {
				problems = append(problems, fmt.Errorf("%s: run is required", commandLabel))
			}
			for _, path := range append(append([]string(nil), command.Inputs...), command.Outputs...) {
				if err := ValidateRelativePath(path); err != nil {
					problems = append(problems, fmt.Errorf("%s: %w", commandLabel, err))
				}
			}
			if err := validateEnvMap(command.Env); err != nil {
				problems = append(problems, fmt.Errorf("%s: env: %w", commandLabel, err))
			}
			if err := validatePlatforms(command.Platforms); err != nil {
				problems = append(problems, fmt.Errorf("%s: %w", commandLabel, err))
			}
		}
	}
	if err := c.validateResources(); err != nil {
		problems = append(problems, err)
	}
	if err := c.validateChecks(); err != nil {
		problems = append(problems, err)
	}
	if err := c.validateRoles(); err != nil {
		problems = append(problems, err)
	}
	return errors.Join(problems...)
}

// ResourcesConfig declares runtime resources owned by each worktree.
type ResourcesConfig struct {
	// Directories are extra named writable directories in the worktree's
	// runtime area, exported as ANVIL_DIR_<NAME>. The standard state, tmp,
	// cache, logs and artifacts directories always exist.
	Directories []string `yaml:"directories"`
	// Ports are stable per-worktree development ports, exported as
	// ANVIL_PORT_<NAME> (and Env when set).
	Ports []PortConfig `yaml:"ports"`
	// Shared are read-only resources used in place, never copied or deleted.
	Shared []SharedResource `yaml:"shared"`
}

// PortConfig declares one named port.
type PortConfig struct {
	Name string `yaml:"name"`
	Env  string `yaml:"env"`
}

// SharedResource is a machine-wide resource (for example cached model
// weights) exported as ANVIL_SHARED_<NAME>. Anvil never copies, writes or
// deletes it; read-only use is a convention, not an enforced permission.
type SharedResource struct {
	Name     string `yaml:"name"`
	Path     string `yaml:"path"`
	Env      string `yaml:"env"`
	Required bool   `yaml:"required"`
}

// ProcessConfig declares one managed development process.
type ProcessConfig struct {
	Name      string            `yaml:"name"`
	Command   string            `yaml:"command"`
	Component string            `yaml:"component"`
	Env       map[string]string `yaml:"env"`
	// StopTimeout is how long `anvil stop` waits after SIGTERM before SIGKILL.
	StopTimeout string `yaml:"stop_timeout"`
}

var resourceNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ResourceEnvSuffix converts a resource name into an environment suffix.
func ResourceEnvSuffix(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

func (c *Config) validateResources() error {
	var problems []error
	names := func(kind string, list []string) {
		seen := map[string]bool{}
		for _, name := range list {
			if !resourceNamePattern.MatchString(name) {
				problems = append(problems, fmt.Errorf("resources.%s: invalid name %q (use lowercase letters, digits, '_' or '-')", kind, name))
			}
			if seen[name] {
				problems = append(problems, fmt.Errorf("resources.%s: duplicate name %q", kind, name))
			}
			seen[name] = true
		}
	}
	reserved := map[string]bool{"state": true, "tmp": true, "cache": true, "logs": true, "artifacts": true}
	for _, name := range c.Resources.Directories {
		if reserved[name] {
			problems = append(problems, fmt.Errorf("resources.directories: %q is always provided", name))
		}
	}
	names("directories", c.Resources.Directories)
	var portNames, sharedNames []string
	for _, port := range c.Resources.Ports {
		portNames = append(portNames, port.Name)
		if port.Env != "" && !IsValidEnvName(port.Env) {
			problems = append(problems, fmt.Errorf("resources.ports %q: invalid env name %q", port.Name, port.Env))
		}
	}
	names("ports", portNames)
	for _, shared := range c.Resources.Shared {
		sharedNames = append(sharedNames, shared.Name)
		if shared.Path == "" {
			problems = append(problems, fmt.Errorf("resources.shared %q: path is required", shared.Name))
		}
		if shared.Env != "" && !IsValidEnvName(shared.Env) {
			problems = append(problems, fmt.Errorf("resources.shared %q: invalid env name %q", shared.Name, shared.Env))
		}
	}
	names("shared", sharedNames)

	var processNames []string
	for _, process := range c.Processes {
		processNames = append(processNames, process.Name)
		if strings.TrimSpace(process.Command) == "" {
			problems = append(problems, fmt.Errorf("processes %q: command is required", process.Name))
		}
		if process.Component != "" {
			if _, ok := c.FindComponent(process.Component); !ok {
				problems = append(problems, fmt.Errorf("processes %q: unknown component %q", process.Name, process.Component))
			}
		}
		if err := validateEnvMap(process.Env); err != nil {
			problems = append(problems, fmt.Errorf("processes %q: env: %w", process.Name, err))
		}
		if process.StopTimeout != "" {
			if _, err := time.ParseDuration(process.StopTimeout); err != nil {
				problems = append(problems, fmt.Errorf("processes %q: stop_timeout: %w", process.Name, err))
			}
		}
	}
	seen := map[string]bool{}
	for _, name := range processNames {
		if !resourceNamePattern.MatchString(name) {
			problems = append(problems, fmt.Errorf("processes: invalid name %q", name))
		}
		if seen[name] {
			problems = append(problems, fmt.Errorf("processes: duplicate name %q", name))
		}
		seen[name] = true
	}
	return errors.Join(problems...)
}

// FindProcess returns the named process definition.
func (c *Config) FindProcess(name string) (*ProcessConfig, bool) {
	for i := range c.Processes {
		if c.Processes[i].Name == name {
			return &c.Processes[i], true
		}
	}
	return nil, false
}

// RolesConfig declares per-role behavior.
type RolesConfig struct {
	Review ReviewRoleConfig `yaml:"review"`
}

// ReviewRoleConfig controls review worktrees. Review worktrees prepare
// components only by default: preset/scaffold steps (databases, site links,
// .env copies) run only when Scaffold is true.
type ReviewRoleConfig struct {
	Scaffold bool `yaml:"scaffold"`
	// Components limits which components are prepared (default: all
	// applicable, non-opt-in components).
	Components []string `yaml:"components"`
	// EnvUnset removes inherited variables (for example production
	// credentials) from commands Anvil runs in review worktrees.
	EnvUnset []string `yaml:"env_unset"`
}

func (c *Config) validateRoles() error {
	var problems []error
	for _, name := range c.Roles.Review.Components {
		if _, ok := c.FindComponent(name); !ok {
			problems = append(problems, fmt.Errorf("roles.review.components: unknown component %q", name))
		}
	}
	for _, name := range c.Roles.Review.EnvUnset {
		if !IsValidEnvName(name) {
			problems = append(problems, fmt.Errorf("roles.review.env_unset: invalid variable name %q", name))
		}
	}
	return errors.Join(problems...)
}
