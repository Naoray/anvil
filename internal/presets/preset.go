package presets

import (
	"github.com/naoray/anvil/internal/config"
)

type Preset interface {
	Name() string
	Detect(path string) bool
	DefaultSteps() []config.StepConfig
	CleanupSteps() []config.CleanupStep
}

// ResolvedPreset is the immutable definition selected for one scaffold or
// cleanup run. Its step accessors return fresh copies so execution cannot
// mutate the catalog's definitions.
type ResolvedPreset struct {
	name         string
	defaultSteps []config.StepConfig
	cleanupSteps []config.CleanupStep
}

func (p ResolvedPreset) Name() string {
	return p.name
}

func (p ResolvedPreset) DefaultSteps() []config.StepConfig {
	return cloneStepConfigs(p.defaultSteps)
}

func (p ResolvedPreset) CleanupSteps() []config.CleanupStep {
	return cloneCleanupSteps(p.cleanupSteps)
}

type basePreset struct {
	name         string
	defaultSteps []config.StepConfig
	cleanupSteps []config.CleanupStep
}

func (p *basePreset) Name() string {
	return p.name
}

func (p *basePreset) DefaultSteps() []config.StepConfig {
	return cloneStepConfigs(p.defaultSteps)
}

func (p *basePreset) CleanupSteps() []config.CleanupStep {
	return cloneCleanupSteps(p.cleanupSteps)
}

func cloneStepConfigs(stepConfigs []config.StepConfig) []config.StepConfig {
	if stepConfigs == nil {
		return nil
	}

	cloned := make([]config.StepConfig, len(stepConfigs))
	for i, stepConfig := range stepConfigs {
		cloned[i] = stepConfig
		cloned[i].Args = append([]string(nil), stepConfig.Args...)
		cloned[i].Keys = append([]string(nil), stepConfig.Keys...)
		cloned[i].Condition = cloneConditionMap(stepConfig.Condition)
		if stepConfig.Enabled != nil {
			enabled := *stepConfig.Enabled
			cloned[i].Enabled = &enabled
		}
	}

	return cloned
}

func cloneCleanupSteps(cleanupSteps []config.CleanupStep) []config.CleanupStep {
	if cleanupSteps == nil {
		return nil
	}

	cloned := make([]config.CleanupStep, len(cleanupSteps))
	for i, cleanupStep := range cleanupSteps {
		cloned[i] = cleanupStep
		cloned[i].Args = append([]string(nil), cleanupStep.Args...)
		cloned[i].Condition = cloneConditionMap(cleanupStep.Condition)
	}

	return cloned
}

func cloneConditionMap(condition map[string]any) map[string]any {
	if condition == nil {
		return nil
	}

	cloned := make(map[string]any, len(condition))
	for key, value := range condition {
		cloned[key] = cloneConditionValue(value)
	}
	return cloned
}

func cloneConditionValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneConditionMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for i, item := range typed {
			cloned[i] = cloneConditionValue(item)
		}
		return cloned
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}
