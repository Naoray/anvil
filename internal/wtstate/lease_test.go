package wtstate

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

func TestLeaseLifecycle(t *testing.T) {
	dir := gitDir(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	lease, token, err := AcquireLease(dir, AcquireRequest{WorktreeID: "wt_1", Owner: "codex", TaskID: "T-1", TTL: time.Hour, Now: now})
	require.NoError(t, err)
	require.Regexp(t, `^lt_[0-9a-f]{48}$`, token)
	require.Equal(t, LeaseActive, lease.State(now))
	doc, err := config.ReadLocalStateMap(dir)
	require.NoError(t, err)
	require.NotContains(t, doc["lease"], token, "only the token hash is stored")

	// Another agent, even naming the same task, cannot take an active lease.
	_, _, err = AcquireLease(dir, AcquireRequest{WorktreeID: "wt_1", Owner: "claude", TaskID: "T-1", Now: now.Add(time.Minute)})
	var leaseErr *LeaseError
	require.True(t, errors.As(err, &leaseErr))
	require.ErrorIs(t, err, ErrLeaseHeld)
	require.ErrorIs(t, ReleaseLease(dir, "lt_wrong", now), ErrLeaseNotOwned)

	// Same token re-acquire = renew; explicit renew extends expiry.
	again, sameToken, err := AcquireLease(dir, AcquireRequest{WorktreeID: "wt_1", Token: token, Owner: "codex", TTL: time.Hour, Now: now.Add(30 * time.Minute)})
	require.NoError(t, err)
	require.Equal(t, token, sameToken)
	require.Equal(t, now.Add(90*time.Minute), again.ExpiresAt())
	renewed, err := RenewLease(dir, token, 2*time.Hour, now.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, now.Add(3*time.Hour), renewed.ExpiresAt())

	// Stale leases are diagnosed and require explicit takeover.
	late := now.Add(4 * time.Hour)
	current, err := ReadLease(dir)
	require.NoError(t, err)
	require.Equal(t, LeaseStale, current.State(late))
	_, _, err = AcquireLease(dir, AcquireRequest{WorktreeID: "wt_1", Owner: "claude", Now: late})
	require.ErrorIs(t, err, ErrLeaseStale)
	taken, newToken, err := AcquireLease(dir, AcquireRequest{WorktreeID: "wt_1", Owner: "claude", TakeoverStale: true, Now: late})
	require.NoError(t, err)
	require.NotEqual(t, token, newToken)
	require.Equal(t, "taken_over_stale", taken.Previous.How)
	require.Equal(t, "codex", taken.Previous.Owner)
	_, err = RenewLease(dir, token, time.Hour, late)
	require.ErrorIs(t, err, ErrLeaseNotOwned, "the old owner cannot renew after takeover")

	require.NoError(t, ReleaseLease(dir, newToken, late))
	released, err := ReadLease(dir)
	require.NoError(t, err)
	require.Equal(t, LeaseNone, released.State(late))
	require.Equal(t, "released", released.Previous.How)
	require.ErrorIs(t, ReleaseLease(dir, newToken, late), ErrNoLease)

	_, _, err = AcquireLease(dir, AcquireRequest{WorktreeID: "wt_1", Owner: "x", Now: late})
	require.NoError(t, err)
	broken, err := BreakLease(dir, "worker crashed; operator decision", late)
	require.NoError(t, err)
	require.Equal(t, "x", broken.Owner)
	after, err := ReadLease(dir)
	require.NoError(t, err)
	require.Equal(t, "broken", after.Previous.How)
	require.Equal(t, "worker crashed; operator decision", after.Previous.Reason)
}

func TestLeaseAcquireIsAtomicAcrossConcurrentAgents(t *testing.T) {
	dir := gitDir(t)
	now := time.Now()
	const agents = 16
	var wg sync.WaitGroup
	winners := make(chan string, agents)
	for i := 0; i < agents; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, token, err := AcquireLease(dir, AcquireRequest{WorktreeID: "wt_1", Owner: "agent", Now: now})
			if err == nil {
				winners <- token
			} else {
				assert.ErrorIs(t, err, ErrLeaseHeld)
			}
		}()
	}
	wg.Wait()
	close(winners)
	require.Len(t, winners, 1, "exactly one agent wins")
}

func TestMetadataRefusesRoleAndPinChanges(t *testing.T) {
	dir := gitDir(t)
	now := time.Now()
	metadata, err := UpdateMetadata(dir, MetadataUpdate{Role: RoleReview, PinnedCommit: "abc", Task: Task{ID: "T-9", PR: "42"}}, now)
	require.NoError(t, err)
	require.Equal(t, RoleReview, metadata.Role)
	_, err = UpdateMetadata(dir, MetadataUpdate{Role: RoleImplementation}, now)
	require.ErrorContains(t, err, "refusing to change")
	_, err = UpdateMetadata(dir, MetadataUpdate{PinnedCommit: "def"}, now)
	require.ErrorContains(t, err, "refusing to repin")
	metadata, err = UpdateMetadata(dir, MetadataUpdate{Task: Task{URL: "https://example.test/T-9"}}, now)
	require.NoError(t, err)
	require.Equal(t, Task{ID: "T-9", URL: "https://example.test/T-9", PR: "42"}, metadata.Task)
	require.True(t, ValidRole("review"))
	require.False(t, ValidRole("admin"))
}
