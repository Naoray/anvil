package steps

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
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
	env := make(map[string]string, len(ctx.Env)+len(t.env))
	for key, value := range ctx.Env {
		env[key] = value
	}
	// Step values expand against the worktree environment and the inherited
	// environment only — never against each other — so results do not
	// depend on map iteration order.
	for key, value := range t.env {
		env[key] = ExpandEnv(value, ctx.Env)
	}
	return dir, env, nil
}

// ExpandEnv expands ${VAR} and $VAR against primary, then the process
// environment. Unknown variables expand to the empty string, as in a shell.
func ExpandEnv(value string, primary map[string]string) string {
	return os.Expand(value, func(name string) string {
		if v, ok := primary[name]; ok {
			return v
		}
		return os.Getenv(name)
	})
}
