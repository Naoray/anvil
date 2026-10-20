package wtstate

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/naoray/anvil/internal/config"
)

const leaseKey = "lease"

// DefaultLeaseTTL is used when acquire does not specify one.
const DefaultLeaseTTL = 2 * time.Hour

// Lease states.
const (
	LeaseNone   = "none"
	LeaseActive = "active"
	LeaseStale  = "stale"
)

// Lease is an advisory claim on a worktree by one agent. Only a SHA-256 of
// the owner token is stored; the token itself is returned once on acquire.
// Task and session identifiers are metadata and never authorize anything.
type Lease struct {
	WorktreeID string    `yaml:"worktree_id" json:"worktree_id"`
	TokenHash  string    `yaml:"token_sha256" json:"-"`
	Owner      string    `yaml:"owner,omitempty" json:"owner,omitempty"`
	TaskID     string    `yaml:"task_id,omitempty" json:"task_id,omitempty"`
	SessionID  string    `yaml:"session_id,omitempty" json:"session_id,omitempty"`
	AcquiredAt time.Time `yaml:"acquired_at" json:"acquired_at"`
	RenewedAt  time.Time `yaml:"renewed_at" json:"renewed_at"`
	TTLSeconds int64     `yaml:"ttl_seconds" json:"ttl_seconds"`
	// Previous records the last lease taken over or broken, for diagnosis.
	Previous *LeaseHistory `yaml:"previous,omitempty" json:"previous,omitempty"`
}

// LeaseHistory describes how an earlier lease ended.
type LeaseHistory struct {
	Owner     string    `yaml:"owner,omitempty" json:"owner,omitempty"`
	TaskID    string    `yaml:"task_id,omitempty" json:"task_id,omitempty"`
	ExpiresAt time.Time `yaml:"expires_at" json:"expires_at"`
	EndedAt   time.Time `yaml:"ended_at" json:"ended_at"`
	How       string    `yaml:"how" json:"how"`
	Reason    string    `yaml:"reason,omitempty" json:"reason,omitempty"`
}

// ExpiresAt is when the lease becomes stale without renewal.
func (l Lease) ExpiresAt() time.Time {
	return l.RenewedAt.Add(time.Duration(l.TTLSeconds) * time.Second)
}

// State classifies the lease at now.
func (l *Lease) State(now time.Time) string {
	if l == nil || l.TokenHash == "" {
		return LeaseNone
	}
	if now.Before(l.ExpiresAt()) {
		return LeaseActive
	}
	return LeaseStale
}

// Lease errors.
var (
	ErrLeaseHeld     = errors.New("worktree is leased by another owner")
	ErrLeaseStale    = errors.New("worktree has a stale lease")
	ErrLeaseNotOwned = errors.New("lease token does not match")
	ErrNoLease       = errors.New("worktree is not leased")
)

// LeaseError carries the conflicting lease.
type LeaseError struct {
	Err   error
	Lease *Lease
}

func (e *LeaseError) Error() string {
	if e.Lease == nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("%v (owner %q, task %q, expires %s)", e.Err, e.Lease.Owner, e.Lease.TaskID, e.Lease.ExpiresAt().UTC().Format(time.RFC3339))
}

func (e *LeaseError) Unwrap() error { return e.Err }

// HashToken returns the stored form of a lease token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func tokenMatches(lease *Lease, token string) bool {
	return token != "" && subtle.ConstantTimeCompare([]byte(lease.TokenHash), []byte(HashToken(token))) == 1
}

// NewLeaseToken returns a random owner token.
func NewLeaseToken() (string, error) {
	return randomID("lt_", 24)
}

func decodeLease(doc map[string]any) (*Lease, error) {
	raw, ok := doc[leaseKey]
	if !ok {
		return nil, nil
	}
	var lease Lease
	if err := config.DecodeSection(raw, &lease); err != nil {
		return nil, fmt.Errorf("malformed lease: %w", err)
	}
	return &lease, nil
}

// ReadLease returns the recorded lease record (nil when never leased). A
// released or broken lease has no token hash and State LeaseNone, but keeps
// its history in Previous.
func ReadLease(worktreePath string) (*Lease, error) {
	doc, err := config.ReadLocalStateMap(worktreePath)
	if err != nil {
		return nil, err
	}
	return decodeLease(doc)
}

func storeLease(doc map[string]any, lease *Lease) error {
	encoded, err := config.EncodeSection(lease)
	if err != nil {
		return err
	}
	doc[leaseKey] = encoded
	return nil
}

// AcquireRequest describes a lease acquisition.
type AcquireRequest struct {
	WorktreeID string
	// Token renews an existing lease held by the same owner; empty
	// generates a new token.
	Token     string
	Owner     string
	TaskID    string
	SessionID string
	TTL       time.Duration
	// TakeoverStale permits replacing a lease whose holder stopped renewing.
	TakeoverStale bool
	Now           time.Time
}

// AcquireLease atomically claims the worktree. It returns the lease and the
// owner token (only meaningful when newly generated).
func AcquireLease(worktreePath string, req AcquireRequest) (*Lease, string, error) {
	if req.TTL <= 0 {
		req.TTL = DefaultLeaseTTL
	}
	var result *Lease
	token := req.Token
	err := config.UpdateLocalState(worktreePath, func(doc map[string]any) error {
		current, err := decodeLease(doc)
		if err != nil {
			return err
		}
		var previous *LeaseHistory
		switch current.State(req.Now) {
		case LeaseActive:
			if !tokenMatches(current, req.Token) {
				return &LeaseError{Err: ErrLeaseHeld, Lease: current}
			}
			current.RenewedAt = req.Now.UTC()
			current.TTLSeconds = int64(req.TTL / time.Second)
			result = current
			return storeLease(doc, current)
		case LeaseStale:
			if !tokenMatches(current, req.Token) && !req.TakeoverStale {
				return &LeaseError{Err: ErrLeaseStale, Lease: current}
			}
			if !tokenMatches(current, req.Token) {
				previous = &LeaseHistory{Owner: current.Owner, TaskID: current.TaskID, ExpiresAt: current.ExpiresAt(), EndedAt: req.Now.UTC(), How: "taken_over_stale"}
			}
		case LeaseNone:
			if current != nil {
				previous = current.Previous
			}
		}
		if token == "" {
			if token, err = NewLeaseToken(); err != nil {
				return err
			}
		}
		now := req.Now.UTC()
		result = &Lease{
			WorktreeID: req.WorktreeID, TokenHash: HashToken(token), Owner: req.Owner, TaskID: req.TaskID, SessionID: req.SessionID,
			AcquiredAt: now, RenewedAt: now, TTLSeconds: int64(req.TTL / time.Second), Previous: previous,
		}
		return storeLease(doc, result)
	})
	if err != nil {
		return nil, "", err
	}
	return result, token, nil
}

// RenewLease extends a lease held by token. A stale lease may be renewed by
// its own holder as long as nobody took it over.
func RenewLease(worktreePath, token string, ttl time.Duration, now time.Time) (*Lease, error) {
	if ttl <= 0 {
		ttl = DefaultLeaseTTL
	}
	var result *Lease
	err := config.UpdateLocalState(worktreePath, func(doc map[string]any) error {
		current, err := decodeLease(doc)
		if err != nil {
			return err
		}
		if current.State(now) == LeaseNone {
			return &LeaseError{Err: ErrNoLease}
		}
		if !tokenMatches(current, token) {
			return &LeaseError{Err: ErrLeaseNotOwned, Lease: current}
		}
		current.RenewedAt = now.UTC()
		current.TTLSeconds = int64(ttl / time.Second)
		result = current
		return storeLease(doc, current)
	})
	return result, err
}

// ReleaseLease ends a lease held by token.
func ReleaseLease(worktreePath, token string, now time.Time) error {
	return config.UpdateLocalState(worktreePath, func(doc map[string]any) error {
		current, err := decodeLease(doc)
		if err != nil {
			return err
		}
		if current.State(now) == LeaseNone {
			return &LeaseError{Err: ErrNoLease}
		}
		if !tokenMatches(current, token) {
			return &LeaseError{Err: ErrLeaseNotOwned, Lease: current}
		}
		return storeLease(doc, &Lease{Previous: &LeaseHistory{Owner: current.Owner, TaskID: current.TaskID, ExpiresAt: current.ExpiresAt(), EndedAt: now.UTC(), How: "released"}})
	})
}

// BreakLease forcibly ends another owner's lease, recording who did so and
// why. It is an explicit, audited operator action.
func BreakLease(worktreePath, reason string, now time.Time) (*Lease, error) {
	var broken *Lease
	err := config.UpdateLocalState(worktreePath, func(doc map[string]any) error {
		current, err := decodeLease(doc)
		if err != nil {
			return err
		}
		if current.State(now) == LeaseNone {
			return &LeaseError{Err: ErrNoLease}
		}
		broken = current
		return storeLease(doc, &Lease{Previous: &LeaseHistory{Owner: current.Owner, TaskID: current.TaskID, ExpiresAt: current.ExpiresAt(), EndedAt: now.UTC(), How: "broken", Reason: reason}})
	})
	return broken, err
}
