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

`--force` suppresses Anvil's interactive prompts. `--no-interactive` and a
nonempty `CI` environment variable also suppress prompts, including first-run
setup, even in a terminal. Run inside the desired worktree or supply WORKTREE
explicitly; missing selection returns an error. `--quiet` keeps prompts enabled.
`--force` does not authorize database resets or drops or bypass ownership checks.

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

Selecting a remote branch with `anvil work <remote>/<branch>` or the interactive
picker creates local `<branch>` at that remote-tracking commit and tracks the
selected remote. Exact local names take precedence, including names beginning
with `origin/`. An unqualified name selects a unique remote match; ambiguous
matches require a remote-qualified name. Use `refs/remotes/<remote>/<branch>`
when a remote's short name collides with a literal local branch name.

Existing local branches retain their commit and upstream. For new branches,
`--base` overrides the starting commit while tracking still uses the selected
remote; `--no-track` disables new tracking, including Git's automatic tracking.
Anvil does not fetch or push automatically.

Worktrees have separate files, indexes and HEADs. Git refs and repository
configuration remain shared across worktrees.

### Reuse installed dependencies

Opt in with a successfully scaffolded source in the same linked repository:

```bash
anvil work feature/next --warm-from feature/current
anvil work feature/next --warm-from feature/current --copy-mode copy
```

Use an exact registered branch, folder name, or worktree path. The selected
target branch and `--base` remain authoritative. `--warm-from` requires scaffold
execution; `--copy-mode` requires `--warm-from`.

`--copy-mode auto` uses native filesystem cloning where supported, with ordinary
copy fallback for unsupported or cross-volume operations. `--copy-mode copy`
always makes ordinary copies. Each dependency install reports `cloned`, `copied`,
or `cold`. Copies never use hardlinks. No startup speedup or physical disk-space
savings are claimed; verification and tool probes have their own cost.

Only `vendor` and `node_modules` are eligible, independently. Reuse requires a
successful recorded locked install, matching manifests/lockfiles, configuration,
toolchain and platform, and unchanged installed contents, modes and links.
Missing, stale or incompatible provenance runs the normal cold installer.
Candidates are staged and checked before publication; unexpected copy failures
fail scaffolding. A warm hit skips its installer while later build and setup
steps still run.

Eligibility is deliberately narrow: plain `npm ci` with complete v2/v3 lockfile
integrity metadata, or scriptless `composer install`. Additional installer
arguments, install hooks, plugins, local/workspace dependencies, native node
addons, and nonstandard Composer paths stay cold. Any `npm-shrinkwrap.json` or
alternate manifest selected through `COMPOSER` stays cold. Normal Laravel
Composer scripts therefore keep Composer cold while npm may still warm.
Composer root versions and source refs must also match; compatible hits rebuild
target autoload files with plugins and scripts disabled.

Provenance lives in the target's `.anvil.local`, alongside its own database state.
Warm copies contain no Git metadata, environment files, ownership or borrowed
bindings, application caches, or live databases. Source and sibling writable
files and Git indexes remain independent; Git refs and configuration are shared
as with ordinary Git worktrees. Inspect the reported result rather than assuming
a requested source was eligible.

### `anvil ensure [WORKTREE]`

Create a worktree if needed, then prepare it idempotently. Designed for coding
agents and scripts: it never prompts and supports `--json`.

```bash
anvil ensure feature/auth            # create (like `anvil work`) or resolve, then prepare
anvil ensure feature-auth --json     # one versioned JSON document on stdout
anvil ensure                         # prepare the worktree containing the current directory
anvil ensure feature/auth --refresh  # rerun every step, ignoring completion records
anvil ensure feature/auth --skip-prepare
```

`WORKTREE` is matched **exactly** by folder name, branch, path or Anvil worktree
ID (`wt_…`); partial matches are never used. When nothing matches, it is treated
as a branch name and created exactly as `anvil work` would (`--base`, `--no-track`
apply). Concurrent `ensure` calls for the same branch converge on one worktree.
`--dry-run` is rejected before any worktree, identity or preparation changes.

Preparation runs the same steps as `anvil scaffold` (preset defaults plus
`scaffold.steps`) under a per-worktree lock and records each completed step in
`.anvil.local` as soon as it succeeds. On the next `ensure`, a step is skipped as
`up_to_date` when its definition is unchanged, its declared `inputs` hash to the
same digest and its declared `outputs` still exist. Steps without `inputs` run
once per definition. Database steps (`db.create`, `db.migrate`, `db.destroy`),
`herd`/`yerd`, `env.read`, `store_as` producers and `always: true` steps run every time under
their own safety policies. A failed or interrupted run resumes at the first
incomplete step. `anvil scaffold` and `anvil work` keep their run-everything
behavior but also record completions, so a later `ensure` builds on them.

The Laravel/PHP presets declare `composer.json`/`composer.lock` → `vendor` and
`package.json`/`package-lock.json` → `node_modules`, so `ensure` reinstalls only
when lockfiles change. Toolchain version changes are not detected; use
`--refresh`. Database flags (`--db-action`, `--reset-database`,
`--borrow-app-db`) behave exactly as on `work` and `scaffold`.

See [docs/agent-api.md](docs/agent-api.md) for the JSON contract and exit codes.

### `anvil run [WORKTREE] -- COMMAND`

Run a command in a worktree's configured environment without requiring a
testing database:

```bash
anvil run feature-x -- cargo test
anvil run feature-x --component eval -- python score.py   # component dir + its env (venv, node_modules/.bin)
anvil run -- npm test                                     # current worktree and directory
```

Exports `ANVIL_*` identity variables (`ANVIL_WORKTREE_ID`, `ANVIL_INSTANCE`, …),
recorded database names as `ANVIL_DB_DATABASE`/`ANVIL_TEST_DB_DATABASE`, and the
project `env`. It never sets `DB_DATABASE`; use `anvil exec` for the Laravel test
database. Child exit status passes through. COMMAND is resolved on the exported
`PATH`, so component-local tools win. Warns when preparation (including the
selected component's) is incomplete; `--require-ready` refuses instead.

### Components (cross-stack projects)

Repositories with several stacks declare components; `anvil ensure` prepares
each one with lockfile-backed defaults from its capabilities:

```yaml
components:
  - name: desktop
    path: apps/desktop
    uses: [node, tauri]      # pnpm install --frozen-lockfile; cargo fetch in src-tauri
  - name: service
    path: service
    uses: [node]             # npm ci
  - name: helper
    path: helpers/mac
    uses: [swift]            # macOS only by default; skipped elsewhere
```

Per-worktree runtime resources and managed processes:

```yaml
resources:
  ports: [{name: web, env: VITE_PORT}]     # stable, collision-free ANVIL_PORT_WEB
  directories: [index-out]                 # plus ANVIL_STATE_DIR/TMP/CACHE/LOG/ARTIFACTS
  shared: [{name: models, path: ~/.cache/models, required: true}]
processes:
  - {name: web, component: desktop, command: 'pnpm dev --port "$VITE_PORT" --strictPort'}
```

```bash
anvil start feature-x --json     # detached, logs in $ANVIL_LOG_DIR
anvil stop feature-x             # verified PID + start-time identity only
anvil remove feature-x --dry-run # also lists processes, ports and runtime dirs
```

Named checks with trustworthy provenance:

```bash
anvil check fast --json                           # commit, dirty-state digest, toolchain, artifacts
anvil check --list --json                         # is the latest result still current? why not?
anvil check benchmark --allow-expensive --reuse   # exclusive `locks`, conservative reuse
```

Capabilities: `node`, `rust`, `python` (uv/poetry/pip), `go`, `php`, `tauri`,
`swift`. See [docs/environments.md](docs/environments.md) for the full schema and
[examples/](examples/) for synthetic Rust/Python, Node/Rust/Tauri/Swift and
Laravel repositories.

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

## Database Migration and Borrowing

Laravel scaffolding migrates and seeds a newly created, owned application
database. Existing application databases and borrowed/shared application data
skip migrations by default. Testing databases remain existence-only unless
explicitly selected. `--force`, CI, quiet mode and `--no-interactive` never
authorize a reset.

```bash
anvil scaffold feature --db-action application=migrate
anvil scaffold feature --db-action application=skip --db-action testing=migrate
anvil scaffold feature --reset-database EXACT_OWNED_NAME
anvil work feature --borrow-app-db owner-worktree
```

`--db-action` accepts `application`, `testing` or an exact database name, with
`skip` or `migrate`. The same selectors work in target YAML:

```yaml
scaffold:
  database_actions:
    application: skip
    testing: migrate
```

A CLI value replaces the same config selector. Exact-name spelling, including
case, is preserved. Selectors that overlap one database are rejected. Repeatable
`--reset-database EXACT_NAME` is the sole built-in authorization for
`migrate:fresh --seed`, and requires an existing exact owned database. Role names,
borrowed/shared targets and newly created databases cannot be reset. All selected
targets are checked before any migration/reset command. Each command uses its
own database environment and an uncached Laravel configuration path without
temporarily rewriting `.env`.

Borrowing is opt-in through `--borrow-app-db WORKTREE` on `work` or `scaffold`.
A registered source path or symlink alias can also identify it. The source must
own an available managed SQL application database at the same supported service
endpoint. Chains, self-borrowing, malformed state and engine/service/endpoint
mismatches fail; socket/URL overrides are rejected as ambiguous. An application
`db.create` step must apply the binding; an omitted/skipped step fails.
Credentials, suffixes and testing records are never copied.

Borrowing shares application data and schema: writes and migrations affect all
borrowers. Use isolated application databases for concurrent work that needs data
independence. A borrower retains its own suffix, testing database and historical
owned records. Re-scaffolding retains the binding and requires the source to
remain registered and available. `anvil exec` exports the borrower's own testing
database and the exact borrowed application identity without checking/starting a
service or requiring a still-registered owner merely to export recorded state.

New server ownership records retain the verified driver, service, host and port
under `endpoint`. Historical resources use that recorded identity even when the
current `.env` or selected site driver changes. Anvil verifies Yerd's actual
reported listener before availability checks and migrations. Unknown or
unverifiable identity cannot grant migration/reset or borrowing authority.
Legacy ownership records remain readable for safe skips and command exports;
remove them with `--keep-db` and inspect/manage those databases separately.

Removing a borrower drops only its owned resources. An owner with active borrowers
cannot be removed destructively, even with `--force`; `--keep-db` preserves its
databases and permits removal. Borrowers retain the binding and never adopt
ownership. Malformed or unreadable sibling state blocks destructive cleanup.
Prune and `unlink --clean` remove selected borrowers before selected owners.
`unlink --clean` uses guarded cleanup and preserves unregistered files.

The Laravel preset copies `.env.example` only when `.env` is missing. Its
`env.write` steps still update the resolved database name and an absent app key.
SQLite defaults to
`database/database.sqlite`; configured relative paths agree between creation and
`DB_DATABASE`. Missing files are created exclusively, preserving existing rows.
Absolute paths, traversal, symlinks, special files, URI forms and `:memory:` are
rejected. SQLite creates no SQL-server testing ownership record, and `anvil exec`
keeps its existing SQLite refusal. Live SQLite copying is not supported.

Custom scaffolds can use `db.migrate`; explicit database-action/reset flags append
that policy when an override omits it. Arbitrary shell commands remain user code:
this policy does not sandbox a custom `migrate:fresh` command.

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
    endpoint: {driver: yerd, service: mysql, host: 127.0.0.1, port: "3306"}
  - name: myapp_swift_runner_test
    engine: mysql
    role: testing
    endpoint: {driver: yerd, service: mysql, host: 127.0.0.1, port: "3306"}
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
when done). If ownership or sibling state is unreadable or invalid, destructive cleanup
stops before site unlink or worktree deletion. `--force` does not bypass borrower
protection; use `--keep-db` to retain databases when removing a worktree.

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

For `work`, `scaffold`, `remove` and `prune`, Anvil loads configuration from the
selected worktree's `anvil.yaml`. This includes a newly created worktree whose commit
differs from the invoking checkout. Target values override linked project
settings; omitted site name, preset, default branch, editor and cleanup settings
retain the linked defaults. If the target has no `anvil.yaml`, linked defaults
apply. Anvil does not substitute the invoking checkout's scaffold YAML.

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
  args: ["migrate", "--no-interaction"]
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
| `dir` | string | Worktree-relative working directory for `command.run`, `bash.run` and binary steps; file conditions are evaluated relative to it |
| `env` | map | Extra environment for command steps; `${VAR}` expands against Anvil's worktree environment, then the inherited environment (never against other values of the same step) |
| `inputs` | array | Worktree-relative files, directories or globs that identify a completed step for `anvil ensure` |
| `outputs` | array | Paths that must still exist for a recorded step to count as complete |
| `always` | boolean | Rerun on every `anvil ensure` |

Steps execute in the order they appear in the configuration file.

Step directories, declared inputs/outputs and file conditions must remain inside
the canonical worktree root, including through symlinks. Existing escaping links
are rejected before preparation changes state or runs any step. Internal links
are allowed. Step environment values all expand against the same worktree and
inherited environment snapshot; another override in the same step does not
change that snapshot.

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

Use `context_var` to compare output saved by an earlier step with a literal string:

```yaml
scaffold:
  steps:
    - name: command.run
      command: "git rev-parse --is-inside-work-tree"
      store_as: IsWorktree
    - name: file.copy
      from: .env.example
      to: .env
      condition:
        context_var:
          name: IsWorktree
          value: "true"
```

Conditions are checked when each step reaches execution, so later steps see the
latest `store_as` output. Quote boolean-looking values: `value` must be a string,
and comparisons are case-sensitive. A missing variable never matches, including
`value: ""`; a present variable with an empty string matches `value: ""`.
Stored command output follows each step's existing trimming behavior.

Sibling conditions are ANDed. `not` negates its operand and still ANDs with its
siblings. A list under `not` is a conjunction before negation; nested `not` is
supported. Configured guards apply to every registered step and to cleanup,
alongside each step's intrinsic eligibility.

Anvil validates all effective condition names and shapes before scaffold state
changes or step execution, including disabled steps and cleanup steps excluded by
`--keep-db`. Unknown names, unsupported fields, non-string `context_var` values,
and malformed nested conditions fail with an error. Structural validation does
not require variables to exist yet. Preflight checks run before step producers,
so a `context_var` used in preflight must already be available then.

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

    # Apply the built-in database migration policy
    - name: db.migrate

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
