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

# Run scaffold steps on an existing worktree
arbor scaffold main
arbor scaffold feature/user-auth

# Destroy the entire project (removes worktrees and bare repo)
arbor destroy
```

## Documentation

See [AGENTS.md](./AGENTS.md) for development guide.

- Command reference
- Configuration files
- Scaffold presets
- Testing strategy

## Commands

### `arbor scaffold [PATH]`

Run scaffold steps for an existing worktree. This is useful when:

- You used `arbor init --skip-scaffold` to clone without running scaffold
- You want to re-run scaffold steps on an existing worktree
- You need to scaffold a worktree you're not currently in

```bash
# Scaffold a specific worktree by path
arbor scaffold main
arbor scaffold feature/user-auth

# When inside a worktree, scaffold current (prompts for confirmation)
arbor scaffold

# When at project root without args, interactively select worktree
arbor scaffold
```

### `arbor init` with `--skip-scaffold`

Skip scaffold steps during init and run them manually later:

```bash
# Clone without scaffolding
arbor init git@github.com:user/repo.git --skip-scaffold

# Scaffold when ready
arbor scaffold main
```

## Configuration

Arbor uses a configuration file to define scaffold steps for `init` and `work` commands. Configuration is read from `arbor.yaml` in your project root.

### Scaffold Steps

Scaffold steps define actions to run when creating a new worktree. Each step can:

- Run commands (bash, binary, composer, npm, etc.)
- Manage databases (create/destroy)
- Read/write environment variables
- Copy files
- Execute Laravel Artisan commands

### Configuration Structure

```yaml
scaffold:
  steps:
    - name: step.name
      enabled: true
      priority: 10
      args: ["--option"]
      condition:
        env_file_contains:
          file: .env
          key: DB_CONNECTION

cleanup:
  steps:
    - name: cleanup.step
```

### Template Variables

All steps support template variables that are replaced at runtime:

| Variable | Description | Example |
|----------|-------------|---------|
| `{{ .Path }}` | Worktree directory name | `feature-auth` |
| `{{ .RepoPath }}` | Project directory name | `myapp` |
| `{{ .RepoName }}` | Repository name | `myapp` |
| `{{ .SiteName }}` | Site/project name | `myapp` |
| `{{ .Branch }}` | Git branch name | `feature-auth` |
| `{{ .DbSuffix }}` | Database suffix (from db.create) | `swift_runner` |
| `{{ .VarName }}` | Custom variable from env.read | Custom values |

### Built-in Steps

#### Database Steps

**`db.create`** - Create a database with unique name

```yaml
- name: db.create
  type: mysql  # or pgsql, auto-detected from DB_CONNECTION if omitted
```

- Generates unique name: `{site_name}_{adjective}_{noun}`
- Auto-detects engine from `DB_CONNECTION` in `.env`
- Retries up to 5 times on collision
- Persists suffix to worktree-local `arbor.yaml` for cleanup

**`db.destroy`** - Clean up databases matching suffix pattern

```yaml
- name: db.destroy
  type: mysql  # matches db.create type
```

- Drops all databases matching the suffix pattern
- Runs automatically during `arbor remove`

#### Environment Steps

**`env.read`** - Read from `.env` and store as variable

```yaml
- name: env.read
  key: DB_HOST
  store_as: DbHost  # optional, defaults to key name
  file: .env        # optional, defaults to .env
```

- Stores value as `{{ .DbHost }}` for later steps
- Fails if key not found

**`env.write`** - Write to `.env` file

```yaml
- name: env.write
  key: DB_DATABASE
  value: "{{ .SiteName }}_{{ .DbSuffix }}"
  file: .env  # optional, defaults to .env
```

- Creates `.env` if missing
- Replaces existing values in-place
- Preserves comments, blank lines, and ordering
- Supports template variables

#### Node.js Steps

**`node.npm`** - npm package manager

```yaml
- name: node.npm
  args: ["install"]
  priority: 10
```

**`node.yarn`** - Yarn package manager

```yaml
- name: node.yarn
  args: ["install"]
  priority: 10
```

**`node.pnpm`** - pnpm package manager

```yaml
- name: node.pnpm
  args: ["install"]
  priority: 10
```

**`node.bun`** - Bun package manager

```yaml
- name: node.bun
  args: ["install"]
  priority: 10
```

#### PHP Steps

**`php.composer`** - Composer dependency manager

```yaml
- name: php.composer
  args: ["install"]
  priority: 10
```

**`php.laravel.artisan`** - Laravel Artisan commands

```yaml
- name: php.laravel.artisan
  args: ["migrate:fresh", "--no-interaction"]
  priority: 20
```

**`herd.link`** - Laravel Herd link

```yaml
- name: herd.link
```

#### Utility Steps

**`bash.run`** - Run bash commands

```yaml
- name: bash.run
  command: echo "Setting up {{ .Path }}"
```

**`file.copy`** - Copy files with template replacement

```yaml
- name: file.copy
  from: .env.example
  to: .env
```

**`command.run`** - Run any command

```yaml
- name: command.run
  command: npm
  args: ["run", "build"]
```

### Step Options

All steps support these configuration options:

| Option | Type | Description |
|--------|------|-------------|
| `enabled` | boolean | Enable/disable step (default: true) |
| `priority` | integer | Execution order (lower runs first, default: 0) |
| `condition` | object | Conditional execution rules |

### Conditions

Steps can be conditionally executed based on environment:

```yaml
condition:
  env_file_contains:
    file: .env
    key: DB_CONNECTION
```

### Example Configuration

Complete example for a Laravel project:

```yaml
scaffold:
  steps:
    # Create database if DB_CONNECTION is set
    - name: db.create
      priority: 5
      condition:
        env_file_contains:
          file: .env
          key: DB_CONNECTION

    # Write database name to .env
    - name: env.write
      priority: 6
      key: DB_DATABASE
      value: "{{ .SiteName }}_{{ .DbSuffix }}"

    # Install dependencies
    - name: php.composer
      priority: 10
      args: ["install"]

    - name: node.npm
      priority: 11
      args: ["install"]

    # Run migrations
    - name: php.laravel.artisan
      priority: 20
      args: ["migrate:fresh", "--no-interaction"]

    # Set domain based on worktree path
    - name: env.write
      priority: 21
      key: APP_DOMAIN
      value: "app.{{ .Path }}.test"

    # Generate application key
    - name: php.laravel.artisan
      priority: 22
      args: ["key:generate"]

cleanup:
  steps:
    # Clean up databases
    - name: db.destroy
```

### What We Handle For You

**Database Naming**
- Automatically generates unique, readable database names
- Format: `{site_name}_{adjective}_{noun}` (e.g., `myapp_swift_runner`)
- Handles collisions with automatic retries
- Enforces PostgreSQL/MySQL length limits

**Database Cleanup**
- Automatically drops databases when worktree is removed
- Uses pattern matching to find all databases with same suffix
- Integrates with `arbor remove` command

**Template Variables**
- All template syntax uses Go's `text/template`
- Handles whitespace variations: `{{ .Path }}`, `{{ .Path }}`, `{{  .Path  }}`
- Fails fast on unknown variables with clear error messages
- Supports dynamic variables from previous steps

**File Operations**
- Atomic writes for environment files
- Preserves file permissions
- Maintains existing formatting (comments, blank lines, ordering)
- Creates directories as needed

**Error Handling**
- Graceful degradation where appropriate
- Clear error messages for configuration issues
- Non-fatal warnings for optional operations

## License

MIT
