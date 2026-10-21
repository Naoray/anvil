package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureFailingChildOutputStaysOnStderr(t *testing.T) {
	for _, name := range []string{"command.run", "bash.run", "php"} {
		t.Run(name, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("shell output fixture requires a POSIX shell")
			}
			command := "echo SYNTHETIC_STDOUT_24; echo SYNTHETIC_STDERR_24 >&2; exit 1"
			step := fmt.Sprintf("    - name: %s\n", name)
			if name != "php" {
				step += fmt.Sprintf("      command: %q\n", command)
			}
			f := newAgentFixture(t, "scaffold:\n  override: true\n  steps:\n"+step, nil)
			if name == "php" {
				binDir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(binDir, "php"), []byte("#!/bin/sh\n"+command+"\n"), 0o755))
				f.env = append(f.env, "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			}
			envelope, stderr, code := f.runJSON(f.repo, "ensure")
			require.Equal(t, 6, code, stderr)
			require.Equal(t, false, envelope["ok"])
			require.Equal(t, "prepare_failed", field(t, envelope, "error", "code"))
			require.Equal(t, []string{"failed"}, stepResults(t, envelope))
			encoded, err := json.Marshal(envelope)
			require.NoError(t, err)
			for _, marker := range []string{"SYNTHETIC_STDOUT_24", "SYNTHETIC_STDERR_24"} {
				require.NotContains(t, string(encoded), marker)
				require.Contains(t, stderr, marker)
			}
		})
	}
}
