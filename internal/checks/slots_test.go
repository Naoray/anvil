package checks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/slots"
)

func gatedRequest(f fixture, check config.CheckConfig, capacity int) (RunRequest, *slots.Manager) {
	manager := slots.New(slots.Dir(f.root), slots.Settings{Capacity: capacity})
	manager.Poll = 5 * time.Millisecond
	request := f.request(check)
	request.Slot = SlotOptions{Manager: manager}
	return request, manager
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return len(strings.Fields(string(content)))
}

// TestGatedReuseMissProbesOnceAndReportsOnce: with a free slot, a reuse miss
// keeps the identity assessed before admission instead of probing again.
func TestGatedReuseMissProbesOnceAndReportsOnce(t *testing.T) {
	f := newFixture(t)
	probes := filepath.Join(t.TempDir(), "probes")
	check := config.CheckConfig{Name: "bench", Expensive: true, Reusable: true, Commands: cmds("true"),
		Toolchain: []string{"echo probe >> " + probes + "; echo tool-v1"}}
	request, _ := gatedRequest(f, check, 1)
	request.Reuse = true
	var misses []string
	request.ReuseMiss = func(reason string) { misses = append(misses, reason) }
	result, err := Run(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, StatusPassed, result.Status)
	require.False(t, result.Reused)
	require.Equal(t, 1, countLines(t, probes), "toolchain probes ran once")
	require.Len(t, misses, 1, "a miss is reported once")
	require.Contains(t, result.Identity.Toolchain[0].Output, "tool-v1")

	again, err := Run(context.Background(), request)
	require.NoError(t, err)
	require.True(t, again.Reused)
	require.Equal(t, 2, countLines(t, probes))
	require.Len(t, misses, 1)
}

func TestGatedRunIdentityIncludesDeclaredSharedResources(t *testing.T) {
	f := newFixture(t)
	modelsDir := t.TempDir()
	writeFile(t, filepath.Join(modelsDir, "SUMS"), "first digest\n")
	models := SharedResource{Name: "models", Path: modelsDir, Identity: config.SharedIdentity{File: "SUMS"}}
	check := config.CheckConfig{Name: "bench", Expensive: true, Reusable: true, Commands: cmds("true"), Resources: []string{"models"}}
	request, _ := gatedRequest(f, check, 1)
	request.Resources = []SharedResource{models}

	first, err := Run(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, first.Identity.Resources, 1)

	request.Reuse = true
	writeFile(t, filepath.Join(modelsDir, "SUMS"), "changed digest\n")
	second, err := Run(context.Background(), request)
	require.NoError(t, err)
	require.False(t, second.Reused, "a changed declared resource makes the prior result stale")
	require.NotEqual(t, first.Identity.Resources[0].Digest, second.Identity.Resources[0].Digest)
}

// TestGatedReuseHitTakesNoSlot: reuse executes nothing, so it never queues.
func TestGatedReuseHitTakesNoSlot(t *testing.T) {
	f := newFixture(t)
	check := config.CheckConfig{Name: "bench", Expensive: true, Reusable: true, Commands: cmds("true")}
	request, manager := gatedRequest(f, check, 1)
	_, err := Run(context.Background(), request)
	require.NoError(t, err)

	holder, err := manager.Acquire(context.Background(), slots.Request{Weight: 1, Check: "other"})
	require.NoError(t, err)
	defer func() { require.NoError(t, holder.Release()) }()
	request.Reuse = true
	request.Slot.Wait = 100 * time.Millisecond
	result, err := Run(context.Background(), request)
	require.NoError(t, err)
	require.True(t, result.Reused)
}

// TestQueueTimeIsRecordedSeparately: started_at and duration exclude the
// queue; queued_ms reports it.
func TestQueueTimeIsRecordedSeparately(t *testing.T) {
	f := newFixture(t)
	check := config.CheckConfig{Name: "bench", Expensive: true, Commands: cmds("true")}
	request, manager := gatedRequest(f, check, 1)
	holder, err := manager.Acquire(context.Background(), slots.Request{Weight: 1, Check: "other"})
	require.NoError(t, err)
	released := make(chan time.Time, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		released <- time.Now()
		if err := holder.Release(); err != nil {
			t.Errorf("release: %v", err)
		}
	}()
	result, err := Run(context.Background(), request)
	require.NoError(t, err)
	require.GreaterOrEqual(t, result.QueuedMS, int64(250))
	require.False(t, result.StartedAt.Before((<-released).Add(-50*time.Millisecond)), "started_at is admission, after the queue wait")
	require.Equal(t, result.FinishedAt.Sub(result.StartedAt).Milliseconds(), result.DurationMS)

	ungated, err := Run(context.Background(), f.request(config.CheckConfig{Name: "plain", Commands: cmds("true")}))
	require.NoError(t, err)
	require.Zero(t, ungated.QueuedMS)
}
