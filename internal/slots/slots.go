// Package slots admits expensive work under a machine-wide, load-aware
// concurrency limit shared by every Anvil project and worktree.
//
// Capacity is held with operating-system file locks (slot-<i>.lock under
// <runtime root>/slots/): a request of weight w holds w slot files, so a
// killed holder frees its capacity when the OS closes its files, never
// because time passed. Waiters queue as tickets in queue.json, guarded by
// queue.lock. Admission is strict head-of-line: responsive tickets are
// ordered by priority (higher first), then arrival; only the head may take
// slots, so a request never overtakes an earlier responsive request of equal
// or higher priority, even when it would fit. Every
// participant deletes tickets whose PID is gone or whose kernel start
// identity no longer matches (a recycled PID). A live ticket whose heartbeat
// is older than StaleAfter (a stopped or wedged process) loses its place
// behind responsive tickets but is kept: it regains its place as soon as
// its owner polls again. Queue state only orders waiters: if queue.json is
// lost or corrupt it is reset with a warning, and capacity stays enforced
// by the slot locks.
package slots

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/procs"
)

// What the head of the queue is waiting for.
const (
	WaitingForSlot = "slot"
	WaitingForLoad = "load"
)

// ErrLoadUnsupported reports that the load average cannot be read here.
var ErrLoadUnsupported = errors.New("load average is unavailable on this platform")

const (
	queueFile = "queue.json"
	queueLock = "queue.lock"
	// DefaultStaleAfter is the heartbeat age after which a live ticket
	// loses head-of-line position.
	DefaultStaleAfter = 30 * time.Second
	defaultPoll       = 200 * time.Millisecond
	// abandonAfter removes tickets whose owner cannot be verified (no
	// start identity on this platform) once they stop heartbeating this long.
	abandonAfter = 10 * time.Minute
)

// queueLockWait bounds one wait for the briefly held queue lock; a waiter
// that times out simply retries at its next poll.
var queueLockWait = 2 * time.Second

// tempPrefix names writeJSON's temp files.
const tempPrefix = ".slots-"

// fileAccessHook observes queue.json and holder-record accesses (tests).
var fileAccessHook func(op, path string)

// errQueueBusy reports a queue lock that stayed held for queueLockWait.
var errQueueBusy = errors.New("slot queue is busy")

// Dir returns the slot directory beneath the machine runtime root.
func Dir(runtimeRoot string) string { return filepath.Join(runtimeRoot, "slots") }

// Ticket is one queued request.
type Ticket struct {
	// ID identifies the ticket; Seq only orders it and may repeat after the
	// queue file was reset.
	ID            string    `json:"id"`
	Seq           int64     `json:"seq"`
	PID           int       `json:"pid"`
	StartIdentity string    `json:"start_identity"`
	Check         string    `json:"check"`
	Worktree      string    `json:"worktree"`
	WorktreeID    string    `json:"worktree_id,omitempty"`
	Priority      int       `json:"priority"`
	Weight        int       `json:"weight"`
	EnqueuedAt    time.Time `json:"enqueued_at"`
	WaitingFor    string    `json:"waiting_for"`
	Heartbeat     time.Time `json:"heartbeat"`
}

type queueState struct {
	NextSeq int64    `json:"next_seq"`
	Tickets []Ticket `json:"tickets"`
}

// Holder describes the run holding one slot (diagnostic only; the lock is
// authoritative).
type Holder struct {
	Slot       int       `json:"slot"`
	Weight     int       `json:"weight"`
	PID        int       `json:"pid"`
	Check      string    `json:"check"`
	Worktree   string    `json:"worktree"`
	WorktreeID string    `json:"worktree_id,omitempty"`
	RunID      string    `json:"run_id,omitempty"`
	Priority   int       `json:"priority"`
	Since      time.Time `json:"since"`
}

// QueueEntry is a ticket as reported by read-only views.
type QueueEntry struct {
	Position   int       `json:"position"`
	WaitingFor string    `json:"waiting_for"`
	Priority   int       `json:"priority"`
	Weight     int       `json:"weight"`
	Check      string    `json:"check"`
	Worktree   string    `json:"worktree"`
	WorktreeID string    `json:"worktree_id,omitempty"`
	PID        int       `json:"pid"`
	EnqueuedAt time.Time `json:"enqueued_at"`
	Heartbeat  time.Time `json:"heartbeat"`
	// Responsive is false when the heartbeat is older than StaleAfter; such
	// tickets queue behind responsive ones.
	Responsive bool `json:"responsive"`
}

// LoadStatus is the load gate's view of the machine.
type LoadStatus struct {
	Supported bool     `json:"supported"`
	Value     *float64 `json:"value"`
	// Max is the configured max_load; 0 means load gating is disabled.
	Max             float64 `json:"max"`
	WaitSeconds     float64 `json:"wait_seconds"`
	UnsupportedNote string  `json:"error,omitempty"`
}

// Status is a machine-wide snapshot.
type Status struct {
	Capacity int          `json:"capacity"`
	InUse    int          `json:"in_use"`
	Load     LoadStatus   `json:"load"`
	Holders  []Holder     `json:"holders"`
	Queue    []QueueEntry `json:"queue"`
	// QueueBusy reports that queue.lock stayed busy: InUse is still exact
	// (probed from the slot locks) but Holders and Queue are unavailable (null).
	QueueBusy bool `json:"queue_busy"`
}

// Progress is reported to a waiter whenever its state changes.
type Progress struct {
	WaitingFor string
	Position   int
	Queued     int
}

// Request asks for slot capacity.
type Request struct {
	Weight     int
	Priority   int
	Check      string
	Worktree   string
	WorktreeID string
	RunID      string
	// Wait bounds the whole wait; 0 waits until ctx is cancelled.
	Wait     time.Duration
	Progress func(Progress)
	Warn     func(string)
}

// CapacityError reports a request that can never be admitted.
type CapacityError struct {
	Check    string
	Weight   int
	Capacity int
}

func (e *CapacityError) Error() string {
	if e.Weight < 1 {
		return fmt.Sprintf("check %q requests %d slots; the weight must be at least 1", e.Check, e.Weight)
	}
	return fmt.Sprintf("check %q needs %d slots but this machine allows %d; raise checks.slots in the global config (or ANVIL_CHECK_SLOTS) or lower the check's slots",
		e.Check, e.Weight, e.Capacity)
}

// BusyError reports that no slot was granted within the requested wait.
type BusyError struct {
	WaitingFor string
	Position   int
	Queued     int
	Capacity   int
	InUse      int
	Waited     time.Duration
	Holders    []Holder
}

func (e *BusyError) Error() string {
	return fmt.Sprintf("no check slot within %s: waiting for %s (position %d of %d, %d of %d slots in use)",
		e.Waited.Round(time.Millisecond), e.WaitingFor, e.Position, e.Queued, e.InUse, e.Capacity)
}

// Manager coordinates one slot directory.
type Manager struct {
	Dir string
	Settings
	// ReadLoad returns the 1-minute load average (default LoadAverage).
	ReadLoad func() (float64, error)
	// Poll is the retry interval while waiting.
	Poll time.Duration
	// StaleAfter is the heartbeat age that costs a live ticket its place.
	StaleAfter time.Duration
}

// New returns a manager for dir with the given settings.
func New(dir string, settings Settings) *Manager {
	return &Manager{Dir: dir, Settings: settings}
}

func (m *Manager) poll() time.Duration {
	if m.Poll > 0 {
		return m.Poll
	}
	return defaultPoll
}

func (m *Manager) staleAfter() time.Duration {
	if m.StaleAfter > 0 {
		return m.StaleAfter
	}
	return DefaultStaleAfter
}

func (m *Manager) readLoad() (float64, error) {
	if m.ReadLoad != nil {
		return m.ReadLoad()
	}
	return LoadAverage()
}

func (m *Manager) slotLock(i int) string {
	return filepath.Join(m.Dir, fmt.Sprintf("slot-%d.lock", i))
}

func (m *Manager) slotRecord(i int) string {
	return filepath.Join(m.Dir, fmt.Sprintf("slot-%d.holder.json", i))
}

// Held is acquired slot capacity. Its locks may be inherited by supervised
// commands, which then keep the capacity until they terminate.
type Held struct {
	locks     []*lockfile.Lock
	records   []string
	written   []Holder
	queueLock string
}

// Locks returns the held slot locks (nil for a nil Held).
func (h *Held) Locks() []*lockfile.Lock {
	if h == nil {
		return nil
	}
	return append([]*lockfile.Lock(nil), h.locks...)
}

// Release frees the slots first, so capacity is never delayed by waiting
// for queue.lock, then removes the holder records under queue.lock — only
// while they are still this holder's: a new holder of a freed slot writes
// its own record under the lock, and records are only reported for held
// slots, so a record left behind (queue.lock busy or unavailable) is
// harmless. Release is safe on nil and idempotent.
func (h *Held) Release() error {
	if h == nil || len(h.locks) == 0 {
		return nil
	}
	var problems []error
	for i := len(h.locks) - 1; i >= 0; i-- {
		problems = append(problems, h.locks[i].Release())
	}
	records, written := h.records, h.written
	h.locks, h.records, h.written = nil, nil, nil
	queue, err := lockfile.AcquireTimeout(h.queueLock, queueLockWait)
	if errors.Is(err, lockfile.ErrLocked) {
		return errors.Join(problems...)
	}
	if err != nil {
		return errors.Join(append(problems, fmt.Errorf("locking slot queue to remove holder records: %w", err))...)
	}
	for i := range written {
		problems = append(problems, removeOwnRecord(records[i], written[i]))
	}
	problems = append(problems, queue.Release())
	return errors.Join(problems...)
}

// removeOwnRecord deletes a holder record unless another holder has
// replaced it. The caller holds queue.lock.
func removeOwnRecord(path string, ours Holder) error {
	observe("read", path)
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var current Holder
	if json.Unmarshal(content, &current) != nil || !sameHolder(current, ours) {
		return nil // replaced (or unreadable): not ours to remove
	}
	observe("remove", path)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func sameHolder(a, b Holder) bool {
	return a.Slot == b.Slot && a.PID == b.PID && a.RunID == b.RunID && a.Check == b.Check &&
		a.Worktree == b.Worktree && a.Since.Equal(b.Since)
}

// releaseLocked unlocks slots taken by an admission that failed before
// returning; the caller holds queue.lock, and any record at these paths is
// stale (the slot was free) or was just written by this admission.
func (h *Held) releaseLocked() error {
	var problems []error
	for i := len(h.locks) - 1; i >= 0; i-- {
		observe("remove", h.records[i])
		if err := os.Remove(h.records[i]); err != nil && !errors.Is(err, os.ErrNotExist) {
			problems = append(problems, err)
		}
		problems = append(problems, h.locks[i].Release())
	}
	h.locks, h.records, h.written = nil, nil, nil
	return errors.Join(problems...)
}

// loadGate decides once per acquisition whether load permits admission.
// The bound is measured from the ticket's enqueue time, so it limits each
// check's total load wait however many heads waited before it.
type loadGate struct {
	settings Settings
	read     func() (float64, error)
	passed   bool
	since    time.Time
}

func (g *loadGate) admit(now time.Time, warn func(string)) bool {
	if g.passed || g.settings.MaxLoad <= 0 {
		g.passed = true
		return true
	}
	value, err := g.read()
	if err != nil {
		g.passed = true
		warn(fmt.Sprintf("load gating skipped: %v", err))
		return true
	}
	if value <= g.settings.MaxLoad {
		g.passed = true
		return true
	}
	if now.Sub(g.since) >= g.settings.LoadWait {
		g.passed = true
		warn(fmt.Sprintf("1-minute load average %.2f still exceeds max_load %.2f after waiting %s; proceeding because slot capacity still bounds concurrency",
			value, g.settings.MaxLoad, g.settings.LoadWait))
		return true
	}
	return false
}

// Acquire queues for weight slots and blocks until admitted, the wait
// elapses (BusyError) or ctx is cancelled. The ticket is removed on every
// non-admitted return.
func (m *Manager) Acquire(ctx context.Context, req Request) (*Held, error) {
	return m.acquire(ctx, req, false)
}

// TryAcquire makes a single admission attempt and never waits: it returns
// nil (and leaves no ticket) unless the request is at the head of the queue
// and its slots are free now. It cannot deadlock, so callers may use it
// while holding other locks.
func (m *Manager) TryAcquire(ctx context.Context, req Request) (*Held, error) {
	return m.acquire(ctx, req, true)
}

func (m *Manager) acquire(ctx context.Context, req Request, once bool) (*Held, error) {
	if req.Weight < 1 || req.Weight > m.Capacity {
		return nil, &CapacityError{Check: req.Check, Weight: req.Weight, Capacity: m.Capacity}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	warn := req.Warn
	if warn == nil {
		warn = func(string) {}
	}
	pid := os.Getpid()
	identity, err := procs.StartIdentity(pid)
	if err != nil && !errors.Is(err, procs.ErrUnsupported) {
		return nil, fmt.Errorf("identifying this process for the slot queue: %w", err)
	}
	id, err := newTicketID()
	if err != nil {
		return nil, err
	}
	start := time.Now()
	own := Ticket{ID: id, PID: pid, StartIdentity: identity, Check: req.Check, Worktree: req.Worktree, WorktreeID: req.WorktreeID,
		Priority: req.Priority, Weight: req.Weight, EnqueuedAt: start.UTC(), WaitingFor: WaitingForSlot, Heartbeat: start.UTC()}
	gate := &loadGate{settings: m.Settings, read: m.readLoad, since: start}
	var last Progress
	for {
		held, progress, err := m.attempt(ctx, &own, gate, req, warn)
		switch {
		case errors.Is(err, errQueueBusy):
			progress = last // retry at the next poll
		case err != nil:
			return nil, errors.Join(err, m.removeTicket(own))
		case held != nil:
			return held, nil
		}
		if once {
			return nil, m.removeTicket(own)
		}
		if progress != last && progress.Position > 0 && req.Progress != nil {
			req.Progress(progress)
		}
		last = progress
		if waited := time.Since(start); req.Wait > 0 && waited >= req.Wait {
			var holders []Holder
			var inUse int
			var holdersErr error
			queueErr := m.withQueue(context.Background(), nil, func(*queueState) (bool, error) {
				holders, inUse, holdersErr = m.holdersLocked()
				return false, nil
			})
			holdersErr = errors.Join(holdersErr, queueErr)
			busy := &BusyError{WaitingFor: progress.WaitingFor, Position: progress.Position, Queued: progress.Queued,
				Capacity: m.Capacity, InUse: inUse, Waited: waited, Holders: holders}
			if busy.WaitingFor == "" {
				busy.WaitingFor = WaitingForSlot
			}
			return nil, errors.Join(busy, holdersErr, m.removeTicket(own))
		}
		timer := time.NewTimer(m.poll())
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errors.Join(ctx.Err(), m.removeTicket(own))
		case <-timer.C:
		}
	}
}

func newTicketID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating slot ticket id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// attempt refreshes the queue (joining it on the first call) and, at its
// head, tries to take the slots.
func (m *Manager) attempt(ctx context.Context, own *Ticket, gate *loadGate, req Request, warn func(string)) (held *Held, progress Progress, err error) {
	err = m.withQueue(ctx, warn, func(state *queueState) (bool, error) {
		now := time.Now().UTC()
		changed := m.prune(state, own.ID, now)
		index := -1
		for i := range state.Tickets {
			if state.Tickets[i].ID == own.ID {
				index = i
			}
		}
		if index < 0 {
			// First call, or the ticket was lost (queue reset, or removed by
			// a participant that could not verify us): (re)join, keeping the
			// original sequence number and so the original place.
			if own.Seq == 0 {
				state.NextSeq++
				own.Seq = state.NextSeq
			}
			state.Tickets = append(state.Tickets, *own)
			index, changed = len(state.Tickets)-1, true
		}
		ticket := &state.Tickets[index]
		if now.Sub(ticket.Heartbeat) >= m.staleAfter()/4 {
			ticket.Heartbeat, changed = now, true
		}
		ordered := order(state.Tickets, now, m.staleAfter())
		position := 0
		for i := range ordered {
			if ordered[i].ID == own.ID {
				position = i + 1
			}
		}
		waitingFor := WaitingForSlot
		if position == 1 {
			if !gate.admit(now, warn) {
				waitingFor = WaitingForLoad
			} else {
				taken, err := m.take(req, now)
				if err != nil {
					return changed, err
				}
				if taken != nil {
					held = taken
					state.Tickets = append(state.Tickets[:index], state.Tickets[index+1:]...)
					return true, nil
				}
			}
		}
		if ticket.WaitingFor != waitingFor {
			ticket.WaitingFor, changed = waitingFor, true
		}
		*own = *ticket
		progress = Progress{WaitingFor: waitingFor, Position: position, Queued: len(ordered)}
		return changed, nil
	})
	if err != nil && held != nil {
		// The slots were taken but the queue could not be updated.
		return nil, progress, errors.Join(err, held.Release())
	}
	return held, progress, err
}

// take acquires req.Weight free slots, all or nothing.
func (m *Manager) take(req Request, now time.Time) (*Held, error) {
	held := &Held{queueLock: filepath.Join(m.Dir, queueLock)}
	var slots []int
	for i := 0; i < m.Capacity && len(held.locks) < req.Weight; i++ {
		lock, err := lockfile.TryAcquire(m.slotLock(i))
		if errors.Is(err, lockfile.ErrLocked) {
			continue
		}
		if err != nil {
			return nil, errors.Join(err, held.releaseLocked())
		}
		held.locks = append(held.locks, lock)
		held.records = append(held.records, m.slotRecord(i))
		slots = append(slots, i)
	}
	if len(held.locks) < req.Weight {
		return nil, held.releaseLocked()
	}
	for i, slot := range slots {
		record := Holder{Slot: slot, Weight: req.Weight, PID: os.Getpid(), Check: req.Check, Worktree: req.Worktree,
			WorktreeID: req.WorktreeID, RunID: req.RunID, Priority: req.Priority, Since: now}
		if err := writeJSON(held.records[i], record); err != nil {
			return nil, errors.Join(err, held.releaseLocked())
		}
		held.written = append(held.written, record)
	}
	return held, nil
}

// prune deletes tickets of exited processes and recycled PIDs.
func (m *Manager) prune(state *queueState, own string, now time.Time) bool {
	kept := make([]Ticket, 0, len(state.Tickets))
	for _, ticket := range state.Tickets {
		if ticket.ID == own || ticketLive(ticket, now) {
			kept = append(kept, ticket)
		}
	}
	changed := len(kept) != len(state.Tickets)
	state.Tickets = kept
	return changed
}

func ticketLive(ticket Ticket, now time.Time) bool {
	switch procs.Check(ticket.PID, ticket.StartIdentity) {
	case procs.StateExited, procs.StateReused:
		return false
	case procs.StateUnknown:
		return now.Sub(ticket.Heartbeat) < abandonAfter
	default:
		return true
	}
}

// order sorts responsive tickets first, then by priority (higher first),
// then by arrival (sequence, then enqueue time, then ID: sequence numbers
// can repeat after a queue reset).
func order(tickets []Ticket, now time.Time, staleAfter time.Duration) []Ticket {
	out := append([]Ticket(nil), tickets...)
	responsive := func(t Ticket) bool { return now.Sub(t.Heartbeat) <= staleAfter }
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := responsive(out[i]), responsive(out[j]); ri != rj {
			return ri
		}
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		if out[i].Seq != out[j].Seq {
			return out[i].Seq < out[j].Seq
		}
		if !out[i].EnqueuedAt.Equal(out[j].EnqueuedAt) {
			return out[i].EnqueuedAt.Before(out[j].EnqueuedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// removeTicket deletes own's ticket. It is not cancellable: it runs after
// cancellation and holds the queue lock only briefly.
func (m *Manager) removeTicket(own Ticket) error {
	if own.Seq == 0 {
		return nil // never joined
	}
	err := m.withQueue(context.Background(), nil, func(state *queueState) (bool, error) {
		for i := range state.Tickets {
			if state.Tickets[i].ID == own.ID {
				state.Tickets = append(state.Tickets[:i], state.Tickets[i+1:]...)
				return true, nil
			}
		}
		return false, nil
	})
	if errors.Is(err, errQueueBusy) {
		return fmt.Errorf("removing slot ticket: %w (it is discarded once this process exits)", err)
	}
	return err
}

// withQueue runs fn on the queue under its lock and writes changes back. A
// malformed queue is reset (warned, then rewritten): it only orders waiters.
func (m *Manager) withQueue(ctx context.Context, warn func(string), fn func(*queueState) (bool, error)) (err error) {
	lock, err := lockfile.AcquireContext(ctx, filepath.Join(m.Dir, queueLock), queueLockWait)
	if errors.Is(err, lockfile.ErrLocked) {
		return errQueueBusy
	}
	if err != nil {
		return fmt.Errorf("locking slot queue: %w", err)
	}
	defer func() { err = errors.Join(err, lock.Release()) }()
	state, err := m.readState()
	reset := false
	var corrupt *CorruptQueueError
	if errors.As(err, &corrupt) {
		if warn != nil {
			warn(corrupt.Error())
		}
		state, reset = corrupt.recovered(), true
	} else if err != nil {
		return err
	}
	changed, err := fn(&state)
	if err != nil || (!changed && !reset) {
		return err
	}
	if err := m.writeState(state); err != nil {
		return err
	}
	if err := m.sweepTemps(time.Now()); err != nil && warn != nil {
		warn("cleaning slot temp files: " + err.Error())
	}
	return nil
}

// staleTempAge is how old an abandoned write's temp file must be before it
// is removed; every writer holds queue.lock, so younger ones are left alone
// only out of caution.
const staleTempAge = time.Hour

// sweepTemps removes temp files of writers killed mid-write (kill -9). The
// caller holds queue.lock.
func (m *Manager) sweepTemps(now time.Time) error {
	matches, err := filepath.Glob(filepath.Join(m.Dir, tempPrefix+"*"))
	if err != nil {
		return err
	}
	var problems []error
	for _, path := range matches {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if !info.Mode().IsRegular() || now.Sub(info.ModTime()) < staleTempAge {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

// CorruptQueueError reports an unreadable queue file. Its ordering is lost;
// capacity is unaffected because the slot locks enforce it.
type CorruptQueueError struct {
	Path string
	Err  error
}

func (e *CorruptQueueError) Error() string {
	return fmt.Sprintf("malformed slot queue %s was reset (waiting order lost; slot locks still bound concurrency): %v", e.Path, e.Err)
}

func (e *CorruptQueueError) Unwrap() error { return e.Err }

func (e *CorruptQueueError) recovered() queueState { return queueState{} }

// readState reads the queue. Its sequence counter never falls below a
// sequence number in use, so a counter lost with an earlier reset cannot
// hand out numbers that collide with rejoining tickets.
func (m *Manager) readState() (queueState, error) {
	path := filepath.Join(m.Dir, queueFile)
	observe("read", path)
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return queueState{}, nil
	}
	if err != nil {
		return queueState{}, fmt.Errorf("reading slot queue: %w", err)
	}
	var state queueState
	if err := json.Unmarshal(content, &state); err != nil {
		return queueState{}, &CorruptQueueError{Path: path, Err: err}
	}
	for _, ticket := range state.Tickets {
		state.NextSeq = max(state.NextSeq, ticket.Seq)
	}
	return state, nil
}

func (m *Manager) writeState(state queueState) error {
	for _, ticket := range state.Tickets {
		state.NextSeq = max(state.NextSeq, ticket.Seq)
	}
	return writeJSON(filepath.Join(m.Dir, queueFile), state)
}

// countHeld probes every slot lock without reading records.
func (m *Manager) countHeld() (int, error) {
	indexes, err := m.slotIndexes()
	if err != nil {
		return 0, err
	}
	count := 0
	var problems []error
	for _, index := range indexes {
		held, err := lockfile.IsHeld(m.slotLock(index))
		if err != nil {
			problems = append(problems, err)
		} else if held {
			count++
		}
	}
	return count, errors.Join(problems...)
}

// slotIndexes lists existing slot lock files, including indexes beyond the
// current capacity.
func (m *Manager) slotIndexes() ([]int, error) {
	matches, err := filepath.Glob(filepath.Join(m.Dir, "slot-*.lock"))
	if err != nil {
		return nil, err
	}
	var indexes []int
	for _, match := range matches {
		index, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(match), "slot-"), ".lock"))
		if err == nil && index >= 0 {
			indexes = append(indexes, index)
		}
	}
	sort.Ints(indexes)
	return indexes, nil
}

// holdersLocked reports every held slot, including slots beyond the current
// capacity left by a participant configured with a larger one. The caller
// holds queue.lock, which serializes record reads with their replacement
// and removal (Windows refuses to replace or delete an open file).
func (m *Manager) holdersLocked() ([]Holder, int, error) {
	holders := []Holder{}
	indexes, err := m.slotIndexes()
	if err != nil {
		return holders, 0, err
	}
	var problems []error
	for _, index := range indexes {
		held, err := lockfile.IsHeld(m.slotLock(index))
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if !held {
			continue
		}
		holder := Holder{Slot: index}
		observe("read", m.slotRecord(index))
		if content, err := os.ReadFile(m.slotRecord(index)); err == nil {
			if err := json.Unmarshal(content, &holder); err != nil {
				holder = Holder{Slot: index}
			}
		}
		holder.Slot = index
		holders = append(holders, holder)
	}
	return holders, len(holders), errors.Join(problems...)
}

// Snapshot reports capacity, load, holders and the live queue without
// changing anything; dead tickets are omitted, not deleted. It reads under
// queue.lock like every other participant and creates nothing when no slot
// has ever been used.
func (m *Manager) Snapshot() (Status, error) {
	status := Status{Capacity: m.Capacity, Queue: []QueueEntry{}, Holders: []Holder{},
		Load: LoadStatus{Max: m.MaxLoad, WaitSeconds: m.LoadWait.Seconds()}}
	if value, err := m.readLoad(); err == nil {
		status.Load.Supported, status.Load.Value = true, &value
	} else {
		status.Load.UnsupportedNote = err.Error()
	}
	if _, err := os.Stat(m.Dir); errors.Is(err, os.ErrNotExist) {
		return status, nil
	}
	lock, err := lockfile.AcquireTimeout(filepath.Join(m.Dir, queueLock), queueLockWait)
	if errors.Is(err, lockfile.ErrLocked) {
		// Records cannot be read safely, but the slot locks can still be
		// probed: never report a busy machine as idle.
		inUse, probeErr := m.countHeld()
		status.InUse, status.QueueBusy, status.Holders, status.Queue = inUse, true, nil, nil
		return status, errors.Join(errQueueBusy, probeErr)
	}
	if err != nil {
		return status, fmt.Errorf("locking slot queue: %w", err)
	}
	holders, inUse, holdersErr := m.holdersLocked()
	state, stateErr := m.readState()
	if err := lock.Release(); err != nil {
		return status, errors.Join(holdersErr, stateErr, err)
	}
	status.Holders, status.InUse = holders, inUse
	if stateErr != nil {
		// A corrupt queue has no recoverable order; report it as a diagnostic.
		return status, errors.Join(holdersErr, stateErr)
	}
	now := time.Now().UTC()
	live := make([]Ticket, 0, len(state.Tickets))
	for _, ticket := range state.Tickets {
		if ticketLive(ticket, now) {
			live = append(live, ticket)
		}
	}
	for i, ticket := range order(live, now, m.staleAfter()) {
		waitingFor := ticket.WaitingFor
		if waitingFor == "" || i > 0 {
			waitingFor = WaitingForSlot
		}
		status.Queue = append(status.Queue, QueueEntry{Position: i + 1, WaitingFor: waitingFor, Priority: ticket.Priority,
			Weight: ticket.Weight, Check: ticket.Check, Worktree: ticket.Worktree, WorktreeID: ticket.WorktreeID, PID: ticket.PID,
			EnqueuedAt: ticket.EnqueuedAt, Heartbeat: ticket.Heartbeat, Responsive: now.Sub(ticket.Heartbeat) <= m.staleAfter()})
	}
	return status, holdersErr
}

func observe(op, path string) {
	if fileAccessHook != nil {
		fileAccessHook(op, path)
	}
}

func writeJSON(path string, value any) (err error) {
	observe("replace", path)
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), tempPrefix)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if removeErr := os.Remove(tmp.Name()); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, removeErr)
			}
		}
	}()
	if _, err = tmp.Write(content); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err = tmp.Sync(); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
