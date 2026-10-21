package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureRejectsEscapingPathsBeforeAnyEffects(t *testing.T) {
	for _, option := range []string{
		"dir: escape",
		"inputs: [escape/missing]",
		"outputs: [escape/*.txt]",
		"inputs: ['*/missing']",
		"condition: {file_exists: escape/file}",
		"condition: {not: {file_contains: {file: escape/file, pattern: value}}}",
		"condition: {env_file_contains: {file: escape/file, key: VALUE}}",
		"condition: {file_exists: ../outside}",
	} {
		t.Run(option, func(t *testing.T) {
			yaml := "scaffold:\n  override: true\n  steps:\n    - name: command.run\n      command: touch earlier\n    - name: command.run\n      command: touch outside-marker\n      " + option + "\n"
			f := newAgentFixture(t, yaml, nil)
			outside := t.TempDir()
			if err := os.Symlink(outside, filepath.Join(f.repo, "escape")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			require.NoError(t, os.WriteFile(filepath.Join(outside, "file"), []byte("VALUE=value"), 0o644))
			envelope, stderr, code := f.runJSON(f.repo, "ensure")
			require.Equal(t, 5, code, stderr)
			require.Equal(t, "config_invalid", field(t, envelope, "error", "code"))
			require.NoFileExists(t, filepath.Join(outside, "outside-marker"))
			require.NoFileExists(t, filepath.Join(f.repo, "earlier"))
			require.NoFileExists(t, filepath.Join(f.repo, ".anvil.local"))
		})
	}
}

func TestEnsureAllowsInternalSymlinkPaths(t *testing.T) {
	f := newAgentFixture(t, "scaffold:\n  override: true\n  steps:\n    - name: command.run\n      command: echo good > output\n      dir: alias\n      inputs: [input]\n      outputs: [output]\n      condition: {file_exists: input}\n", map[string]string{"component/input": "value"})
	if err := os.Symlink("component", filepath.Join(f.repo, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, expected := range []string{"ran", "up_to_date"} {
		envelope, stderr, code := f.runJSON(f.repo, "ensure")
		require.Equal(t, 0, code, stderr)
		require.Equal(t, []string{expected}, stepResults(t, envelope))
	}
	require.FileExists(t, filepath.Join(f.repo, "component", "output"))
}

func TestEnsureRechecksDirectoryAfterEarlierStep(t *testing.T) {
	f := newAgentFixture(t, "scaffold:\n  override: true\n  steps:\n    - name: command.run\n      command: mv safe original; mv escape safe\n    - name: command.run\n      command: touch outside-marker\n      dir: safe\n", map[string]string{"safe/input": "value"})
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(f.repo, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	envelope, stderr, code := f.runJSON(f.repo, "ensure")
	require.Equal(t, 6, code, stderr)
	require.Equal(t, "prepare_failed", field(t, envelope, "error", "code"))
	require.NoFileExists(t, filepath.Join(outside, "outside-marker"))
}
