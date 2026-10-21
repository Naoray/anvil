package checks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/slots"
)

// SlotOptions admits slot-weighted checks through the machine-wide
// semaphore. A nil Manager disables gating.
type SlotOptions struct {
	Manager  *slots.Manager
	Priority int
	// Wait bounds the queue wait; 0 waits until the run is cancelled.
	Wait     time.Duration
	Progress func(slots.Progress)
	Warn     func(string)
}

// SlotWeight is a check's weight in the machine-wide slot semaphore:
// its declared slots, 1 for an expensive check without slots, else 0
// (not gated).
func SlotWeight(check config.CheckConfig) int {
	if check.Slots > 0 {
		return check.Slots
	}
	if check.Expensive {
		return 1
	}
	return 0
}

func (r RunRequest) slotGated() bool {
	return r.Slot.Manager != nil && SlotWeight(r.Check) > 0
}

// reuseAssessment is the identity a run executes against and, when reuse
// was requested, the reuse decision.
type reuseAssessment struct {
	Targets  []CommandTarget
	Identity Identity
	// Reused is a current passing result returned instead of running.
	Reused *Result
	// Reason explains a miss (only with RunRequest.Reuse).
	Reason string
}

// assessReuse is the single entry point that resolves command targets,
// computes the identity under the given guards and, with req.Reuse, decides
// whether a stored result can be returned instead of running.
func assessReuse(ctx context.Context, req RunRequest, store Store, guards []*lockfile.Lock) (reuseAssessment, error) {
	identityReq := IdentityRequest{Guards: guards, WorktreePath: req.Worktree.Path, RuntimeDir: req.RuntimeDir, Check: req.Check,
		Env: req.Env, Resolve: req.Resolve, Unset: req.Unset, SecretEnv: req.SecretEnv,
		Resources: req.Resources, RuntimeRoot: req.RuntimeRoot, CacheWrite: true, Warn: req.Warn}
	targets, err := resolveTargets(identityReq)
	if err != nil {
		return reuseAssessment{}, fmt.Errorf("resolving check commands: %w", err)
	}
	identityReq.Targets = targets
	identity, err := ComputeIdentity(ctx, identityReq)
	if err != nil {
		return reuseAssessment{}, fmt.Errorf("computing check identity: %w", err)
	}
	assessment := reuseAssessment{Targets: targets, Identity: identity}
	if !req.Reuse {
		return assessment, nil
	}
	reused, reason, err := store.reusable(req.Check, identity)
	if err != nil {
		return reuseAssessment{}, fmt.Errorf("assessing reuse: %w", err)
	}
	if reused != nil {
		reused.Reused = true
	}
	assessment.Reused, assessment.Reason = reused, reason
	return assessment, nil
}

// admission is everything a run holds before executing: its slots, its
// resource locks and the identity assessed under them.
type admission struct {
	slot       *slots.Held
	locks      heldLocks
	assessment reuseAssessment
	queued     time.Duration
}

func (a *admission) guards(gate, active *lockfile.Lock) []*lockfile.Lock {
	return append(append([]*lockfile.Lock{gate, active}, a.slot.Locks()...), a.locks...)
}

func (a *admission) release() error {
	if a == nil {
		return nil
	}
	err := errors.Join(a.locks.release(), a.slot.Release())
	a.locks, a.slot = nil, nil
	return err
}

// admit takes the check's slots (gated checks) and resource locks and
// assesses its identity under them.
//
// Slots are queued for after the per-worktree active lock and before
// resource locks: a queued check holds no resource lock (so ungated checks
// sharing a resource are never stuck behind the slot queue), and an admitted
// check that waits for a resource lock waits on a holder that is already
// executing or needs no slot, so the waits cannot form a cycle.
//
// With --reuse, a gated check is assessed under its resource locks first: a
// hit executes nothing and takes no slot. On a miss a single non-blocking
// slot attempt is made while the locks are still held (it never waits, so
// it cannot deadlock); if it succeeds the assessment stands and toolchain
// probes run once. Otherwise the locks are released, the check queues, and
// the identity is assessed again after admission because the tree may have
// changed while it waited.
func admit(ctx context.Context, req RunRequest, store Store, gate, active *lockfile.Lock, runID string) (*admission, error) {
	a := &admission{}
	slotReq := slotRequest(req, runID)
	if req.slotGated() && req.Reuse {
		locks, err := takeResourceLocks(ctx, req, runID)
		if err != nil {
			return nil, err
		}
		a.locks = locks
		assessment, err := assessReuse(ctx, req, store, a.guards(gate, active))
		if err != nil {
			return nil, errors.Join(err, a.release())
		}
		a.assessment = assessment
		if assessment.Reused != nil {
			return a, nil
		}
		slot, err := req.Slot.Manager.TryAcquire(ctx, slotReq)
		if err != nil {
			return nil, errors.Join(err, a.release())
		}
		if slot != nil {
			a.slot = slot
			return a, nil
		}
		if err := a.release(); err != nil {
			return nil, err
		}
	}
	if req.slotGated() {
		start := time.Now()
		slot, err := req.Slot.Manager.Acquire(ctx, slotReq)
		if err != nil {
			return nil, err
		}
		a.slot, a.queued = slot, time.Since(start)
	}
	locks, err := takeResourceLocks(ctx, req, runID)
	if err != nil {
		return nil, errors.Join(err, a.release())
	}
	a.locks = locks
	if a.assessment, err = assessReuse(ctx, req, store, a.guards(gate, active)); err != nil {
		return nil, errors.Join(err, a.release())
	}
	return a, nil
}

func takeResourceLocks(ctx context.Context, req RunRequest, runID string) (heldLocks, error) {
	return acquireResourceLocks(ctx, req.RuntimeRoot, req.Check.Locks, req.LockWait, LockHolder{
		PID: os.Getpid(), WorktreeID: req.Worktree.ID, Worktree: req.Worktree.Path, Check: req.Check.Name, RunID: runID, Since: req.now().UTC(),
	})
}

// slotRequest describes the run to the slot queue. Warnings repeated by the
// non-blocking attempt and the queued one are reported once.
func slotRequest(req RunRequest, runID string) slots.Request {
	warn := req.Slot.Warn
	if warn != nil {
		var mu sync.Mutex
		seen := map[string]bool{}
		next := warn
		warn = func(message string) {
			mu.Lock()
			repeated := seen[message]
			seen[message] = true
			mu.Unlock()
			if !repeated {
				next(message)
			}
		}
	}
	return slots.Request{
		Weight: SlotWeight(req.Check), Priority: req.Slot.Priority, Check: req.Check.Name,
		Worktree: req.Worktree.Path, WorktreeID: req.Worktree.ID, RunID: runID,
		Wait: req.Slot.Wait, Progress: req.Slot.Progress, Warn: warn,
	}
}
