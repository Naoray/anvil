package wtstate

import (
	"fmt"
	"time"

	"github.com/naoray/anvil/internal/config"
)

const metadataKey = "metadata"

// Worktree roles. A role describes purpose; it is not a security boundary.
const (
	RoleImplementation = "implementation"
	RoleReview         = "review"
)

// Task is generic external task metadata (Solo, GitHub, Linear, ...). It is
// descriptive only and never authorizes anything.
type Task struct {
	ID  string `yaml:"id,omitempty" json:"id,omitempty"`
	URL string `yaml:"url,omitempty" json:"url,omitempty"`
	PR  string `yaml:"pr,omitempty" json:"pr,omitempty"`
}

// Metadata is declared purpose recorded for a worktree.
type Metadata struct {
	Role string `yaml:"role,omitempty" json:"role,omitempty"`
	// PinnedCommit is the exact commit a review worktree was created at.
	PinnedCommit string    `yaml:"pinned_commit,omitempty" json:"pinned_commit,omitempty"`
	Task         Task      `yaml:"task,omitempty" json:"task"`
	CreatedAt    time.Time `yaml:"created_at,omitempty" json:"created_at,omitempty"`
}

// ValidRole reports whether role is known.
func ValidRole(role string) bool {
	return role == RoleImplementation || role == RoleReview
}

func decodeMetadata(doc map[string]any) (Metadata, error) {
	var metadata Metadata
	if raw, ok := doc[metadataKey]; ok {
		if err := config.DecodeSection(raw, &metadata); err != nil {
			return Metadata{}, fmt.Errorf("malformed worktree metadata: %w", err)
		}
	}
	return metadata, nil
}

// ReadMetadata returns recorded metadata (zero value when none).
func ReadMetadata(worktreePath string) (Metadata, error) {
	doc, err := config.ReadLocalStateMap(worktreePath)
	if err != nil {
		return Metadata{}, err
	}
	return decodeMetadata(doc)
}

// MetadataUpdate changes metadata; empty fields keep their current value.
type MetadataUpdate struct {
	Role         string
	PinnedCommit string
	Task         Task
}

// UpdateMetadata applies an update atomically. A recorded role or pin is
// never silently replaced: changing either is refused.
func UpdateMetadata(worktreePath string, update MetadataUpdate, now time.Time) (Metadata, error) {
	var result Metadata
	err := config.UpdateLocalState(worktreePath, func(doc map[string]any) error {
		metadata, err := decodeMetadata(doc)
		if err != nil {
			return err
		}
		if update.Role != "" {
			if metadata.Role != "" && metadata.Role != update.Role {
				return fmt.Errorf("worktree role is %q; refusing to change it to %q", metadata.Role, update.Role)
			}
			metadata.Role = update.Role
		}
		if update.PinnedCommit != "" {
			if metadata.PinnedCommit != "" && metadata.PinnedCommit != update.PinnedCommit {
				return fmt.Errorf("review worktree is pinned to %s; refusing to repin it to %s (create another review worktree)", metadata.PinnedCommit, update.PinnedCommit)
			}
			metadata.PinnedCommit = update.PinnedCommit
		}
		if update.Task.ID != "" {
			metadata.Task.ID = update.Task.ID
		}
		if update.Task.URL != "" {
			metadata.Task.URL = update.Task.URL
		}
		if update.Task.PR != "" {
			metadata.Task.PR = update.Task.PR
		}
		if metadata.CreatedAt.IsZero() {
			metadata.CreatedAt = now.UTC().Truncate(time.Second)
		}
		encoded, err := config.EncodeSection(metadata)
		if err != nil {
			return err
		}
		doc[metadataKey] = encoded
		result = metadata
		return nil
	})
	return result, err
}
