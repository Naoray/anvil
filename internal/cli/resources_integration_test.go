package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/procs"
)

const resourcesFixtureYAML = `env:
  APP_URL: http://127.0.0.1:${ANVIL_PORT_WEB}
resources:
  directories: [index-out]
  ports:
    - name: web
      env: VITE_PORT
    - name: api
  shared:
    - name: models
      path: ${ANVIL_TEST_SHARED}
      env: MODEL_DIR
      required: true
processes:
  - name: web
    command: echo "$VITE_PORT" > "$ANVIL_STATE_DIR/web.port"; exec sleep 60
    stop_timeout: 2s
`

func envValue(t *testing.T, f *agentFixture, wt string, names ...string) []string {
	t.Helper()
	script := ""
	for _, name := range names {
		script += `printf '%s\n' "$` + name + `";`
	}
	stdout, stderr, code := f.run(f.repo, "run", wt, "--", "sh", "-c", script)
	require.Equal(t, 0, code, stderr)
	return strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
}

func processRecords(t *testing.T, wt string) map[string]any {
	t.Helper()
	doc, err := config.ReadLocalStateMap(wt)
	require.NoError(t, err)
	records, _ := doc["processes"].(map[string]any)
	return records
}

func TestResourcesPortsDirectoriesProcessesAndCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("managed processes require macOS or Linux")
	}
	shared := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(shared, "weights.bin"), []byte("immutable"), 0o444))
	f := newAgentFixture(t, resourcesFixtureYAML, nil)
	f.env = subprocessEnvFrom(append(f.env, "ANVIL_TEST_SHARED="+shared, "ANVIL_PORT_RANGE=43100-43199"))

	for _, branch := range []string{"feature/a", "feature/b"} {
		_, stderr, code := f.runJSON(f.repo, "ensure", branch)
		require.Equal(t, 0, code, stderr)
	}
	a := envValue(t, f, "feature-a", "ANVIL_PORT_WEB", "VITE_PORT", "ANVIL_PORT_API", "ANVIL_STATE_DIR", "ANVIL_DIR_INDEX_OUT", "MODEL_DIR", "APP_URL", "ANVIL_TMP_DIR")
	b := envValue(t, f, "feature-b", "ANVIL_PORT_WEB", "ANVIL_PORT_API", "ANVIL_STATE_DIR")
	require.Equal(t, a[0], a[1], "alias exports the same port")
	require.NotEqual(t, a[0], a[2])
	require.NotContains(t, []string{a[0], a[2]}, b[0], "no collisions across worktrees")
	require.NotContains(t, []string{a[0], a[2]}, b[1])
	require.NotEqual(t, a[3], b[2], "separate state directories")
	require.DirExists(t, a[3])
	require.DirExists(t, a[4])
	require.DirExists(t, a[7])
	require.Equal(t, shared, a[5], "shared resources are used in place")
	require.Equal(t, "http://127.0.0.1:"+a[0], a[6])
	require.False(t, strings.HasPrefix(a[3], f.worktreePath("feature-a")), "runtime state lives outside the working tree")

	_, _, code := f.runJSON(f.repo, "ensure", "feature-a")
	require.Equal(t, 0, code)
	require.Equal(t, a[0], envValue(t, f, "feature-a", "ANVIL_PORT_WEB")[0], "ports are stable across ensure")

	// Managed process lifecycle.
	envelope, stderr, code := f.runJSON(f.repo, "start", "feature-a")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "started", field(t, envelope, "data", "processes", 0, "result"))
	pid := int(field(t, envelope, "data", "processes", 0, "pid").(float64))
	require.Eventually(t, func() bool {
		content, err := os.ReadFile(filepath.Join(a[3], "web.port"))
		return err == nil && strings.TrimSpace(string(content)) == a[0]
	}, 5*time.Second, 20*time.Millisecond, "process received its port")
	envelope, _, code = f.runJSON(f.repo, "start", "feature-a")
	require.Equal(t, 0, code)
	require.Equal(t, "already_running", field(t, envelope, "data", "processes", 0, "result"))

	// start has no side-effect-free mode; stop previews with --dry-run.
	_, _, code = f.runJSON(f.repo, "start", "feature-a", "--dry-run")
	require.Equal(t, 2, code)
	envelope, _, code = f.runJSON(f.repo, "stop", "feature-a", "--dry-run")
	require.Equal(t, 0, code)
	require.Equal(t, "would_stop", field(t, envelope, "data", "processes", 0, "result"))
	_, err := procs.StartIdentity(pid)
	require.NoError(t, err, "dry-run never signals")

	envelope, stderr, code = f.runJSON(f.repo, "stop", "feature-a")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "terminated", field(t, envelope, "data", "processes", 0, "result"))
	require.Empty(t, processRecords(t, f.worktreePath("feature-a")))
	identity, err := procs.StartIdentity(pid)
	require.Error(t, err, "process gone: %s", identity)

	// PID reuse: a record whose identity no longer matches is never signaled.
	envelope, _, code = f.runJSON(f.repo, "start", "feature-b")
	require.Equal(t, 0, code)
	victim := int(field(t, envelope, "data", "processes", 0, "pid").(float64))
	wtB := f.worktreePath("feature-b")
	tamperProcessIdentity(t, wtB, "web", "linux:someone-else:1")
	envelope, _, code = f.runJSON(f.repo, "stop", "feature-b")
	require.Equal(t, 0, code)
	require.Equal(t, "not_signaled_pid_reused", field(t, envelope, "data", "processes", 0, "result"))
	victimIdentity, err := procs.StartIdentity(victim)
	require.NoError(t, err, "unrelated process left running")
	_, err = procs.Stop(victim, victimIdentity, 2*time.Second)
	require.NoError(t, err)

	// Cleanup: dry-run changes nothing; removal stops, releases and deletes.
	_, _, code = f.runJSON(f.repo, "start", "feature-a")
	require.Equal(t, 0, code)
	stdout, stderr, code := f.run(f.repo, "remove", "feature-a", "--force", "--dry-run")
	require.Equal(t, 0, code, stdout+stderr)
	combined := stdout + stderr
	require.Contains(t, combined, "Would stop managed process web")
	require.Contains(t, combined, "Would release port "+a[0])
	require.Contains(t, combined, "Would remove runtime directory")
	require.DirExists(t, a[3])
	require.Len(t, processRecords(t, f.worktreePath("feature-a")), 1)

	records := processRecords(t, f.worktreePath("feature-a"))
	running := records["web"].(map[string]any)["pid"].(int)
	stdout, stderr, code = f.run(f.repo, "remove", "feature-a", "--force")
	require.Equal(t, 0, code, stdout+stderr)
	require.NoDirExists(t, f.worktreePath("feature-a"))
	require.NoDirExists(t, filepath.Dir(a[3]), "runtime directory removed")
	_, err = procs.StartIdentity(running)
	require.Error(t, err, "owned process stopped")
	require.FileExists(t, filepath.Join(shared, "weights.bin"), "shared resources preserved")
	require.Equal(t, b[0], envValue(t, f, "feature-b", "ANVIL_PORT_WEB")[0], "other worktree's port untouched")

	// The released port becomes available to new worktrees.
	_, _, code = f.runJSON(f.repo, "ensure", "feature/c")
	require.Equal(t, 0, code)
	portC, err := strconv.Atoi(envValue(t, f, "feature-c", "ANVIL_PORT_WEB")[0])
	require.NoError(t, err)
	require.NotZero(t, portC)
}

func TestEnsureRefusesMissingRequiredSharedResource(t *testing.T) {
	f := newAgentFixture(t, resourcesFixtureYAML, nil)
	f.env = subprocessEnvFrom(append(f.env, "ANVIL_TEST_SHARED="+filepath.Join(t.TempDir(), "absent")))
	envelope, _, code := f.runJSON(f.repo, "ensure", "feature/x")
	require.Equal(t, 6, code)
	require.Contains(t, field(t, envelope, "error", "message"), "required shared resources are missing")
	require.NoFileExists(t, filepath.Join(f.worktreePath("feature-x"), ".anvil.local"))
}

func tamperProcessIdentity(t *testing.T, wt, name, identity string) {
	t.Helper()
	path := filepath.Join(wt, config.LocalStateFile)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(content, &doc))
	doc["processes"].(map[string]any)[name].(map[string]any)["identity"] = identity
	content, err = yaml.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, content, 0o644))
}
