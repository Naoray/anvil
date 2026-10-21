package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/agentapi"
)

// agentResult is what an agent-facing command produces: data for the JSON
// envelope plus nonfatal warnings.
type agentResult struct {
	Data     any
	Warnings []string
}

// agentCommand describes one machine-readable command invocation.
type agentCommand struct {
	Name     string
	Mutating bool
	// DryRun marks mutating commands that implement --dry-run; every other
	// mutating command refuses it before any effect.
	DryRun bool
	// Human renders a successful result for terminals; nil prints nothing.
	Human func(result agentResult) error
}

// runAgentCommand executes fn with the shared JSON contract: in --json mode
// anything fn (or code it calls) writes to stdout is redirected to stderr,
// and exactly one envelope is written to the real stdout. Failures map to
// stable exit statuses in both modes.
func runAgentCommand(cmd *cobra.Command, spec agentCommand, fn func() (agentResult, error)) error {
	jsonMode := mustGetBool(cmd, "json")
	if spec.Mutating && !spec.DryRun && mustGetBool(cmd, "dry-run") {
		// The command has no side-effect-free mode: refuse before fn runs.
		fn = func() (agentResult, error) {
			return agentResult{}, agentapi.Errorf(agentapi.CodeInvalidArguments,
				"%s does not support --dry-run (it would have effects); use read-only commands to inspect first", spec.Name)
		}
	}
	if !jsonMode {
		result, err := fn()
		for _, warning := range result.Warnings {
			fmt.Fprintln(os.Stderr, "warning: "+warning)
		}
		if err != nil {
			classified := agentapi.Wrap(agentapi.CodeInternal, err)
			return &ChildExitError{Code: classified.ExitCode(), Message: "error: " + classified.Message}
		}
		if spec.Human != nil {
			return spec.Human(result)
		}
		return nil
	}

	realStdout := os.Stdout
	os.Stdout = os.Stderr
	result, err := fn()
	os.Stdout = realStdout

	envelope := agentapi.NewEnvelope(spec.Name, spec.Mutating, result.Data, result.Warnings, err)
	if writeErr := envelope.Write(realStdout); writeErr != nil {
		return &ChildExitError{Code: agentapi.ExitGeneral, Message: fmt.Sprintf("writing JSON response: %v", writeErr)}
	}
	if err != nil {
		classified := agentapi.Wrap(agentapi.CodeInternal, err)
		fmt.Fprintln(os.Stderr, "error: "+classified.Message)
		return &ChildExitError{Code: classified.ExitCode()}
	}
	return nil
}

// addJSONFlag registers --json; JSON mode never prompts.
func addJSONFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "Write one versioned JSON document to stdout (diagnostics go to stderr)")
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	})
}

// flagError classifies cobra argument errors as invalid arguments.
func invalidArgs(err error) error {
	if err == nil {
		return nil
	}
	var classified *agentapi.Error
	if errors.As(err, &classified) {
		return err
	}
	return agentapi.Wrap(agentapi.CodeInvalidArguments, err)
}

// jsonRequested reports whether --json was given, even when flag parsing
// failed before cobra recorded it.
func jsonRequested(cmd *cobra.Command) bool {
	if cmd == nil || cmd.Flags().Lookup("json") == nil {
		return false
	}
	if flag := cmd.Flags().Lookup("json"); flag.Changed {
		return flag.Value.String() == "true"
	}
	for _, arg := range os.Args[1:] {
		if arg == "--" {
			break
		}
		if arg == "--json" || arg == "--json=true" || arg == "--json=1" {
			return true
		}
	}
	return false
}

// finishCommandError turns failures that happen before RunE (argument and
// flag validation) into the same single JSON envelope agent commands write.
func finishCommandError(cmd *cobra.Command, err error) error {
	var childErr *ChildExitError
	if errors.As(err, &childErr) || !jsonRequested(cmd) {
		return err
	}
	classified := agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	name := strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" ")
	envelope := agentapi.NewEnvelope(name, false, nil, nil, classified)
	if writeErr := envelope.Write(os.Stdout); writeErr != nil {
		return &ChildExitError{Code: agentapi.ExitGeneral, Message: fmt.Sprintf("writing JSON response: %v", writeErr)}
	}
	return &ChildExitError{Code: classified.ExitCode(), Message: "error: " + classified.Message}
}
