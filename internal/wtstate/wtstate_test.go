package wtstate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

func gitDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out, err := exec.Command("git", "init", dir).CombinedOutput()
	require.NoError(t, err, string(out))
	canonical, err := CanonicalPath(dir)
	require.NoError(t, err)
	return canonical
}

func TestEnsureIdentityStableAndPreservesState(t *testing.T) {
	dir := gitDir(t)
	require.NoError(t, config.WriteLocalState(dir, config.LocalState{DbSuffix: "brave_otter"}))

	first, status, err := EnsureIdentity(dir)
	require.NoError(t, err)
	require.Equal(t, IdentityMissing, status)
	require.Regexp(t, `^wt_[0-9a-f]{16}$`, first.ID)
	require.Equal(t, dir, first.Path)

	second, status, err := EnsureIdentity(dir)
	require.NoError(t, err)
	require.Equal(t, IdentityOK, status)
	require.Equal(t, first, second)

	state, err := config.ReadLocalState(dir)
	require.NoError(t, err)
	require.Equal(t, "brave_otter", state.DbSuffix)
}

func TestIdentityMovedKeepsIDCopiedGetsNewID(t *testing.T) {
	original := gitDir(t)
	identity, _, err := EnsureIdentity(original)
	require.NoError(t, err)
	require.NoError(t, config.UpdateLocalState(original, func(doc map[string]any) error {
		doc["lease"] = map[string]any{"owner": "x"}
		doc["processes"] = []any{}
		return nil
	}))
	content, err := os.ReadFile(filepath.Join(original, config.LocalStateFile))
	require.NoError(t, err)

	copyDir := gitDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(copyDir, config.LocalStateFile), content, 0o644))
	_, status, err := ReadIdentity(copyDir)
	require.NoError(t, err)
	require.Equal(t, IdentityCopied, status)
	copied, _, err := EnsureIdentity(copyDir)
	require.NoError(t, err)
	require.NotEqual(t, identity.ID, copied.ID)
	doc, err := config.ReadLocalStateMap(copyDir)
	require.NoError(t, err)
	require.NotContains(t, doc, "lease")
	require.NotContains(t, doc, "processes")

	moved := gitDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(moved, config.LocalStateFile), content, 0o644))
	require.NoError(t, os.RemoveAll(original))
	movedIdentity, status, err := EnsureIdentity(moved)
	require.NoError(t, err)
	require.Equal(t, IdentityMoved, status)
	require.Equal(t, identity.ID, movedIdentity.ID)
	require.Equal(t, moved, movedIdentity.Path)
}

func TestPrepareLifecycleAndInterruptedDetection(t *testing.T) {
	dir := gitDir(t)
	status, err := EffectivePrepareStatus(dir, nil)
	require.NoError(t, err)
	require.Equal(t, PrepareNone, status)

	now := time.Now()
	require.NoError(t, BeginPrepare(dir, "plan1", now))
	require.NoError(t, RecordStep(dir, "a", StepRecord{Name: "a", Definition: "d1", CompletedAt: now}))
	require.NoError(t, RecordStep(dir, "old", StepRecord{Name: "old", Definition: "d0", CompletedAt: now}))

	state, err := ReadPrepare(dir)
	require.NoError(t, err)
	status, err = EffectivePrepareStatus(dir, state)
	require.NoError(t, err)
	require.Equal(t, PrepareInterrupted, status, "running record without a lock holder is interrupted")

	lock, err := AcquirePrepareLock(dir)
	require.NoError(t, err)
	status, err = EffectivePrepareStatus(dir, state)
	require.NoError(t, err)
	require.Equal(t, PrepareRunning, status)
	require.NoError(t, lock.Release())

	failure := errors.New("step b failed: exit status 1\nsecret output line")
	require.NoError(t, FinishPrepare(dir, now, "b", failure, []string{"a", "b"}))
	state, err = ReadPrepare(dir)
	require.NoError(t, err)
	require.Equal(t, PrepareFailed, state.Status)
	require.Equal(t, "step b failed: exit status 1", state.Error)
	require.Contains(t, state.Steps, "a", "completed steps survive failure for resume")

	require.NoError(t, BeginPrepare(dir, "plan1", now))
	require.NoError(t, FinishPrepare(dir, now, "", nil, []string{"a"}))
	state, err = ReadPrepare(dir)
	require.NoError(t, err)
	require.Equal(t, PrepareComplete, state.Status)
	require.Empty(t, state.Error)
	require.Equal(t, []string{"a"}, keys(state.Steps), "stale step records pruned on success")
}

func keys(m map[string]StepRecord) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}
