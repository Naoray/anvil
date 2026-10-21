package steps

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
	"github.com/naoray/anvil/internal/utils"
)

// execTarget is the optional working directory and environment of a command
// step. The zero value runs in the worktree root with the inherited
// environment, preserving pre-existing step behavior.
type execTarget struct {
	dir string
	env map[string]string
}

func newExecTarget(cfg config.StepConfig) execTarget {
	return execTarget{dir: cfg.Dir, env: cfg.Env}
}

// resolve returns the absolute working directory and the extra environment.
// Extra values are Anvil's worktree environment (ctx.Env) plus the step's own
// values, which may reference ${VAR} from the worktree or inherited
// environment (not other step values).
func (t execTarget) resolve(ctx *types.ScaffoldContext) (string, map[string]string, error) {
	dir := ctx.WorktreePath
	if t.dir != "" {
		if err := config.ValidateRelativePath(t.dir); err != nil {
			return "", nil, err
		}
		dir = filepath.Join(ctx.WorktreePath, t.dir)
		if err := config.CheckResolvedContained(ctx.WorktreePath, dir, t.dir); err != nil {
			return "", nil, err
		}
		info, err := os.Stat(dir)
		if err != nil {
			return "", nil, fmt.Errorf("step directory %s: %w", t.dir, err)
		}
		if !info.IsDir() {
			return "", nil, fmt.Errorf("step directory %s is not a directory", t.dir)
		}
	}
	if len(ctx.Env) == 0 && len(t.env) == 0 {
		return dir, nil, nil
	}
	return dir, ResolveEnv(ctx.Env, t.env), nil
}

// componentVariables are seeded into the expansion base first so component
// values can reference their own component's identity.
var componentVariables = []string{"ANVIL_COMPONENT", "ANVIL_COMPONENT_DIR"}

// ResolveEnv returns the extra environment a step receives: the worktree
// environment plus the step's own values. Step values expand against the
// worktree environment, the step's component identity and the inherited
// environment only — never against each other — so results do not depend
// on map iteration order.
func ResolveEnv(base, stepEnv map[string]string) map[string]string {
	expansion := make(map[string]string, len(base)+len(componentVariables))
	for key, value := range base {
		expansion[key] = value
	}
	for _, key := range componentVariables {
		if value, ok := stepEnv[key]; ok {
			expansion[key] = value
		}
	}
	env := make(map[string]string, len(base)+len(stepEnv))
	for key, value := range base {
		env[key] = value
	}
	for key, value := range stepEnv {
		env[key] = utils.ExpandEnv(value, expansion)
	}
	return env
}
