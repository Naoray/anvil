package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/lifecycle"
	"github.com/naoray/anvil/internal/lockfile"
)

// acquireWorktreeUse takes the shared lifecycle gate before any state or
// preparation lock. Revalidate Git registration under the gate: standalone
// fallback locks must never authorize writes after Git teardown has started.
func acquireWorktreeUse(pc *ProjectContext, target git.Worktree) (*lockfile.Lock, error) {
	gate, err := lifecycle.AcquireShared(target.Path)
	if errors.Is(err, lockfile.ErrLocked) {
		return nil, agentapi.Errorf(agentapi.CodeCleanupBlocked, "worktree cleanup is in progress")
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, agentapi.Wrap(agentapi.CodeWorktreeNotFound, err)
		}
		return nil, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	if _, err := config.WorktreeGitDir(target.Path); err != nil {
		return nil, errors.Join(agentapi.Wrap(agentapi.CodeWorktreeNotFound, fmt.Errorf("worktree disappeared during startup: %w", err)), gate.Release())
	}
	worktrees, err := git.ListWorktrees(pc.GitDir)
	if err != nil {
		return nil, errors.Join(agentapi.Wrap(agentapi.CodeGitFailed, err), gate.Release())
	}
	registered, err := resolveExactWorktree(worktrees, target.Path)
	if err != nil {
		return nil, errors.Join(err, gate.Release())
	}
	if registered == nil {
		return nil, errors.Join(agentapi.Errorf(agentapi.CodeWorktreeNotFound, "worktree %s is no longer registered", target.Path), gate.Release())
	}
	if _, err := git.HeadCommit(target.Path); err != nil {
		return nil, errors.Join(agentapi.Wrap(agentapi.CodeGitFailed, err), gate.Release())
	}
	return gate, nil
}
