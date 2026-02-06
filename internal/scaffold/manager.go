package scaffold

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/steps"
	"github.com/naoray/anvil/internal/scaffold/types"
	"github.com/naoray/anvil/internal/scaffold/words"
	"github.com/naoray/anvil/internal/ui"
)

type ScaffoldManager struct {
	presets     map[string]Preset
	presetOrder []string
	registry    StepRegistry
}

// StepRegistry defines the interface for step creation.
// This abstraction allows for dependency injection and testing.
type StepRegistry interface {
	Create(name string, cfg config.StepConfig) (types.ScaffoldStep, error)
	ListRegistered() []string
}

type Preset interface {
	Name() string
	Detect(path string) bool
	DefaultSteps() []config.StepConfig
	CleanupSteps() []config.CleanupStep
}

// NewScaffoldManager creates a new scaffold manager using the global step registry.
// Deprecated: Use NewScaffoldManagerWithRegistry instead for explicit dependency injection.
func NewScaffoldManager() *ScaffoldManager {
	return NewScaffoldManagerWithRegistry(nil)
}

// NewScaffoldManagerWithRegistry creates a new scaffold manager with the given step registry.
// If registry is nil, the global registry is used for backward compatibility.
func NewScaffoldManagerWithRegistry(registry StepRegistry) *ScaffoldManager {
	if registry == nil {
		registry = &globalStepRegistryAdapter{}
	}
	return &ScaffoldManager{
		presets:     make(map[string]Preset),
		presetOrder: make([]string, 0),
		registry:    registry,
	}
}

// globalStepRegistryAdapter adapts the global step functions to the StepRegistry interface.
// This provides backward compatibility during the migration to explicit registry.
type globalStepRegistryAdapter struct{}

func (a *globalStepRegistryAdapter) Create(name string, cfg config.StepConfig) (types.ScaffoldStep, error) {
	return steps.Create(name, cfg)
}

func (a *globalStepRegistryAdapter) ListRegistered() []string {
	return steps.ListRegistered()
}

func (m *ScaffoldManager) RegisterPreset(preset Preset) {
	m.presets[preset.Name()] = preset
	m.presetOrder = append(m.presetOrder, preset.Name())
}

func (m *ScaffoldManager) GetPreset(name string) (Preset, bool) {
	preset, ok := m.presets[name]
	return preset, ok
}

func (m *ScaffoldManager) DetectPreset(path string) string {
	for _, name := range m.presetOrder {
		if preset, ok := m.presets[name]; ok && preset.Detect(path) {
			return preset.Name()
		}
	}
	return ""
}

func (m *ScaffoldManager) GetStepsForWorktree(cfg *config.Config, worktreePath, branch string) ([]types.ScaffoldStep, error) {
	var stepsList []types.ScaffoldStep

	presetName := cfg.Preset
	if presetName == "" {
		presetName = m.DetectPreset(worktreePath)
	}

	if preset, ok := m.GetPreset(presetName); ok {
		for _, stepConfig := range preset.DefaultSteps() {
			step, err := m.registry.Create(stepConfig.Name, stepConfig)
			if err != nil {
				return nil, fmt.Errorf("creating step %q: %w", stepConfig.Name, err)
			}
			stepsList = append(stepsList, step)
		}
	}

	if cfg.Scaffold.Override {
		overrideSteps, err := m.stepsFromConfig(cfg.Scaffold.Steps)
		if err != nil {
			return nil, err
		}
		stepsList = overrideSteps
	} else {
		additionalSteps, err := m.stepsFromConfig(cfg.Scaffold.Steps)
		if err != nil {
			return nil, err
		}
		stepsList = append(stepsList, additionalSteps...)
	}

	return stepsList, nil
}

func (m *ScaffoldManager) GetCleanupSteps(cfg *config.Config, worktreePath, branch string) ([]types.ScaffoldStep, error) {
	var stepsList []types.ScaffoldStep

	presetName := cfg.Preset
	if presetName == "" {
		presetName = m.DetectPreset(worktreePath)
	}

	if preset, ok := m.GetPreset(presetName); ok {
		for _, cleanupConfig := range preset.CleanupSteps() {
			stepConfig := m.cleanupConfigToStepConfig(cleanupConfig)
			step, err := m.registry.Create(cleanupConfig.Name, stepConfig)
			if err != nil {
				return nil, fmt.Errorf("creating cleanup step %q: %w", cleanupConfig.Name, err)
			}
			stepsList = append(stepsList, step)
		}
	}

	for _, cleanupConfig := range cfg.Cleanup.Steps {
		stepConfig := m.cleanupConfigToStepConfig(cleanupConfig)
		step, err := m.registry.Create(cleanupConfig.Name, stepConfig)
		if err != nil {
			return nil, fmt.Errorf("creating cleanup step %q: %w", cleanupConfig.Name, err)
		}
		stepsList = append(stepsList, step)
	}

	return stepsList, nil
}

func (m *ScaffoldManager) cleanupConfigToStepConfig(cleanupConfig config.CleanupStep) config.StepConfig {
	stepConfig := config.StepConfig{
		Name: cleanupConfig.Name,
		Args: nil,
	}
	if cleanupConfig.Name == "herd" {
		stepConfig.Args = []string{"unlink"}
	}
	for k, v := range cleanupConfig.Condition {
		if k == "command" {
			if cmd, ok := v.(string); ok {
				stepConfig.Command = cmd
			}
		}
	}
	return stepConfig
}

func (m *ScaffoldManager) stepsFromConfig(stepConfigs []config.StepConfig) ([]types.ScaffoldStep, error) {
	stepsList := make([]types.ScaffoldStep, 0, len(stepConfigs))

	for _, cfg := range stepConfigs {
		step, err := m.registry.Create(cfg.Name, cfg)
		if err != nil {
			return nil, fmt.Errorf("creating step %q: %w", cfg.Name, err)
		}
		stepsList = append(stepsList, step)
	}

	return stepsList, nil
}

func (m *ScaffoldManager) RunScaffold(worktreePath, branch, repoName, siteName, preset string, cfg *config.Config, dryRun, verbose, quiet bool) error {
	ctx := m.newScaffoldContext(worktreePath, branch, repoName, siteName, preset)

	// Run pre-flight checks with spinner
	if !quiet {
		if err := m.runPreFlightWithSpinner(&ctx, &cfg.Scaffold); err != nil {
			return err
		}
	} else {
		// Quiet mode: run without spinner
		if err := m.runPreFlightChecks(&ctx, &cfg.Scaffold); err != nil {
			return err
		}
	}

	// Migrate db_suffix from anvil.yaml to .anvil.local if present
	if !dryRun {
		if _, err := config.MigrateDbSuffixToLocal(worktreePath); err != nil {
			return fmt.Errorf("migrating db_suffix: %w", err)
		}
	}

	// Load local state instead of worktree config
	localState, err := config.ReadLocalState(worktreePath)
	if err != nil {
		return fmt.Errorf("reading local state: %w", err)
	}

	if localState.DbSuffix == "" {
		newSuffix := words.GenerateSuffix()
		ctx.SetDbSuffix(newSuffix)
		if !dryRun {
			if err := config.WriteLocalState(worktreePath, config.LocalState{DbSuffix: newSuffix}); err != nil {
				return fmt.Errorf("writing db_suffix to local state: %w", err)
			}
		}
	} else {
		ctx.SetDbSuffix(localState.DbSuffix)
	}

	stepsList, err := m.GetStepsForWorktree(cfg, worktreePath, branch)
	if err != nil {
		return fmt.Errorf("getting scaffold steps: %w", err)
	}

	opts := m.stepOptionsFromFlags(dryRun, verbose, quiet)

	executor := NewStepExecutor(stepsList, &ctx, opts)
	if err := executor.Execute(); err != nil {
		return err
	}

	return nil
}

func (m *ScaffoldManager) RunCleanup(worktreePath, branch, repoName, siteName, preset string, cfg *config.Config, dryRun, verbose, quiet bool) error {
	ctx := m.newScaffoldContext(worktreePath, branch, repoName, siteName, preset)

	stepsList, err := m.GetCleanupSteps(cfg, worktreePath, branch)
	if err != nil {
		return fmt.Errorf("getting cleanup steps: %w", err)
	}

	opts := m.stepOptionsFromFlags(dryRun, verbose, quiet)

	executor := NewStepExecutor(stepsList, &ctx, opts)
	if err := executor.Execute(); err != nil {
		return err
	}

	return nil
}

func (m *ScaffoldManager) newScaffoldContext(worktreePath, branch, repoName, siteName, preset string) types.ScaffoldContext {
	path := filepath.Base(worktreePath)
	repoPath := filepath.Base(filepath.Dir(worktreePath))
	return types.ScaffoldContext{
		WorktreePath: worktreePath,
		Branch:       branch,
		RepoName:     repoName,
		SiteName:     siteName,
		Preset:       preset,
		Env:          make(map[string]string),
		Path:         path,
		RepoPath:     repoPath,
		Vars:         make(map[string]string),
	}
}

func (m *ScaffoldManager) stepOptionsFromFlags(dryRun, verbose, quiet bool) types.StepOptions {
	return types.StepOptions{
		DryRun:  dryRun,
		Verbose: verbose,
		Quiet:   quiet,
	}
}

// runPreFlightChecks validates dependencies before scaffold execution.
// Returns an error with detailed information if any checks fail.
func (m *ScaffoldManager) runPreFlightChecks(ctx *types.ScaffoldContext, cfg *config.ScaffoldConfig) error {
	// Skip if no pre-flight configured
	if cfg.PreFlight == nil || len(cfg.PreFlight.Condition) == 0 {
		return nil
	}

	// Evaluate the condition
	result, err := ctx.EvaluateCondition(cfg.PreFlight.Condition)
	if err != nil {
		return fmt.Errorf("pre-flight check error: %w", err)
	}

	if !result {
		// Generate detailed error message showing what failed
		return m.generatePreFlightError(ctx, cfg.PreFlight.Condition)
	}

	return nil
}

// runPreFlightWithSpinner runs pre-flight checks with a spinner.
func (m *ScaffoldManager) runPreFlightWithSpinner(ctx *types.ScaffoldContext, cfg *config.ScaffoldConfig) error {
	// Skip if no pre-flight configured
	if cfg.PreFlight == nil || len(cfg.PreFlight.Condition) == 0 {
		return nil
	}

	var checkErr error
	err := ui.RunWithSpinner("Running pre-flight checks", func() error {
		checkErr = m.runPreFlightChecks(ctx, cfg)
		return checkErr
	})

	if err != nil {
		return err
	}

	return checkErr
}

// generatePreFlightError creates a detailed error message showing which checks failed.
func (m *ScaffoldManager) generatePreFlightError(ctx *types.ScaffoldContext, conditions map[string]interface{}) error {
	var errorParts []string

	collected := m.collectPreFlightValues(conditions)

	missingEnv := uniqueStringsPreserveOrder(m.checkMissingEnvVars(collected.envs))
	if len(missingEnv) > 0 {
		errorParts = append(errorParts,
			fmt.Sprintf("Missing environment variables:\n  - %s",
				strings.Join(missingEnv, "\n  - ")))
	}

	missingCommands := uniqueStringsPreserveOrder(m.checkMissingCommands(collected.commands))
	if len(missingCommands) > 0 {
		errorParts = append(errorParts,
			fmt.Sprintf("Missing commands:\n  - %s",
				strings.Join(missingCommands, "\n  - ")))
	}

	missingFiles, fileErrors := m.checkMissingFiles(ctx, collected.files)
	missingFiles = uniqueStringsPreserveOrder(missingFiles)
	if len(missingFiles) > 0 {
		errorParts = append(errorParts,
			fmt.Sprintf("Missing files:\n  - %s",
				strings.Join(missingFiles, "\n  - ")))
	}

	fileErrors = uniqueStringsPreserveOrder(fileErrors)
	if len(fileErrors) > 0 {
		errorParts = append(errorParts,
			fmt.Sprintf("File check errors:\n  - %s",
				strings.Join(fileErrors, "\n  - ")))
	}

	if len(errorParts) > 0 {
		return fmt.Errorf("pre-flight checks failed:\n\n%s\n\nPlease resolve these issues and try again",
			strings.Join(errorParts, "\n\n"))
	}

	return fmt.Errorf("pre-flight checks failed")
}

type preFlightValues struct {
	envs     []string
	commands []string
	files    []string
}

func (m *ScaffoldManager) collectPreFlightValues(conditions map[string]interface{}) preFlightValues {
	var values preFlightValues
	collectPreFlightValuesFromCondition(conditions, &values)
	return values
}

func collectPreFlightValuesFromCondition(condition interface{}, values *preFlightValues) {
	switch v := condition.(type) {
	case map[string]interface{}:
		if notValue, ok := v["not"]; ok {
			collectPreFlightValuesFromCondition(notValue, values)
			return
		}

		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := v[key]
			switch key {
			case "env_exists":
				values.envs = append(values.envs, extractStringValues(value, "env")...)
			case "command_exists":
				values.commands = append(values.commands, extractStringValues(value, "command")...)
			case "file_exists":
				values.files = append(values.files, extractStringValues(value, "file")...)
			}
		}
	case []interface{}:
		for _, item := range v {
			collectPreFlightValuesFromCondition(item, values)
		}
	case []map[string]interface{}:
		for _, item := range v {
			collectPreFlightValuesFromCondition(item, values)
		}
	}
}

func extractStringValues(value interface{}, mapKey string) []string {
	var values []string

	switch v := value.(type) {
	case string:
		values = append(values, v)
	case []string:
		values = append(values, v...)
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
	case map[string]interface{}:
		if s, ok := v[mapKey].(string); ok {
			values = append(values, s)
		}
	}

	return values
}

func uniqueStringsPreserveOrder(values []string) []string {
	if len(values) == 0 {
		return values
	}

	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}

	return unique
}

// checkMissingEnvVars returns list of environment variables that don't exist.
func (m *ScaffoldManager) checkMissingEnvVars(value interface{}) []string {
	var missing []string

	for _, envName := range extractStringValues(value, "env") {
		if _, exists := os.LookupEnv(envName); !exists {
			missing = append(missing, envName)
		}
	}

	return missing
}

// checkMissingCommands returns list of commands that don't exist in PATH.
func (m *ScaffoldManager) checkMissingCommands(value interface{}) []string {
	var missing []string

	for _, cmdName := range extractStringValues(value, "command") {
		if _, err := exec.LookPath(cmdName); err != nil {
			missing = append(missing, cmdName)
		}
	}

	return missing
}

// checkMissingFiles returns list of files that don't exist in worktree.
func (m *ScaffoldManager) checkMissingFiles(ctx *types.ScaffoldContext, value interface{}) ([]string, []string) {
	var missing []string
	var errors []string

	for _, path := range extractStringValues(value, "file") {
		fullPath := filepath.Join(ctx.WorktreePath, path)
		if _, err := os.Stat(fullPath); err != nil {
			if os.IsNotExist(err) {
				missing = append(missing, path)
				continue
			}
			errors = append(errors, fmt.Sprintf("%s: %v", path, err))
		}
	}

	return missing, errors
}
