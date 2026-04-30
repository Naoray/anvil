---
name: anvil-agent
description: Use when working in repositories managed by Anvil, a git worktree manager for agentic development. Covers Anvil commands, worktree workflow, config files, and development conventions for AI coding agents including Codex and Claude Code.
---

# Anvil Agent

Anvil manages git worktrees for agentic development. Use this skill when a repository has `anvil.yaml`, references Anvil in `AGENTS.md`, or the user asks you to create, inspect, open, sync, scaffold, or remove Anvil worktrees.

## Core Commands

```bash
anvil link <repo>          # Link a repository for worktree management
anvil work <branch>        # Create or checkout a worktree
anvil list                 # List linked worktrees
anvil info <branch>        # Print the path to a worktree
anvil scaffold <branch>    # Run scaffold steps for a worktree
anvil sync                 # Sync current worktree with upstream
anvil remove <branch>      # Remove a worktree
anvil prune                # Remove merged worktrees
```

## Workflow

1. Create isolated feature work with `anvil work feature/name`.
2. Move into the worktree path from the command output or `anvil info feature-name`.
3. Implement and test inside the worktree.
4. Wait for user review before committing.
5. Clean up with `anvil remove feature-name` or `anvil prune` after the work is landed.

## Config Files

- Repository config: `anvil.yaml`
- Local worktree state: `.anvil.local` (should be gitignored)
- Global config: `~/.config/anvil/anvil.yaml`

## Development Rules

- Use TDD for behavior changes: write a failing test, confirm it fails, then implement.
- Run focused tests first, then broader tests before handoff.
- Run `golangci-lint run ./...` before commit when available.
- Handle errors explicitly; do not ignore returned errors.
- Do not commit until the user has reviewed and approved the changes.
