package capabilities

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
)

// Component plan statuses.
const (
	StatusActive  = "active"
	StatusSkipped = "skipped"
)

// ComponentPlan is the resolved, side-effect-free view of one component on
// this machine.
type ComponentPlan struct {
	Name      string              `json:"name"`
	Path      string              `json:"path"`
	AbsPath   string              `json:"abs_path"`
	Uses      []string            `json:"uses"`
	Requires  []string            `json:"requires"`
	Missing   []string            `json:"missing,omitempty"`
	Platforms []string            `json:"platforms,omitempty"`
	OptIn     bool                `json:"opt_in"`
	Status    string              `json:"status"`
	Reason    string              `json:"reason,omitempty"`
	Env       map[string]string   `json:"-"`
	Steps     []config.StepConfig `json:"-"`
}

// Options select components for one operation.
type Options struct {
	// Selected names opt-in components to include. Naming a component also
	// makes missing requirements fatal for it.
	Selected []string
	GOOS     string
	LookPath func(string) (string, error)
}

// Plan resolves every declared component. It fails when a selected component
// does not exist, or when an applicable component with on_missing: fail lacks
// required commands, so preparation can refuse before any effect.
func Plan(worktreePath string, cfg *config.Config, opts Options) ([]ComponentPlan, error) {
	if err := validateConditions(cfg.Components); err != nil {
		return nil, &InvalidPlanError{Err: err}
	}
	selected := map[string]bool{}
	for _, name := range opts.Selected {
		if _, ok := cfg.FindComponent(name); !ok {
			return nil, &UnknownComponentError{Name: name}
		}
		selected[name] = true
	}
	var problems []error
	plans := make([]ComponentPlan, 0, len(cfg.Components))
	for _, component := range cfg.Components {
		plan, err := planComponent(worktreePath, component, selected[component.Name], opts)
		if err != nil {
			problems = append(problems, err)
		}
		plans = append(plans, plan)
	}
	return plans, errors.Join(problems...)
}

// UnknownComponentError reports a selected component that is not declared.
type UnknownComponentError struct{ Name string }

func (e *UnknownComponentError) Error() string {
	return fmt.Sprintf("unknown component %q", e.Name)
}

func planComponent(worktreePath string, component config.Component, selected bool, opts Options) (ComponentPlan, error) {
	dir := component.ComponentDir()
	absDir := filepath.Join(worktreePath, dir)
	plan := ComponentPlan{
		Name:      component.Name,
		Path:      dir,
		AbsPath:   absDir,
		Uses:      append([]string{}, component.Uses...),
		OptIn:     component.OptIn,
		Status:    StatusActive,
		Platforms: append([]string(nil), component.Platforms...),
	}
	if plan.Path == "" {
		plan.Path = "."
	}

	contributions := make([]Contribution, 0, len(component.Uses))
	for _, capability := range component.Uses {
		contribution := Contribute(capability, absDir, opts.GOOS)
		if contribution.Err != nil {
			return plan, &InvalidPlanError{Err: fmt.Errorf("component %q: %w", component.Name, contribution.Err)}
		}
		contributions = append(contributions, contribution)
	}
	if len(plan.Platforms) == 0 {
		for _, contribution := range contributions {
			plan.Platforms = append(plan.Platforms, contribution.Platforms...)
		}
	}
	plan.Requires = uniqueSorted(append(append([]string(nil), component.Requires...), requiresOf(contributions)...))
	plan.Env = componentEnv(component, contributions, absDir, opts.GOOS)

	switch {
	case component.OptIn && !selected:
		return skipped(plan, "opt-in component not requested"), nil
	case len(plan.Platforms) > 0 && !contains(plan.Platforms, opts.GOOS):
		return skipped(plan, fmt.Sprintf("platform %s not in %s", opts.GOOS, strings.Join(plan.Platforms, ", "))), nil
	}
	if info, err := os.Stat(absDir); err != nil || !info.IsDir() {
		plan = skipped(plan, fmt.Sprintf("directory %s does not exist", plan.Path))
		if component.OnMissing == config.OnMissingSkip && !selected {
			return plan, nil
		}
		return plan, fmt.Errorf("component %q: directory %s does not exist", component.Name, plan.Path)
	}
	// A component directory that escapes through a symlink is refused
	// regardless of on_missing: commands would run outside the worktree.
	if dir != "" {
		if err := config.CheckContained(worktreePath, dir); err != nil {
			return skipped(plan, err.Error()), fmt.Errorf("component %q: %w", component.Name, err)
		}
	}

	for _, command := range plan.Requires {
		if _, err := opts.LookPath(command); err != nil {
			plan.Missing = append(plan.Missing, command)
		}
	}
	if len(plan.Missing) > 0 {
		plan = skipped(plan, "missing required commands: "+strings.Join(plan.Missing, ", "))
		if component.OnMissing == config.OnMissingSkip && !selected {
			return plan, nil
		}
		return plan, fmt.Errorf("component %q: missing required commands: %s", component.Name, strings.Join(plan.Missing, ", "))
	}

	steps, err := compileSteps(component, contributions, dir, plan.Env)
	if err != nil {
		return skipped(plan, err.Error()), fmt.Errorf("component %q: %w", component.Name, err)
	}
	plan.Steps = steps
	return plan, nil
}

func contributionsFor(component config.Component, absDir, goos string) []Contribution {
	contributions := make([]Contribution, 0, len(component.Uses))
	for _, capability := range component.Uses {
		contributions = append(contributions, Contribute(capability, absDir, goos))
	}
	return contributions
}

// ValidationSteps compiles the preparation steps of every declared
// component, applicable on this machine or not, so callers can validate all
// conditions before any effect. A component condition without prepare
// commands is represented by a no-op step carrying it.
func ValidationSteps(worktreePath string, cfg *config.Config, goos string) ([]config.StepConfig, error) {
	var steps []config.StepConfig
	var problems []error
	for _, component := range cfg.Components {
		dir := component.ComponentDir()
		absDir := filepath.Join(worktreePath, dir)
		contributions := contributionsFor(component, absDir, goos)
		compiled, err := compileSteps(component, contributions, dir, componentEnv(component, contributions, absDir, goos))
		if err != nil {
			problems = append(problems, fmt.Errorf("component %q: %w", component.Name, err))
			continue
		}
		if len(compiled) == 0 && len(component.When) > 0 {
			compiled = []config.StepConfig{{Name: config.StepCommandRun, Command: "true", ConditionHolder: config.ConditionHolder{Condition: component.When}}}
		}
		steps = append(steps, compiled...)
	}
	return steps, errors.Join(problems...)
}

func skipped(plan ComponentPlan, reason string) ComponentPlan {
	plan.Status = StatusSkipped
	plan.Reason = reason
	plan.Steps = nil
	return plan
}

func requiresOf(contributions []Contribution) []string {
	var out []string
	for _, contribution := range contributions {
		out = append(out, contribution.Requires...)
	}
	return out
}

// componentEnv merges capability and component environment. PATH entries
// are absolute so commands find component-local tools from any directory.
func componentEnv(component config.Component, contributions []Contribution, absDir, goos string) map[string]string {
	env := map[string]string{"ANVIL_COMPONENT": component.Name, "ANVIL_COMPONENT_DIR": absDir}
	var pathPrepend []string
	for _, contribution := range contributions {
		for key, value := range contribution.Env {
			env[key] = strings.ReplaceAll(value, "${ANVIL_COMPONENT_DIR}", absDir)
		}
		for _, entry := range contribution.PathPrepend {
			pathPrepend = append(pathPrepend, filepath.Join(absDir, entry))
		}
	}
	if len(pathPrepend) > 0 {
		separator := string(os.PathListSeparator)
		if goos == "windows" {
			separator = ";"
		}
		env["PATH"] = strings.Join(pathPrepend, separator) + separator + "${PATH}"
	}
	for key, value := range component.Env {
		env[key] = value
	}
	return env
}

func compileSteps(component config.Component, contributions []Contribution, dir string, env map[string]string) ([]config.StepConfig, error) {
	var commands []config.PrepareCommand
	if !component.Override {
		for _, contribution := range contributions {
			for _, command := range contribution.Prepare {
				prepared := config.PrepareCommand{Run: command.Run, Outputs: command.Outputs}
				for _, input := range command.Inputs {
					prepared.Inputs = append(prepared.Inputs, filepath.ToSlash(filepath.Join(command.Dir, input)))
				}
				if command.Dir != "" {
					prepared.Run = "cd " + shellQuote(filepath.ToSlash(command.Dir)) + " && " + command.Run
					for i, output := range prepared.Outputs {
						prepared.Outputs[i] = filepath.ToSlash(filepath.Join(command.Dir, output))
					}
				}
				commands = append(commands, prepared)
			}
		}
	}
	commands = append(commands, component.Prepare...)

	steps := make([]config.StepConfig, 0, len(commands))
	for _, command := range commands {
		condition, err := mergeConditions(component.When, command.When)
		if err != nil {
			return nil, err
		}
		if len(command.Platforms) > 0 {
			if _, exists := condition["os"]; exists {
				return nil, fmt.Errorf("prepare command %q: platforms conflicts with an os condition", command.Run)
			}
			if condition == nil {
				condition = map[string]any{}
			}
			platforms := make([]any, len(command.Platforms))
			for i, platform := range command.Platforms {
				platforms[i] = platform
			}
			condition["os"] = platforms
		}
		stepEnv := make(map[string]string, len(env)+len(command.Env))
		for key, value := range env {
			stepEnv[key] = value
		}
		for key, value := range command.Env {
			stepEnv[key] = value
		}
		steps = append(steps, config.StepConfig{
			Name:            config.StepCommandRun,
			Command:         command.Run,
			Dir:             dir,
			Env:             stepEnv,
			Inputs:          append([]string(nil), command.Inputs...),
			Outputs:         append([]string(nil), command.Outputs...),
			Always:          command.Always,
			ConditionHolder: config.ConditionHolder{Condition: condition},
		})
	}
	return steps, nil
}

func mergeConditions(first, second map[string]any) (map[string]any, error) {
	if len(first) == 0 && len(second) == 0 {
		return nil, nil
	}
	merged := make(map[string]any, len(first)+len(second))
	for key, value := range first {
		merged[key] = value
	}
	for key, value := range second {
		if _, exists := merged[key]; exists {
			return nil, fmt.Errorf("condition %q is set on both the component and its prepare command", key)
		}
		merged[key] = value
	}
	return merged, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

// Steps flattens the preparation steps of active components in order.
func Steps(plans []ComponentPlan) []config.StepConfig {
	var steps []config.StepConfig
	for _, plan := range plans {
		if plan.Status == StatusActive {
			steps = append(steps, plan.Steps...)
		}
	}
	return steps
}

// InvalidPlanError identifies configuration errors independently of missing tools.
type InvalidPlanError struct{ Err error }

func (e *InvalidPlanError) Error() string { return e.Err.Error() }
func (e *InvalidPlanError) Unwrap() error { return e.Err }

func validateConditions(components []config.Component) error {
	for _, component := range components {
		if err := types.ValidateConditions(component.When); err != nil {
			return fmt.Errorf("component %q: %w", component.Name, err)
		}
		steps, err := compileSteps(component, nil, component.ComponentDir(), nil)
		if err != nil {
			return fmt.Errorf("component %q: %w", component.Name, err)
		}
		for _, step := range steps {
			if err := types.ValidateConditions(step.Condition); err != nil {
				return fmt.Errorf("component %q prepare command: %w", component.Name, err)
			}
		}
	}
	return nil
}
