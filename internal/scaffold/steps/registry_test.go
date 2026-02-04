package steps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artisanexperiences/arbor/internal/config"
	"github.com/artisanexperiences/arbor/internal/scaffold/types"
)

func TestRegistry_StepRegistration(t *testing.T) {
	t.Run("env.read step is registered", func(t *testing.T) {
		step, err := Create("env.read", config.StepConfig{
			Key:     "DB_DATABASE",
			StoreAs: "DatabaseName",
		})

		require.NoError(t, err)
		assert.Equal(t, "env.read", step.Name())
	})

	t.Run("env.write step is registered", func(t *testing.T) {
		step, err := Create("env.write", config.StepConfig{
			Key:   "DB_DATABASE",
			Value: "{{ .SiteName }}_{{ .DbSuffix }}",
		})

		require.NoError(t, err)
		assert.Equal(t, "env.write", step.Name())
	})

	t.Run("node.bun step is registered", func(t *testing.T) {
		step, err := Create("node.bun", config.StepConfig{
			Args: []string{"install"},
		})

		require.NoError(t, err)
		assert.Equal(t, "node.bun", step.Name())

		binaryStep, ok := step.(*BinaryStep)
		assert.True(t, ok, "Expected BinaryStep type")
		assert.Equal(t, "bun", binaryStep.binary)
		assert.Equal(t, []string{"install"}, binaryStep.args)
	})

	t.Run("db.create step is registered", func(t *testing.T) {
		step, err := Create("db.create", config.StepConfig{})

		require.NoError(t, err)
		assert.Equal(t, "db.create", step.Name())
	})

	t.Run("db.destroy step is registered", func(t *testing.T) {
		step, err := Create("db.destroy", config.StepConfig{})

		require.NoError(t, err)
		assert.Equal(t, "db.destroy", step.Name())
	})

	t.Run("unregistered step returns error", func(t *testing.T) {
		step, err := Create("nonexistent.step", config.StepConfig{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown step")
		assert.Contains(t, err.Error(), "nonexistent.step")
		assert.Nil(t, step)
	})

	t.Run("all expected steps are registered", func(t *testing.T) {
		// Test binary steps with minimal config
		binarySteps := []string{
			"php",
			"php.composer",
			"php.laravel",
			"node.npm",
			"node.yarn",
			"node.pnpm",
			"node.bun",
			"herd",
		}

		for _, stepName := range binarySteps {
			step, err := Create(stepName, config.StepConfig{})
			require.NoError(t, err, "Binary step '%s' should be registered", stepName)
			assert.Equal(t, stepName, step.Name())
		}

		// Test steps with required fields
		testCases := []struct {
			stepName string
			cfg      config.StepConfig
		}{
			{"file.copy", config.StepConfig{From: "a.txt", To: "b.txt"}},
			{"bash.run", config.StepConfig{Command: "echo test"}},
			{"command.run", config.StepConfig{Command: "echo test"}},
			{"env.read", config.StepConfig{Key: "TEST_KEY"}},
			{"env.write", config.StepConfig{Key: "TEST_KEY"}},
			{"db.create", config.StepConfig{}},
			{"db.destroy", config.StepConfig{}},
		}

		for _, tc := range testCases {
			step, err := Create(tc.stepName, tc.cfg)
			require.NoError(t, err, "Step '%s' should be registered", tc.stepName)
			assert.Equal(t, tc.stepName, step.Name())
		}
	})
}

func TestRegistry_ListRegistered(t *testing.T) {
	names := ListRegistered()

	assert.NotEmpty(t, names)
	assert.Contains(t, names, "php")
	assert.Contains(t, names, "env.read")
	assert.Contains(t, names, "env.write")
	assert.Contains(t, names, "db.create")
	assert.Contains(t, names, "db.destroy")

	// Verify list is sorted
	for i := 1; i < len(names); i++ {
		assert.LessOrEqual(t, names[i-1], names[i], "List should be sorted alphabetically")
	}
}

func TestRegistry_DuplicateRegistration(t *testing.T) {
	t.Run("duplicate registration panics", func(t *testing.T) {
		// This test creates a temporary registry to test panic behavior
		// without affecting the global registry
		defer func() {
			r := recover()
			assert.NotNil(t, r, "Expected panic for duplicate registration")
			assert.Contains(t, r, "already registered")
		}()

		// Register a duplicate step - this should panic
		Register("php", func(cfg config.StepConfig) types.ScaffoldStep {
			return nil
		})
	})
}

// mockStep is a test double for scaffold steps
type mockStep struct {
	name string
}

func (m *mockStep) Name() string {
	return m.name
}

func (m *mockStep) Description() string {
	return "mock step"
}

func (m *mockStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	return nil
}

func (m *mockStep) Condition(ctx *types.ScaffoldContext) bool {
	return true
}

// Tests for the explicit Registry struct
func TestExplicitRegistry_NewRegistry(t *testing.T) {
	t.Run("creates empty registry", func(t *testing.T) {
		registry := NewRegistry()

		require.NotNil(t, registry)
		assert.Empty(t, registry.ListRegistered())
	})
}

func TestExplicitRegistry_Register(t *testing.T) {
	t.Run("registers step successfully", func(t *testing.T) {
		registry := NewRegistry()

		factory := func(cfg config.StepConfig) types.ScaffoldStep {
			return &mockStep{name: cfg.Name}
		}

		registry.Register("test.step", factory)

		registered := registry.ListRegistered()
		assert.Len(t, registered, 1)
		assert.Contains(t, registered, "test.step")
	})

	t.Run("maintains registration order", func(t *testing.T) {
		registry := NewRegistry()

		factory := func(cfg config.StepConfig) types.ScaffoldStep {
			return &mockStep{name: cfg.Name}
		}

		registry.Register("zebra.step", factory)
		registry.Register("alpha.step", factory)
		registry.Register("mike.step", factory)

		registered := registry.ListRegistered()
		// Should be sorted alphabetically
		assert.Equal(t, []string{"alpha.step", "mike.step", "zebra.step"}, registered)
	})

	t.Run("panics on duplicate registration", func(t *testing.T) {
		registry := NewRegistry()

		factory := func(cfg config.StepConfig) types.ScaffoldStep {
			return &mockStep{name: cfg.Name}
		}

		registry.Register("duplicate.test", factory)

		assert.Panics(t, func() {
			registry.Register("duplicate.test", factory)
		})
	})
}

func TestExplicitRegistry_Create(t *testing.T) {
	t.Run("creates registered step", func(t *testing.T) {
		registry := NewRegistry()

		factory := func(cfg config.StepConfig) types.ScaffoldStep {
			return &mockStep{name: cfg.Name}
		}

		registry.Register("create.test", factory)

		cfg := config.StepConfig{Name: "create.test"}
		step, err := registry.Create("create.test", cfg)

		require.NoError(t, err)
		assert.NotNil(t, step)
		assert.Equal(t, "create.test", step.Name())
	})

	t.Run("returns error for unknown step", func(t *testing.T) {
		registry := NewRegistry()

		cfg := config.StepConfig{Name: "unknown.step"}
		step, err := registry.Create("unknown.step", cfg)

		assert.Error(t, err)
		assert.Nil(t, step)
		assert.Contains(t, err.Error(), "unknown step")
	})
}

func TestExplicitRegistry_RegisterDefaults(t *testing.T) {
	t.Run("registers all default steps", func(t *testing.T) {
		registry := NewRegistry()
		registry.RegisterDefaults()

		registered := registry.ListRegistered()
		assert.Len(t, registered, 16) // 8 binary steps + 8 other steps

		// Verify all expected steps are present
		expectedSteps := []string{
			"bash.run",
			"command.run",
			"db.create",
			"db.destroy",
			"env.copy",
			"env.read",
			"env.write",
			"file.copy",
			"herd",
			"node.bun",
			"node.npm",
			"node.pnpm",
			"node.yarn",
			"php",
			"php.composer",
			"php.laravel",
		}

		for _, stepName := range expectedSteps {
			assert.Contains(t, registered, stepName, "Step '%s' should be registered", stepName)
		}
	})

	t.Run("can create steps after registering defaults", func(t *testing.T) {
		registry := NewRegistry()
		registry.RegisterDefaults()

		testCases := []struct {
			name string
			cfg  config.StepConfig
		}{
			{"php", config.StepConfig{Name: "php", Args: []string{"-v"}}},
			{"file.copy", config.StepConfig{Name: "file.copy", From: "a.txt", To: "b.txt"}},
			{"bash.run", config.StepConfig{Name: "bash.run", Command: "echo hello"}},
			{"env.read", config.StepConfig{Name: "env.read", Key: "TEST_KEY"}},
			{"db.create", config.StepConfig{Name: "db.create", Type: "mysql"}},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				step, err := registry.Create(tc.name, tc.cfg)
				require.NoError(t, err, "Failed to create step %s", tc.name)
				assert.NotNil(t, step)
			})
		}
	})
}

func TestExplicitRegistry_Isolation(t *testing.T) {
	t.Run("registries are isolated from each other", func(t *testing.T) {
		registry1 := NewRegistry()
		registry2 := NewRegistry()

		factory := func(cfg config.StepConfig) types.ScaffoldStep {
			return &mockStep{name: cfg.Name}
		}

		// Register step in registry1 only
		registry1.Register("isolated.step", factory)

		// registry2 should not have it
		_, err := registry2.Create("isolated.step", config.StepConfig{Name: "isolated.step"})
		assert.Error(t, err, "Expected error when creating step from isolated registry2")

		// registry1 should have it
		step, err := registry1.Create("isolated.step", config.StepConfig{Name: "isolated.step"})
		require.NoError(t, err)
		assert.NotNil(t, step)
	})
}
