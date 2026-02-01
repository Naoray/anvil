package config

import (
	"fmt"
)

// StepValidator is an interface for step-specific configuration validation.
// Each step type can implement this interface to validate its required fields.
type StepValidator interface {
	Validate() error
}

// BaseStepConfig contains fields common to all steps.
type BaseStepConfig struct {
	Name      string                 `mapstructure:"name"`
	Enabled   *bool                  `mapstructure:"enabled"`
	Condition map[string]interface{} `mapstructure:"condition"`
}

// BinaryStepConfig represents configuration for binary execution steps (php, npm, etc.)
type BinaryStepConfig struct {
	BaseStepConfig
	Args    []string `mapstructure:"args"`
	StoreAs string   `mapstructure:"store_as"`
}

// Validate checks that the binary step config is valid.
// Binary steps only require a name; all other fields are optional.
func (c BinaryStepConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("binary step: 'name' is required")
	}
	return nil
}

// FileCopyConfig represents configuration for file.copy step
type FileCopyConfig struct {
	BaseStepConfig
	From string `mapstructure:"from"`
	To   string `mapstructure:"to"`
}

// Validate checks that required fields are present for file.copy step
func (c FileCopyConfig) Validate() error {
	if c.From == "" {
		return fmt.Errorf("file.copy: 'from' is required")
	}
	if c.To == "" {
		return fmt.Errorf("file.copy: 'to' is required")
	}
	return nil
}

// BashRunConfig represents configuration for bash.run step
type BashRunConfig struct {
	BaseStepConfig
	Command string `mapstructure:"command"`
	StoreAs string `mapstructure:"store_as"`
}

// Validate checks that required fields are present for bash.run step
func (c BashRunConfig) Validate() error {
	if c.Command == "" {
		return fmt.Errorf("bash.run: 'command' is required")
	}
	return nil
}

// CommandRunConfig represents configuration for command.run step
type CommandRunConfig struct {
	BaseStepConfig
	Command string `mapstructure:"command"`
	StoreAs string `mapstructure:"store_as"`
}

// Validate checks that required fields are present for command.run step
func (c CommandRunConfig) Validate() error {
	if c.Command == "" {
		return fmt.Errorf("command.run: 'command' is required")
	}
	return nil
}

// EnvReadConfig represents configuration for env.read step
type EnvReadConfig struct {
	BaseStepConfig
	Key     string `mapstructure:"key"`
	StoreAs string `mapstructure:"store_as"`
	File    string `mapstructure:"file"`
}

// Validate checks that required fields are present for env.read step
func (c EnvReadConfig) Validate() error {
	if c.Key == "" {
		return fmt.Errorf("env.read: 'key' is required")
	}
	return nil
}

// EnvWriteConfig represents configuration for env.write step
type EnvWriteConfig struct {
	BaseStepConfig
	Key   string `mapstructure:"key"`
	Value string `mapstructure:"value"`
	File  string `mapstructure:"file"`
}

// Validate checks that required fields are present for env.write step
func (c EnvWriteConfig) Validate() error {
	if c.Key == "" {
		return fmt.Errorf("env.write: 'key' is required")
	}
	return nil
}

// EnvCopyConfig represents configuration for env.copy step
type EnvCopyConfig struct {
	BaseStepConfig
	Source     string   `mapstructure:"source"`
	SourceFile string   `mapstructure:"source_file"`
	Key        string   `mapstructure:"key"`
	Keys       []string `mapstructure:"keys"`
	File       string   `mapstructure:"file"`
}

// Validate checks that required fields are present for env.copy step
func (c EnvCopyConfig) Validate() error {
	if c.Source == "" {
		return fmt.Errorf("env.copy: 'source' is required")
	}
	if c.Key == "" && len(c.Keys) == 0 {
		return fmt.Errorf("env.copy: either 'key' or 'keys' must be specified")
	}
	return nil
}

// DbCreateConfig represents configuration for db.create step
type DbCreateConfig struct {
	BaseStepConfig
	Args []string `mapstructure:"args"`
	Type string   `mapstructure:"type"`
}

// Validate checks that the db.create step config is valid.
// All fields are optional for db.create.
func (c DbCreateConfig) Validate() error {
	return nil
}

// DbDestroyConfig represents configuration for db.destroy step
type DbDestroyConfig struct {
	BaseStepConfig
	Args []string `mapstructure:"args"`
	Type string   `mapstructure:"type"`
}

// Validate checks that the db.destroy step config is valid.
// All fields are optional for db.destroy.
func (c DbDestroyConfig) Validate() error {
	return nil
}

// ValidateStepConfig validates a StepConfig based on its step type.
// The stepName parameter is used to determine the step type for validation.
// This is the main entry point for step validation.
func ValidateStepConfig(stepName string, cfg StepConfig) error {
	base := BaseStepConfig{
		Name:      stepName,
		Enabled:   cfg.Enabled,
		Condition: cfg.Condition,
	}

	switch stepName {
	case "file.copy":
		return FileCopyConfig{
			BaseStepConfig: base,
			From:           cfg.From,
			To:             cfg.To,
		}.Validate()
	case "bash.run":
		return BashRunConfig{
			BaseStepConfig: base,
			Command:        cfg.Command,
			StoreAs:        cfg.StoreAs,
		}.Validate()
	case "command.run":
		return CommandRunConfig{
			BaseStepConfig: base,
			Command:        cfg.Command,
			StoreAs:        cfg.StoreAs,
		}.Validate()
	case "env.read":
		return EnvReadConfig{
			BaseStepConfig: base,
			Key:            cfg.Key,
			StoreAs:        cfg.StoreAs,
			File:           cfg.File,
		}.Validate()
	case "env.write":
		return EnvWriteConfig{
			BaseStepConfig: base,
			Key:            cfg.Key,
			Value:          cfg.Value,
			File:           cfg.File,
		}.Validate()
	case "env.copy":
		return EnvCopyConfig{
			BaseStepConfig: base,
			Source:         cfg.Source,
			SourceFile:     cfg.SourceFile,
			Key:            cfg.Key,
			Keys:           cfg.Keys,
			File:           cfg.File,
		}.Validate()
	case "db.create":
		return DbCreateConfig{
			BaseStepConfig: base,
			Args:           cfg.Args,
			Type:           cfg.Type,
		}.Validate()
	case "db.destroy":
		return DbDestroyConfig{
			BaseStepConfig: base,
			Args:           cfg.Args,
			Type:           cfg.Type,
		}.Validate()
	default:
		// Binary steps (php, npm, composer, etc.) and unknown steps
		return BinaryStepConfig{
			BaseStepConfig: base,
			Args:           cfg.Args,
			StoreAs:        cfg.StoreAs,
		}.Validate()
	}
}
