package config

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
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
	return errors.Join(problems...)
}
