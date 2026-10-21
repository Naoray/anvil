package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
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
	// Human renders a successful result for terminals; nil prints nothing.
	Human func(result agentResult) error
}

// runAgentCommand executes fn with the shared JSON contract: in --json mode
// anything fn (or code it calls) writes to stdout is redirected to stderr,
// and exactly one envelope is written to the real stdout. Failures map to
// stable exit statuses in both modes.
func runAgentCommand(cmd *cobra.Command, spec agentCommand, fn func() (agentResult, error)) error {
	jsonMode := mustGetBool(cmd, "json")
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
func addJSONFlag(cmd *cobra.Command, spec agentCommand) {
	cmd.Flags().Bool("json", false, "Write one versioned JSON document to stdout (diagnostics go to stderr)")
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[agentMutatingAnnotation] = strconv.FormatBool(spec.Mutating)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	})
}

const agentMutatingAnnotation = "anvil.agent.mutating"

// jsonRequested also sees flags after a parse error, without consuming child
// arguments after -- or a value belonging to another known flag.
func jsonRequested(cmd *cobra.Command, args []string) bool {
	requested := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if strings.HasPrefix(arg, "--") {
			if name == "json" {
				requested = true
				if hasValue {
					parsed, err := strconv.ParseBool(value)
					requested = err == nil && parsed
				}
			}
			if flag := cmd.Flags().Lookup(name); flag != nil && flag.NoOptDefVal == "" && !hasValue {
				i++
			}
		} else if len(arg) == 2 && strings.HasPrefix(arg, "-") {
			if flag := cmd.Flags().ShorthandLookup(arg[1:]); flag != nil && flag.NoOptDefVal == "" {
				i++
			}
		}
	}
	return requested
}

// agentValidationError is the boundary for Cobra failures before RunE.
func agentValidationError(cmd *cobra.Command, err error) error {
	if cmd == nil {
		return err
	}
	mutating, agentEnabled := cmd.Annotations[agentMutatingAnnotation]
	var childErr *ChildExitError
	if !agentEnabled || errors.As(err, &childErr) || !jsonRequested(cmd, os.Args[1:]) {
		return err
	}
	if setErr := cmd.Flags().Set("json", "true"); setErr != nil {
		return setErr
	}
	return runAgentCommand(cmd, agentCommand{Name: cmd.Name(), Mutating: mutating == "true"}, func() (agentResult, error) {
		return agentResult{}, invalidArgs(err)
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
