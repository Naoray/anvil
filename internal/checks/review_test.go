package checks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/lifecycle"
	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/workenv"
)

// targetEnv is a command environment with extra values, as a component or
// command override would produce.
func (f fixture) targetEnv(extra map[string]string) func(config.CheckCommand) (CommandTarget, error) {
	return func(config.CheckCommand) (CommandTarget, error) {
		env := f.env()
		for key, value := range extra {
			env.Set(workenv.Variable{Name: key, Value: value})
		}
		return CommandTarget{Dir: f.worktree, Env: env}, nil
	}
}

func (f fixture) identityWith(t *testing.T, check config.CheckConfig, resolve func(config.CheckCommand) (CommandTarget, error)) Identity {
	t.Helper()
	identity, err := ComputeIdentity(context.Background(), IdentityRequest{WorktreePath: f.worktree, RuntimeDir: f.runtime, Check: check, Env: f.env(), Resolve: resolve})
	require.NoError(t, err)
	return identity
}

func TestIdentityHashesEachCommandsEffectiveEnvironment(t *testing.T) {
	f := newFixture(t)
	check := config.CheckConfig{Name: "env", Commands: cmds(`test "$MODE" = pass`)}
	pass := f.identityWith(t, check, f.targetEnv(map[string]string{"MODE": "pass"}))
	fail := f.identityWith(t, check, f.targetEnv(map[string]string{"MODE": "fail"}))
	require.NotEqual(t, pass.EnvDigest, fail.EnvDigest, "component/command values are part of the identity")
	require.Contains(t, pass.EnvNames, "MODE")

	t.Setenv("PR27_INHERITED", "a")
	a := f.identityWith(t, check, f.targetEnv(nil))
	t.Setenv("PR27_INHERITED", "b")
	b := f.identityWith(t, check, f.targetEnv(nil))
	require.NotEqual(t, a.EnvDigest, b.EnvDigest, "inherited values reach commands, so they are hashed")

	t.Setenv("SHLVL", "3")
	before := f.identityWith(t, check, f.targetEnv(nil))
	t.Setenv("SHLVL", "4")
	require.NotEqual(t, before.EnvDigest, f.identityWith(t, check, f.targetEnv(nil)).EnvDigest, "every inherited non-secret value actually received affects identity")

	t.Setenv("PR27_TOKEN", "one")
	secretA := f.identityWith(t, check, f.targetEnv(nil))
	t.Setenv("PR27_TOKEN", "two")
	secretB := f.identityWith(t, check, f.targetEnv(nil))
	require.Equal(t, secretA.EnvDigest, secretB.EnvDigest, "secret values are never hashed")
	require.Contains(t, secretA.EnvNames, "PR27_TOKEN")
}

func TestInputSymlinksAreContainedAndHashedByTarget(t *testing.T) {
	f := newFixture(t)
	outside := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(outside, []byte("good"), 0o600))
	dir := filepath.Join(f.worktree, "fixture")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "input")))

	check := config.CheckConfig{Name: "in", Commands: cmds("true"), Inputs: []string{"fixture"}}
	_, err := ComputeIdentity(context.Background(), IdentityRequest{WorktreePath: f.worktree, RuntimeDir: f.runtime, Check: check, Env: f.env()})
	require.ErrorContains(t, err, "symlink", "links cannot yield a reusable identity")

	require.NoError(t, os.Remove(filepath.Join(dir, "input")))
	inside := filepath.Join(f.worktree, "data.txt")
	require.NoError(t, os.WriteFile(inside, []byte("good"), 0o644))
	require.NoError(t, os.Symlink(inside, filepath.Join(dir, "input")))
	_, err = ComputeIdentity(context.Background(), IdentityRequest{WorktreePath: f.worktree, RuntimeDir: f.runtime, Check: check, Env: f.env()})
	require.ErrorContains(t, err, "symlink")
	require.NoError(t, os.WriteFile(inside, []byte("bad"), 0o644))
	_, err = ComputeIdentity(context.Background(), IdentityRequest{WorktreePath: f.worktree, RuntimeDir: f.runtime, Check: check, Env: f.env()})
	require.ErrorContains(t, err, "symlink", "changed target contents cannot reuse a pass")

	leaf := config.CheckConfig{Name: "leaf", Commands: cmds("true"), Inputs: []string{"fixture/input"}}
	_, err = ComputeIdentity(context.Background(), IdentityRequest{WorktreePath: f.worktree, RuntimeDir: f.runtime, Check: leaf, Env: f.env()})
	require.ErrorContains(t, err, "symlink")
	require.NoError(t, os.WriteFile(inside, []byte("worse"), 0o644))
	_, err = ComputeIdentity(context.Background(), IdentityRequest{WorktreePath: f.worktree, RuntimeDir: f.runtime, Check: leaf, Env: f.env()})
	require.ErrorContains(t, err, "symlink", "a changed leaf link cannot reuse a pass")
}

func TestDirectoryArtifactsAreRecordedAndVerified(t *testing.T) {
	f := newFixture(t)
	check := config.CheckConfig{Name: "bundle", Commands: cmds(`mkdir -p "$ANVIL_ARTIFACTS_DIR/bundle" && echo ok > "$ANVIL_ARTIFACTS_DIR/bundle/result"`),
		Artifacts: []string{"${ANVIL_ARTIFACTS_DIR}/bundle"}}
	result, err := Run(context.Background(), f.request(check))
	require.NoError(t, err)
	require.Equal(t, StatusError, result.Status)
	require.Contains(t, result.Error, "directory artifacts are unsupported")
	require.Empty(t, result.Artifacts)
	intact, reason := ArtifactsIntact(result)
	require.False(t, intact, reason)

	bundle := filepath.Join(f.runtime, "artifacts", "bundle")
	require.NoError(t, os.WriteFile(filepath.Join(bundle, "result"), []byte("tampered\n"), 0o644))
	intact, _ = ArtifactsIntact(result)
	require.False(t, intact, "tampered content")
	require.NoError(t, os.WriteFile(filepath.Join(bundle, "result"), []byte("ok\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(bundle, "extra"), []byte("x"), 0o644))
	intact, _ = ArtifactsIntact(result)
	require.False(t, intact, "added file")
	require.NoError(t, os.Remove(filepath.Join(bundle, "extra")))
	intact, _ = ArtifactsIntact(result)
	require.False(t, intact, "restoring files does not make an unsupported directory artifact reusable")
	require.NoError(t, os.Remove(filepath.Join(bundle, "result")))
	intact, _ = ArtifactsIntact(result)
	require.False(t, intact, "deleted file")
}

func holdResourceLock(t *testing.T, root, name string) *lockfile.Lock {
	t.Helper()
	path, _ := lockPaths(root, name)
	lock, err := lockfile.TryAcquire(path)
	require.NoError(t, err)
	return lock
}

func TestIdentityIsCapturedAfterResourceLocks(t *testing.T) {
	f := newFixture(t)
	input := filepath.Join(f.worktree, "in.txt")
	require.NoError(t, os.WriteFile(input, []byte("A"), 0o644))
	check := config.CheckConfig{Name: "bench", Commands: cmds("cat in.txt > out.txt"), Inputs: []string{"in.txt"}, Locks: []string{"gate"}}
	holder := holdResourceLock(t, f.root, "gate")

	request := f.request(check)
	request.LockWait = -1
	done := make(chan *Result, 1)
	go func() {
		result, err := Run(context.Background(), request)
		if err != nil {
			t.Errorf("run: %v", err)
		}
		done <- result
	}()
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, os.WriteFile(input, []byte("B"), 0o644))
	consumed := f.identity(t, check)
	require.NoError(t, holder.Release())
	result := <-done
	require.NotNil(t, result)
	require.Equal(t, "B", readString(t, filepath.Join(f.worktree, "out.txt")))
	require.Equal(t, consumed.InputsDigest, result.Identity.InputsDigest, "the recorded identity is the one the command ran against")
}

func readString(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}

func TestLockWaitHonorsCancellationAndReleasesEarlierLocks(t *testing.T) {
	f := newFixture(t)
	marker := filepath.Join(f.worktree, "ran")
	check := config.CheckConfig{Name: "gpu", Commands: cmds("touch ran"), Locks: []string{"b-later", "a-first"}}
	holder := holdResourceLock(t, f.root, "b-later")
	defer func() { require.NoError(t, holder.Release()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	request := f.request(check)
	request.LockWait = -1
	start := time.Now()
	result, err := Run(ctx, request)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, result)
	require.Less(t, time.Since(start), 3*time.Second)
	require.NoFileExists(t, marker, "no command started")

	first := holdResourceLock(t, f.root, "a-first")
	require.NoError(t, first.Release(), "the earlier lock in the sorted set was released")
}

func TestToolchainProbesAreSupervised(t *testing.T) {
	f := newFixture(t)
	previous := probeTimeout
	probeTimeout = 300 * time.Millisecond
	defer func() { probeTimeout = previous }()

	check := config.CheckConfig{Name: "probe", Commands: cmds("true"), Toolchain: []string{"sleep 30 & wait"}}
	start := time.Now()
	_, err := ComputeIdentity(context.Background(), IdentityRequest{WorktreePath: f.worktree, RuntimeDir: f.runtime, Check: check, Env: f.env()})
	require.Less(t, time.Since(start), 4*time.Second, "a grandchild holding stdout does not outlive the probe deadline")
	require.ErrorIs(t, err, context.DeadlineExceeded, "a probe timeout cannot produce a reusable identity")

	probeTimeout = time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start = time.Now()
	_, err = ComputeIdentity(ctx, IdentityRequest{WorktreePath: f.worktree, RuntimeDir: f.runtime, Check: check, Env: f.env()})
	require.Less(t, time.Since(start), 4*time.Second, "cancellation ends the probe")
	require.Error(t, err, "a cancelled probe does not produce an identity")
}

func TestRetentionKeepsTheNewestRun(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, 10, 20, 20, 15, 0, 0, time.UTC)
	runs := filepath.Join(f.runtime, "checks", "fast", "runs")
	for i := 0; i < retainedRuns+5; i++ {
		// Same second as the new run, with suffixes that sort after it.
		require.NoError(t, os.MkdirAll(filepath.Join(runs, fmt.Sprintf("20261020T201500Z-ffff%04x", i)), 0o700))
	}
	request := f.request(config.CheckConfig{Name: "fast", Commands: cmds("echo hi")})
	request.Now = func() time.Time { return now }
	result, err := Run(context.Background(), request)
	require.NoError(t, err)
	require.DirExists(t, result.RunDir)
	require.FileExists(t, result.Commands[0].Log)
	latest, err := Store{RuntimeDir: f.runtime}.Latest("fast")
	require.NoError(t, err)
	require.Equal(t, result.RunID, latest.RunID)
	entries, err := os.ReadDir(runs)
	require.NoError(t, err)
	require.Len(t, entries, retainedRuns)
}

func TestGateLivesOutsideTheWorktreeAndItsGitDir(t *testing.T) {
	// Removal holds the gate while git deletes the worktree's directories;
	// Windows cannot delete a directory containing an open file.
	f := newFixture(t)
	gate, err := lifecycle.Path(f.worktree)
	require.NoError(t, err)
	gitDir, err := config.WorktreeGitDir(f.worktree)
	require.NoError(t, err)
	for _, root := range []string{f.worktree, gitDir} {
		rel, err := filepath.Rel(root, gate)
		require.NoError(t, err)
		require.True(t, rel == ".." || len(rel) > 2 && rel[:3] == ".."+string(filepath.Separator), "gate %s must not be inside %s", gate, root)
	}
	again, err := lifecycle.Path(f.worktree)
	require.NoError(t, err)
	require.Equal(t, gate, again, "stable for one worktree")
}
