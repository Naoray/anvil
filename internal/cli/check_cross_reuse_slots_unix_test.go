// Slot scenarios use sh and the Unix slot fixture.

//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/lockfile"
)

// A cross-worktree reuse hit on a slot-gated check executes nothing and
// takes no machine-wide slot, even when every slot is held.
func TestCheckReuseAcrossWorktreesTakesNoSlot(t *testing.T) {
	f, trace, wts := slotFixture(t, 1, `checks:
  bench:
    expensive: true
    reusable: true
    commands: ['echo run >> "TRACE/runs.log"']
`, "author", "reviewer")
	runs := func() int {
		content, err := os.ReadFile(filepath.Join(trace, "runs.log"))
		require.NoError(t, err)
		return strings.Count(string(content), "run\n")
	}
	envelope, stderr, code := f.runJSON(wts["author"], "check", "bench", "--allow-expensive")
	require.Equal(t, 0, code, stderr)
	authorID := field(t, envelope, "data", "results", 0, "worktree", "id").(string)
	require.Equal(t, 1, runs())

	slot, err := lockfile.Acquire(filepath.Join(f.runtimeRoot(), "slots", "slot-0.lock"))
	require.NoError(t, err)
	defer func() { require.NoError(t, slot.Release()) }()

	envelope, stderr, code = f.runJSON(wts["reviewer"], "check", "bench", "--allow-expensive", "--reuse", "--slot-wait", "300ms")
	require.Equal(t, 0, code, stderr)
	result := field(t, envelope, "data", "results", 0).(map[string]any)
	require.Equal(t, true, result["reused"], "warnings: %v", envelope["warnings"])
	require.Equal(t, authorID, field(t, result, "assessment", "reused_from", "worktree_id"))
	require.Equal(t, float64(0), result["queued_ms"])
	require.NotContains(t, stderr, "waiting for slot")
	require.Empty(t, f.queued(), "the hit never queued")
	require.Equal(t, 1, runs(), "nothing ran in the reviewer worktree")
}
