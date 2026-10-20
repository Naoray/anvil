package cli

import (
	"path/filepath"

	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/wtstate"
)

// worktreeSummary is the stable JSON identity of one worktree.
type worktreeSummary struct {
	ID             string `json:"id,omitempty"`
	IdentityStatus string `json:"identity_status"`
	Name           string `json:"name"`
	Path           string `json:"path"`
	Branch         string `json:"branch,omitempty"`
	Detached       bool   `json:"detached"`
	Head           string `json:"head,omitempty"`
}

// summarizeWorktree reads identity and HEAD without writing anything.
func summarizeWorktree(wt git.Worktree) (worktreeSummary, []string) {
	summary := worktreeSummary{
		Name:     filepath.Base(wt.Path),
		Path:     wt.Path,
		Branch:   wt.Branch,
		Detached: wt.Detached,
	}
	var warnings []string
	if canonical, err := wtstate.CanonicalPath(wt.Path); err == nil {
		summary.Path = canonical
	}
	identity, status, err := wtstate.ReadIdentity(wt.Path)
	if err != nil {
		warnings = append(warnings, "cannot read worktree identity: "+err.Error())
	} else {
		summary.IdentityStatus = string(status)
		if identity != nil && status != wtstate.IdentityCopied {
			summary.ID = identity.ID
		}
		if status == wtstate.IdentityCopied {
			warnings = append(warnings, "worktree state was copied from "+identity.Path+"; run 'anvil ensure' to assign a new identity")
		}
	}
	if head, err := git.HeadCommit(wt.Path); err == nil {
		summary.Head = head
	}
	return summary, warnings
}
