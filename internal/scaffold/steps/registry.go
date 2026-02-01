package steps

import (
	"fmt"
	"sort"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

type StepFactory func(cfg config.StepConfig) types.ScaffoldStep

// Registry provides explicit step registration and creation.
// Use NewRegistry() to create an instance, or use the global functions
// for backward compatibility during migration.
type Registry struct {
	factories map[string]StepFactory
	order     []string
}

// NewRegistry creates a new step registry with no registered steps.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]StepFactory),
		order:     make([]string, 0),
	}
}

// Register adds a step factory to the registry.
// Panics if a step with the same name is already registered.
func (r *Registry) Register(name string, factory StepFactory) {
	if _, exists := r.factories[name]; exists {
		panic(fmt.Sprintf("step %q already registered", name))
	}
	r.factories[name] = factory
	r.order = append(r.order, name)
}

// Create instantiates a step by name with the given configuration.
// Returns an error if the step is not registered.
func (r *Registry) Create(name string, cfg config.StepConfig) (types.ScaffoldStep, error) {
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

	// Other steps
	r.Register("file.copy", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewFileCopyStep(cfg.From, cfg.To)
	})
	r.Register("bash.run", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewBashRunStep(cfg.Command, cfg.StoreAs)
	})
	r.Register("command.run", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewCommandRunStep(cfg.Command, cfg.StoreAs)
	})
	r.Register("env.read", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewEnvReadStep(cfg)
	})
	r.Register("env.write", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewEnvWriteStep(cfg)
	})
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
