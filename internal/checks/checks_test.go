package checks

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/workenv"
)

type fixture struct {
	worktree string
	runtime  string
	root     string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("check commands use sh")
	}
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "t@e.st"}, {"config", "user.name", "T"}} {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src.txt"), []byte("v1"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("out/\n"), 0o644))
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "init"}} {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	root := t.TempDir()
	return fixture{worktree: dir, runtime: filepath.Join(root, "worktrees", "wt_0000000000000001"), root: root}
}

func (f fixture) env() *workenv.Environment {
	env := &workenv.Environment{}
	env.Set(workenv.Variable{Name: "ANVIL_RUNTIME_DIR", Value: f.runtime})
	env.Set(workenv.Variable{Name: "ANVIL_ARTIFACTS_DIR", Value: filepath.Join(f.runtime, "artifacts")})
	env.Set(workenv.Variable{Name: "API_TOKEN", Value: "hunter2", Secret: true})
	return env
}

func (f fixture) request(check config.CheckConfig) RunRequest {
	return RunRequest{
		Check: check, Worktree: WorktreeRef{ID: "wt_0000000000000001", Path: f.worktree}, RuntimeDir: f.runtime, RuntimeRoot: f.root,
		Env: f.env(),
		Resolve: func(command config.CheckCommand) (CommandTarget, error) {
			return CommandTarget{Dir: f.worktree, Env: f.env()}, nil
		},
	}
}

func (f fixture) identity(t *testing.T, check config.CheckConfig) Identity {
	t.Helper()
	identity, err := ComputeIdentity(context.Background(), IdentityRequest{WorktreePath: f.worktree, RuntimeDir: f.runtime, Check: check, Env: f.env()})
	require.NoError(t, err)
	return identity
}

func cmds(runs ...string) []config.CheckCommand {
	out := make([]config.CheckCommand, len(runs))
	for i, run := range runs {
		out[i] = config.CheckCommand{Run: run}
	}
	return out
}

func TestPassingRunProvenanceAndStaleness(t *testing.T) {
	f := newFixture(t)
	check := config.CheckConfig{Name: "fast", Commands: cmds(`echo "$ANVIL_CHECK_IDENTITY" > identity.out`, "mkdir -p out && echo data > out/report.json"),
		Artifacts: []string{"out/*.json"}, Toolchain: []string{"echo tool-1.0"}}
	result, err := Run(context.Background(), f.request(check))
	require.NoError(t, err)
	require.Equal(t, StatusPassed, result.Status)
	require.Equal(t, ResultSchema, result.Schema)
	require.False(t, result.Identity.Git.Dirty)
	require.Len(t, result.Commands, 2)
	require.Len(t, result.Artifacts, 1)
	require.Equal(t, "tool-1.0", result.Identity.Toolchain[0].Output)
	require.Contains(t, result.Identity.EnvNames, "API_TOKEN")
	require.NotNil(t, result.FinishedAt)
	content, err := os.ReadFile(filepath.Join(f.worktree, "identity.out"))
	require.NoError(t, err)
	require.Equal(t, result.Identity.Digest, strings.TrimSpace(string(content)), "runners see the identity")

	stored, err := Store{RuntimeDir: f.runtime}.Latest("fast")
	require.NoError(t, err)
	require.Equal(t, result.RunID, stored.RunID)

	// The check itself left an untracked file: the tree it validated was
	// clean, and the current dirty tree is not equivalent to it.
	assessment := Assess(stored, f.identity(t, check))
	require.False(t, assessment.Current)
	require.Equal(t, []string{"uncommitted changes differ"}, assessment.Reasons)
	require.NoError(t, os.Remove(filepath.Join(f.worktree, "identity.out")))
	assessment = Assess(stored, f.identity(t, check))
	require.True(t, assessment.PassingAndCurrent)

	require.NoError(t, os.WriteFile(filepath.Join(f.worktree, "src.txt"), []byte("dirty"), 0o644))
	assessment = Assess(stored, f.identity(t, check))
	require.False(t, assessment.Current)
	require.Contains(t, assessment.Reasons, "uncommitted changes differ")
	require.NoError(t, os.WriteFile(filepath.Join(f.worktree, "src.txt"), []byte("v1"), 0o644))

	changed := check
	changed.Toolchain = []string{"echo tool-2.0"}
	assessment = Assess(stored, f.identity(t, changed))
	require.ElementsMatch(t, []string{"check definition changed", "toolchain changed"}, assessment.Reasons)

	intact, _ := ArtifactsIntact(stored)
	require.True(t, intact)
	require.NoError(t, os.WriteFile(filepath.Join(f.worktree, "out", "report.json"), []byte("tampered"), 0o644))
	intact, reason := ArtifactsIntact(stored)
	require.False(t, intact)
	require.Contains(t, reason, "changed")
	require.Equal(t, Assessment{Reasons: []string{"never run"}}, Assess(nil, Identity{}))
}

func TestDirtyRunNeverMatchesClean(t *testing.T) {
	f := newFixture(t)
	check := config.CheckConfig{Name: "fast", Commands: cmds("true")}
	require.NoError(t, os.WriteFile(filepath.Join(f.worktree, "src.txt"), []byte("wip"), 0o644))
	result, err := Run(context.Background(), f.request(check))
	require.NoError(t, err)
	require.True(t, result.Identity.Git.Dirty)
	require.Equal(t, 1, result.Identity.Git.DirtyPaths)
	require.NoError(t, os.WriteFile(filepath.Join(f.worktree, "src.txt"), []byte("v1"), 0o644))
	assessment := Assess(result, f.identity(t, check))
	require.False(t, assessment.Current, "a pass on a dirty tree is not a pass for HEAD")
}

func TestFailureStopsAndPassesExitStatus(t *testing.T) {
	f := newFixture(t)
	result, err := Run(context.Background(), f.request(config.CheckConfig{Name: "lint", Commands: cmds("echo boom; exit 3", "touch never")}))
	require.NoError(t, err)
	require.Equal(t, StatusFailed, result.Status)
	require.Equal(t, 3, result.ExitCode)
	require.Len(t, result.Commands, 1)
	require.NoFileExists(t, filepath.Join(f.worktree, "never"))
	log, err := os.ReadFile(result.Commands[0].Log)
	require.NoError(t, err)
	require.Equal(t, "boom\n", string(log))
	require.False(t, Assess(result, f.identity(t, config.CheckConfig{Name: "lint", Commands: cmds("echo boom; exit 3", "touch never")})).PassingAndCurrent)
}

func TestTimeoutKillsProcessGroup(t *testing.T) {
	f := newFixture(t)
	pidFile := filepath.Join(t.TempDir(), "child")
	check := config.CheckConfig{Name: "slow", Commands: []config.CheckCommand{{Run: "sleep 30 & echo $! > " + pidFile + "; wait", Timeout: "300ms"}}}
	start := time.Now()
	result, err := Run(context.Background(), f.request(check))
	require.NoError(t, err)
	require.Less(t, time.Since(start), 10*time.Second)
	require.Equal(t, StatusTimedOut, result.Status)
	require.Equal(t, 124, result.ExitCode)
	require.True(t, result.Commands[0].TimedOut)
	content, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return exec.Command("kill", "-0", strings.TrimSpace(string(content))).Run() != nil
	}, 5*time.Second, 50*time.Millisecond, "grandchild was killed with its group")
}

func TestInterruptionIsRecordedNotPassed(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	result, err := Run(ctx, f.request(config.CheckConfig{Name: "bench", Commands: cmds("sleep 30", "true")}))
	require.NoError(t, err)
	require.Equal(t, StatusInterrupted, result.Status)
	require.Equal(t, 130, result.ExitCode)
	latest, err := Store{RuntimeDir: f.runtime}.Latest("bench")
	require.NoError(t, err)
	require.Equal(t, StatusInterrupted, latest.Status)
}

func TestAbandonedRunIsReportedInterrupted(t *testing.T) {
	f := newFixture(t)
	store := Store{RuntimeDir: f.runtime}
	runDir := store.runDir("bench", "20260101T000000Z-dead")
	require.NoError(t, os.MkdirAll(runDir, 0o700))
	require.NoError(t, store.save(&Result{Schema: ResultSchema, Check: "bench", RunID: "20260101T000000Z-dead", Status: StatusRunning, RunDir: runDir}))
	latest, err := store.Latest("bench")
	require.NoError(t, err)
	require.Equal(t, StatusInterrupted, latest.Status, "a run without a live holder never stays 'running' or passes")
}

func TestExclusiveResourceLocks(t *testing.T) {
	if os.Getenv("ANVIL_CHECK_LOCK_CHILD") != "" {
		lock, err := lockfile.Acquire(filepath.Join(os.Getenv("ANVIL_CHECK_LOCK_ROOT"), "locks", "gpu.lock"))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(os.Getenv("ANVIL_CHECK_LOCK_READY"), nil, 0o600))
		time.Sleep(time.Hour)
		require.NoError(t, lock.Release())
		return
	}
	f := newFixture(t)
	ready := filepath.Join(t.TempDir(), "ready")
	holder := exec.Command(os.Args[0], "-test.run=^TestExclusiveResourceLocks$")
	holder.Env = append(os.Environ(), "ANVIL_CHECK_LOCK_CHILD=1", "ANVIL_CHECK_LOCK_ROOT="+f.root, "ANVIL_CHECK_LOCK_READY="+ready)
	require.NoError(t, holder.Start())
	require.Eventually(t, func() bool { _, err := os.Stat(ready); return err == nil }, 10*time.Second, 10*time.Millisecond)

	bench := config.CheckConfig{Name: "bench", Locks: []string{"gpu"}, Commands: cmds("touch ran")}
	_, err := Run(context.Background(), f.request(bench))
	var busy *BusyError
	require.True(t, errors.As(err, &busy), "fails fast while another live process holds the lock: %v", err)
	require.NoFileExists(t, filepath.Join(f.worktree, "ran"))
	latest, err := Store{RuntimeDir: f.runtime}.Latest("bench")
	require.NoError(t, err)
	require.Nil(t, latest, "nothing recorded when the lock was unavailable")

	// Independent unlocked checks run concurrently meanwhile.
	var wg sync.WaitGroup
	for _, name := range []string{"fast", "lint"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			result, err := Run(context.Background(), f.request(config.CheckConfig{Name: name, Commands: cmds("sleep 0.2")}))
			require.NoError(t, err)
			require.Equal(t, StatusPassed, result.Status)
		}(name)
	}
	wg.Wait()

	// The holder dies (interrupted): the OS releases the lock; no timeout-based stealing.
	require.NoError(t, holder.Process.Kill())
	require.Error(t, holder.Wait())
	req := f.request(bench)
	req.LockWait = 5 * time.Second
	result, err := Run(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, StatusPassed, result.Status)
}

func TestSameCheckCannotRunTwiceInOneWorktree(t *testing.T) {
	f := newFixture(t)
	store := Store{RuntimeDir: f.runtime}
	lock, err := lockfile.Acquire(store.activeLockPath("fast"))
	require.NoError(t, err)
	defer lock.Release()
	_, err = Run(context.Background(), f.request(config.CheckConfig{Name: "fast", Commands: cmds("true")}))
	require.ErrorIs(t, err, ErrAlreadyRunning)
}

func TestInputsUnderRuntimeDirAffectIdentity(t *testing.T) {
	f := newFixture(t)
	recordings := filepath.Join(f.runtime, "artifacts", "recordings")
	require.NoError(t, os.MkdirAll(recordings, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(recordings, "r1.jsonl"), []byte("a"), 0o600))
	check := config.CheckConfig{Name: "score", Commands: cmds("true"), Inputs: []string{"${ANVIL_ARTIFACTS_DIR}/recordings", "contract.yaml"}}
	first := f.identity(t, check)
	require.NoError(t, os.WriteFile(filepath.Join(f.worktree, "contract.yaml"), []byte("v2"), 0o644))
	second := f.identity(t, check)
	require.NotEqual(t, first.InputsDigest, second.InputsDigest, "new scoring contract → new identity")
	require.NoError(t, os.WriteFile(filepath.Join(recordings, "r1.jsonl"), []byte("b"), 0o600))
	third := f.identity(t, check)
	require.NotEqual(t, second.InputsDigest, third.InputsDigest, "changed recordings → new identity")

	_, err := ComputeIdentity(context.Background(), IdentityRequest{WorktreePath: f.worktree, RuntimeDir: f.runtime, Env: f.env(),
		Check: config.CheckConfig{Name: "x", Commands: cmds("true"), Inputs: []string{"/etc/passwd"}}})
	require.ErrorContains(t, err, "must stay inside")
}

func TestResolvePathsRejectsSymlinkEscapes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	f := newFixture(t)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(f.worktree, "escape")))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret"), filepath.Join(f.worktree, "leak")))

	for _, pattern := range []string{"escape/secret", "escape/*", "leak"} {
		_, err := ResolvePaths(f.worktree, f.runtime, []string{pattern}, nil)
		require.ErrorContains(t, err, "symlink", pattern)
	}

	inside := filepath.Join(f.worktree, "real")
	require.NoError(t, os.MkdirAll(inside, 0o755))
	require.NoError(t, os.Symlink(inside, filepath.Join(f.worktree, "alias")))
	_, err := ResolvePaths(f.worktree, f.runtime, []string{"alias"}, nil)
	require.ErrorContains(t, err, "symlink", "inside-root aliases also fail closed")
}
