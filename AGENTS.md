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
| Deployment plans | `.ai/plans/` |

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
- Linting must pass before commit (`golangci-lint run ./...`)

### Test-Driven Development (TDD)

Before implementing new functionality:

1. **Write failing tests first** - Create test cases that describe the expected behavior
2. **Run tests to verify they fail** - Confirm the tests fail with current implementation
3. **Implement the feature** - Write code until tests pass
4. **Refactor if needed** - Improve implementation while keeping tests green
5. **Run full test suite** - Ensure no regressions in existing functionality

Example workflow for a new scaffold step:
```bash
# 1. Create test file for the step
touch internal/scaffold/steps/composer_install_test.go

# 2. Write failing tests that describe expected behavior
# 3. Run tests to confirm they fail
go test ./internal/scaffold/steps/... -v

# 4. Implement the step
# 5. Run tests again to verify they pass
go test ./internal/scaffold/steps/... -v
```

This approach ensures:
- Clear specification of expected behavior
- Immediate feedback on implementation
- Confidence when refactoring
- Documentation through tests

## Common Tasks

### Add a New CLI Command

1. Create `internal/cli/commandname.go` following existing command patterns
2. Define cobra.Command struct with Use, Short, Long, RunE
3. Add command to root in `internal/cli/root.go` init function
4. Add tests in `internal/cli/commandname_test.go`
5. Update documentation:
   - Update `.ai/plans/arbor.md` command table
   - Add full command documentation in `.ai/plans/arbor.md`
   - Update `README.md` quick start section if needed

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

**Phase 5: Distribution** - Complete

All phases 1-5 are complete. The project has:
- Core infrastructure (worktree management, config)
- Scaffold system with presets (Laravel, PHP)
- Interactive commands (work, prune)
- Distribution via GitHub Actions

See `.ai/plans/arbor.md` for the detailed phase history and learnings.

## Refactoring Work

When working on the idiomatic refactor (`.ai/plans/idiomatic-refactor.md`):

1. **Read the plan first** - Always read `.ai/plans/idiomatic-refactor.md` before starting any refactoring work
2. **Work phase by phase** - Complete one phase before moving to the next
3. **Document findings** - After completing each phase, update the "Findings" section with:
   - Decisions made during implementation
   - Challenges encountered and how they were resolved
   - Code patterns established for consistency
   - Notes relevant to subsequent phases
4. **Mark tasks complete** - Change `- [ ]` to `- [x]` for each completed task
5. **Run verification** - After each phase:
   ```bash
   go test ./... -v
   go test ./... -race
   golangci-lint run ./...
   ```

### Refactoring Principles

- **Preserve behavior** - Refactoring should not change external behavior
- **One concern at a time** - Don't mix refactoring with feature work
- **Test before and after** - Ensure tests pass before starting, and still pass after
- **Small commits** - Commit after each logical change for easy rollback
- **Follow existing patterns** - When in doubt, match the style of surrounding code

### Code Quality Standards

When refactoring, enforce these standards:

1. **No ignored errors** - Handle all errors explicitly, don't use `_, _ =`
2. **No data races** - Use proper synchronization for concurrent access
3. **Meaningful error messages** - Include context in error wrapping
4. **Single source of truth** - No duplicated logic or constants
5. **Dependency injection** - Prefer passing dependencies over global state
6. **Testability** - Write code that can be unit tested

## Notes

- The user will review changes file-by-file before committing, **always** wait for confirmation before committing code
