---
name: anvil-agent
description: Use when working in repositories managed by Anvil, a git worktree manager for agentic development. Covers worktree commands and workflow, scaffold configuration, isolated application and test databases, read-only command execution with anvil exec, safe cleanup, and development conventions for AI coding agents including Codex and Claude Code.
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
anvil exec -- <command>    # Run with the worktree test database exported
anvil sync                 # Sync current worktree with upstream
anvil remove <branch>      # Remove a worktree
anvil remove <branch> --dry-run  # Preview database and worktree cleanup
anvil remove <branch> --keep-db  # Remove the worktree but retain databases
anvil prune                # Remove merged worktrees
```

## Workflow

1. Create isolated feature work with `anvil work feature/name`.
2. Move into the worktree path from the command output or `anvil info feature-name`.
3. Implement and test inside the worktree.
4. In Laravel worktrees, run test and pre-push commands through `anvil exec -- <command>` so they use the worktree's isolated test database.
5. Wait for user review before committing.
6. Preview cleanup with `anvil remove feature-name --dry-run`, then clean up with `anvil remove feature-name` or `anvil prune` after the work is landed.

## Config Files

- Repository config: `anvil.yaml`
- Local worktree state: `.anvil.local` (generated and gitignored; contains `db_suffix` and owned database records)
- Global config: `~/.config/anvil/anvil.yaml`

Do not rewrite `.anvil.local` casually. Anvil preserves unknown fields and uses its ownership records to decide which databases cleanup may drop.

## Test Database Isolation

The Laravel preset creates and records an application database plus an empty testing database:

```yaml
databases:
  - name: myapp_swift_runner
    engine: mysql
    role: application
  - name: myapp_swift_runner_test
    engine: mysql
    role: testing
```

Use `anvil exec` for local test commands instead of invoking them bare. It is read-only: it resolves the recorded databases, exports `DB_DATABASE` and `ANVIL_TEST_DB_DATABASE` for the testing database plus `ANVIL_DB_DATABASE` when the application database is known, then runs the child without modifying databases or `.anvil.local`.

- Child exit codes pass through. Exit `127` means missing; `126` means found but not executable; `1` is an Anvil resolution error.
- `anvil remove <worktree> --dry-run` performs live enumeration but no mutation and prints the exact owned application, testing, and parallel-worker databases it would drop.
- `anvil remove <worktree> --keep-db` removes the worktree while preserving every database.
- Unreadable or invalid ownership state stops removal before the worktree is deleted unless the user explicitly supplies `--force`; forced removal leaves databases untouched.
- Pre-v1.8 worktrees may lack a testing record. Do not assume one or let `anvil exec` create it; scaffold a testing-role `db.create` step or create a fresh worktree.

## Laravel Scaffold Cache

After the Laravel preset creates or updates `.env`, it clears the PHPStan/Larastan result cache when `vendor/bin/phpstan` exists. Projects without that binary are skipped. Account for this command when diagnosing scaffold failures or writing preset tests.

## Development Rules

- Use TDD for behavior changes: write a failing test, confirm it fails, then implement.
- Run focused tests first, then broader tests before handoff.
- Run `golangci-lint run ./...` before commit when available.
- Handle errors explicitly; do not ignore returned errors.
- Do not commit until the user has reviewed and approved the changes.
