//go:build !windows

package steps

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
)

// fakeNativeToolchain installs fake node/npm executables whose versions come
// from FAKE_NODE_VERSION/FAKE_NPM_VERSION. `npm ci` installs a dependency with
// an install script, a native addon and a node-gyp file embedding the
// worktree path; `npm rebuild` regenerates that file for the current worktree.
func fakeNativeToolchain(t *testing.T) {
	t.Helper()
	tools := t.TempDir()
	for name, body := range map[string]string{
		"node": "#!/bin/sh\necho \"{\\\"versions\\\":{\\\"node\\\":\\\"${FAKE_NODE_VERSION:-22.0.0}\\\"},\\\"platform\\\":\\\"linux\\\",\\\"arch\\\":\\\"x64\\\"}\"\n",
		"npm": `#!/bin/sh
case "$1" in
--version) echo "${FAKE_NPM_VERSION:-10.0.0}";;
config) echo '{}';;
prefix) pwd -P;;
ci)
 mkdir -p node_modules/native-dep/build/Release
 printf '%s' '{"name":"native-dep","version":"1.0.0","scripts":{"install":"node-gyp rebuild"}}' > node_modules/native-dep/package.json
 printf 'addon' > node_modules/native-dep/build/Release/addon.node
 pwd -P > node_modules/native-dep/build/config.gypi
 echo ci >> calls;;
rebuild)
 echo "rebuild output"
 echo rebuild >> calls
 if [ -n "$FAKE_REBUILD_FAIL" ]; then echo "gyp ERR! fixture failure"; exit 1; fi
 pwd -P > node_modules/native-dep/build/config.gypi
 if [ "$FAKE_REBUILD_MODE" = symlink ]; then mkdir -p node_modules/.bin; ln -s "$FAKE_REBUILD_TARGET" node_modules/.bin/evil; fi
 if [ "$FAKE_REBUILD_MODE" = private ]; then echo SECRET=1 > node_modules/.env; fi;;
*) echo "$1" >> calls;;
esac
`,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(tools, name), []byte(body), 0755))
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_NODE_VERSION", "")
	t.Setenv("FAKE_NPM_VERSION", "")
	t.Setenv("FAKE_REBUILD_FAIL", "")
	t.Setenv("FAKE_REBUILD_MODE", "")
}

func writeNativeProject(t *testing.T, root string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"fixture","scripts":{"postinstall":"node setup.js"}}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{"lockfileVersion":3,"packages":{"":{"name":"fixture"},"node_modules/native-dep":{"version":"1.0.0","integrity":"sha512-fixture","hasInstallScript":true}}}`), 0644))
}

func captureStepStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	read, write, err := os.Pipe()
	require.NoError(t, err)
	original := os.Stdout
	os.Stdout = write
	runErr := fn()
	os.Stdout = original
	require.NoError(t, write.Close())
	data, err := io.ReadAll(read)
	require.NoError(t, err)
	require.NoError(t, read.Close())
	return string(data), runErr
}

func readDependencyStamp(t *testing.T, root string) map[string]any {
	t.Helper()
	value, err := config.ReadLocalStateMap(root)
	require.NoError(t, err)
	installs, _ := value["dependency_installs"].(map[string]any)
	record, _ := installs["node_modules"].(map[string]any)
	return record
}

func readCalls(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "calls"))
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return string(data)
}

func physicalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	return resolved
}

var trustedRebuild = config.WarmNodeModulesConfig{TrustInstallScripts: true, Rebuild: "npm rebuild"}

func TestTrustedNativeWarmRebuildsDestination(t *testing.T) {
	fakeNativeToolchain(t)
	source, target := t.TempDir(), t.TempDir()
	for _, root := range []string{source, target} {
		writeNativeProject(t, root)
	}
	step := NewBinaryStep("node.npm", "npm", []string{"ci"}, "")
	require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: source}, types.StepOptions{Quiet: true, WarmNodeModules: trustedRebuild}))
	require.NotEmpty(t, readDependencyStamp(t, source)["tree"])

	output, err := captureStepStdout(t, func() error {
		return step.Run(&types.ScaffoldContext{WorktreePath: target}, types.StepOptions{WarmSource: source, CopyMode: "copy", WarmNodeModules: trustedRebuild})
	})
	require.NoError(t, err)
	require.Contains(t, output, "node_modules: copied")
	require.NotContains(t, output, "rebuild output", "rebuild output is shown only on failure or verbose")
	require.Equal(t, "rebuild\n", readCalls(t, target), "warm hit skips npm ci but rebuilds")
	gypi, err := os.ReadFile(filepath.Join(target, "node_modules", "native-dep", "build", "config.gypi"))
	require.NoError(t, err)
	require.NotContains(t, string(gypi), source)

	record := readDependencyStamp(t, target)
	require.NotEmpty(t, record["tree"])
	require.Equal(t, source, record["source"])
	require.Equal(t, "npm rebuild", record["rebuild"])
	require.Equal(t, true, record["trust_install_scripts"])
	scripts, err := yaml.Marshal(record["trusted_scripts"])
	require.NoError(t, err)
	require.Contains(t, string(scripts), "node_modules/native-dep")
	require.Contains(t, string(scripts), "postinstall")
}

func TestTrustedNativeWarmToolchainChangesGoCold(t *testing.T) {
	for _, variable := range []string{"FAKE_NODE_VERSION", "FAKE_NPM_VERSION"} {
		t.Run(variable, func(t *testing.T) {
			fakeNativeToolchain(t)
			source, target := t.TempDir(), t.TempDir()
			for _, root := range []string{source, target} {
				writeNativeProject(t, root)
			}
			step := NewBinaryStep("node.npm", "npm", []string{"ci"}, "")
			require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: source}, types.StepOptions{Quiet: true, WarmNodeModules: trustedRebuild}))
			t.Setenv(variable, "99.0.0")
			output, err := captureStepStdout(t, func() error {
				return step.Run(&types.ScaffoldContext{WorktreePath: target}, types.StepOptions{WarmSource: source, CopyMode: "copy", WarmNodeModules: trustedRebuild})
			})
			require.NoError(t, err)
			require.Contains(t, output, "node_modules: cold")
			require.Equal(t, "ci\n", readCalls(t, target))
		})
	}
}

func TestTrustedNativeWarmDefaultPolicyStaysCold(t *testing.T) {
	fakeNativeToolchain(t)
	source, target := t.TempDir(), t.TempDir()
	for _, root := range []string{source, target} {
		writeNativeProject(t, root)
	}
	step := NewBinaryStep("node.npm", "npm", []string{"ci"}, "")
	require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: source}, types.StepOptions{Quiet: true}))
	require.Empty(t, readDependencyStamp(t, source))
	output, err := captureStepStdout(t, func() error {
		return step.Run(&types.ScaffoldContext{WorktreePath: target}, types.StepOptions{WarmSource: source, CopyMode: "copy"})
	})
	require.NoError(t, err)
	require.Contains(t, output, "cold")
	require.Equal(t, "ci\n", readCalls(t, target))
}

func TestTrustedNativeWarmRebuildFailureRemovesDestination(t *testing.T) {
	fakeNativeToolchain(t)
	source, target := t.TempDir(), t.TempDir()
	for _, root := range []string{source, target} {
		writeNativeProject(t, root)
	}
	step := NewBinaryStep("node.npm", "npm", []string{"ci"}, "")
	require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: source}, types.StepOptions{Quiet: true, WarmNodeModules: trustedRebuild}))
	t.Setenv("FAKE_REBUILD_FAIL", "1")
	output, err := captureStepStdout(t, func() error {
		return step.Run(&types.ScaffoldContext{WorktreePath: target}, types.StepOptions{WarmSource: source, CopyMode: "copy", WarmNodeModules: trustedRebuild})
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "gyp ERR! fixture failure")
	require.NotContains(t, output, "copied")
	require.NotContains(t, output, "cloned")
	_, statErr := os.Lstat(filepath.Join(target, "node_modules"))
	require.True(t, os.IsNotExist(statErr), "failed warm tree must be removed")
	require.Empty(t, readDependencyStamp(t, target))
	stages, globErr := filepath.Glob(filepath.Join(target, ".anvil-warm-*"))
	require.NoError(t, globErr)
	require.Empty(t, stages)
	// Source stays intact and attested.
	require.NotEmpty(t, readDependencyStamp(t, source)["tree"])
	gypi, readErr := os.ReadFile(filepath.Join(source, "node_modules", "native-dep", "build", "config.gypi"))
	require.NoError(t, readErr)
	require.Equal(t, physicalPath(t, source), strings.TrimSpace(string(gypi)))

	// A retry without the failure installs cold and cleanly.
	t.Setenv("FAKE_REBUILD_FAIL", "")
	require.NoError(t, os.Remove(filepath.Join(target, "calls")))
	output, err = captureStepStdout(t, func() error {
		return step.Run(&types.ScaffoldContext{WorktreePath: target}, types.StepOptions{WarmSource: source, CopyMode: "copy", WarmNodeModules: trustedRebuild})
	})
	require.NoError(t, err)
	require.Contains(t, output, "node_modules: copied")
}

func TestTrustedNativeWarmVerboseShowsRebuild(t *testing.T) {
	fakeNativeToolchain(t)
	source, target := t.TempDir(), t.TempDir()
	for _, root := range []string{source, target} {
		writeNativeProject(t, root)
	}
	step := NewBinaryStep("node.npm", "npm", []string{"ci"}, "")
	require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: source}, types.StepOptions{Quiet: true, WarmNodeModules: trustedRebuild}))
	output, err := captureStepStdout(t, func() error {
		return step.Run(&types.ScaffoldContext{WorktreePath: target}, types.StepOptions{Verbose: true, WarmSource: source, CopyMode: "copy", WarmNodeModules: trustedRebuild})
	})
	require.NoError(t, err)
	require.Contains(t, output, "Running: npm rebuild")
	require.Contains(t, output, "rebuild output")
}

func TestTrustedNativeWarmUncertifiableRebuildFails(t *testing.T) {
	for _, mode := range []string{"symlink", "private"} {
		t.Run(mode, func(t *testing.T) {
			fakeNativeToolchain(t)
			source, target := t.TempDir(), t.TempDir()
			for _, root := range []string{source, target} {
				writeNativeProject(t, root)
			}
			step := NewBinaryStep("node.npm", "npm", []string{"ci"}, "")
			require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: source}, types.StepOptions{Quiet: true, WarmNodeModules: trustedRebuild}))
			t.Setenv("FAKE_REBUILD_MODE", mode)
			t.Setenv("FAKE_REBUILD_TARGET", filepath.Join(source, "node_modules", "native-dep"))
			output, err := captureStepStdout(t, func() error {
				return step.Run(&types.ScaffoldContext{WorktreePath: target}, types.StepOptions{WarmSource: source, CopyMode: "copy", WarmNodeModules: trustedRebuild})
			})
			require.Error(t, err)
			require.NotContains(t, output, "copied")
			require.NotContains(t, output, "cloned")
			_, statErr := os.Lstat(filepath.Join(target, "node_modules"))
			require.True(t, os.IsNotExist(statErr), "uncertifiable warm tree must be removed")
			require.Empty(t, readDependencyStamp(t, target))
			require.DirExists(t, filepath.Join(source, "node_modules", "native-dep"))
		})
	}
}

func TestTrustedNativeWarmRetryAfterKilledRebuildInstallsCold(t *testing.T) {
	fakeNativeToolchain(t)
	source, target := t.TempDir(), t.TempDir()
	for _, root := range []string{source, target} {
		writeNativeProject(t, root)
	}
	step := NewBinaryStep("node.npm", "npm", []string{"ci"}, "")
	require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: source}, types.StepOptions{Quiet: true, WarmNodeModules: trustedRebuild}))
	// A kill -9 during the rebuild leaves a partial, unstamped tree.
	partial := filepath.Join(target, "node_modules", "native-dep", "build")
	require.NoError(t, os.MkdirAll(partial, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(partial, "config.gypi"), []byte(source), 0644))
	output, err := captureStepStdout(t, func() error {
		return step.Run(&types.ScaffoldContext{WorktreePath: target}, types.StepOptions{WarmSource: source, CopyMode: "copy", WarmNodeModules: trustedRebuild})
	})
	require.NoError(t, err)
	require.Contains(t, output, "node_modules: cold (existing unverified tree)")
	require.NotContains(t, output, "copied")
	require.Equal(t, "ci\n", readCalls(t, target), "the normal installer replaces the unverified tree")
	gypi, err := os.ReadFile(filepath.Join(partial, "config.gypi"))
	require.NoError(t, err)
	require.Equal(t, physicalPath(t, target), strings.TrimSpace(string(gypi)))
	record := readDependencyStamp(t, target)
	require.NotEmpty(t, record["tree"])
	require.Nil(t, record["source"], "a cold install records no warm source")
}
