package steps

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/michaeldyrynda/arbor/internal/config"
)

func TestRegistry_StepRegistration(t *testing.T) {
	t.Run("env.read step is registered", func(t *testing.T) {
		step := Create("env.read", config.StepConfig{
			Key:     "DB_DATABASE",
			StoreAs: "DatabaseName",
		})

		assert.NotNil(t, step)
		assert.Equal(t, "env.read", step.Name())
	})

	t.Run("env.write step is registered", func(t *testing.T) {
		step := Create("env.write", config.StepConfig{
			Key:   "DB_DATABASE",
			Value: "{{ .SiteName }}_{{ .DbSuffix }}",
		})

		assert.NotNil(t, step)
		assert.Equal(t, "env.write", step.Name())
	})

	t.Run("node.bun step is registered", func(t *testing.T) {
		step := Create("node.bun", config.StepConfig{
			Args: []string{"install"},
		})

		assert.NotNil(t, step)
		assert.Equal(t, "node.bun", step.Name())

		binaryStep, ok := step.(*BinaryStep)
		assert.True(t, ok, "Expected BinaryStep type")
		assert.Equal(t, "bun", binaryStep.binary)
		assert.Equal(t, []string{"install"}, binaryStep.args)
	})

	t.Run("db.create step is registered", func(t *testing.T) {
		step := Create("db.create", config.StepConfig{})

		assert.NotNil(t, step)
		assert.Equal(t, "db.create", step.Name())
	})

	t.Run("db.destroy step is registered", func(t *testing.T) {
		step := Create("db.destroy", config.StepConfig{})

		assert.NotNil(t, step)
		assert.Equal(t, "db.destroy", step.Name())
	})

	t.Run("unregistered step returns nil", func(t *testing.T) {
		step := Create("nonexistent.step", config.StepConfig{})
		assert.Nil(t, step)
	})

	t.Run("all expected steps are registered", func(t *testing.T) {
		expectedSteps := []string{
			"php",
			"php.composer",
			"php.laravel.artisan",
			"node.npm",
			"node.yarn",
			"node.pnpm",
			"node.bun",
			"herd",
			"file.copy",
			"bash.run",
			"command.run",
			"env.read",
			"env.write",
			"db.create",
			"db.destroy",
		}

		for _, stepName := range expectedSteps {
			step := Create(stepName, config.StepConfig{})
			assert.NotNil(t, step, "Step '%s' should be registered", stepName)
			assert.Equal(t, stepName, step.Name())
		}
	})
}
