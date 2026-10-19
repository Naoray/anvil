package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/naoray/anvil/internal/config"
)

func TestWorkWarmTargetConfigRuntimeGuardAndDatabaseState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX runtime command fixture")
	}
	for _, tool := range []string{"npm", "node"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " required for real dependency integration")
		}
	}
	var payload bytes.Buffer
	gz := gzip.NewWriter(&payload)
	tw := tar.NewWriter(gz)
	for name, data := range map[string]string{"package/package.json": `{"name":"anvil-warm-fixture","version":"1.0.0","main":"index.js"}`, "package/index.js": `module.exports="ready";`} {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(data))}))
		_, err := tw.Write([]byte(data))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write(payload.Bytes())
		if err != nil {
			t.Log(err)
		}
	}))
	defer server.Close()
	digest := sha512.Sum512(payload.Bytes())
	repo, source, env := promptPolicyFixture(t, true)
	env = append(env, "NPM_CONFIG_CACHE="+t.TempDir(), "NPM_CONFIG_AUDIT=false", "NPM_CONFIG_FUND=false", "NPM_CONFIG_UPDATE_NOTIFIER=false")
	manifest := map[string]any{"name": "warm-cli-fixture", "version": "1.0.0", "dependencies": map[string]string{"anvil-warm-fixture": "1.0.0"}, "scripts": map[string]string{"build": "node build.js"}}
	lock := map[string]any{"name": "warm-cli-fixture", "version": "1.0.0", "lockfileVersion": 3, "requires": true, "packages": map[string]any{"": manifest, "node_modules/anvil-warm-fixture": map[string]any{"version": "1.0.0", "resolved": server.URL + "/fixture.tgz", "integrity": "sha512-" + base64.StdEncoding.EncodeToString(digest[:])}}}
	for name, value := range map[string]any{"package.json": manifest, "package-lock.json": lock} {
		data, err := json.Marshal(value)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(source, name), data, 0644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(source, "build.js"), []byte(`const fs=require('fs');if(require('anvil-warm-fixture')!=='ready')throw Error('wrong dependency');fs.writeFileSync('built','ready');`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(source, ".gitignore"), []byte("node_modules/\n.anvil.local\nbuilt\nmatched\nskipped\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(source, "anvil.yaml"), []byte(`preset: php
scaffold:
  override: true
  steps:
    - name: command.run
      command: git rev-parse --is-inside-work-tree
      store_as: IsWorktree
    - name: node.npm
      args: [ci]
      condition:
        context_var: {name: IsWorktree, value: "true"}
    - name: node.npm
      args: [run, build]
    - name: file.copy
      from: built
      to: matched
      condition:
        context_var: {name: IsWorktree, value: "true"}
    - name: file.copy
      from: built
      to: skipped
      condition:
        context_var: {name: IsWorktree, value: "false"}
`), 0644))
	runGitCommand(t, source, "add", ".")
	runGitCommand(t, source, "commit", "-m", "Combined warm fixture")
	endpoint := &config.DatabaseEndpoint{Driver: "herd", Service: "mysql", Host: "127.0.0.1", Port: "3307"}
	sourceBinding := &config.BorrowedApplication{SourcePath: repo, Name: "source_borrowed", Engine: "mysql", Driver: "herd", Service: "mysql", Host: "127.0.0.1", Port: "3307"}
	initial := config.LocalState{DbSuffix: "source_suffix", BorrowedApplication: sourceBinding, Databases: []config.OwnedDatabase{{Name: "source_history", Engine: "mysql", Role: config.DbRoleAuxiliary, Endpoint: endpoint}, {Name: "source_testing", Engine: "mysql", Role: config.DbRoleTesting, Endpoint: endpoint}}}
	require.NoError(t, config.WriteLocalState(source, initial))
	run := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, getAnvilBinary(t), args...)
		cmd.Dir = repo
		cmd.Env = env
		data, err := cmd.CombinedOutput()
		require.NoError(t, ctx.Err(), string(data))
		require.NoError(t, err, string(data))
		return string(data)
	}
	readRaw := func(root string) map[string]any {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, ".anvil.local"))
		require.NoError(t, err)
		var state map[string]any
		require.NoError(t, yaml.Unmarshal(data, &state))
		return state
	}
	// Database writer first, then successful cold install/provenance writer.
	run("scaffold", "feature-x", "--no-interactive")
	actual, err := config.ReadLocalState(source)
	require.NoError(t, err)
	require.Equal(t, &initial, actual)
	require.NotEmpty(t, readRaw(source)["dependency_installs"])
	// Malformed invoking YAML cannot override the selected target's committed plan.
	require.NoError(t, os.WriteFile(filepath.Join(repo, "anvil.yaml"), []byte("scaffold: ["), 0644))
	for _, mode := range []string{"auto", "copy"} {
		target := filepath.Join(filepath.Dir(source), "warm-"+mode)
		output := run("work", "warm-"+mode, target, "--base", "feature-x", "--warm-from", "feature-x", "--copy-mode", mode, "--no-track", "--no-interactive")
		if mode == "copy" {
			require.Contains(t, output, "node_modules: copied")
		} else if runtime.GOOS == "darwin" {
			require.Contains(t, output, "node_modules: cloned")
		} else {
			require.Regexp(t, `node_modules: (cloned|copied)`, output)
		}
		data, err := os.ReadFile(filepath.Join(target, "matched"))
		require.NoError(t, err)
		require.Equal(t, "ready", string(data))
		require.NoFileExists(t, filepath.Join(target, "skipped"))
		state, err := config.ReadLocalState(target)
		require.NoError(t, err)
		require.Nil(t, state.BorrowedApplication)
		require.Empty(t, state.Databases)
		require.NotEqual(t, initial.DbSuffix, state.DbSuffix)
		stamp := readRaw(target)["dependency_installs"]
		require.NotEmpty(t, stamp)
		// Successful warm provenance first, then database writer; history and endpoint are retained by subsequent stamp writes.
		own := config.LocalState{Databases: []config.OwnedDatabase{{Name: "target_history", Engine: "mysql", Role: config.DbRoleAuxiliary, Endpoint: endpoint}, {Name: "target_testing", Engine: "mysql", Role: config.DbRoleTesting, Endpoint: endpoint}}, BorrowedApplication: &config.BorrowedApplication{SourcePath: repo, Name: "target_borrowed", Engine: "mysql", Driver: "herd", Service: "mysql", Host: "127.0.0.1", Port: "3307"}}
		require.NoError(t, config.WriteLocalState(target, own))
		require.Equal(t, stamp, readRaw(target)["dependency_installs"])
		run("scaffold", "warm-"+mode, "--no-interactive")
		state, err = config.ReadLocalState(target)
		require.NoError(t, err)
		require.Equal(t, own.Databases, state.Databases)
		require.Equal(t, own.BorrowedApplication, state.BorrowedApplication)
		require.NotEmpty(t, readRaw(target)["dependency_installs"])
		require.NoError(t, os.WriteFile(filepath.Join(target, "node_modules", "anvil-warm-fixture", "index.js"), []byte("target changed"), 0644))
	}
	original, err := os.ReadFile(filepath.Join(source, "node_modules", "anvil-warm-fixture", "index.js"))
	require.NoError(t, err)
	require.Equal(t, `module.exports="ready";`, string(original))
}
