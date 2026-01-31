# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.1] - 2026-01-30

### Added
- Output capture for scaffold steps with `store_as` option
  - Capture command output from `bash.run`, `command.run`, and all binary steps
  - Store output as template variables for use in subsequent steps
  - Automatic whitespace trimming of captured output
  - Works with: `php`, `php.composer`, `php.laravel.artisan`, `node.npm`, `node.yarn`, `node.pnpm`, `node.bun`, `herd`

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

[0.4.1]: https://github.com/michaeldyrynda/arbor/compare/v0.4.0...v0.4.1
[0.3.1]: https://github.com/michaeldyrynda/arbor/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/michaeldyrynda/arbor/compare/v0.2.4...v0.3.0
[0.2.0]: https://github.com/michaeldyrynda/arbor/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/michaeldyrynda/arbor/compare/v0.0.2...v0.1.0
[0.0.2]: https://github.com/michaeldyrynda/arbor/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/michaeldyrynda/arbor/releases/tag/v0.0.1
