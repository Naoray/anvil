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

### Via Homebrew (Recommended for macOS/Linux)

```bash
brew tap artisanexperiences/tap
brew install arbor
```

**Upgrade:**
```bash
brew upgrade arbor
```

### Via Direct Download

Download the latest release for your platform from the [releases page](https://github.com/artisanexperiences/arbor/releases).

### Via Go Install

```bash
go install github.com/artisanexperiences/arbor/cmd/arbor@latest
```

*Note: Installing via `go install` builds without version information. Use Homebrew or download releases for proper version metadata.*

### Build from Source

```bash
# Clone the repository
git clone https://github.com/artisanexperiences/arbor.git
cd arbor

# Build for your platform
go build -o arbor ./cmd/arbor

# Or build with version information
VERSION=$(git describe --tags --always)
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
go build -ldflags "-X main.Version=$VERSION -X main.Commit=$COMMIT -X main.BuildDate=$DATE" -o arbor ./cmd/arbor
```

## Quick Start

```bash
# Check arbor version
arbor version

# Initialise a new Laravel project
arbor init git@github.com:user/my-laravel-app.git

# Create a feature worktree
arbor work feature/user-auth

# Create a worktree from a specific base branch
arbor work feature/user-auth -b develop

# Sync current worktree with upstream (defaults to main, uses rebase)
arbor sync

# Sync with a specific upstream branch
arbor sync --upstream develop

# Sync using merge instead of rebase
arbor sync --strategy merge

# Save sync settings to arbor.yaml for future use
arbor sync --upstream develop --strategy rebase --save

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

### `arbor sync`

Synchronizes the current worktree branch with an upstream branch by fetching the latest changes and rebasing or merging.

```bash
# Sync with default settings (upstream: main, strategy: rebase)
arbor sync

# Sync with a specific upstream branch
arbor sync --upstream develop
arbor sync -u develop

# Sync using merge instead of rebase
arbor sync --strategy merge
arbor sync -s merge

# Use a specific remote
arbor sync --remote upstream
arbor sync -r upstream

# Skip all confirmations
arbor sync --yes
arbor sync -y

# Save sync settings to arbor.yaml for future use
arbor sync --save

# Combination of options
arbor sync --upstream main --strategy rebase --save
```

**Configuration:**

Sync settings can be persisted in `arbor.yaml`:

```yaml
sync:
  upstream: main
  strategy: rebase
  remote: origin
```

The command resolves settings in this order:
1. CLI flags (`--upstream`, `--strategy`, `--remote`)
2. Project config (`arbor.yaml`)
3. Project `default_branch`
4. Interactive selection (if in interactive mode)

**Notes:**
- Must be run from within a worktree (not project root)
- Fails if worktree is on detached HEAD
- Warns if worktree has uncommitted changes
- Detects and blocks if rebase or merge is already in progress
- Provides guidance when conflicts occur

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

Arbor uses a three-tier configuration system to separate team configuration from local state.

### Configuration Hierarchy

#### 1. Project Config (`<project-root>/arbor.yaml`)

Located at the project root (alongside `.bare/`), this file contains:
- Scaffold steps and cleanup steps
- Preset selection
- Tool configurations
- Project-wide settings

This file is **not versioned** (the project root is not a git repository).

During `arbor init`, if an `arbor.yaml` file is found in the repository, you'll be prompted to copy it to the project root.

#### 2. Repository Config (`<worktree>/arbor.yaml`)

Located inside each worktree and **committed to git**, this file contains:
- Team default scaffold steps
- Shared cleanup steps
- Tool configurations

This file serves as the source of truth for team configuration and is copied to the project root during `arbor init`.

#### 3. Local State (`<worktree>/.arbor.local`)

Located inside each worktree and **NOT versioned** (should be in `.gitignore`), this file contains:
- `db_suffix` - unique database suffix for the worktree
- Other worktree-specific runtime state

This file is automatically created by Arbor and should never be committed.

**Example `.gitignore` entry:**
```
.arbor.local
```

**Example `.arbor.local` file:**
```yaml
db_suffix: "sunset"
```

### Sharing Team Configuration

To share scaffold configuration with your team:

1. Create `arbor.yaml` in your repository with scaffold steps:
```yaml
preset: laravel
scaffold:
  steps:
    - name: file.copy
      from: .env.example
      to: .env
    - name: db.create
    - name: php.composer
      args: ["install"]
```

2. Commit and push to git:
```bash
git add arbor.yaml
git commit -m "Add Arbor scaffold configuration"
git push
```

3. Team members run `arbor init`:
```bash
arbor init user/repo
# → Found arbor.yaml in repository. Copy to project root for team config? [Y/n]
# → Press Enter to use team config
```

The config will be automatically copied to their project root and used for all worktrees.

### Scaffold Steps

Scaffold steps define actions to run when creating a new worktree. Each step can:

- Run commands (bash, binary, composer, npm, etc.)
- Manage databases (create/destroy)
- Read/write environment variables
- Copy files
- Execute Laravel Artisan commands

### Pre-Flight Checks

Pre-flight checks validate dependencies **before** any scaffold steps execute. This prevents worktrees from being left in a broken state due to missing requirements.

**Configuration:**

```yaml
scaffold:
  pre_flight:
    condition:
      # Check environment variables are set
      env_exists:
        - OP_VAULT
        - OP_ITEM
      
      # Check commands/binaries are installed
      command_exists:
        - op        # 1Password CLI
        - herd      # Laravel Herd
        - composer
      
      # Check required files exist
      file_exists:
        - .env.op
        - package.json
  
  steps:
    # Your scaffold steps here
```

**Supported Conditions:**

All condition types support both single values and arrays:

| Condition | Single Value | Array | Description |
|-----------|--------------|-------|-------------|
| `env_exists` | `env_exists: API_KEY` | `env_exists: [API_KEY, API_SECRET]` | Check OS environment variables are set |
| `command_exists` | `command_exists: docker` | `command_exists: [docker, docker-compose]` | Check commands are available in PATH |
| `file_exists` | `file_exists: .env` | `file_exists: [.env, composer.json]` | Check files exist in worktree |
| `os` | `os: darwin` | `os: [darwin, linux]` | Check operating system |

You can combine multiple condition types:

```yaml
pre_flight:
  condition:
    env_exists:
      - OP_VAULT
      - OP_ITEM
    command_exists: op
    file_exists: .env.op
    os: darwin
```

**Error Messages:**

When pre-flight checks fail, you'll see a detailed breakdown:

```
✗ Running pre-flight checks

Pre-flight checks failed:

Missing environment variables:
  - OP_VAULT
  - OP_ITEM

Missing commands:
  - op

Missing files:
  - .env.op

Please resolve these issues and try again.
```

**Example: 1Password Integration**

```yaml
scaffold:
  pre_flight:
    condition:
      env_exists:
        - OP_VAULT
        - OP_ITEM
      command_exists: op
      file_exists: .env.op
  
  steps:
    - name: bash.run
      command: "op inject -i .env.op -o .env"
      
    - name: php.composer
      args: ["install"]
```

This ensures that before any steps run:
- The `op` CLI is installed
- Environment variables `OP_VAULT` and `OP_ITEM` are set
- The `.env.op` template file exists

**Notes:**

- Pre-flight checks are **skipped** when using `--skip-scaffold`
- File paths in `file_exists` are relative to the worktree (no template variables)
- All checks must pass for scaffold to proceed

### Configuration Structure

```yaml
scaffold:
  steps:
    - name: step.name
      enabled: true
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
| `{{ .VarName }}` | Custom variable from env.read or captured output | Custom values |

### Built-in Steps

#### Database Steps

**`db.create`** - Create a database with unique name

```yaml
- name: db.create
  type: mysql       # or pgsql, auto-detected from DB_CONNECTION if omitted
  args: ["--prefix", "app"]  # optional: customize database prefix
```

- Generates unique name: `{prefix}_{adjective}_{noun}` or `{site_name}_{adjective}_{noun}`
- Suffix is generated once per `init` or `work` invocation and shared across all `db.create` steps
- Auto-detects engine from `DB_CONNECTION` in `.env`
- Retries up to 5 times on collision
- Persists suffix to `.arbor.local` for cleanup

**Multiple databases with shared suffix:**

```yaml
scaffold:
  steps:
    - name: db.create
      args: ["--prefix", "app"]
    - name: db.create
      args: ["--prefix", "quotes"]
    - name: db.create
      args: ["--prefix", "knowledge"]
```

Result: Creates `app_cool_engine`, `quotes_cool_engine`, `knowledge_cool_engine` (same suffix, different prefixes)

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

**`env.copy`** - Copy keys from another worktree's `.env` file

```yaml
# Copy a single key
- name: env.copy
  source: ../main           # Source worktree path (relative or absolute)
  key: API_KEY

# Copy multiple keys
- name: env.copy
  source: ../main
  keys:
    - API_KEY
    - API_SECRET
    - STRIPE_KEY
  source_file: .env         # optional, defaults to .env
  file: .env                # optional target file, defaults to .env
```

- Copies environment variables from a source worktree to the current worktree
- Useful for copying API keys, secrets, or other values from main to feature branches
- Creates target `.env` if missing
- Updates existing keys in-place
- Supports relative paths (resolved from worktree) or absolute paths

#### Node.js Steps

**`node.npm`** - npm package manager

```yaml
- name: node.npm
  args: ["install"]
```

**`node.yarn`** - Yarn package manager

```yaml
- name: node.yarn
  args: ["install"]
```

**`node.pnpm`** - pnpm package manager

```yaml
- name: node.pnpm
  args: ["install"]
```

**`node.bun`** - Bun package manager

```yaml
- name: node.bun
  args: ["install"]
```

#### PHP Steps

**`php.composer`** - Composer dependency manager

```yaml
- name: php.composer
  args: ["install"]
```

**`php.laravel`** - Laravel Artisan commands

```yaml
- name: php.laravel
  args: ["migrate:fresh", "--no-interaction"]
```

Capture command output:

```yaml
- name: php.laravel
  args: ["--version"]
  store_as: LaravelVersion

- name: env.write
  key: APP_FRAMEWORK_VERSION
  value: "{{ .LaravelVersion }}"
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

Capture output for use in later steps:

```yaml
- name: bash.run
  command: "git rev-parse --short HEAD"
  store_as: GitCommit

- name: env.write
  key: BUILD_COMMIT
  value: "{{ .GitCommit }}"
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

Capture output for use in later steps:

```yaml
- name: command.run
  command: "date +%Y-%m-%d"
  store_as: BuildDate

- name: env.write
  key: BUILD_DATE
  value: "{{ .BuildDate }}"
```

### Step Options

All steps support these configuration options:

| Option | Type | Description |
|--------|------|-------------|
| `enabled` | boolean | Enable/disable step (default: true) |
| `condition` | object | Conditional execution rules |
| `args` | array | Arguments passed to the step (e.g., `["--prefix", "app"]`) |
| `store_as` | string | Store command output as template variable (trimmed, on success only) |

Steps execute in the order they appear in the configuration file.

### Conditions

Steps can be conditionally executed based on environment. Conditions support both single values and arrays:

```yaml
# Single value conditions
condition:
  env_file_contains:
    file: .env
    key: DB_CONNECTION

# Array conditions - check multiple items at once
condition:
  env_exists:
    - API_KEY
    - API_SECRET
  command_exists:
    - docker
    - docker-compose
  file_exists:
    - .env
    - composer.json
```

### Example Configuration

Complete example for a Laravel project:

```yaml
scaffold:
  steps:
    # Create database if DB_CONNECTION is set
    - name: db.create
      condition:
        env_file_contains:
          file: .env
          key: DB_CONNECTION

    # Write database name to .env
    - name: env.write
      key: DB_DATABASE
      value: "{{ .SiteName }}_{{ .DbSuffix }}"

    # Install dependencies
    - name: php.composer
      args: ["install"]

    - name: node.npm
      args: ["install"]

    # Run migrations
    - name: php.laravel
      args: ["migrate:fresh", "--no-interaction"]

    # Set domain based on worktree path
    - name: env.write
      key: APP_DOMAIN
      value: "app.{{ .Path }}.test"

    # Generate application key
    - name: php.laravel
      args: ["key:generate"]

cleanup:
  steps:
    # Clean up databases
    - name: db.destroy
```

**Example: Multiple databases with shared suffix**

For applications that need multiple databases (e.g., main app, quotes, knowledge):

```yaml
scaffold:
  steps:
    # Create three databases with different prefixes but shared suffix
    - name: db.create
      args: ["--prefix", "app"]

    - name: db.create
      args: ["--prefix", "quotes"]

    - name: db.create
      args: ["--prefix", "knowledge"]

    # Write the main database name to .env
    - name: env.write
      key: DB_DATABASE
      value: "app_{{ .DbSuffix }}"

    # Write other database names to .env (optional)
    - name: env.write
      key: DB_QUOTES_DATABASE
      value: "quotes_{{ .DbSuffix }}"

    - name: env.write
      key: DB_KNOWLEDGE_DATABASE
      value: "knowledge_{{ .DbSuffix }}"
```

This creates: `app_cool_engine`, `quotes_cool_engine`, `knowledge_cool_engine`

### What We Handle For You

**Database Naming**
- Automatically generates unique, readable database names
- Suffix is generated once per `init` or `work` invocation
- Format: `{prefix}_{adjective}_{noun}` or `{site_name}_{adjective}_{noun}` (e.g., `myapp_swift_runner`, `app_cool_engine`)
- Multiple `db.create` steps share the same suffix, allowing consistent database naming
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
