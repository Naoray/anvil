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

type basePreset struct {
	name         string
	defaultSteps []config.StepConfig
	cleanupSteps []config.CleanupStep
}

func (p *basePreset) Name() string {
	return p.name
}

func (p *basePreset) DefaultSteps() []config.StepConfig {
	return p.defaultSteps
}

func (p *basePreset) CleanupSteps() []config.CleanupStep {
	return p.cleanupSteps
}
