// Package wtstate owns the typed sections Anvil records in a worktree's
// .anvil.local: stable identity, preparation progress, role metadata, leases
// and managed processes. Every write goes through config.UpdateLocalState, so
// sections written by different commands never overwrite each other and
// unknown fields survive.
package wtstate

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/naoray/anvil/internal/config"
)

const identityKey = "worktree"

// Identity is the stable identifier Anvil assigns to one worktree. Branch
// names are not unique (review worktrees are detached), so the random ID is
// the only identifier ownership records are keyed by. Path detects copies.
type Identity struct {
	ID        string    `yaml:"id" json:"id"`
	Path      string    `yaml:"path" json:"path"`
	CreatedAt time.Time `yaml:"created_at" json:"created_at"`
}

// IdentityStatus classifies a recorded identity against the current path.
type IdentityStatus string

const (
	IdentityMissing IdentityStatus = "missing"
	IdentityOK      IdentityStatus = "ok"
	// IdentityMoved means the record names a different path that no longer
	// holds the same identity (for example after `git worktree move`).
	IdentityMoved IdentityStatus = "moved"
	// IdentityCopied means another live directory carries the same identity;
	// this copy must not act on the original's ownership records.
	IdentityCopied IdentityStatus = "copied"
)

// CanonicalPath resolves symlinks so the same directory has one identity.
func CanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	eval, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return eval, nil
}

// NewID returns a random worktree identifier.
func NewID() (string, error) {
	return randomID("wt_", 8)
}

func randomID(prefix string, size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating identifier: %w", err)
	}
	return prefix + hex.EncodeToString(buf), nil
}

func decodeIdentity(doc map[string]any) (*Identity, error) {
	raw, ok := doc[identityKey]
	if !ok {
		return nil, nil
	}
	var identity Identity
	if err := config.DecodeSection(raw, &identity); err != nil {
		return nil, fmt.Errorf("malformed worktree identity: %w", err)
	}
	if identity.ID == "" {
		return nil, nil
	}
	return &identity, nil
}

// classify compares a recorded identity with the canonical current path.
func classify(identity *Identity, canonical string) IdentityStatus {
	if identity == nil {
		return IdentityMissing
	}
	if identity.Path == canonical {
		return IdentityOK
	}
	other, err := config.ReadLocalStateMap(identity.Path)
	if err == nil {
		if recorded, err := decodeIdentity(other); err == nil && recorded != nil && recorded.ID == identity.ID {
			if _, statErr := os.Stat(identity.Path); statErr == nil {
				return IdentityCopied
			}
		}
	}
	return IdentityMoved
}

// ReadIdentity inspects the identity without writing.
func ReadIdentity(worktreePath string) (*Identity, IdentityStatus, error) {
	canonical, err := CanonicalPath(worktreePath)
	if err != nil {
		return nil, "", err
	}
	doc, err := config.ReadLocalStateMap(canonical)
	if err != nil {
		return nil, "", err
	}
	identity, err := decodeIdentity(doc)
	if err != nil {
		return nil, "", err
	}
	return identity, classify(identity, canonical), nil
}

// copiedSections are ownership records that belong to the original directory
// and must never be acted upon from a copy.
var copiedSections = []string{"lease", "processes"}

// EnsureIdentity assigns or repairs the worktree identity. A moved worktree
// keeps its ID; a copied one receives a new ID and drops the original's
// lease and process records.
func EnsureIdentity(worktreePath string) (Identity, IdentityStatus, error) {
	canonical, err := CanonicalPath(worktreePath)
	if err != nil {
		return Identity{}, "", err
	}
	// Resolve a possible source before taking the destination's write lock.
	// State readers share that lock; looking up another worktree inside the
	// callback could deadlock two copied worktrees repairing concurrently.
	observed, observedStatus, err := ReadIdentity(canonical)
	if err != nil {
		return Identity{}, "", err
	}
	var result Identity
	var status IdentityStatus
	err = config.UpdateLocalState(canonical, func(doc map[string]any) error {
		identity, err := decodeIdentity(doc)
		if err != nil {
			return err
		}
		switch {
		case identity == nil:
			status = IdentityMissing
		case identity.Path == canonical:
			status = IdentityOK
		case observed != nil && identity.ID == observed.ID && identity.Path == observed.Path:
			status = observedStatus
		default:
			return fmt.Errorf("worktree identity changed while resolving its source; run ensure again")
		}
		switch status {
		case IdentityOK:
			result = *identity
			return nil
		case IdentityMoved:
			identity.Path = canonical
			result = *identity
		case IdentityMissing, IdentityCopied:
			id, err := NewID()
			if err != nil {
				return err
			}
			result = Identity{ID: id, Path: canonical, CreatedAt: time.Now().UTC().Truncate(time.Second)}
			if status == IdentityCopied {
				for _, key := range copiedSections {
					delete(doc, key)
				}
			}
		}
		encoded, err := config.EncodeSection(result)
		if err != nil {
			return err
		}
		doc[identityKey] = encoded
		return nil
	})
	return result, status, err
}
