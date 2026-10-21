package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/capabilities"
	"github.com/naoray/anvil/internal/scaffold"
	"github.com/naoray/anvil/internal/wtstate"
)

var runCmd = &cobra.Command{
	Use:   "run [WORKTREE] -- COMMAND [ARGS...]",
	Short: "Run a command in a worktree's configured environment",
	Long: `Run COMMAND inside a worktree with Anvil's worktree environment exported.

The environment contains ANVIL_* identity variables (ANVIL_WORKTREE_ID,
ANVIL_WORKTREE_PATH, ANVIL_INSTANCE, ...), recorded database names as
ANVIL_DB_DATABASE / ANVIL_TEST_DB_DATABASE when known, the project 'env', and
with --component that component's environment (for example its virtualenv
or node_modules/.bin on PATH). Unlike 'anvil exec', run does not require or
export a testing database and never sets DB_DATABASE.

WORKTREE is matched exactly (folder, branch, path or worktree ID). Without it,
the worktree containing the current directory is used and the command runs in
the current directory. With WORKTREE the command runs at the worktree root;
--component runs it in the component directory. '--' is required before
COMMAND. COMMAND is looked up on the exported PATH (component-local tools
win) relative to the child's directory. The child's exit status is passed
through; 127 means not found and 126 means not executable.

Read-only: run never prepares, allocates or writes state. It warns on stderr
when preparation is incomplete, including the selected component's own
steps and requirements; --require-ready refuses instead.`,
	SilenceUsage:      true,
	SilenceErrors:     true,
	ValidArgsFunction: completeWorktreeNames,
	RunE:              runRun,
}

func runRun(cmd *cobra.Command, args []string) error {
	dash := cmd.ArgsLenAtDash()
	if dash < 0 {
		return agentapi.Errorf(agentapi.CodeInvalidArguments, "use 'anvil run [WORKTREE] -- COMMAND [ARGS...]'")
	}
	if dash > 1 {
		return agentapi.Errorf(agentapi.CodeInvalidArguments, "at most one WORKTREE may precede '--'")
	}
	command := args[dash:]
	if len(command) == 0 {
		return agentapi.Errorf(agentapi.CodeInvalidArguments, "COMMAND is required after '--'")
	}

	pc, err := openAgentProject()
	if err != nil {
		return err
	}
	wt, err := selectAgentWorktree(pc, args[:dash])
	if err != nil {
		return err
	}
	componentName := mustGetString(cmd, "component")
	wenv, err := loadWorktreeEnv(pc, *wt, worktreeEnvOptions{Component: componentName})
	if err != nil {
		return err
	}

	dir, err := runDirectory(wenv, dash == 1, componentName)
	if err != nil {
		return err
	}

	state, err := wtstate.ReadPrepare(wenv.Worktree.Path)
	if err != nil {
		return agentapi.Wrap(agentapi.CodeInternal, err)
	}
	status, err := wtstate.EffectivePrepareStatus(wenv.Worktree.Path, state)
	if err != nil {
		return agentapi.Wrap(agentapi.CodeInternal, err)
	}
	if status != wtstate.PrepareComplete {
		if mustGetBool(cmd, "require-ready") {
			return agentapi.Errorf(agentapi.CodeNotPrepared, "worktree preparation is %s; run 'anvil ensure' first", status)
		}
		fmt.Fprintf(os.Stderr, "warning: worktree preparation is %s; run 'anvil ensure' to prepare it\n", status)
	}
	if componentName != "" && wenv.Component != nil {
		if err := checkComponentReady(pc, wenv, mustGetBool(cmd, "require-ready")); err != nil {
			return err
		}
	}

	env := wenv.Env.Environ(os.Environ(), nil, runtime.GOOS == "windows")
	return runChildIn(dir, command, env)
}

// checkComponentReady verifies the selected component's own preparation:
// its requirements are met and each applicable step has a current
// completion record. It refuses with --require-ready and warns otherwise.
func checkComponentReady(pc *ProjectContext, wenv *worktreeEnv, requireReady bool) error {
	component := wenv.Component
	problem := ""
	if component.Status != capabilities.StatusActive {
		problem = fmt.Sprintf("component %s is %s: %s", component.Name, component.Status, component.Reason)
	} else {
		// Steps are verified against the environment ensure exports, which
		// carries no selected component.
		base, err := loadWorktreeEnv(pc, wenv.Worktree, worktreeEnvOptions{})
		if err != nil {
			return err
		}
		ready, reason, err := wenv.PC.ScaffoldManager().StepsPrepared(scaffold.ReadinessRequest{
			WorktreePath: wenv.Worktree.Path,
			Branch:       wenv.Worktree.Branch,
			Env:          base.Env.Map(),
			Config:       wenv.PC.Config,
			Steps:        component.Steps,
		})
		if err != nil {
			return agentapi.Wrap(agentapi.CodeInternal, err)
		}
		if !ready {
			problem = fmt.Sprintf("component %s is not prepared: %s", component.Name, reason)
		}
	}
	if problem == "" {
		return nil
	}
	if requireReady {
		return agentapi.Errorf(agentapi.CodeNotPrepared, "%s; run 'anvil ensure --component %s' first", problem, component.Name)
	}
	if component.Status == capabilities.StatusActive {
		fmt.Fprintf(os.Stderr, "warning: %s\n", problem)
	}
	return nil
}

// runDirectory picks the child's working directory.
func runDirectory(wenv *worktreeEnv, explicitWorktree bool, componentName string) (string, error) {
	if componentName != "" {
		if wenv.Component == nil {
			return "", agentapi.Errorf(agentapi.CodeComponentNotFound, "unknown component %q", componentName)
		}
		if wenv.Component.Status != capabilities.StatusActive {
			fmt.Fprintf(os.Stderr, "warning: component %s is %s: %s\n", componentName, wenv.Component.Status, wenv.Component.Reason)
		}
		return wenv.Component.AbsPath, nil
	}
	if explicitWorktree {
		return wenv.Worktree.Path, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", agentapi.Wrap(agentapi.CodeInternal, err)
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		return abs, nil
	}
	return cwd, nil
}

func init() {
	runCmd.Flags().String("component", "", "Run in this component's directory with its environment")
	runCmd.Flags().Bool("require-ready", false, "Refuse unless preparation is complete")
	rootCmd.AddCommand(runCmd)
}
