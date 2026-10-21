package steps

import (
	"fmt"
	"path/filepath"
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
	if err := types.ValidateConditions(cfg.Condition); err != nil {
		return nil, fmt.Errorf("invalid condition for step %q: %w", name, err)
	}
	step := factory(cfg)
	if len(cfg.Condition) != 0 || cfg.Enabled != nil || cfg.Dir != "" || len(cfg.Inputs)+len(cfg.Outputs) != 0 {
		return &configuredStep{ScaffoldStep: step, condition: cfg.Condition, enabled: cfg.Enabled, dir: cfg.Dir, cfg: cfg}, nil
	}
	return step, nil
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
	r.registerDefaults(managedDatabaseFactory(config.SiteDriverYerd, nil))
}

// RegisterDefaultsForSiteDriver registers built-in steps with the database
// lifecycle used by the selected local site driver.
func (r *Registry) RegisterDefaultsForSiteDriver(siteDriver config.SiteDriver, commander anvilexec.Commander) {
	r.registerDefaults(managedDatabaseFactory(siteDriver, commander))
}

// RegisterDefaultsWithDatabaseFactory registers built-in steps with an
// explicit database client factory (used by tests with mock clients).
func (r *Registry) RegisterDefaultsWithDatabaseFactory(factory DatabaseClientFactory) {
	r.registerDefaults(factory)
}

func (r *Registry) registerDefaults(databaseFactory DatabaseClientFactory) {
	// Binary steps
	for _, b := range binaries {
		name := b.name
		binary := b.binary
		r.Register(name, func(cfg config.StepConfig) types.ScaffoldStep {
			return NewBinaryStep(name, binary, cfg.Args, cfg.StoreAs).WithTarget(cfg)
		})
	}

	// Other steps
	r.Register(config.StepFileCopy, func(cfg config.StepConfig) types.ScaffoldStep {
		return NewFileCopyStep(cfg.From, cfg.To)
	})

	r.Register(config.StepBashRun, func(cfg config.StepConfig) types.ScaffoldStep {
		return NewBashRunStep(cfg.Command, cfg.StoreAs).WithTarget(cfg)
	})

	r.Register(config.StepCommandRun, func(cfg config.StepConfig) types.ScaffoldStep {
		return NewCommandRunStep(cfg.Command, cfg.StoreAs).WithTarget(cfg)
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

	r.Register("db.migrate", func(cfg config.StepConfig) types.ScaffoldStep { return &DbMigrateStep{clientFactory: databaseFactory} })
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

// configuredStep adds the same configured guard to every registered step while
// retaining the step's intrinsic eligibility (such as file or binary presence).
type configuredStep struct {
	types.ScaffoldStep
	condition map[string]any
	enabled   *bool
	dir       string
	cfg       config.StepConfig
}

func (s *configuredStep) IsEnabled() bool {
	if s.enabled != nil && !*s.enabled {
		return false
	}
	if step, ok := s.ScaffoldStep.(interface{ IsEnabled() bool }); ok {
		return step.IsEnabled()
	}
	return true
}

func (s *configuredStep) Condition(ctx *types.ScaffoldContext) (bool, error) {
	if err := ValidatePaths(ctx.WorktreePath, s.cfg); err != nil {
		return false, err
	}
	root := ctx.WorktreePath
	if s.dir != "" {
		root = filepath.Join(ctx.WorktreePath, s.dir)
	}
	eligible, err := ctx.EvaluateConditionAt(root, s.condition)
	if err != nil || !eligible {
		return false, err
	}
	return s.ScaffoldStep.Condition(ctx)
}

func (s *configuredStep) GetArgs() []string {
	if step, ok := s.ScaffoldStep.(interface{ GetArgs() []string }); ok {
		return step.GetArgs()
	}
	return nil
}
