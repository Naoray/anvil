package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

func validateBinaryStepConfig(stepName string) error {
	if stepName == "" {
		return fmt.Errorf("binary step: 'name' is required")
	}
	return nil
}

func validateFileCopyStepConfig(cfg StepConfig) error {
	if cfg.From == "" {
		return fmt.Errorf("file.copy: 'from' is required")
	}
	if cfg.To == "" {
		return fmt.Errorf("file.copy: 'to' is required")
	}
	return nil
}

func validateBashRunStepConfig(cfg StepConfig) error {
	if cfg.Command == "" {
		return fmt.Errorf("bash.run: 'command' is required")
	}
	return nil
}

func validateCommandRunStepConfig(cfg StepConfig) error {
	if cfg.Command == "" {
		return fmt.Errorf("command.run: 'command' is required")
	}
	return nil
}

func validateEnvReadStepConfig(cfg StepConfig) error {
	if cfg.Key == "" {
		return fmt.Errorf("env.read: 'key' is required")
	}
	return nil
}

func validateEnvWriteStepConfig(cfg StepConfig) error {
	if cfg.Key == "" {
		return fmt.Errorf("env.write: 'key' is required")
	}
	return nil
}

func validateEnvCopyStepConfig(cfg StepConfig) error {
	if cfg.Source == "" {
		return fmt.Errorf("env.copy: 'source' is required")
	}
	if cfg.Key == "" && len(cfg.Keys) == 0 {
		return fmt.Errorf("env.copy: either 'key' or 'keys' must be specified")
	}
	return nil
}

func validateDbCreateStepConfig(cfg StepConfig) error {
	switch cfg.Role {
	case "", DbRoleApplication, DbRoleTesting:
		return nil
	default:
		return fmt.Errorf("db.create: invalid role %q (supported roles: application, testing)", cfg.Role)
	}
}

func validateDbDestroyStepConfig(StepConfig) error {
	return nil
}

// ValidateStepConfig validates a StepConfig based on its step type.
// The stepName parameter is used to determine the step type for validation.
// This is the main entry point for step validation.
func ValidateStepConfig(stepName string, cfg StepConfig) error {
	if err := validateStepPaths(stepName, cfg); err != nil {
		return err
	}
	switch stepName {
	case StepFileCopy:
		return validateFileCopyStepConfig(cfg)
	case StepBashRun:
		return validateBashRunStepConfig(cfg)
	case StepCommandRun:
		return validateCommandRunStepConfig(cfg)
	case StepEnvRead:
		return validateEnvReadStepConfig(cfg)
	case StepEnvWrite:
		return validateEnvWriteStepConfig(cfg)
	case StepEnvCopy:
		return validateEnvCopyStepConfig(cfg)
	case StepDbCreate:
		return validateDbCreateStepConfig(cfg)
	case "db.migrate":
		return nil
	case StepDbDestroy:
		return validateDbDestroyStepConfig(cfg)
	default:
		// Binary steps (php, npm, composer, etc.) and unknown steps.
		return validateBinaryStepConfig(stepName)
	}
}

// ValidateRelativePath rejects absolute paths and paths that leave the
// worktree; Anvil only reads, runs in or records paths inside it.
func ValidateRelativePath(path string) error {
	if path == "" {
		return fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") {
		return fmt.Errorf("path %q must be relative to the worktree", path)
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("path %q leaves the worktree", path)
	}
	return nil
}

func validateStepPaths(stepName string, cfg StepConfig) error {
	if cfg.Dir != "" {
		if err := ValidateRelativePath(cfg.Dir); err != nil {
			return fmt.Errorf("%s: dir: %w", stepName, err)
		}
	}
	for _, path := range cfg.Inputs {
		if err := ValidateRelativePath(path); err != nil {
			return fmt.Errorf("%s: inputs: %w", stepName, err)
		}
	}
	for _, path := range cfg.Outputs {
		if err := ValidateRelativePath(path); err != nil {
			return fmt.Errorf("%s: outputs: %w", stepName, err)
		}
	}
	for key := range cfg.Env {
		if !IsValidEnvName(key) {
			return fmt.Errorf("%s: env: invalid variable name %q", stepName, key)
		}
	}
	return nil
}

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// IsValidEnvName reports whether name is a portable environment variable name.
func IsValidEnvName(name string) bool {
	return envNamePattern.MatchString(name)
}
