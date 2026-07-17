package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
