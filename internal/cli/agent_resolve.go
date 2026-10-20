package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/wtstate"
)

// openAgentProject opens the linked project and classifies failures.
func openAgentProject() (*ProjectContext, error) {
	pc, err := OpenProjectFromCWD()
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeProjectNotFound, err)
	}
	return pc, nil
}

// worktreeMatchesExactly reports whether query names wt by exact path, folder
// name, branch or Anvil worktree ID. Agent commands never use partial matches.
func worktreeMatchesExactly(wt git.Worktree, query string) bool {
	if query == "" {
		return false
	}
	if strings.ContainsAny(query, `/\`) || filepath.IsAbs(query) {
		if git.PathsEqual(wt.Path, query) {
			return true
		}
	}
	if filepath.Base(wt.Path) == query || wt.Branch == query {
		return true
	}
	if strings.HasPrefix(query, "wt_") {
		identity, status, err := wtstate.ReadIdentity(wt.Path)
		return err == nil && identity != nil && status == wtstate.IdentityOK && identity.ID == query
	}
	return false
}

// resolveExactWorktree finds one registered worktree. It returns (nil, nil)
// when nothing matches so callers decide whether absence is an error.
func resolveExactWorktree(worktrees []git.Worktree, query string) (*git.Worktree, error) {
	var matches []git.Worktree
	for _, wt := range worktrees {
		if wt.Bare {
			continue
		}
		if worktreeMatchesExactly(wt, query) {
			matches = append(matches, wt)
		}
	}
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return &matches[0], nil
	default:
		paths := make([]string, len(matches))
		for i, match := range matches {
			paths[i] = match.Path
		}
		return nil, agentapi.Errorf(agentapi.CodeAmbiguous, "%q matches %d worktrees; use the path or worktree ID", query, len(matches)).
			WithDetails(map[string]any{"matches": paths})
	}
}

// selectAgentWorktree resolves WORKTREE, or the worktree containing the
// current directory when no argument is given.
func selectAgentWorktree(pc *ProjectContext, args []string) (*git.Worktree, error) {
	worktrees, err := git.ListWorktrees(pc.GitDir)
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeGitFailed, err)
	}
	if len(args) > 0 && args[0] != "" {
		wt, err := resolveExactWorktree(worktrees, args[0])
		if err != nil {
			return nil, err
		}
		if wt == nil {
			return nil, agentapi.Errorf(agentapi.CodeWorktreeNotFound, "no worktree named %q (exact folder, branch, path or worktree ID required)", args[0]).
				WithDetails(map[string]any{"query": args[0]})
		}
		return wt, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	wt, err := selectWorktreeByContainment(worktrees, cwd)
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	if wt == nil || wt.Bare {
		return nil, agentapi.Errorf(agentapi.CodeInvalidArguments, "not inside a worktree; pass WORKTREE explicitly")
	}
	return wt, nil
}
