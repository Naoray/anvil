package cli

import (
	"errors"
	"os/exec"
	"runtime"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/capabilities"
	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/workenv"
	"github.com/naoray/anvil/internal/wtstate"
)

// worktreeEnv is the shared, read-only view of one worktree's declared
// configuration and observed state used by every agent command.
type worktreeEnv struct {
	PC             *ProjectContext
	Worktree       git.Worktree
	Identity       *wtstate.Identity
	IdentityStatus wtstate.IdentityStatus
	State          *config.LocalState
	Components     []capabilities.ComponentPlan
	// ComponentErr reports unsatisfied component requirements; inspection
	// commands surface it as a warning, preparation refuses.
	ComponentErr error
	Component    *capabilities.ComponentPlan
	Env          *workenv.Environment
	PrepareEnv   *workenv.Environment
}

type worktreeEnvOptions struct {
	// Component selects one component for its directory and environment.
	Component string
	// Selected opt-in components to include in the plan.
	Selected []string
	// Resources are extra exported variables (directories, ports).
	Resources []workenv.Variable
}

// loadWorktreeEnv derives configuration, components and environment for wt
// without writing anything.
func loadWorktreeEnv(pc *ProjectContext, wt git.Worktree, opts worktreeEnvOptions) (*worktreeEnv, error) {
	target, err := pc.ForWorktree(wt.Path)
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeConfigInvalid, err)
	}
	result := &worktreeEnv{PC: target, Worktree: wt}
	if canonical, err := wtstate.CanonicalPath(wt.Path); err == nil {
		result.Worktree.Path = canonical
	}
	result.Identity, result.IdentityStatus, err = wtstate.ReadIdentity(wt.Path)
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	result.State, err = config.ReadLocalState(wt.Path)
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeInternal, err)
	}

	selected := append([]string(nil), opts.Selected...)
	if opts.Component != "" {
		selected = append(selected, opts.Component)
	}
	result.Components, result.ComponentErr = capabilities.Plan(result.Worktree.Path, target.Config, capabilities.Options{
		Selected: selected,
		GOOS:     runtime.GOOS,
		LookPath: exec.LookPath,
	})
	var unknown *capabilities.UnknownComponentError
	if errors.As(result.ComponentErr, &unknown) {
		return nil, agentapi.Errorf(agentapi.CodeComponentNotFound, "%s", unknown.Error())
	}

	var component *workenv.ComponentEnv
	if opts.Component != "" {
		for i := range result.Components {
			if result.Components[i].Name == opts.Component {
				result.Component = &result.Components[i]
				component = &workenv.ComponentEnv{Name: opts.Component, Env: result.Components[i].Env}
			}
		}
	}

	identity := workenv.Identity{Path: result.Worktree.Path, Branch: wt.Branch, Project: target.ProjectName}
	if result.Identity != nil && result.IdentityStatus != wtstate.IdentityCopied {
		identity.ID = result.Identity.ID
	}
	result.PrepareEnv = workenv.Build(workenv.Options{
		Identity:  identity,
		Config:    target.Config,
		State:     result.State,
		Resources: opts.Resources,
	})
	result.Env = result.PrepareEnv
	if component != nil {
		result.Env = workenv.Build(workenv.Options{
			Identity:  identity,
			Config:    target.Config,
			State:     result.State,
			Component: component,
			Resources: opts.Resources,
		})
	}
	return result, nil
}
