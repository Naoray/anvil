package cli

import (
	"fmt"
	"sort"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/dblifecycle"
	"github.com/naoray/anvil/internal/git"
)

// skipMissingRemovalTargets drops worktrees whose registered path provably no
// longer exists, reporting each to warn: nothing on disk is left to clean up.
// A locked absent worktree fails closed, as in dblifecycle.CheckCleanupWithWarnings,
// and a path whose absence cannot be proven is an error.
func skipMissingRemovalTargets(worktrees []git.Worktree, warn func(string)) ([]git.Worktree, error) {
	kept := make([]git.Worktree, 0, len(worktrees))
	for _, wt := range worktrees {
		missing, err := dblifecycle.PathMissing(wt.Path)
		if err != nil {
			return nil, fmt.Errorf("inspecting worktree %q: %w", wt.Path, err)
		}
		if !missing {
			kept = append(kept, wt)
			continue
		}
		warning, err := dblifecycle.SkipAbsentWorktree(wt.Path, wt.Locked, wt.LockReason)
		if err != nil {
			return nil, err
		}
		warn(warning)
	}
	return kept, nil
}

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
