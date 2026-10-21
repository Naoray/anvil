package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComponentMetadataAvailableDuringEnvExpansion(t *testing.T) {
	yaml := "components:\n  - name: tool\n    path: tool\n    env:\n      OBSERVED: \"${ANVIL_COMPONENT_DIR}/cache\"\n      LABEL: \"${ANVIL_COMPONENT}:component\"\n    prepare:\n      - run: printf '%s|%s|%s|%s' \"$OBSERVED\" \"$LABEL\" \"$COMMAND_PATH\" \"$COMMAND_LABEL\" > observed.txt\n        env:\n          COMMAND_PATH: \"${ANVIL_COMPONENT_DIR}/command\"\n          COMMAND_LABEL: \"${ANVIL_COMPONENT}:command\"\n"
	f := newAgentFixture(t, yaml, map[string]string{"tool/README.md": "tool"})
	fakeToolchain(t, f)
	f.env = subprocessEnvFrom(append(f.env, "ANVIL_COMPONENT=stale", "ANVIL_COMPONENT_DIR=/stale"))
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/metadata")
	require.Equal(t, 0, code, stderr)
	dir := filepath.Join(f.worktreePath("feature-metadata"), "tool")
	content, err := os.ReadFile(filepath.Join(dir, "observed.txt"))
	require.NoError(t, err)
	require.Equal(t, dir+"/cache|tool:component|"+dir+"/command|tool:command", string(content))
	stdout, stderr, code := f.run(f.repo, "run", "feature-metadata", "--component", "tool", "--", "sh", "-c", "printf '%s|%s' \"$OBSERVED\" \"$LABEL\"")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, dir+"/cache|tool:component", stdout)
}
