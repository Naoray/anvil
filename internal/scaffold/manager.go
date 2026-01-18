package scaffold

import (
	"fmt"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/scaffold/steps"
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

type ScaffoldManager struct {
	presets map[string]Preset
}

type Preset interface {
	Name() string
	Detect(path string) bool
	DefaultSteps() []types.ScaffoldStep
	CleanupSteps() []types.ScaffoldStep
}

func NewScaffoldManager() *ScaffoldManager {
	return &ScaffoldManager{
		presets: make(map[string]Preset),
	}
}

func (m *ScaffoldManager) RegisterPreset(preset Preset) {
	m.presets[preset.Name()] = preset
}

func (m *ScaffoldManager) GetPreset(name string) (Preset, bool) {
	preset, ok := m.presets[name]
	return preset, ok
}

func (m *ScaffoldManager) DetectPreset(path string) string {
	for _, preset := range m.presets {
		if preset.Detect(path) {
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
		stepsList = append(stepsList, preset.DefaultSteps()...)
	}

	if cfg.Scaffold.Override {
		stepsList = m.stepsFromConfig(cfg.Scaffold.Steps, worktreePath, branch)
	} else {
		additionalSteps := m.stepsFromConfig(cfg.Scaffold.Steps, worktreePath, branch)
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
		stepsList = append(stepsList, preset.CleanupSteps()...)
	}

	for _, cleanupStep := range cfg.Cleanup {
		step := m.createStepFromCleanup(cleanupStep, worktreePath, branch)
		if step != nil {
			stepsList = append(stepsList, step)
		}
	}

	return stepsList, nil
}

func (m *ScaffoldManager) stepsFromConfig(stepConfigs []config.StepConfig, worktreePath, branch string) []types.ScaffoldStep {
	stepsList := make([]types.ScaffoldStep, 0, len(stepConfigs))

	for _, cfg := range stepConfigs {
		step := steps.Create(cfg.Name, cfg)
		if step != nil {
			stepsList = append(stepsList, step)
		}
	}

	return stepsList
}

func (m *ScaffoldManager) createStepFromCleanup(cleanup config.CleanupStep, worktreePath, branch string) types.ScaffoldStep {
	stepConfig := config.StepConfig{
		Name: cleanup.Name,
		Args: nil,
	}

	if cleanup.Name == "herd" {
		stepConfig.Args = []string{"unlink"}
	}

	for k, v := range cleanup.Condition {
		if k == "command" {
			if cmd, ok := v.(string); ok {
				stepConfig.Command = cmd
			}
		}
	}

	return steps.Create(cleanup.Name, stepConfig)
}

func (m *ScaffoldManager) RunScaffold(worktreePath, branch, preset string, cfg *config.Config, dryRun, verbose bool) error {
	ctx := types.ScaffoldContext{
		WorktreePath: worktreePath,
		Branch:       branch,
		Preset:       preset,
		Env:          make(map[string]string),
	}

	stepsList, err := m.GetStepsForWorktree(cfg, worktreePath, branch)
	if err != nil {
		return fmt.Errorf("getting scaffold steps: %w", err)
	}

	opts := types.StepOptions{
		DryRun:  dryRun,
		Verbose: verbose,
	}

	executor := NewStepExecutor(stepsList, ctx, opts)
	if err := executor.Execute(); err != nil {
		return err
	}

	return nil
}

func (m *ScaffoldManager) RunCleanup(worktreePath, branch, preset string, cfg *config.Config, dryRun, verbose bool) error {
	ctx := types.ScaffoldContext{
		WorktreePath: worktreePath,
		Branch:       branch,
		Preset:       preset,
		Env:          make(map[string]string),
	}

	stepsList, err := m.GetCleanupSteps(cfg, worktreePath, branch)
	if err != nil {
		return fmt.Errorf("getting cleanup steps: %w", err)
	}

	opts := types.StepOptions{
		DryRun:  dryRun,
		Verbose: verbose,
	}

	executor := NewStepExecutor(stepsList, ctx, opts)
	if err := executor.Execute(); err != nil {
		return err
	}

	return nil
}
