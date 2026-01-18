# AGENTS.md - Development Guide for Arbor

This file provides important context for developing the Arbor project.

## Source of Truth

The complete specification and development workflow is located at:
```
.ai/plans/arbor.md
```

**Always read `.ai/plans/arbor.md` before starting work.** It contains:
- Command specifications
- Configuration file formats
- Scaffold step definitions
- Preset configurations
- Detailed development workflow

## Development Location

All development occurs **inside a worktree**. This allows:
- Feature development on dedicated branches
- Clean separation from the bare repository
- Easy worktree creation/removal for testing

```bash
# Start development in a worktree
arbor work feature/my-feature
cd feature-my-feature
# Make changes, test, commit
arbor remove feature-my-feature  # When done
```

## Quick Reference

### File Locations

| Purpose | Location |
|---------|----------|
| CLI commands | `internal/cli/` |
| Config management | `internal/config/` |
| Git operations | `internal/git/` |
| Scaffold system | `internal/scaffold/` |
| Presets | `internal/presets/` |
| Utilities | `internal/utils/` |
| Entry point | `cmd/arbor/main.go` |
| Tests | Alongside implementation files (`*_test.go`) |

### Config Files

| Config | Location | Purpose |
|--------|----------|---------|
| Project | `arbor.yaml` in worktree root | Project-specific settings |
| Global | `~/.config/arbor/arbor.yaml` | User defaults |
| Plan | `.ai/plans/arbor.md` | Complete specification |

### Step Naming

Steps use dot notation: `language.tool.command`
- `php.composer.install`
- `node.npm.run`
- `herd.link`
- `bash.run`

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid arguments |
| 3 | Worktree not found |
| 4 | Git operation failed |
| 5 | Configuration error |
| 6 | Scaffold step failed |

## Testing

### Running Tests

```bash
# All tests
go test ./... -v

# With coverage
go test ./... -cover

# Specific package
go test ./internal/utils/... -v
```

### Test Requirements

- New functionality requires unit tests
- CLI commands require integration tests
- All tests must pass before commit

## Common Tasks

### Add a New CLI Command

1. Create `internal/cli/commandname.go`
2. Define cobra.Command struct
3. Add to root in `internal/cli/root.go`
4. Add tests in `internal/cli/commandname_test.go`

### Add a New Scaffold Step

1. Create step implementation in `internal/scaffold/steps/`
2. Register in step executor
3. Add tests
4. Document in `.ai/plans/arbor.md`

### Add a New Preset

1. Create `internal/presets/presetname.go`
2. Implement Preset interface
3. Register in preset manager
4. Document in `.ai/plans/arbor.md`

## Current Phase

**Phase 1: Core Infrastructure** - Complete

See `.ai/plans/arbor.md` for the current phase status and next steps.

## Notes

- The `scripts/` directory contains example scripts and is not part of the repository
- Review changes file-by-file before committing
