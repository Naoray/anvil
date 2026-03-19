# Anvil — Claude Development Guide

See [AGENTS.md](./AGENTS.md) for full architecture, workflows, and conventions.

## Key Constraints

- **TDD required**: Write failing tests before implementing. See AGENTS.md for workflow.
- **Always wait for user review** before committing — never auto-commit.
- **Development in worktrees**: Use `anvil work <branch>` for feature branches.
- **Lint before commit**: `golangci-lint run ./...` must pass (pinned to v2.1.2).
- **No ignored errors**: Handle all errors explicitly; never `_, _ =`.
- **No data races**: Use proper synchronization for concurrent access.

## Release

Use the `/release` skill (`.opencode/skills/release/SKILL.md`).

## Framework-Specific Rules

Rules auto-load from `.claude/rules/` for matching file types:
- `charm.md` — Bubble Tea / Charm TUI patterns (`**/*.go`)
- `go-cli.md` — Cobra CLI patterns (`**/*.go`)
