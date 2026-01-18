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
	{"node.npm", "npm", 10},
	{"node.yarn", "yarn", 10},
	{"node.pnpm", "pnpm", 10},
	{"php.laravel.artisan", "php", 20},
	{"herd", "herd", 60},
}

func init() {
	for _, b := range binaries {
		name := b.name
		binary := b.binary
		priority := b.priority
		Register(name, func(cfg config.StepConfig) types.ScaffoldStep {
			return NewBinaryStep(name, binary, cfg.Args, priority)
		})
	}

	Register("file.copy", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewFileCopyStep(cfg.From, cfg.To)
	})
	Register("bash.run", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewBashRunStep(cfg.Command)
	})
	Register("command.run", func(cfg config.StepConfig) types.ScaffoldStep {
		return NewCommandRunStep(cfg.Command)
	})
}
