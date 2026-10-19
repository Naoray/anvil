//go:build !windows

package steps

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestBinaryWarmHitSkipsInstallButBuildRuns(t *testing.T) {
	tools := t.TempDir()
	for name, body := range map[string]string{
		"node": "#!/bin/sh\necho fixture-node\n",
		"npm":  "#!/bin/sh\nif [ \"$1\" = --version ]; then echo fixture-npm; exit 0; fi\nif [ \"$1\" = config ]; then echo '{}'; exit 0; fi\nif [ \"$1\" = prefix ]; then pwd -P; exit 0; fi\nif [ \"$1\" = ci ]; then mkdir -p node_modules; echo installed > node_modules/value; echo ci >> calls; else echo build >> calls; fi\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(tools, name), []byte(body), 0755))
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	source, target := t.TempDir(), t.TempDir()
	for _, root := range []string{source, target} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"fixture","scripts":{"build":"build"}}`), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{"lockfileVersion":3,"packages":{}}`), 0644))
	}
	step := NewBinaryStep("node.npm", "npm", []string{"ci"}, "")
	require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: source}, types.StepOptions{Quiet: true}))
	require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: target}, types.StepOptions{Quiet: true, WarmSource: source, CopyMode: "copy"}))
	_, err := os.Stat(filepath.Join(target, "calls"))
	require.True(t, os.IsNotExist(err), "warm hit must not run npm ci")
	require.NoError(t, NewBinaryStep("node.npm", "npm", []string{"run", "build"}, "").Run(&types.ScaffoldContext{WorktreePath: target}, types.StepOptions{Quiet: true, WarmSource: source, CopyMode: "copy"}))
	calls, err := os.ReadFile(filepath.Join(target, "calls"))
	require.NoError(t, err)
	require.Equal(t, "build\n", string(calls))
}

func TestComposerWarmRefreshesAutoloadAndRejectsDifferentRootIdentity(t *testing.T) {
	for _, infer := range []bool{false, true} {
		t.Run(map[bool]string{false: "compatible", true: "inferred versions differ"}[infer], func(t *testing.T) {
			if infer {
				t.Setenv("WARM_TEST_INFER_VERSION", "1")
			} else {
				t.Setenv("WARM_TEST_INFER_VERSION", "")
			}

			tools := t.TempDir()
			t.Setenv("COMPOSER_HOME", t.TempDir())
			for name, body := range map[string]string{
				"php": "#!/bin/sh\necho fixture-php\n",
				"composer": `#!/bin/sh
if [ "$1" = --version ]; then echo fixture-composer; exit 0; fi
if [ "$1" = --no-plugins ] && [ "$2" = --no-scripts ] && [ "$3" = show ]; then
 mkdir -p "${COMPOSER_VENDOR_DIR:-vendor}"
 if [ "$WARM_TEST_INFER_VERSION" = 1 ]; then printf '{"name":"test/fixture","versions":["dev-%s"],"source":{"reference":"same"}}\n' "$(basename "$(pwd -P)")"; else echo '{"name":"test/fixture","versions":["1.0.0"],"source":{"reference":"same"}}'; fi
 exit 0
fi
if [ "$1" = --no-plugins ] && [ "$2" = config ]; then
 case "$3" in
 --global) echo "$COMPOSER_HOME";;
 vendor-dir) echo "$(pwd -P)/vendor";;
 bin-dir) echo "$(pwd -P)/vendor/bin";;
 esac
 exit 0
fi
if [ "$1" = install ]; then mkdir -p vendor; echo dependency > vendor/value; fi
basename "$(pwd -P)" > vendor/autoload-context
echo "$*" >> calls
`,
			} {
				require.NoError(t, os.WriteFile(filepath.Join(tools, name), []byte(body), 0755))
			}
			t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
			source, target := t.TempDir(), t.TempDir()
			for _, root := range []string{source, target} {
				require.NoError(t, os.WriteFile(filepath.Join(root, "composer.json"), []byte(`{"name":"test/fixture"}`), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(root, "composer.lock"), []byte(`{"packages":[],"packages-dev":[]}`), 0644))
			}
			step := NewBinaryStep("php.composer", "composer", []string{"install"}, "")
			require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: source}, types.StepOptions{Quiet: true}))
			require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: target}, types.StepOptions{Quiet: true, WarmSource: source, CopyMode: "copy"}))
			metadata, err := os.ReadFile(filepath.Join(target, "vendor", "autoload-context"))
			require.NoError(t, err)
			require.Equal(t, filepath.Base(target)+"\n", string(metadata))
			calls, err := os.ReadFile(filepath.Join(target, "calls"))
			require.NoError(t, err)
			expected := "dump-autoload --no-interaction --no-plugins --no-scripts\n"
			if infer {
				expected = "install\n"
			}
			require.Equal(t, expected, string(calls))
		})
	}
}
