# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.9.2] - 2026-02-04

### Added
- **Homebrew Support** - Arbor is now available via Homebrew
  - Install with `brew tap artisanexperiences/tap && brew install arbor`
  - Automated formula updates on new releases
  - Support for macOS (arm64/amd64) and Linux (arm64/amd64)

### Changed
- **BREAKING:** Migrated repository from `michaeldyrynda` to `artisanexperiences` organization
  - Module path changed from `github.com/michaeldyrynda/arbor` to `github.com/artisanexperiences/arbor`
  - All import paths updated across the codebase
  - GitHub URLs updated in documentation
  - Users installing via `go install` must update their commands

### Added
- MIT LICENSE file with proper copyright attribution
- Automated Homebrew formula update workflow
- Enhanced installation documentation with multiple installation methods

## [0.9.1] - 2026-02-04

### Added
- Laravel preset now captures and stores the generated app key for reuse
  - App key is stored as `AppKey` variable in scaffold context
  - Can be referenced in subsequent steps via `{{ .AppKey }}` template
  - Automatically written back to `.env` file after generation

### Fixed
- Ensure consistency of variables being passed via ldflags during build

## [0.9.0] - 2026-02-04

### Added
- Pre-flight checks to validate dependencies before scaffold execution
  - Check for required commands, files, environment variables, and configuration
  - Conditional evaluation support for complex dependency validation
  - Clear error messages listing missing dependencies
  - Abort scaffold early when dependencies are not met

- Improved diagnostics and error messages for scaffold operations

### Fixed
- Correct ldflags path for version info in build configuration

## [0.8.1] - 2026-02-03

### Fixed

- Prevent `arbor init` from overwriting copied repository config
  - Now skips unnecessary SaveProject call when repo config is copied
  - Only saves when explicitly setting a preset flag
  
- Preserve YAML formatting and key ordering in config files
  - Use AST-based yaml.Node instead of map serialization
  - Maintain original structure, comments, and whitespace
  - Only modify values that actually changed

## [0.8.0] - 2026-02-03

### Added

- Separate local state from team config
  - New `.arbor.local` file for runtime state (gitignored)
  - Local state stores `db_suffix` for database naming
  - Automatic migration from old config format on first run
  - No manual intervention required - seamless upgrade

### Changed

- `arbor.yaml` now contains only team-shared configuration
  - Scaffold steps and presets remain in team config
  - Database suffix moves to local state file
  - Cleaner separation between shared and local settings

### Removed

- Deprecated worktree config helpers from config package
  - Simplified configuration management
  - Reduced code complexity

## [0.7.0] - 2026-02-02

### Added

- New `arbor sync` command for synchronizing worktrees with upstream branches
  - Fetch from remote and rebase (default) or merge with upstream
  - Interactive prompts for upstream branch and strategy selection
  - Configuration persistence to `arbor.yaml`
  - Support for custom remotes (default: origin)
  - Conflict detection with actionable error messages
  - Pre-flight checks for detached HEAD, dirty worktree, and in-progress operations

### Fixed

- Added missing `repair` and `version` commands to the CLI banner

## [0.6.0] - 2026-02-02

### Added

- New `arbor repair` command for fixing git configuration in existing projects
  - Configure fetch refspec in `.bare` directory for remote branch tracking
  - Automatically set up branch tracking for all local branches with remote counterparts
  - Interactive prompts for remote URL confirmation and editing
  - `--dry-run` flag to preview changes without applying
  - `--refspec-only` and `--tracking-only` flags for partial repairs
  - Idempotent - safe to run multiple times

- Automatic branch tracking in `arbor work` command
  - New worktrees automatically set up upstream tracking to origin
  - `--no-track` flag to skip tracking setup when needed
  - Non-fatal errors - worktree creation continues even if tracking fails

- Automatic fetch refspec configuration in `arbor init`
  - Bare repositories now automatically configured for remote tracking
  - Enables fetch, merge, and rebase operations from remote branches

- New git helper functions for remote and branch management
  - `ConfigureFetchRefspec()` - Set up remote.origin.url and fetch refspec
  - `SetBranchUpstream()` - Configure branch tracking
  - `GetBranchRefs()` - List local and remote branches
  - `HasFetchRefspec()` and `HasBranchTracking()` - Check configuration state

### Fixed

- Windows CI failures resolved

## [0.5.0] - 2026-02-01

### Added
- Gradient block letter header with tree-themed styling for CLI commands
- New scaffold step `env.copy` for copying environment variables between worktrees
- Step validation framework with `Validate()` interface for all scaffold steps
- File system interface abstraction for testable I/O operations
- Per-step configuration validation for early error detection

### Changed
- Renamed `php.laravel.artisan` to `php.laravel` for consistency
- Replaced Viper config library with yaml.v3 for cleaner config writes
- Migrated to embedded `BaseStepConfig` for all step configurations

### Refactored
- Implemented explicit step registry with ordered slice for deterministic iteration
- Introduced Command Executor interface for testable command execution

## [0.4.2] - 2026-02-01

### Added
- ARM64 architecture support for release binaries (macOS ARM64, Linux ARM64)
- Checksum generation for all release artifacts (SHA256)
- Reproducible builds with version information embedded in binary

### Fixed
- Resolve golangci-lint v2 errors
- Update documentation accuracy and cleanup config schema

### Changed
- Upgrade golangci-lint to v2.1.2 for Go 1.24 compatibility
- Upgrade golangci-lint-action to v7 for golangci-lint v2 support
- Align Go versions across CI workflows
- Pin linter version for consistent builds

### Refactored
- Extract helpers for better code organization
- Implement deterministic preset detection
- Add condition accessors for cleaner code
- Return errors from step registry instead of silent failures

## [0.4.1] - 2026-01-30

### Added
- Output capture for scaffold steps with `store_as` option
  - Capture command output from `bash.run`, `command.run`, and all binary steps
  - Store output as template variables for use in subsequent steps
  - Automatic whitespace trimming of captured output
  - Works with: `php`, `php.composer`, `php.laravel`, `node.npm`, `node.yarn`, `node.pnpm`, `node.bun`, `herd`

## [0.3.1] - 2026-01-29

### Fixed
- Database naming now uses sanitized site name to handle hyphenated branch names correctly
- Laravel preset writes DB_DATABASE to .env after database creation to ensure migrations run against correct database
- Default branch worktrees now use saved SiteName instead of folder name to prevent Herd link collisions
- Scaffold command now correctly detects when running from project root vs worktree
- env.write step race conditions resolved with file locking for concurrent execution
- env.write now respects configured priority and creates parent directories as needed

### Changed
- Removed priority system in favor of sequential step execution for predictable ordering
- Steps now execute in the exact order they appear in configuration

## [0.3.0] - 2026-01-28

### Added
- New scaffold steps: env.read, env.write, db.create, db.destroy
- Template variable system with dynamic substitution using Go's text/template
- Built-in template variables: Path, RepoPath, RepoName, SiteName, Branch, DbSuffix, SanitizedSiteName
- Custom variables from env.read steps available to subsequent steps
- Support for multiple databases with shared suffix generation
- DatabaseClient abstraction with mock support for testing
- Readable database name generation using adjective_noun word lists
- Automatic retry on database name collisions (up to 5 attempts)
- Persistent suffix storage in worktree-local config for cleanup
- Support for MySQL, PostgreSQL, and SQLite

### Improved
- Mock implementations for database operations eliminate need for containerized tests
- Improved test coverage across scaffold steps

## [0.2.0] - 2026-01-20

### Major Changes
- Complete interactive UI overhaul using Charm libraries
  - Styled tables for 'arbor list'
  - Interactive prompts for all commands
  - Spinners for long-running operations
  - Command output styling
  - Root command banner and global flags
  - Tree-themed color palette

### Enhanced
- Enhanced 'arbor remove' command
  - Add --delete-branch flag
  - Interactive prompt for branch deletion
  - Improved worktree picker when folder arg missing

### Added
- New 'arbor destroy' command for project cleanup

### Fixed
- Strip '+' prefix from branch names
- Force delete when user confirms branch deletion
- Prevent deletion of main worktree
- Ensure site name on init, folder name on work
- CI workflow updated to Go 1.24
- Various test fixes
- Show worktree picker when folder arg missing, regardless of --force
- Use IsInteractive() for initial arg prompts instead of ShouldPrompt

## [0.1.0] - 2026-01-20

### Added
- 'arbor list' command to display worktrees with their status
- Comprehensive documentation updates

### Fixed
- OS condition test to use runtime.GOOS

### Refactored
- Complete Phase 6 polish & cross-platform fixes
- Complete Phase 5 performance improvements
- Complete Phase 4 code consolidation
- Complete Phase 3 error handling improvements
- Complete Phase 2 quick wins
- Complete Phase 1 critical fixes

### Testing
- Add Phase 0 safety net tests for refactor

## [0.0.2] - 2026-01-20

### Added
- 'arbor list' command to display worktrees
- Update documentation with list command
- Use tag annotation for release notes

## [0.0.1] - 2026-01-19

### Added
- Initial release
- Git worktree management
- Project initialization with scaffolding
- Laravel and PHP presets
- Interactive commands (work, prune)
- Multi-platform builds and CI/CD

[0.9.0]: https://github.com/artisanexperiences/arbor/compare/v0.8.1...v0.9.0
[0.8.1]: https://github.com/artisanexperiences/arbor/compare/v0.8.0...v0.8.1
[0.8.0]: https://github.com/artisanexperiences/arbor/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/artisanexperiences/arbor/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/artisanexperiences/arbor/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/artisanexperiences/arbor/compare/v0.4.2...v0.5.0
[0.4.2]: https://github.com/artisanexperiences/arbor/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/artisanexperiences/arbor/compare/v0.4.0...v0.4.1
[0.3.1]: https://github.com/artisanexperiences/arbor/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/artisanexperiences/arbor/compare/v0.2.4...v0.3.0
[0.2.0]: https://github.com/artisanexperiences/arbor/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/artisanexperiences/arbor/compare/v0.0.2...v0.1.0
[0.0.2]: https://github.com/artisanexperiences/arbor/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/artisanexperiences/arbor/releases/tag/v0.0.1
