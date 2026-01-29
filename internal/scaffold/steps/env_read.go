package steps

import (
	"fmt"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
	"github.com/michaeldyrynda/arbor/internal/utils"
)

type EnvReadStep struct {
	name    string
	key     string
	storeAs string
	file    string
}

func NewEnvReadStep(cfg config.StepConfig) *EnvReadStep {
	return &EnvReadStep{
		name:    "env.read",
		key:     cfg.Key,
		storeAs: cfg.StoreAs,
		file:    cfg.File,
	}
}

func (s *EnvReadStep) Name() string {
	return s.name
}

func (s *EnvReadStep) Priority() int {
	return 0
}

func (s *EnvReadStep) Condition(ctx *types.ScaffoldContext) bool {
	return true
}

func (s *EnvReadStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	file := s.file
	if file == "" {
		file = ".env"
	}

	env := utils.ReadEnvFile(ctx.WorktreePath, file)
	if value, ok := env[s.key]; ok {
		varName := s.storeAs
		if varName == "" {
			varName = s.key
		}
		ctx.SetVar(varName, value)
		if opts.Verbose {
			fmt.Printf("  Read %s=%s from %s as %s\n", s.key, value, file, varName)
		}
		return nil
	}

	return fmt.Errorf("key '%s' not found in %s", s.key, file)
}
