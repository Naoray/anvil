package steps

import (
	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
)

func createDefaultStep(name string, cfg config.StepConfig) (types.ScaffoldStep, error) {
	registry := NewRegistry()
	registry.RegisterDefaults()
	return registry.Create(name, cfg)
}
