package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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
		resolved, err := config.ResolveContainedPath(ctx.WorktreePath, filepath.Join(ctx.WorktreePath, t.dir))
		if err != nil {
			return "", nil, err
		}
		dir = resolved
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
	return commandEnvironment(worktreeEnv, stepEnv, runtime.GOOS == "windows")
}

func commandEnvironment(worktreeEnv, stepEnv map[string]string, caseInsensitive bool) map[string]string {
	if len(worktreeEnv) == 0 && len(stepEnv) == 0 {
		return nil
	}
	normalize := func(name string) string {
		if caseInsensitive {
			return strings.ToUpper(name)
		}
		return name
	}
	base := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			base[normalize(key)] = value
		}
	}
	env := make(map[string]string, len(worktreeEnv)+len(stepEnv))
	spellings := make(map[string]string)
	set := func(key, value string) {
		name := normalize(key)
		if previous, ok := spellings[name]; ok && previous != key {
			delete(env, previous)
		}
		spellings[name] = key
		env[key] = value
	}
	for _, key := range environmentNames(worktreeEnv) {
		value := worktreeEnv[key]
		base[normalize(key)] = value
		set(key, value)
	}
	for _, key := range []string{"ANVIL_COMPONENT", "ANVIL_COMPONENT_DIR"} {
		if value, ok := stepEnv[key]; ok {
			base[normalize(key)] = value
		}
	}
	for _, key := range environmentNames(stepEnv) {
		value := stepEnv[key]
		if key == "ANVIL_COMPONENT" || key == "ANVIL_COMPONENT_DIR" {
			set(key, value)
		} else {
			set(key, os.Expand(value, func(name string) string { return base[normalize(name)] }))
		}
	}
	return env
}

func environmentNames(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
