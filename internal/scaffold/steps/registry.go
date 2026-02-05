package steps

import (
	"fmt"
	"sort"

	"github.com/artisanexperiences/arbor/internal/config"
	"github.com/artisanexperiences/arbor/internal/scaffold/types"
	"github.com/artisanexperiences/arbor/internal/scaffold/validation"
)

type StepFactory func(cfg config.StepConfig) types.ScaffoldStep

// Registry provides explicit step registration and creation.
// Use NewRegistry() to create an instance, or use the global functions
// for backward compatibility during migration.
type Registry struct {
	factories  map[string]StepFactory
	validators map[string]*validation.Validator
	order      []string
}

// NewRegistry creates a new step registry with no registered steps.
func NewRegistry() *Registry {
	return &Registry{
		factories:  make(map[string]StepFactory),
		validators: make(map[string]*validation.Validator),
		order:      make([]string, 0),
	}
}

// Register adds a step factory to the registry.
// Panics if a step with the same name is already registered.
func (r *Registry) Register(name string, factory StepFactory) {
	r.RegisterWithValidator(name, factory, nil)
}

// RegisterWithValidator adds a step factory with an optional validator to the registry.
// The validator will be used to validate configuration before creating the step.
// Panics if a step with the same name is already registered.
func (r *Registry) RegisterWithValidator(name string, factory StepFactory, validator *validation.Validator) {
	if _, exists := r.factories[name]; exists {
		panic(fmt.Sprintf("step %q already registered", name))
	}
	r.factories[name] = factory
	if validator != nil {
		r.validators[name] = validator
	}
	r.order = append(r.order, name)
}

// Create instantiates a step by name with the given configuration.
// Validates the configuration before creating the step using registered validators.
// Falls back to built-in validation if no validator is registered.
// Returns an error if the step is not registered or config is invalid.
func (r *Registry) Create(name string, cfg config.StepConfig) (types.ScaffoldStep, error) {
	// Use registered validator if available
	if validator, ok := r.validators[name]; ok && validator != nil {
		if err := validator.Validate(cfg); err != nil {
			return nil, err
		}
	} else {
		// Fall back to built-in validation
		if err := config.ValidateStepConfig(name, cfg); err != nil {
			return nil, fmt.Errorf("invalid config for step %q: %w", name, err)
		}
	}

	if factory, ok := r.factories[name]; ok {
		return factory(cfg), nil
	}
	return nil, fmt.Errorf("unknown step %q (available: %v)", name, r.ListRegistered())
}

// ListRegistered returns a sorted list of all registered step names.
func (r *Registry) ListRegistered() []string {
	names := make([]string, len(r.order))
	copy(names, r.order)
	sort.Strings(names)
	return names
}

// RegisterDefaults registers all built-in steps.
func (r *Registry) RegisterDefaults() {
	// Binary steps
	for _, b := range binaries {
		name := b.name
		binary := b.binary
		r.Register(name, func(cfg config.StepConfig) types.ScaffoldStep {
			return NewBinaryStepWithCondition(name, cfg, binary)
		})
	}

	// Other steps with validators
	r.RegisterWithValidator("file.copy", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewFileCopyStep(cfg.From, cfg.To)
	}, validation.NewFileCopyValidator())

	r.RegisterWithValidator("bash.run", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewBashRunStep(cfg.Command, cfg.StoreAs)
	}, validation.NewBashRunValidator())

	r.RegisterWithValidator("command.run", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewCommandRunStep(cfg.Command, cfg.StoreAs)
	}, validation.NewCommandRunValidator())

	r.RegisterWithValidator("env.read", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewEnvReadStep(cfg)
	}, validation.NewEnvReadValidator())

	r.RegisterWithValidator("env.write", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewEnvWriteStep(cfg)
	}, validation.NewEnvWriteValidator())

	r.RegisterWithValidator("env.copy", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewEnvCopyStep(cfg)
	}, validation.NewEnvCopyValidator())

	// Steps without custom validators (use built-in validation)
	r.Register("db.create", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewDbCreateStep(cfg)
	})
	r.Register("db.destroy", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewDbDestroyStep(cfg)
	})
}

// Global registry for backward compatibility during migration.
// Deprecated: Use NewRegistry() instead for new code.
var globalRegistry = NewRegistry()

// Register adds a step factory to the global registry.
// Deprecated: Use Registry.Register() instead.
func Register(name string, factory StepFactory) {
	globalRegistry.Register(name, factory)
}

// Create instantiates a step by name using the global registry.
// Deprecated: Use Registry.Create() instead.
func Create(name string, cfg config.StepConfig) (types.ScaffoldStep, error) {
	return globalRegistry.Create(name, cfg)
}

// ListRegistered returns a sorted list of all registered steps from the global registry.
// Deprecated: Use Registry.ListRegistered() instead.
func ListRegistered() []string {
	return globalRegistry.ListRegistered()
}

type binaryDefinition struct {
	name   string
	binary string
}

var binaries = []binaryDefinition{
	{"php", "php"},
	{"php.composer", "composer"},
	{"php.laravel", "php artisan"},
	{"node.npm", "npm"},
	{"node.yarn", "yarn"},
	{"node.pnpm", "pnpm"},
	{"node.bun", "bun"},
	{"herd", "herd"},
}

func init() {
	// Initialize global registry with default steps for backward compatibility.
	// New code should use NewRegistry() and RegisterDefaults() explicitly.
	globalRegistry.RegisterDefaults()
}
