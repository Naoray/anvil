package steps

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestRegistryCreateMatchesDirectConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.StepConfig
	}{
		{name: "php", cfg: config.StepConfig{}},
		{name: "php.composer", cfg: config.StepConfig{}},
		{name: "php.laravel", cfg: config.StepConfig{}},
		{name: "node.npm", cfg: config.StepConfig{}},
		{name: "node.yarn", cfg: config.StepConfig{}},
		{name: "node.pnpm", cfg: config.StepConfig{}},
		{name: "node.bun", cfg: config.StepConfig{}},
		{name: "herd", cfg: config.StepConfig{}},
		{name: "yerd", cfg: config.StepConfig{}},
		{name: "file.copy valid", cfg: config.StepConfig{Name: config.StepFileCopy, From: "source.txt", To: "dest.txt"}},
		{name: "file.copy missing from", cfg: config.StepConfig{Name: config.StepFileCopy, To: "dest.txt"}},
		{name: "bash.run valid", cfg: config.StepConfig{Name: config.StepBashRun, Command: "echo hello"}},
		{name: "bash.run missing command", cfg: config.StepConfig{Name: config.StepBashRun}},
		{name: "command.run valid", cfg: config.StepConfig{Name: config.StepCommandRun, Command: "echo hello"}},
		{name: "command.run missing command", cfg: config.StepConfig{Name: config.StepCommandRun}},
		{name: "env.read valid", cfg: config.StepConfig{Name: config.StepEnvRead, Key: "DB_DATABASE"}},
		{name: "env.read missing key", cfg: config.StepConfig{Name: config.StepEnvRead}},
		{name: "env.write valid", cfg: config.StepConfig{Name: config.StepEnvWrite, Key: "DB_DATABASE"}},
		{name: "env.write missing key", cfg: config.StepConfig{Name: config.StepEnvWrite}},
		{name: "env.copy key only", cfg: config.StepConfig{Name: config.StepEnvCopy, Source: "../main", Key: "DB_DATABASE"}},
		{name: "env.copy keys only", cfg: config.StepConfig{Name: config.StepEnvCopy, Source: "../main", Keys: []string{"DB_DATABASE", "DB_USERNAME"}}},
		{name: "env.copy missing key and keys", cfg: config.StepConfig{Name: config.StepEnvCopy, Source: "../main"}},
		{name: "db.create valid", cfg: config.StepConfig{Name: config.StepDbCreate, Type: "mysql"}},
		{name: "db.create invalid role", cfg: config.StepConfig{Name: config.StepDbCreate, Role: "invalid"}},
		{name: "db.destroy valid", cfg: config.StepConfig{Name: config.StepDbDestroy}},
	}

	registry := NewRegistry()
	registry.RegisterDefaults()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stepName := tt.cfg.Name
			switch tt.name {
			case "php", "php.composer", "php.laravel", "node.npm", "node.yarn", "node.pnpm", "node.bun", "herd", "yerd":
				stepName = tt.name
			}

			directErr := config.ValidateStepConfig(stepName, tt.cfg)
			step, createErr := registry.Create(stepName, tt.cfg)

			if directErr != nil {
				require.Error(t, createErr)
				assert.Nil(t, step)
				assert.EqualError(t, createErr, fmt.Sprintf("invalid config for step %q: %s", stepName, directErr))
				return
			}

			require.NoError(t, createErr)
			assert.NotNil(t, step)
		})
	}
}

func TestRegistryCreateUnknownStepChecksRegistrationBeforeValidation(t *testing.T) {
	registry := NewRegistry()

	step, err := registry.Create("unknown.step", config.StepConfig{})

	assert.Nil(t, step)
	assert.EqualError(t, err, `unknown step "unknown.step" (available: [])`)
}

func TestRegistryCreateCustomRegistrationUsesDefaultConfigValidation(t *testing.T) {
	registry := NewRegistry()
	factoryCalled := false
	registry.Register("custom.step", func(cfg config.StepConfig) types.ScaffoldStep {
		factoryCalled = true
		return &mockStep{name: cfg.Name}
	})

	cfg := config.StepConfig{}
	directErr := config.ValidateStepConfig("custom.step", cfg)
	step, err := registry.Create("custom.step", cfg)

	require.NoError(t, directErr)
	require.NoError(t, err)
	assert.True(t, factoryCalled)
	assert.NotNil(t, step)
}
