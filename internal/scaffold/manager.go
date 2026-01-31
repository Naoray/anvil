package scaffold

import (
	"fmt"
	"path/filepath"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/scaffold/steps"
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
	"github.com/michaeldyrynda/arbor/internal/scaffold/words"
)

type ScaffoldManager struct {
	presets     map[string]Preset
	presetOrder []string
}

type Preset interface {
	Name() string
	Detect(path string) bool
	DefaultSteps() []config.StepConfig
	CleanupSteps() []config.CleanupStep
}

func NewScaffoldManager() *ScaffoldManager {
	return &ScaffoldManager{
		presets:     make(map[string]Preset),
		presetOrder: make([]string, 0),
	}
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
			step, err := steps.Create(stepConfig.Name, stepConfig)
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
			step, err := steps.Create(cleanupConfig.Name, stepConfig)
			if err != nil {
				return nil, fmt.Errorf("creating cleanup step %q: %w", cleanupConfig.Name, err)
			}
			stepsList = append(stepsList, step)
		}
	}

	for _, cleanupConfig := range cfg.Cleanup.Steps {
		stepConfig := m.cleanupConfigToStepConfig(cleanupConfig)
		step, err := steps.Create(cleanupConfig.Name, stepConfig)
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
		step, err := steps.Create(cfg.Name, cfg)
		if err != nil {
			return nil, fmt.Errorf("creating step %q: %w", cfg.Name, err)
		}
		stepsList = append(stepsList, step)
	}

	return stepsList, nil
}

func (m *ScaffoldManager) RunScaffold(worktreePath, branch, repoName, siteName, preset string, cfg *config.Config, dryRun, verbose, quiet bool) error {
	ctx := m.newScaffoldContext(worktreePath, branch, repoName, siteName, preset)

	worktreeConfig, err := config.ReadWorktreeConfig(worktreePath)
	if err != nil {
		return fmt.Errorf("reading worktree config: %w", err)
	}

	if worktreeConfig.DbSuffix == "" {
		newSuffix := words.GenerateSuffix()
		ctx.SetDbSuffix(newSuffix)
		if !dryRun {
			if err := config.WriteWorktreeConfig(worktreePath, map[string]string{"db_suffix": newSuffix}); err != nil {
				return fmt.Errorf("writing db_suffix to worktree config: %w", err)
			}
		}
	} else {
		ctx.SetDbSuffix(worktreeConfig.DbSuffix)
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
