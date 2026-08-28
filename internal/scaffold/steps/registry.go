package steps

import (
	"fmt"
	"sort"

	"github.com/naoray/anvil/internal/config"
	anvilexec "github.com/naoray/anvil/internal/exec"
	"github.com/naoray/anvil/internal/scaffold/types"
)

type StepFactory func(cfg config.StepConfig) types.ScaffoldStep

// Registry provides explicit step registration and creation.
// Use NewRegistry() to create an instance.
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
// Validates the configuration with the config package before creating the step.
// Returns an error if the step is not registered or the config is invalid.
func (r *Registry) Create(name string, cfg config.StepConfig) (types.ScaffoldStep, error) {
	factory, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown step %q (available: %v)", name, r.ListRegistered())
	}
	if err := config.ValidateStepConfig(name, cfg); err != nil {
		return nil, fmt.Errorf("invalid config for step %q: %w", name, err)
	}
	return factory(cfg), nil
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
	r.registerDefaults(NewYerdDatabaseClientFactory(nil))
}

// RegisterDefaultsForSiteDriver registers built-in steps with the database
// lifecycle used by the selected local site driver.
func (r *Registry) RegisterDefaultsForSiteDriver(siteDriver config.SiteDriver, commander anvilexec.Commander) {
	databaseFactory := NewYerdDatabaseClientFactory(commander)
	if siteDriver == config.SiteDriverHerd {
		databaseFactory = NewHerdDatabaseClientFactory(commander)
	}
	r.registerDefaults(databaseFactory)
}

func (r *Registry) registerDefaults(databaseFactory DatabaseClientFactory) {
	// Binary steps
	for _, b := range binaries {
		name := b.name
		binary := b.binary
		r.Register(name, func(cfg config.StepConfig) types.ScaffoldStep {
			return NewBinaryStepWithCondition(name, cfg, binary)
		})
	}

	// Other steps
	r.Register(config.StepFileCopy, func(cfg config.StepConfig) types.ScaffoldStep {
		return NewFileCopyStep(cfg.From, cfg.To)
	})

	r.Register(config.StepBashRun, func(cfg config.StepConfig) types.ScaffoldStep {
		return NewBashRunStep(cfg.Command, cfg.StoreAs)
	})

	r.Register(config.StepCommandRun, func(cfg config.StepConfig) types.ScaffoldStep {
		return NewCommandRunStep(cfg.Command, cfg.StoreAs)
	})

	r.Register(config.StepEnvRead, func(cfg config.StepConfig) types.ScaffoldStep {
		return NewEnvReadStep(cfg)
	})

	r.Register(config.StepEnvWrite, func(cfg config.StepConfig) types.ScaffoldStep {
		return NewEnvWriteStep(cfg)
	})

	r.Register(config.StepEnvCopy, func(cfg config.StepConfig) types.ScaffoldStep {
		return NewEnvCopyStep(cfg)
	})

	// Database steps use config-owned validation as well.
	r.Register(config.StepDbCreate, func(cfg config.StepConfig) types.ScaffoldStep {
		return NewDbCreateStepWithFactory(cfg, databaseFactory)
	})
	r.Register(config.StepDbDestroy, func(cfg config.StepConfig) types.ScaffoldStep {
		return NewDbDestroyStepWithFactory(cfg, databaseFactory)
	})
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
	{"yerd", "yerd"},
}
