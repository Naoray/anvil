package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errRootCommandRun = errors.New("root command run")

func executeCommandForFlagValidation(t *testing.T, command *cobra.Command, args []string, flags ...string) error {
	t.Helper()
	lookupFlag := func(name string) *pflag.Flag {
		if flag := rootCmd.PersistentFlags().Lookup(name); flag != nil {
			return flag
		}
		return command.Flags().Lookup(name)
	}

	originalRunE := command.RunE
	originalPersistentPreRunE := rootCmd.PersistentPreRunE
	originalSilenceUsage := rootCmd.SilenceUsage
	originalValues := make(map[string]string, len(flags))
	originalChanged := make(map[string]bool, len(flags))
	for _, name := range flags {
		flag := lookupFlag(name)
		if flag == nil {
			t.Fatalf("flag %q is not defined on %s", name, command.Name())
		}
		originalValues[name] = flag.Value.String()
		originalChanged[name] = flag.Changed
	}

	command.RunE = func(*cobra.Command, []string) error {
		return errRootCommandRun
	}
	rootCmd.PersistentPreRunE = nil
	rootCmd.SilenceUsage = true
	rootCmd.SetArgs(args)

	defer func() {
		command.RunE = originalRunE
		rootCmd.PersistentPreRunE = originalPersistentPreRunE
		rootCmd.SilenceUsage = originalSilenceUsage
		rootCmd.SetArgs(nil)
		for name, value := range originalValues {
			flag := lookupFlag(name)
			if err := flag.Value.Set(value); err != nil {
				t.Errorf("resetting %s flag: %v", name, err)
			}
			flag.Changed = originalChanged[name]
		}
	}()

	return rootCmd.Execute()
}

func TestRootCommand_RejectsVerboseAndQuietTogether(t *testing.T) {
	err := executeCommandForFlagValidation(t, rootCmd, []string{"--verbose", "--quiet"}, "verbose", "quiet")

	assert.EqualError(t, err, "if any flags in the group [verbose quiet] are set none of the others can be; [quiet verbose] were all set")
}

func TestRootCommand_AcceptsLegalOutputAndDryRunCombinations(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "normal", args: nil},
		{name: "verbose", args: []string{"--verbose"}},
		{name: "quiet", args: []string{"--quiet"}},
		{name: "dry-run", args: []string{"--dry-run"}},
		{name: "verbose dry-run", args: []string{"--verbose", "--dry-run"}},
		{name: "quiet dry-run", args: []string{"--quiet", "--dry-run"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executeCommandForFlagValidation(t, rootCmd, tt.args, "dry-run", "verbose", "quiet")

			assert.ErrorIs(t, err, errRootCommandRun)
		})
	}
}

func TestSkipFirstRunCommands_IncludesExec(t *testing.T) {
	assert.True(t, skipFirstRunCommands["exec"], "exec must never trigger the first-run wizard")
}

func TestPrintBanner_ListsExec(t *testing.T) {
	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = writer
	printBanner()
	os.Stdout = oldStdout
	require.NoError(t, writer.Close())

	var buf bytes.Buffer
	_, err = io.Copy(&buf, reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	assert.Contains(t, buf.String(), "exec")
}

func TestCompletion_IncludesExec(t *testing.T) {
	// Cobra's generated shell scripts resolve command names dynamically through
	// the hidden __complete command, so that is the surface completion uses.
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{cobra.ShellCompNoDescRequestCmd, ""})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	require.NoError(t, rootCmd.Execute())
	assert.Contains(t, strings.Split(buf.String(), "\n"), "exec")
}
