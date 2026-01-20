# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.2.0]: https://github.com/michaeldyrynda/arbor/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/michaeldyrynda/arbor/compare/v0.0.2...v0.1.0
[0.0.2]: https://github.com/michaeldyrynda/arbor/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/michaeldyrynda/arbor/releases/tag/v0.0.1
