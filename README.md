# Anvil

Anvil is a self-contained binary for managing git worktrees to assist with agentic development of applications. It is cross-project, cross-language, and cross-environment compatible.

## Development

All development occurs inside a worktree:

```bash
# Create a worktree for development
anvil work feature/new-feature
cd feature-new-feature

# Make changes, test, commit
go test ./...
anvil work another-feature  # Create another if needed

# When done with a worktree
cd ..
anvil remove feature-new-feature
```

## Installation

### Via Homebrew (Recommended for macOS/Linux)

```bash
brew tap naoray/tap
brew install anvil
```

**Upgrade:**
```bash
brew upgrade anvil
```

### Via Direct Download

Download the latest release for your platform from the [releases page](https://github.com/naoray/anvil/releases).

### Via Go Install

```bash
go install github.com/naoray/anvil/cmd/anvil@latest
```

*Note: Installing via `go install` builds without version information. Use Homebrew or download releases for proper version metadata.*

### Build from Source

```bash
# Clone the repository
git clone https://github.com/naoray/anvil.git
cd anvil

# Build for your platform
go build -o anvil ./cmd/anvil

# Or build with version information
VERSION=$(git describe --tags --always)
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
go build -ldflags "-X main.Version=$VERSION -X main.Commit=$COMMIT -X main.BuildDate=$DATE" -o anvil ./cmd/anvil
```

## Setup

After installation, run the setup wizard to configure anvil:

```bash
anvil install
```

The wizard will:
1. Check your PATH
2. Detect Yerd/Herd/Valet and select a site driver
3. Install shell completions
4. Set a default projects root
5. Optionally install AI CLI skills (Codex CLI, Claude Code, etc.)

### Shell Completions

Shell completions are installed automatically by the wizard. To install or reinstall manually:

```bash
# Install completions for your current shell
anvil completion zsh    # installs to zsh site-functions
anvil completion bash   # installs to bash_completion.d
anvil completion fish   # installs to fish completions dir

# Print completion script to stdout (for manual setup)
anvil completion zsh --print
```

After installation, restart your shell to activate the completion. Existing zsh shells may need a manual refresh, and Anvil leaves zsh completion cache files untouched. For **zsh** with Homebrew, the completion file is written to `$(brew --prefix)/share/zsh/site-functions/_anvil`. For user-local installs, it goes to `~/.zsh/completions/_anvil`. Add the directory to your `fpath` if needed, then run `autoload -Uz compinit && compinit`:

```zsh
# ~/.zshrc
fpath=(~/.zsh/completions $fpath)
autoload -Uz compinit && compinit
```

## Quick Start

```bash
# Check anvil version
anvil version

# Link an existing project (auto-detects name from git remote)
anvil link

# Create a feature worktree
anvil work feature/user-auth

# Create a worktree from a specific base branch
anvil work feature/user-auth -b develop

# Create a worktree without running scaffold steps
anvil work feature/user-auth --skip-scaffold

# Print path to a worktree
anvil info feature/user-auth

# Open a worktree in your IDE and browser
anvil open feature/user-auth

# Sync current worktree with upstream (defaults to main, uses rebase)
anvil sync

# Sync with a specific upstream branch
anvil sync --upstream develop

# Sync using merge instead of rebase
anvil sync --strategy merge

# Save sync settings to anvil.yaml for future use
anvil sync --upstream develop --strategy rebase --save

# List all worktrees with their status
anvil list

# Remove a worktree when done
anvil remove feature/user-auth

# Clean up merged worktrees
anvil prune

# Run scaffold steps on an existing worktree
anvil scaffold main
anvil scaffold feature/user-auth

# Copy anvil.yaml from default branch to project root
anvil pull-config

# Repair git configuration (fetch refspec, branch tracking)
anvil repair

# Setup global configuration and detect tools
anvil install

# Generate shell completion scripts
anvil completion zsh >> ~/.zshrc

# Unlink a project (stop managing it)
anvil unlink
```

## Documentation

See [AGENTS.md](./AGENTS.md) for development guide.

- Command reference
- Configuration files
- Scaffold presets
- Testing strategy

## Commands

### `anvil sync`

Synchronizes the current worktree branch with an upstream branch by fetching the latest changes and rebasing or merging.

**Auto-Stashing (Default):**

By default, `anvil sync` automatically stashes changes before syncing, including:
- Tracked modifications
- Untracked files

**Note:** Ignored files (like `node_modules`, `vendor`, `.env`) are **not** stashed for performance reasons. This is safe because git does not modify ignored files during rebase/merge operations, and skipping them makes sync much faster on large projects.

After a successful sync, the stashed changes are automatically restored.

```bash
# Sync with default settings (upstream: main, strategy: rebase, auto-stash: on)
anvil sync

# Sync with a specific upstream branch
anvil sync --upstream develop
anvil sync -u develop

# Sync using merge instead of rebase
anvil sync --strategy merge
anvil sync -s merge

# Use a specific remote
anvil sync --remote upstream
anvil sync -r upstream

# Disable auto-stashing (not recommended)
anvil sync --no-auto-stash

# Skip all confirmations
anvil sync --yes
anvil sync -y

# Save sync settings to anvil.yaml for future use
anvil sync --save

# Combination of options
anvil sync --upstream main --strategy rebase --save
```

**Configuration:**

Sync settings can be persisted in `anvil.yaml`:

```yaml
sync:
  upstream: main
  strategy: rebase
  remote: origin
  auto_stash: true  # Default: true, set to false to disable
```

The command resolves settings in this order:
1. CLI flags (`--upstream`, `--strategy`, `--remote`, `--no-auto-stash`)
2. Project config (`anvil.yaml`)
3. Project `default_branch`
4. Interactive selection (if in interactive mode)

**Notes:**
- Must be run from within a worktree (not project root)
- Fails if worktree is on detached HEAD
- Auto-stashes all changes by default (can be disabled with `--no-auto-stash`)
- If stash pop fails due to conflicts, the stash is preserved and instructions are provided
- Detects and blocks if rebase or merge is already in progress
- Provides guidance when conflicts occur

### `anvil scaffold [PATH]`

Run scaffold steps for an existing worktree. This is useful when:

- You want to re-run scaffold steps on an existing worktree
- You need to scaffold a worktree you're not currently in

```bash
# Scaffold a specific worktree by path
anvil scaffold main
anvil scaffold feature/user-auth

# When inside a worktree, scaffold current (prompts for confirmation)
anvil scaffold

# When at project root without args, interactively select worktree
anvil scaffold
```

### `anvil open <WORKTREE>`

Open a worktree in your IDE and its locally linked site in the browser with a single command. Supports fuzzy matching by folder name, branch name, or partial match.

```bash
# Open in both IDE and browser
anvil open feature-auth

# Partial match
anvil open auth

# IDE only
anvil open auth --editor

# Browser only
anvil open auth --browser

# Use a specific editor command
anvil open auth --editor-cmd=zed
```

**URL Resolution:**

The browser URL is determined by reading `APP_URL` from the worktree's `.env` file. If `APP_URL` is missing or points to localhost (a common `.env.example` default), it falls back to `https://<folder-name>.test`.

**Editor Configuration:**

The IDE command is resolved in this order:
1. `--editor-cmd` flag
2. Project config (`editor_cmd` in `anvil.yaml`)
3. Global config (`editor_cmd` in `~/.config/anvil/anvil.yaml`)
4. Default: `cursor`

### `anvil work [BRANCH] [PATH]`

Create or checkout a feature worktree.

```bash
# Create a worktree for a new branch
anvil work feature/user-auth

# Create from a specific base branch
anvil work feature/user-auth -b develop

# Skip scaffold steps (run later with `anvil scaffold`)
anvil work feature/user-auth --skip-scaffold

# Skip remote tracking setup
anvil work feature/user-auth --no-track

# Interactive mode — select from available branches
anvil work
```

### `anvil pull-config`

Copy `anvil.yaml` from the default branch worktree to the project root. Useful for propagating team configuration changes from the main branch.

```bash
# Interactive — prompts before overwriting
anvil pull-config

# Force overwrite without prompt
anvil pull-config --force

# Preview what would happen
anvil pull-config --dry-run
```

### `anvil repair`

Repair git configuration for an existing anvil project. Fixes fetch refspec and branch tracking.

```bash
# Full repair (refspec + tracking)
anvil repair

# Preview changes
anvil repair --dry-run

# Only fix fetch refspec
anvil repair --refspec-only

# Only fix branch tracking
anvil repair --tracking-only
```

### `anvil install`

Setup global configuration and detect available tools (gh, yerd, herd, php, composer, npm). Creates `~/.config/anvil/anvil.yaml`.

```bash
anvil install
```

### `anvil completion`

Generate shell completion scripts for tab-completing worktree names in `open`, `scaffold`, `info`, and `remove` commands.

```bash
# Zsh
anvil completion zsh > "${fpath[1]}/_anvil"

# Bash
anvil completion bash > /etc/bash_completion.d/anvil

# Fish
anvil completion fish > ~/.config/fish/completions/anvil.fish

# PowerShell
anvil completion powershell > anvil.ps1
```

## Test Database Isolation

Parallel worktrees used to share one test database: Laravel's `phpunit.xml`
commonly pins `DB_DATABASE`, and phpunit.xml values beat `.env`, so test runs
from two worktrees wiped each other's data. Anvil scaffolds a dedicated test
database per worktree and provides `anvil exec` to run commands with it
exported.

### How it works

Scaffolding a worktree (Laravel preset) creates two databases and records
them in `.anvil.local` (generated, git-ignored, no credentials; unknown
fields are preserved on rewrite):

```yaml
db_suffix: swift_runner
databases:
  - name: myapp_swift_runner
    engine: mysql
    role: application
  - name: myapp_swift_runner_test
    engine: mysql
    role: testing
```

Anvil delegates server-backed database operations to the selected site driver.
With `site_driver: yerd`, Yerd owns service status plus database creation,
listing, and removal. With `site_driver: herd`, Anvil starts the selected Herd
service and uses the database binaries provided by Herd for logical database
creation, listing, and removal. Anvil has no embedded SQL clients; it still
owns worktree-safe naming, collision checks, and `.anvil.local` records.

- `role: application` (the default) and `role: testing` each use the
  first-created database for that role as the canonical record used by
  `anvil exec`. Additional distinct application or testing databases are
  retained as cleanup-only `auxiliary` records, so every database remains
  eligible for exact cleanup. `auxiliary` is an internal ownership role and
  is not valid in a user-configured `db.create` step.
- `role: testing` derives `<site>_<suffix>_test` (capped at 54 characters so
  Laravel's parallel-worker suffixes still fit MySQL/MariaDB's 64 and
  PostgreSQL's 63 identifier limits), creates it empty — no migrations; your
  test runner handles schema per run — and re-scaffolds idempotently.
- Custom scaffold steps can reference the name via `{{ .TestDatabaseName }}`.

### `anvil exec`

Runs a command with the worktree's test database exported. Strictly
read-only: it never creates or modifies databases or `.anvil.local`.

```bash
# Run the test suite against the worktree's test database
anvil exec -- ./scripts/git/run-tests.sh --parallel

# Works without -- too; child flags pass through
anvil exec php artisan test

# Inspect what the child sees
anvil exec -- php -r 'echo getenv("DB_DATABASE");'
```

Environment exported to the child (inherited values are replaced,
case-insensitively on Windows):

```
DB_DATABASE=<test database>                # e.g. myapp_swift_runner_test
ANVIL_TEST_DB_DATABASE=<test database>
ANVIL_DB_DATABASE=<application database>   # omitted if unknown
```

Exit codes: the child's exit code is passed through (on Unix via `execve`,
on Windows via typed propagation). `127` means the command was not found,
`126` means it was found but could not be executed, and `1` is anvil's own
resolution failure. Windows note: anvil remains the parent process; Ctrl+C
reaches the child via the shared console, but there is no Unix-style signal
forwarding or job-object management.

### Pre-push and CI recipes

Anvil never installs or owns git hooks and never patches tracked
`phpunit.xml`. Wrap the outer script instead:

```bash
# Run the whole pre-push script with the test database exported
anvil exec -- ./scripts/git/pre-push.sh
```

Local, untracked pre-push shim (`.git/hooks/pre-push`):

```sh
#!/bin/sh
exec anvil exec -- ./scripts/git/pre-push.sh "$@"
```

`exec` replaces the shim process, so git's ref-list stdin reaches the script
unchanged.

### Cleaning up

`anvil remove` drops the exact owned databases recorded in `.anvil.local`
plus Laravel parallel-worker databases for both families
(`<app>_test_<n>` and `<test>_test_<n>`):

```bash
# Preview the exact drop set (live enumeration, zero mutation)
anvil remove feature-x --dry-run

# Remove the worktree but keep every database
anvil remove feature-x --keep-db
```

`--dry-run` prints one `Would drop database: <name>` line per database and
never drops databases or removes the worktree. `--keep-db` prints the
preserved names (parallel-worker databases are kept too; drop them manually
when done). If `.anvil.local` is unreadable or contains records anvil does
not understand, `remove` stops before deleting the worktree unless `--force`
is given — databases are then left untouched and unrecorded.

### Limitations (v1.8)

- `force="true"` or `<server>` entries in `phpunit.xml` beat the exported
  environment and are not detected (a config scanner is deferred).
- A cached Laravel config (`bootstrap/cache/config.php`) beats the
  environment and is not detected.
- Bare invocations that bypass `anvil exec` keep today's shared behavior.
- `env -i`, containers without environment forwarding, and `DB_URL`-only
  connections are out of scope.
- Laravel caches parallel-worker databases between runs; run
  `php artisan test --recreate-databases` after schema-heavy branch
  switches.
- Pre-1.8 worktrees have no recorded testing database: create a new
  worktree, or configure a scaffold override containing ONLY a `db.create`
  step with `role: testing` and run that. `anvil exec` never creates or
  modifies databases or `.anvil.local`.
- One database server connection per worktree (MySQL, MariaDB, or PostgreSQL).
  SQLite worktrees are per-worktree files and already isolated, so
  `anvil exec` is not needed there.

## Configuration

Anvil uses a three-tier configuration system to separate team configuration from local state.

The local PHP site driver is selected globally in `~/.config/anvil/anvil.yaml`:

```yaml
site_driver: yerd
```

Supported values are `yerd` and `herd`. When `site_driver` is omitted, Anvil
prefers Yerd when it is on `PATH`, falls back to Herd, and otherwise defaults
to Yerd. The setup wizard saves the detected selection. Both selections use
their manager's SQL services for server-backed database steps.

### Configuration Hierarchy

#### 1. Project Config (`<project-root>/anvil.yaml`)

Located at the project root, this file contains:
- Scaffold steps and cleanup steps
- Preset selection
- Tool configurations
- Project-wide settings

This file can be committed to git for team sharing.

**Example:**
```yaml
preset: laravel
editor_cmd: cursor   # IDE for `anvil open` (optional)
sync:
  upstream: main
  strategy: rebase
scaffold:
  steps:
    - name: file.copy
      from: .env.example
      to: .env
```

#### 2. Worktree Config (`<worktree>/anvil.yaml`)

Located inside each worktree, this file can contain worktree-specific overrides:
- Team default scaffold steps
- Shared cleanup steps
- Tool configurations

#### 3. Local State (`<worktree>/.anvil.local`)

Located inside each worktree and **NOT versioned** (should be in `.gitignore`), this file contains:
- `db_suffix` - unique database suffix for the worktree
- Other worktree-specific runtime state

This file is automatically created by Anvil and should never be committed.

**Example `.gitignore` entry:**
```
.anvil.local
```

**Example `.anvil.local` file:**
```yaml
db_suffix: "sunset"
```

### Sharing Team Configuration

To share scaffold configuration with your team:

1. Create `anvil.yaml` in your repository with scaffold steps:
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
git add anvil.yaml
git commit -m "Add Anvil scaffold configuration"
git push
```

3. Team members link the project:
```bash
cd my-project
anvil link
```

The config will be used for all worktrees.

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
        - yerd      # Yerd local PHP environment
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
| `{{ .DatabaseName }}` | Full database name (truncated to 63 chars) | `myapp_swift_runner` |
| `{{ .TestDatabaseName }}` | Test database name (capped at 54 chars) | `myapp_swift_runner_test` |
| `{{ .VarName }}` | Custom variable from env.read or captured output | Custom values |

### Built-in Steps

#### Database Steps

**`db.create`** - Create a database with unique name

```yaml
- name: db.create
  type: mysql       # mysql, mariadb, or pgsql; auto-detected if omitted
  args: ["--prefix", "app"]  # optional: customize database prefix
```

- Generates unique name: `{prefix}_{adjective}_{noun}` or `{site_name}_{adjective}_{noun}`
- Loads and reuses a persisted worktree suffix; otherwise generates a new suffix once, persists it, and shares it across all `db.create` steps in the run
- Auto-detects engine from `DB_CONNECTION` in `.env`
- With `site_driver: yerd`, uses `yerd service start`, `yerd db create`, and
  `yerd db list`; direct host, port, username, and password overrides are
  rejected because Yerd owns the connection
- With `site_driver: herd`, uses `herd services:start` and the Herd-provided
  `mysql`, `mariadb`, `createdb`, `psql`, and `dropdb` binaries. Connection
  values come from `.env` or explicit step arguments; passwords are passed as
  process environment values, not command arguments
- Retries up to 5 times on collision
- Persists the suffix and every created database to `.anvil.local` for exact
  cleanup

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

**`db.destroy`** - Clean up owned databases and their parallel-worker databases

```yaml
- name: db.destroy
  type: mysql  # matches db.create type
```

- Reads `.anvil.local` ownership records and drops every exact database,
  including cleanup-only `auxiliary` records, then enumerates their Laravel
  parallel-worker databases
- For pre-v1.8 worktrees without ownership records, falls back to suffix-based
  discovery
- Runs automatically during `anvil remove`
- See [Test Database Isolation](#test-database-isolation) for the canonical
  ownership-first cleanup path and its fail-closed safety gate

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
  value: "{{ .DatabaseName }}"
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

**`yerd`** - Yerd site management

```yaml
- name: yerd
  args: ["link", "{{ .SiteName }}"]
- name: yerd
  args: ["secure", "{{ .SiteName }}"]
```

The `herd` binary step remains available when `site_driver: herd` is configured.
Database steps also use Herd's service lifecycle and Herd-provided database
binaries in that mode.

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
      value: "{{ .DatabaseName }}"

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
- Loads and reuses a persisted worktree suffix; otherwise generates a new suffix once, persists it, and shares it across all `db.create` steps in the run
- Format: `{prefix}_{adjective}_{noun}` or `{site_name}_{adjective}_{noun}` (e.g., `myapp_swift_runner`, `app_cool_engine`)
- Multiple `db.create` steps share the same suffix, allowing consistent database naming
- Handles collisions with automatic retries
- Enforces PostgreSQL/MySQL/MariaDB length limits

**Database Cleanup**
- Uses `.anvil.local` ownership records to drop exact databases and enumerate
  their parallel-worker databases
- Limits suffix matching to the pre-v1.8/no-record legacy fallback
- Integrates with `anvil remove`; see [Test Database Isolation](#test-database-isolation)
  for the canonical cleanup behavior and fail-closed ownership gate

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
- The Laravel preset clears PHPStan/Larastan's result cache after creating or updating `.env` when `vendor/bin/phpstan` is available; projects without PHPStan are skipped

**Error Handling**
- Graceful degradation where appropriate
- Clear error messages for configuration issues
- Non-fatal warnings for optional operations

## License

MIT
