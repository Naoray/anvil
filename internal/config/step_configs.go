package config

import "fmt"

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
	case StepDbDestroy:
		return validateDbDestroyStepConfig(cfg)
	default:
		// Binary steps (php, npm, composer, etc.) and unknown steps.
		return validateBinaryStepConfig(stepName)
	}
}
