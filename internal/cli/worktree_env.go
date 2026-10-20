package cli

import (
	"errors"
	"os/exec"
	"runtime"
	"sort"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/capabilities"
	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/resources"
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

	// Observed runtime resources (never created by loadWorktreeEnv).
	RuntimeRoot string
	Directories *resources.Directories
	Ports       []resources.PortState
	Shared      []resources.SharedStatus
	// SharedErr reports missing required shared resources.
	SharedErr error
	// Warnings are nonfatal inspection problems.
	Warnings []string

	Metadata wtstate.Metadata
	Lease    *wtstate.Lease
	// Unset lists inherited variables removed for this worktree's role.
	Unset []string
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
	if result.Metadata, err = wtstate.ReadMetadata(wt.Path); err != nil {
		return nil, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	if result.Lease, err = wtstate.ReadLease(wt.Path); err != nil {
		return nil, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	if result.Metadata.Role == wtstate.RoleReview {
		result.Unset = append([]string(nil), target.Config.Roles.Review.EnvUnset...)
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
	resourceVars := result.describeResources(identity.ID)
	result.Env = workenv.Build(workenv.Options{
		Identity:  identity,
		Config:    target.Config,
		State:     result.State,
		Component: component,
		Resources: append(resourceVars, opts.Resources...),
	})
	return result, nil
}

// describeResources reports directories, port reservations and shared
// resources without creating or allocating anything.
func (w *worktreeEnv) describeResources(worktreeID string) []workenv.Variable {
	var vars []workenv.Variable
	cfg := w.PC.Config
	statuses, sharedVars, err := resources.DescribeShared(cfg)
	w.Shared, w.SharedErr = statuses, err
	if err != nil {
		w.Warnings = append(w.Warnings, err.Error())
	}
	root, err := resources.Root()
	if err != nil {
		w.Warnings = append(w.Warnings, "runtime directory unavailable: "+err.Error())
		return sharedVars
	}
	w.RuntimeRoot = root
	if worktreeID == "" {
		return sharedVars
	}
	dirs, dirVars, err := resources.DescribeDirectories(root, worktreeID, cfg)
	if err != nil {
		w.Warnings = append(w.Warnings, err.Error())
		return sharedVars
	}
	w.Directories = &dirs
	vars = append(vars, dirVars...)
	if ports, err := resources.NewPorts(root); err != nil {
		w.Warnings = append(w.Warnings, err.Error())
	} else if states, err := ports.Lookup(worktreeID); err != nil {
		w.Warnings = append(w.Warnings, err.Error())
	} else {
		w.Ports = states
		reservations := make([]resources.Reservation, len(states))
		for i, state := range states {
			reservations[i] = state.Reservation
		}
		vars = append(vars, resources.PortVariables(cfg, reservations)...)
		if len(states) < len(cfg.Resources.Ports) {
			w.Warnings = append(w.Warnings, "declared ports are not allocated yet; run 'anvil ensure'")
		}
	}
	return append(vars, sharedVars...)
}

// provisionResources creates directories and allocates ports for a worktree
// with a recorded identity. It is the only resource-mutating path.
func provisionResources(w *worktreeEnv) error {
	if w.Identity == nil || w.IdentityStatus == wtstate.IdentityCopied {
		return agentapi.Errorf(agentapi.CodeInternal, "worktree has no identity; run 'anvil ensure'")
	}
	root, err := resources.Root()
	if err != nil {
		return agentapi.Wrap(agentapi.CodeInternal, err)
	}
	if _, _, err := resources.ProvisionDirectories(root, w.Identity.ID, w.Worktree.Path, w.PC.Config); err != nil {
		return agentapi.Wrap(agentapi.CodeResourceBusy, err)
	}
	if len(w.PC.Config.Resources.Ports) == 0 {
		return nil
	}
	ports, err := resources.NewPorts(root)
	if err != nil {
		return agentapi.Wrap(agentapi.CodeConfigInvalid, err)
	}
	names := make([]string, len(w.PC.Config.Resources.Ports))
	for i, port := range w.PC.Config.Resources.Ports {
		names[i] = port.Name
	}
	if _, err := ports.Allocate(w.Identity.ID, w.Worktree.Path, names); err != nil {
		return agentapi.Wrap(agentapi.CodeResourceBusy, err)
	}
	return nil
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// variableFor classifies an ad-hoc command variable.
func variableFor(name, value string, cfg *config.Config) workenv.Variable {
	return workenv.Variable{Name: name, Value: value, Source: workenv.SourceProject, Secret: workenv.IsSecretName(name, cfg.SecretEnv)}
}
