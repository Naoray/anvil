package types

type ScaffoldContext struct {
	WorktreePath string
	Branch       string
	Preset       string
	Env          map[string]string
}

type StepOptions struct {
	Args    []string
	DryRun  bool
	Verbose bool
}

type ScaffoldStep interface {
	Name() string
	Run(ctx ScaffoldContext, opts StepOptions) error
	Priority() int
	Condition(ctx ScaffoldContext) bool
}
