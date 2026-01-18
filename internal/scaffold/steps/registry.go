package steps

import (
	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

type StepFactory func(cfg config.StepConfig) types.ScaffoldStep

var registry = make(map[string]StepFactory)

func Register(name string, factory StepFactory) {
	registry[name] = factory
}

func Create(name string, cfg config.StepConfig) types.ScaffoldStep {
	if factory, ok := registry[name]; ok {
		return factory(cfg)
	}
	return nil
}

type binaryDefinition struct {
	name     string
	binary   string
	priority int
}

var binaries = []binaryDefinition{
	{"php", "php", 5},
	{"php.composer", "composer", 10},
	{"php.laravel.artisan", "php artisan", 20},
	{"node.npm", "npm", 10},
	{"node.yarn", "yarn", 10},
	{"node.pnpm", "pnpm", 10},
	{"herd", "herd", 60},
}

func init() {
	for _, b := range binaries {
		name := b.name
		binary := b.binary
		defaultPriority := b.priority
		Register(name, func(cfg config.StepConfig) types.ScaffoldStep {
			priority := defaultPriority
			if cfg.Priority != 0 {
				priority = cfg.Priority
			}
			return NewBinaryStepWithCondition(name, cfg, binary, priority)
		})
	}

	Register("file.copy", func(cfg config.StepConfig) types.ScaffoldStep {
		priority := 9
		if cfg.Priority != 0 {
			priority = cfg.Priority
		}
		return NewFileCopyStep(cfg.From, cfg.To, priority)
	})
	Register("bash.run", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewBashRunStep(cfg.Command)
	})
	Register("command.run", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewCommandRunStep(cfg.Command)
	})
	Register("database.create", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewDatabaseStep(cfg, 8)
	})
}
