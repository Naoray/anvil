# Anvil — Claude Code Skill

Anvil is a git worktree manager for agentic development. This skill provides context for working with anvil projects.

## Key Commands

```bash
anvil link <repo>          # Link a repository for worktree management
anvil work <branch>        # Create or checkout a worktree
anvil list                 # List all worktrees
anvil remove <branch>      # Remove a worktree
anvil prune                # Remove merged worktrees
anvil info <branch>        # Print the path to a worktree
anvil scaffold <branch>    # Run scaffold steps for a worktree
anvil sync                 # Sync current worktree with upstream
```

## Worktree Workflow

When working on a feature:
1. `anvil work feat/my-feature` — creates isolated worktree
2. Implement in the worktree directory
3. `anvil prune` or `anvil remove feat/my-feature` — clean up when done

## Configuration

Project config lives in `anvil.yaml` at the repository root. Global config at `~/.config/anvil/anvil.yaml`.

## Development Conventions

- **TDD required**: Write failing tests before implementing
- **Always wait for user review** before committing
- **Development in worktrees**: Use `anvil work <branch>` for feature branches
- **Lint before commit**: `golangci-lint run ./...` must pass
- **No ignored errors**: Handle all errors explicitly
