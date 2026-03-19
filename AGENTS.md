# AGENTS.md - Development Guide for Anvil

This file provides important context for developing the Anvil project.

## Documentation

The project documentation is split between user-facing and development documentation:

**User Documentation** (kept up-to-date via release process):
- [README.md](./README.md) - Complete command reference, configuration guide, and scaffold step documentation

**Development Documentation:**
- This file (AGENTS.md) - Development workflow, architecture, and contribution guidelines
- `.ai/plans/anvil.md` - Historical development plan (phases 1-5, complete). Note: This file is gitignored and contains the original implementation plan.

## Development Location

All development occurs **inside a worktree**. This allows:
- Feature development on dedicated branches
- Clean separation from the main repository
- Easy worktree creation/removal for testing

```bash
# Start development in a worktree
anvil work feature/my-feature
anvil info feature-my-feature
# Make changes, test, commit
anvil remove feature-my-feature  # When done
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
| UI components | `internal/ui/` |
| Shell completions | `internal/cli/completion.go` |
| Entry point | `cmd/anvil/main.go` |
| Tests | Alongside implementation files (`*_test.go`) |

### Config Files

| Config | Location | Purpose | Versioned? |
|--------|----------|---------|------------|
| Project | `<project-root>/anvil.yaml` | Project-specific settings, scaffold config, pre-flight checks | No (not in a repo) |
| Repository | `<worktree>/anvil.yaml` | Team defaults | Yes (committed to git) |
| Local State | `<worktree>/.anvil.local` | Runtime state (db_suffix) | No (gitignored) |
| Global | `~/.config/anvil/anvil.yaml` | User defaults | No (local machine) |

### Step Naming

Steps use simplified dot notation where the tool namespace maps to the binary:

**Binary Steps:** Execute the corresponding tool with configured arguments
- `php` - Run PHP with args
- `php.composer` - Run composer (e.g., `install`, `update`)
- `php.laravel` - Run artisan commands
- `node.npm` - Run npm (e.g., `ci`, `run build`)
- `node.yarn` - Run yarn
- `node.pnpm` - Run pnpm
- `node.bun` - Run bun
- `herd` - Run herd (e.g., `link --secure`, `unlink`)
- `herd.link` - Link worktree to Herd

**Special Steps:** Perform scaffold operations
- `file.copy` - Copy files
- `env.read` - Read .env values
- `env.write` - Write .env values
- `env.copy` - Copy between env files
- `db.create` - Create database
- `db.destroy` - Drop database
- `bash.run` - Run bash commands
- `command.run` - Run arbitrary commands

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

# With race detector
go test ./... -race

# With coverage
go test ./... -cover

# Specific package
go test ./internal/utils/... -v
```

### Linting

Install golangci-lint (pinned to v2.1.2):

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v2.1.2
```

Ensure `~/go/bin` is in your PATH:

```bash
export PATH=$PATH:$(go env GOPATH)/bin
```

Run linter:

```bash
golangci-lint run ./...
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
5. If the command accepts a worktree argument, add `ValidArgsFunction: completeWorktreeNames` for shell completion
6. Update documentation:
   - Update `README.md` with command reference and examples
   - Update `internal/cli/root.go` `printBanner()` to include the new command in the banner list
   - Update AGENTS.md quick reference section if needed

### Add a New Scaffold Step

1. Create step implementation in `internal/scaffold/steps/`
2. Register in step executor
3. Add tests
4. Document in `README.md` scaffold steps section
5. Consider adding pre-flight checks if the step requires specific dependencies (environment variables, commands, files)

### Add a New Preset

1. Create `internal/presets/presetname.go`
2. Implement Preset interface
3. Register in preset manager
4. Document in `README.md`

## Current Phase

**Post-v1.0 — Active Development**

All foundational phases (1-5) are complete. The project is in active feature development:
- Core: worktree management, config hierarchy, scaffold system
- Presets: Laravel, PHP, Laravel-Shared-DB
- Commands: link, unlink, work, list, info, open, sync, remove, prune, scaffold, pull-config, repair, install, completion
- Distribution: Homebrew, GitHub Releases, Go Install
- Shell completions for worktree arguments (zsh, bash, fish, powershell)

### Code Quality Standards

When refactoring, enforce these standards:

1. **No ignored errors** - Handle all errors explicitly, don't use `_, _ =`
2. **No data races** - Use proper synchronization for concurrent access
3. **Meaningful error messages** - Include context in error wrapping
4. **Single source of truth** - No duplicated logic or constants
5. **Dependency injection** - Prefer passing dependencies over global state
6. **Testability** - Write code that can be unit tested

#### Error Handling Conventions

**Return errors for:**
- User input validation failures (unknown step names, invalid config)
- I/O operations (file read/write, network calls, database operations)
- Business logic failures that callers should handle

**Panic for:**
- Programmer errors that indicate code bugs (duplicate step registration, nil pointer dereference)
- Invalid assumptions that should never happen in production
- Initialization failures that prevent the program from functioning

**Guideline:** If the error could be triggered by user input or external conditions, return it. If it indicates a programming mistake or unrecoverable state, panic.

#### Deterministic Iteration

Go map iteration order is random. For deterministic behavior:

- Maintain an ordered slice alongside maps when iteration order matters
- Use the slice for iteration, the map for O(1) lookups
- Example: `presetOrder []string` in ScaffoldManager ensures consistent preset detection

```go
// Good: Deterministic iteration
type Manager struct {
    presets     map[string]Preset
    presetOrder []string  // Maintains registration order
}

// Bad: Random iteration order
for _, preset := range m.presets {  // Order varies between runs
    // ...
}
```

## Release Management

When preparing a new release, follow the release skill:

**Trigger:** `/release`

**Skill Location:** `.opencode/skills/release/SKILL.md`

**What it does:**
1. Analyzes commits to recommend version bump (MAJOR/MINOR/PATCH)
2. Runs all tests and quality checks
3. Updates CHANGELOG.md with proper formatting
4. Updates README.md to reflect current features
5. Creates commit and tag
6. Monitors CI/CD and verifies release

**Requirements:**
- All changes committed
- Working directory clean
- `gh` CLI tool installed
- Tests must pass

**Version Format:** Always use `v` prefix (e.g., `v0.4.1`)

**Process:**
1. User triggers `/release`
2. Skill analyzes repository and presents version options
3. User selects version
4. Skill runs quality gates (tests, build, vet)
5. Skill updates documentation (CHANGELOG, README)
6. Skill creates draft or final release based on user choice
7. Skill commits, tags, and pushes
8. Skill monitors CI/CD and verifies release success

## Notes

- The user will review changes file-by-file before committing, **always** wait for confirmation before committing code
- **Never** force commit (`git add -f`) a file that is in the `.gitignore` file unless explicitly instructed to do so
  - Files in `.gitignore` are excluded from version control for a reason
  - Even if a plan document or other file needs to be updated, respect the gitignore rules
  - Wait for explicit user instruction before overriding gitignore
- For releases, the skill handles the process automatically once approved
