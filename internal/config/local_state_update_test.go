package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func incrementCounter(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, UpdateLocalState(dir, func(doc map[string]any) error {
		count, _ := doc["counter"].(int)
		doc["counter"] = count + 1
		return nil
	}))
}

func TestUpdateLocalStatePreservesUnknownFieldsAndRejectsOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LocalStateFile)
	require.NoError(t, os.WriteFile(path, []byte("db_suffix: swift\nfuture_field:\n  nested: true\n"), 0o640))

	require.NoError(t, UpdateLocalState(dir, func(doc map[string]any) error {
		doc["added"] = "yes"
		return nil
	}))
	doc, err := ReadLocalStateMap(dir)
	require.NoError(t, err)
	require.Equal(t, "swift", doc["db_suffix"])
	require.Equal(t, map[string]any{"nested": true}, doc["future_field"])
	require.Equal(t, "yes", doc["added"])
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "existing mode is preserved")

	before, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Error(t, UpdateLocalState(dir, func(doc map[string]any) error {
		doc["added"] = "no"
		return fmt.Errorf("refuse")
	}))
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temporary files remain")
}

func TestUpdateLocalStateRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "elsewhere")
	require.NoError(t, os.WriteFile(target, []byte("{}\n"), 0o644))
	require.NoError(t, os.Symlink(target, filepath.Join(dir, LocalStateFile)))
	require.ErrorContains(t, UpdateLocalState(dir, func(map[string]any) error { return nil }), "regular file")
}

func TestUpdateLocalStateConcurrentGoroutinesLoseNoUpdates(t *testing.T) {
	dir := t.TempDir()
	const workers = 24
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			incrementCounter(t, dir)
		}()
	}
	wg.Wait()
	doc, err := ReadLocalStateMap(dir)
	require.NoError(t, err)
	require.Equal(t, workers, doc["counter"])
}

func TestUpdateLocalStateCrossProcessLoseNoUpdates(t *testing.T) {
	if dir := os.Getenv("ANVIL_STATE_CHILD_DIR"); dir != "" {
		n, err := strconv.Atoi(os.Getenv("ANVIL_STATE_CHILD_N"))
		require.NoError(t, err)
		for i := 0; i < n; i++ {
			incrementCounter(t, dir)
		}
		return
	}
	dir := t.TempDir()
	out, err := exec.Command("git", "init", dir).CombinedOutput()
	require.NoError(t, err, string(out))
	const processes, perProcess = 4, 15
	cmds := make([]*exec.Cmd, processes)
	for i := range cmds {
		cmds[i] = exec.Command(os.Args[0], "-test.run=^TestUpdateLocalStateCrossProcessLoseNoUpdates$")
		cmds[i].Env = append(os.Environ(), "ANVIL_STATE_CHILD_DIR="+dir, "ANVIL_STATE_CHILD_N="+strconv.Itoa(perProcess))
		require.NoError(t, cmds[i].Start())
	}
	for _, cmd := range cmds {
		require.NoError(t, cmd.Wait())
	}
	doc, err := ReadLocalStateMap(dir)
	require.NoError(t, err)
	require.Equal(t, processes*perProcess, doc["counter"])
	require.FileExists(t, filepath.Join(dir, ".git", LocalStateLockFile), "lock lives in the private git dir")
}

func TestWorktreeGitDirLinkedWorktree(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	out, err := exec.Command("git", "init", root).CombinedOutput()
	require.NoError(t, err, string(out))
	linked := filepath.Join(base, "linked")
	out, err = exec.Command("git", "-C", root, "worktree", "add", "--orphan", linked).CombinedOutput()
	require.NoError(t, err, string(out))
	gitDir, err := WorktreeGitDir(linked)
	require.NoError(t, err)
	require.Equal(t, "linked", filepath.Base(gitDir))
	require.Equal(t, filepath.Join(gitDir, "x"), WorktreeLockPath(linked, "x"))
	_, err = WorktreeGitDir(t.TempDir())
	require.Error(t, err)
}

func TestSectionRoundTrip(t *testing.T) {
	type section struct {
		Name  string   `yaml:"name"`
		Items []string `yaml:"items,omitempty"`
	}
	encoded, err := EncodeSection(section{Name: "a", Items: []string{"x"}})
	require.NoError(t, err)
	var decoded section
	require.NoError(t, DecodeSection(encoded, &decoded))
	require.Equal(t, section{Name: "a", Items: []string{"x"}}, decoded)
	content, err := yaml.Marshal(map[string]any{"s": encoded})
	require.NoError(t, err)
	require.Contains(t, string(content), "name: a")
}

func TestWorktreeLockPathFallbackIsPerUser(t *testing.T) {
	path := WorktreeLockPath(t.TempDir(), "x.lock")
	require.Equal(t, fmt.Sprintf("anvil-locks-%d", os.Getuid()), filepath.Base(filepath.Dir(path)),
		"a shared temp dir created by another user must never block this one")
}
