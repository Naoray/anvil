package cli

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureValidatesSkippedComponentConditionsBeforeEffects(t *testing.T) {
	unsupported := "linux"
	if runtime.GOOS == "linux" {
		unsupported = "darwin"
	}
	cases := map[string]string{
		"opt-in component":     "    opt_in: true\n    when: {bogus_condition: x}\n",
		"unsupported platform": "    platforms: [" + unsupported + "]\n    when: {bogus_condition: x}\n    prepare:\n      - run: true\n",
		"missing requirement":  "    requires: [missing-test-tool]\n    on_missing: skip\n    prepare:\n      - run: true\n        when: {bogus_condition: x}\n",
		"merged conflict":      "    opt_in: true\n    when: {file_exists: a}\n    prepare:\n      - run: true\n        when: {file_exists: b}\n",
		"platform conflict":    "    opt_in: true\n    prepare:\n      - run: true\n        platforms: [linux]\n        when: {os: linux}\n",
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			yaml := "scaffold:\n  override: true\n  steps:\n    - name: command.run\n      command: touch command-ran\ncomponents:\n  - name: skipped\n    path: .\n" + extra
			f := newAgentFixture(t, yaml, nil)
			fakeToolchain(t, f)
			envelope, stderr, code := f.runJSON(f.repo, "ensure", "feature/invalid")
			require.Equal(t, 5, code, stderr)
			require.Equal(t, "config_invalid", field(t, envelope, "error", "code"))
			wt := f.worktreePath("feature-invalid")
			require.NoFileExists(t, filepath.Join(wt, ".anvil.local"))
			require.NoFileExists(t, filepath.Join(wt, "command-ran"))
		})
	}
}
