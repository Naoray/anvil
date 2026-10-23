package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestWorkWarmTrustedNativeAddon drives `anvil work --warm-from` through the
// real binary with fake node/npm executables: npm ci installs a dependency
// with an install script, a native addon and a node-gyp file embedding the
// worktree path; npm rebuild regenerates it for the destination.
func TestWorkWarmTrustedNativeAddon(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake toolchain")
	}
	tools := t.TempDir()
	for name, body := range map[string]string{
		"node": "#!/bin/sh\necho \"{\\\"versions\\\":{\\\"node\\\":\\\"${FAKE_NODE_VERSION:-22.0.0}\\\"},\\\"platform\\\":\\\"linux\\\",\\\"arch\\\":\\\"x64\\\"}\"\n",
		"npm": `#!/bin/sh
case "$1" in
--version) echo 10.0.0;;
config) echo '{}';;
prefix) pwd -P;;
ci)
 mkdir -p node_modules/native-dep/build/Release
 printf '%s' '{"name":"native-dep","version":"1.0.0","scripts":{"install":"node-gyp rebuild"}}' > node_modules/native-dep/package.json
 printf 'addon' > node_modules/native-dep/build/Release/addon.node
 pwd -P > node_modules/native-dep/build/config.gypi
 echo ci >> calls;;
rebuild)
 echo rebuild >> calls
 if [ -n "$FAKE_REBUILD_FAIL" ]; then echo "gyp ERR! fixture failure"; exit 1; fi
 pwd -P > node_modules/native-dep/build/config.gypi;;
*) echo "$1" >> calls;;
esac
`,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(tools, name), []byte(body), 0755))
	}
	repo, source, env := promptPolicyFixture(t, true)
	env = subprocessEnvFrom(append(env, "PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_NODE_VERSION=", "FAKE_REBUILD_FAIL="))
	files := map[string]string{
		"package.json":      `{"name":"fixture","scripts":{"postinstall":"node setup.js"}}`,
		"package-lock.json": `{"lockfileVersion":3,"packages":{"":{"name":"fixture"},"node_modules/native-dep":{"version":"1.0.0","integrity":"sha512-fixture","hasInstallScript":true}}}`,
		".gitignore":        "node_modules/\n.anvil.local\ncalls\n",
		"anvil.yaml": `preset: php
warm:
  node_modules:
    trust_install_scripts: true
    rebuild: npm rebuild
scaffold:
  override: true
  steps:
    - name: node.npm
      args: [ci]
`,
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(source, name), []byte(content), 0644))
	}
	runGitCommand(t, source, "add", ".")
	runGitCommand(t, source, "commit", "-m", "Trusted warm fixture")
	run := func(extra []string, args ...string) (string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, getAnvilBinary(t), args...)
		cmd.Dir = repo
		cmd.Env = subprocessEnvFrom(append(append([]string(nil), env...), extra...))
		data, err := cmd.CombinedOutput()
		require.NoError(t, ctx.Err(), string(data))
		return string(data), err
	}
	stampOf := func(root string) map[string]any {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, ".anvil.local"))
		if os.IsNotExist(err) {
			return nil
		}
		require.NoError(t, err)
		var state map[string]any
		require.NoError(t, yaml.Unmarshal(data, &state))
		installs, _ := state["dependency_installs"].(map[string]any)
		record, _ := installs["node_modules"].(map[string]any)
		return record
	}
	calls := func(root string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, "calls"))
		require.NoError(t, err)
		return string(data)
	}
	output, err := run(nil, "scaffold", "feature-x", "--no-interactive")
	require.NoError(t, err, output)
	require.Equal(t, "ci\n", calls(source))
	require.NotEmpty(t, stampOf(source)["tree"], output)

	work := func(name string, extra ...string) (string, string, error) {
		target := filepath.Join(filepath.Dir(source), name)
		output, err := run(extra, "work", name, target, "--base", "feature-x", "--warm-from", "feature-x", "--copy-mode", "copy", "--no-track", "--no-interactive")
		return target, output, err
	}

	target, output, err := work("warm-trusted")
	require.NoError(t, err, output)
	require.Contains(t, output, "node_modules: copied")
	require.Equal(t, "rebuild\n", calls(target), "warm hit skips npm ci and runs the declared rebuild")
	gypi, err := os.ReadFile(filepath.Join(target, "node_modules", "native-dep", "build", "config.gypi"))
	require.NoError(t, err)
	require.Equal(t, target, strings.TrimSpace(string(gypi)))
	record := stampOf(target)
	require.NotEmpty(t, record["tree"])
	require.Equal(t, source, record["source"])
	require.Equal(t, "npm rebuild", record["rebuild"])
	require.Equal(t, true, record["trust_install_scripts"])
	require.NotEmpty(t, record["trusted_scripts"])
	addon := filepath.Join("node_modules", "native-dep", "build", "Release", "addon.node")
	require.NoError(t, os.WriteFile(filepath.Join(target, addon), []byte("target changed"), 0755))
	original, err := os.ReadFile(filepath.Join(source, addon))
	require.NoError(t, err)
	require.Equal(t, "addon", string(original))

	target, output, err = work("warm-other-node", "FAKE_NODE_VERSION=23.0.0")
	require.NoError(t, err, output)
	require.Contains(t, output, "node_modules: cold")
	require.Equal(t, "ci\n", calls(target))

	target, output, err = work("warm-rebuild-fails", "FAKE_REBUILD_FAIL=1")
	require.Error(t, err, output)
	require.Contains(t, output, "gyp ERR! fixture failure")
	require.NotContains(t, output, "node_modules: copied")
	require.NoDirExists(t, filepath.Join(target, "node_modules"))
	require.Empty(t, stampOf(target))
}
