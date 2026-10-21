package config

import (
	"fmt"
	"os"
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

// CheckContained verifies that a worktree-relative path (or glob) cannot
// leave the worktree through symlinks. The deepest existing ancestor of the
// path's literal prefix is resolved and must stay inside the canonical
// worktree. Paths that do not exist yet are checked again when used.
func CheckContained(worktreePath, rel string) error {
	if err := ValidateRelativePath(rel); err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(worktreePath)
	if err != nil {
		return fmt.Errorf("resolving worktree: %w", err)
	}
	literal := rel
	if i := strings.IndexAny(rel, "*?["); i >= 0 {
		literal = filepath.Dir(rel[:i] + "x")
	}
	candidate := filepath.Join(worktreePath, literal)
	for {
		if _, err := os.Lstat(candidate); err == nil {
			break
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return nil
		}
		candidate = parent
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", rel, err)
	}
	return checkWithin(root, resolved, rel)
}

// CheckResolvedContained verifies an existing absolute path resolves inside
// the worktree (used for glob matches at the moment they are read).
func CheckResolvedContained(worktreePath, path, label string) error {
	root, err := filepath.EvalSymlinks(worktreePath)
	if err != nil {
		return fmt.Errorf("resolving worktree: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", label, err)
	}
	return checkWithin(root, resolved, label)
}

func checkWithin(root, resolved, label string) error {
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("path %q resolves outside the worktree (%s)", label, resolved)
	}
	return nil
}

// CheckStepContainment rejects step directories and declared files that
// escape the worktree through existing symlinks.
func CheckStepContainment(worktreePath string, cfg StepConfig) error {
	paths := append(append([]string(nil), cfg.Inputs...), cfg.Outputs...)
	for i := range paths {
		paths[i] = filepath.Join(cfg.Dir, paths[i])
	}
	if cfg.Dir != "" {
		paths = append([]string{cfg.Dir}, paths...)
	}
	for _, path := range paths {
		if err := CheckContained(worktreePath, path); err != nil {
			return fmt.Errorf("%s: %w", cfg.Name, err)
		}
	}
	return nil
}
