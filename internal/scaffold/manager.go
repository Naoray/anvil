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
	presets map[string]Preset
}

type Preset interface {
	Name() string
	Detect(path string) bool
	DefaultSteps() []config.StepConfig
	CleanupSteps() []config.CleanupStep
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
		for _, stepConfig := range preset.DefaultSteps() {
			step := steps.Create(stepConfig.Name, stepConfig)
			if step != nil {
				stepsList = append(stepsList, step)
			}
		}
	}

	if cfg.Scaffold.Override {
		stepsList = m.stepsFromConfig(cfg.Scaffold.Steps)
	} else {
		additionalSteps := m.stepsFromConfig(cfg.Scaffold.Steps)
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
			step := steps.Create(cleanupConfig.Name, stepConfig)
			if step != nil {
				stepsList = append(stepsList, step)
			}
		}
	}

	for _, cleanupConfig := range cfg.Cleanup.Steps {
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
		step := steps.Create(cleanupConfig.Name, stepConfig)
		if step != nil {
			stepsList = append(stepsList, step)
		}
	}

	return stepsList, nil
}

func (m *ScaffoldManager) stepsFromConfig(stepConfigs []config.StepConfig) []types.ScaffoldStep {
	stepsList := make([]types.ScaffoldStep, 0, len(stepConfigs))

	for _, cfg := range stepConfigs {
		step := steps.Create(cfg.Name, cfg)
		if step != nil {
			stepsList = append(stepsList, step)
		}
	}

	return stepsList
}

func (m *ScaffoldManager) RunScaffold(worktreePath, branch, repoName, siteName, preset string, cfg *config.Config, dryRun, verbose, quiet bool) error {
	path := filepath.Base(worktreePath)
	repoPath := filepath.Base(filepath.Dir(worktreePath))
	ctx := types.ScaffoldContext{
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

	opts := types.StepOptions{
		DryRun:  dryRun,
		Verbose: verbose,
		Quiet:   quiet,
	}

	executor := NewStepExecutor(stepsList, &ctx, opts)
	if err := executor.Execute(); err != nil {
		return err
	}

	return nil
}

func (m *ScaffoldManager) RunCleanup(worktreePath, branch, repoName, siteName, preset string, cfg *config.Config, dryRun, verbose, quiet bool) error {
	path := filepath.Base(worktreePath)
	repoPath := filepath.Base(filepath.Dir(worktreePath))
	ctx := types.ScaffoldContext{
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

	stepsList, err := m.GetCleanupSteps(cfg, worktreePath, branch)
	if err != nil {
		return fmt.Errorf("getting cleanup steps: %w", err)
	}

	opts := types.StepOptions{
		DryRun:  dryRun,
		Verbose: verbose,
		Quiet:   quiet,
	}

	executor := NewStepExecutor(stepsList, &ctx, opts)
	if err := executor.Execute(); err != nil {
		return err
	}

	return nil
}
