package cli

import (
	"fmt"
	"sort"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
)

// Borrow chains are unsupported, so borrowers can be removed before all owners.
func orderDatabaseRemovals(worktrees []git.Worktree) error {
	borrowers := map[string]bool{}
	for _, wt := range worktrees {
		state, err := config.ReadLocalState(wt.Path)
		if err != nil {
			return fmt.Errorf("reading cleanup state for %q: %w", wt.Path, err)
		}
		borrowers[wt.Path] = state.BorrowedApplication != nil
	}
	sort.SliceStable(worktrees, func(i, j int) bool { return borrowers[worktrees[i].Path] && !borrowers[worktrees[j].Path] })
	return nil
}
