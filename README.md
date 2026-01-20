# Arbor

Arbor is a self-contained binary for managing git worktrees to assist with agentic development of applications. It is cross-project, cross-language, and cross-environment compatible.

## Development

All development occurs inside a worktree:

```bash
# Create a worktree for development
arbor work feature/new-feature
cd feature-new-feature

# Make changes, test, commit
go test ./...
arbor work another-feature  # Create another if needed

# When done with a worktree
cd ..
arbor remove feature-new-feature
```

## Installation

```bash
# Clone and build
git clone git@github.com:michaeldyrynda/arbor.git
cd arbor
go build -o arbor ./cmd/arbor

# Or install via Homebrew (coming soon)
brew install arbor
```

## Quick Start

```bash
# Initialise a new Laravel project
arbor init git@github.com:user/my-laravel-app.git

# Create a feature worktree
arbor work feature/user-auth

# Create a worktree from a specific base branch
arbor work feature/user-auth -b develop

# List all worktrees with their status
arbor list

# Remove a worktree when done
arbor remove feature/user-auth

# Clean up merged worktrees
arbor prune

# Destroy the entire project (removes worktrees and bare repo)
arbor destroy
```

## Documentation

See [AGENTS.md](./AGENTS.md) for development guide.
See [.ai/plans/arbor.md](./.ai/plans/arbor.md) for complete specification including:
- Command reference
- Configuration files
- Scaffold presets
- Testing strategy

## License

MIT
