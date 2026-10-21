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
)

func TestRetentionPreservesLatestWithSameSecondRunIDs(t *testing.T) {
	f := newFixture(t)
	store := Store{RuntimeDir: f.runtime}
	now := time.Date(2026, 10, 21, 8, 0, 0, 900000000, time.UTC)
	for i := 0; i < retainedRuns; i++ {
		id := fmt.Sprintf("20261021T080000Z-ffffffff-%02d", i)
		dir := store.runDir("fast", id)
		require.NoError(t, os.MkdirAll(dir, 0o700))
		result := &Result{Schema: ResultSchema, Check: "fast", RunID: id, RunDir: dir, Status: StatusPassed, StartedAt: now.Add(-time.Duration(retainedRuns-i) * time.Millisecond)}
		require.NoError(t, store.save(result))
	}
	request := f.request(config.CheckConfig{Name: "fast", Commands: cmds("echo newest")})
	request.Now = func() time.Time { return now }
	result, err := Run(context.Background(), request)
	require.NoError(t, err)
	require.FileExists(t, result.Commands[0].Log)
	require.FileExists(t, filepath.Join(result.RunDir, "result.json"))
	latest, err := store.Latest("fast")
	require.NoError(t, err)
	require.Equal(t, result.RunID, latest.RunID)
	dirs, err := os.ReadDir(filepath.Dir(result.RunDir))
	require.NoError(t, err)
	require.Len(t, dirs, retainedRuns)
	// The latest pointer is authoritative even under clock rollback or an exact
	// chronology tie; random/lexical ID order must never delete its run.
	result.StartedAt = now.Add(-time.Hour)
	require.NoError(t, store.save(result))
	extra := store.runDir("fast", "extra")
	require.NoError(t, os.MkdirAll(extra, 0o700))
	require.NoError(t, writeJSON(filepath.Join(extra, "result.json"), &Result{RunID: "extra", StartedAt: now}))
	require.NoError(t, store.prune("fast"))
	require.FileExists(t, result.Commands[0].Log)
}
