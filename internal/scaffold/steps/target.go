package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
// values, which may reference ${VAR} from either the worktree or inherited
// environment.
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
	return dir, CommandEnvironment(ctx.Env, t.env), nil
}

// CommandEnvironment expands step values against an immutable snapshot of
// inherited/worktree values plus the component identity. It is shared by
// execution and preparation cache identities.
func CommandEnvironment(worktreeEnv, stepEnv map[string]string) map[string]string {
	if len(worktreeEnv) == 0 && len(stepEnv) == 0 {
		return nil
	}
	base := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			base[key] = value
		}
	}
	env := make(map[string]string, len(worktreeEnv)+len(stepEnv))
	for key, value := range worktreeEnv {
		base[key] = value
		env[key] = value
	}
	for _, key := range []string{"ANVIL_COMPONENT", "ANVIL_COMPONENT_DIR"} {
		if value, ok := stepEnv[key]; ok {
			base[key] = value
		}
	}
	for key, value := range stepEnv {
		if key == "ANVIL_COMPONENT" || key == "ANVIL_COMPONENT_DIR" {
			env[key] = value
		} else {
			env[key] = os.Expand(value, func(name string) string { return base[name] })
		}
	}
	return env
}
