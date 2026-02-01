# Arbor - Git Worktree Manager for Agentic Development

Arbor is a self-contained binary for managing git worktrees to assist with agentic development of applications. It is cross-project, cross-language, and cross-environment compatible.

## Quick Reference

### Commands
| Command | Description |
|---------|-------------|
| `arbor init [REPO] [PATH]` | Initialise new repository with worktree |
| `arbor work [BRANCH] [PATH] [-b, --base BASE]` | Create/checkout feature worktree |
| `arbor list [--json] [--porcelain] [--sort-by] [--reverse]` | List all worktrees |
| `arbor remove [BRANCH] [-f, --force]` | Remove worktree with cleanup |
| `arbor prune [-f, --force]` | Remove merged worktrees |
| `arbor install` | Setup global configuration |

### Config Files
| File | Location | Purpose |
|------|----------|---------|
| `arbor.yaml` | Project root | Project-specific settings |
| `arbor.yaml` | `$XDG_CONFIG_HOME/arbor/` or `~/.config/arbor/` | Global settings |

### Step Identifier Format

Steps use simplified dot notation where the tool namespace maps directly to the binary:

**Binary Steps:** `php`, `php.composer`, `php.laravel`, `node.npm`, `node.yarn`, `node.pnpm`, `node.bun`, `herd`
- These execute the corresponding binary with configured arguments
- The step name is the binary identifier, not the full command

**Special Steps:** `file.copy`, `bash.run`, `command.run`, `env.read`, `env.write`, `env.copy`, `db.create`, `db.destroy`
- These perform specific scaffold operations beyond simple command execution

---

## Development Workflow

### Phase Completion Criteria

Each phase must meet these requirements before completion:

1. **All implementation complete** - All items in the phase checklist are checked off
2. **Tests passing** - `go test ./...` passes with no failures
3. **Tests written** - Unit tests for new functionality, integration tests for CLI commands
4. **Code reviewed** - File-by-file review completed by maintainer
5. **Learnings documented** - Section at end of phase filled with important decisions and challenges
6. **Plan updated** - Phase marked complete, learnings recorded, next phase prepared

### Before Each Phase

1. Review the phase checklist and understand requirements
2. Create new branch for the phase work (unless specified otherwise)
3. Write failing tests to guide implementation (TDD approach)

### During Implementation

1. Implement one feature at a time
2. Run tests frequently: `go test ./... -v`
3. Add tests for new functionality
4. Document decisions in code comments where non-obvious

### Before Committing

1. Run full test suite: `go test ./... -cover`
2. Review all changed files
3. Update this plan:
   - Mark phase items complete with `[x]`
   - Fill in learnings section
   - Prepare next phase checklist
4. Present changes for review

### After Review Approval

1. Commit with descriptive message following convention:
   ```
   phase(N): Brief description

   - Item 1
   - Item 2

   Learnings:
   - Decision made
   - Challenge overcome
   ```
2. Push branch if applicable
3. Proceed to next phase

---

## Commands

### `arbor init [REPO] [PATH]`

Initialises a new repository as a bare git repository with an initial worktree.

**Arguments:**
- `REPO` - Repository URL (supports both full URLs and short GH format)
  - Full: `git@github.com:michaeldyrynda/arbor.git`
  - Short: `michaeldyrynda/arbor` (requires `gh` CLI)
- `PATH` - Optional target directory (defaults to repository basename)

**Behaviour:**
1. Detects if `gh` CLI is available using `command -v gh`
2. Resolves repository URL:
   - If contains `/` but not `@` or `:` → assume GH short format, use `gh repo clone`
   - Otherwise → use direct git URL
3. Creates directory structure:
   ```
   .
   ├── .bare/           # Bare git repository
   ├── .git             # Points to .bare (worktree marker)
   ├── main/            # Default branch worktree
   ├── feature-x/       # Additional worktrees
   └── feature-y/
   ```
4. Detects default branch (main, master, develop, etc.)
5. Creates `arbor.yaml` project configuration with discovered default branch
6. Prompts user to set project preset if not specified:
   - Uses detection to suggest preset (Laravel, Generic PHP, etc.)
   - User can confirm suggestion or set explicitly
7. Runs scaffold preset steps for the initial worktree

**Path Sanitisation:**
- Repository basename (e.g., `arbor` from `git@github.com/.../arbor.git`)
- `/` converted to `-` (prevents nested directories)

**Examples:**
```bash
arbor init arbor                           # Uses gh repo clone
arbor init arbor custom-name               # Custom directory name
arbor init git@github.com:user/repo.git    # Direct git URL
arbor init user/repo                       # GH short format
```

---

### `arbor work [BRANCH] [PATH] [-b, --base BASE]`

Creates or checks out a new worktree for a feature branch.

**Arguments:**
- `BRANCH` - Name of the feature branch
- `PATH` - Optional custom path (defaults to sanitised branch name)
- `-b, --base BASE` - Base branch for new worktree (defaults to default branch)

**Behaviour:**
1. Sanitises branch name for path (replace `/` with `-`)
2. Interactive mode (no BRANCH provided):
   - Lists available remote and local branches
   - Allows selection via fzf or numbered menu
   - Allows entering a new branch name
3. Checks if branch already exists:
   - If exists → check out existing worktree
   - If not → create new worktree from base branch
4. Runs scaffold preset for the new worktree

**Examples:**
```bash
arbor work feature/user-auth              # From default branch
arbor work feature/user-auth custom-path  # Custom path
arbor work fix/login-bug -b develop       # From develop branch
arbor work                                # Interactive branch selection

---

### `arbor list [--json] [--porcelain] [--sort-by] [--reverse]`

Lists all worktrees with their status.

**Flags:**
- `--json` - Output as JSON array (for picklist integration)
- `--porcelain` - Machine-parseable single-line format
- `--sort-by string` - Sort by: `name`, `branch`, `created` (default: `name`)
- `--reverse` - Reverse sort order

**Status Indicators:**
- `● current` - The currently checked-out worktree (bold, highlighted)
- `★ main` - The main/default branch worktree
- `✓ merged` - Branch has commits that were merged into default branch
- `○ active` - Branch has unique commits not in default branch

**Examples:**
```bash
arbor list                      # List all worktrees in styled table format
arbor list --json               # Output as JSON for picklist integration
arbor list --sort-by branch     # Sort by branch name
arbor list --reverse            # Reverse sort order
```

**Output Format (default):**
Styled table output with colors and symbols (via Charm Lipgloss):

```
🌳 Arbor Worktrees

WORKTREE    BRANCH           STATUS
feature-x   feature/x        ● current  ★ main
feature-y   feature/y        ○ active
bugfix-123  bugfix/issue-123 ✓ merged

3 worktrees • 1 merged
```

**Output Features:**
- Color-coded status badges (green for current, muted for merged)
- Bold highlighting for current worktree row
- Unicode symbols (●, ★, ✓, ○)
- Summary line with worktree count and merged count

---

### `arbor remove [BRANCH] [-f, --force]`

Removes a worktree and runs preset-defined cleanup steps.

**Arguments:**
- `BRANCH` - Name of the branch/worktree to remove
- `-f, --force` - Skip confirmation and cleanup prompts

**Behaviour:**
1. Verifies the worktree exists
2. Interactive confirmation (skipped with `--force`)
3. Runs preset-defined cleanup steps:
   - `herd` - Remove Herd site link (runs `herd unlink`)
   - `db.destroy` - Remove worktree-specific databases
   - Custom cleanup steps defined in preset
4. Removes worktree via `git worktree remove`
5. Cleans up empty directory

**Examples:**
```bash
arbor remove feature/user-auth
arbor remove feature/user-auth --force
```

**Preset Cleanup Steps:**
```yaml
cleanup:
  - name: herd
  - name: db.destroy
  - name: bash.run
    command: |
      echo "Consider cleaning database: {{ .DB_DATABASE }}"
    condition:
      env_exists: DB_CONNECTION
```

---

### `arbor prune [-f, --force]`

Removes merged worktrees automatically.

**Arguments:**
- `-f, --force` - Skip interactive confirmation

**Behaviour:**
1. Lists all worktrees with their merge status
2. Identifies merged worktrees
3. Interactive review of worktrees to remove (default)
4. Runs cleanup steps for each removed worktree
5. Removes selected worktrees

**Examples:**
```bash
arbor prune              # Interactive mode
arbor prune --force      # Auto-remove all merged worktrees
```

---

### `arbor install`

Sets up global configuration and detects available tools.

**Behaviour:**
1. Detects platform (macOS, Linux, Windows)
2. Creates global config directory:
   - macOS/Linux: `$XDG_CONFIG_HOME/arbor/` or `$HOME/.config/arbor/`
   - Windows: `%APPDATA%\arbor\`
3. Generates `arbor.yaml` with default settings
4. Detects available tools (gh, herd, php, composer, npm)
5. Detects tool versions (with awareness of project-specific versions)

**Global Config Location:**
- **macOS/Linux**: `$XDG_CONFIG_HOME/arbor/arbor.yaml` or `$HOME/.config/arbor/arbor.yaml`
- **Windows**: `%APPDATA%\arbor\arbor.yaml`

**Default Global arbor.yaml:**
```yaml
default_branch: main
detected_tools:
  gh: true
  herd: true
  php: true
  composer: true
  npm: true
```

---

## Configuration Files

### Project Configuration (`arbor.yaml`)

Located in the project root alongside `.bare`. This file defines project-specific settings and can be inherited by worktrees.

**Structure:**
```yaml
# Project identification
preset: laravel

# Default branch for new worktrees
default_branch: main

# Scaffold steps to run when creating worktrees
scaffold:
  # Add steps to the preset defaults
  steps:
    - name: php.composer
      args: ["install"]
      enabled: false  # Disable specific step
    - name: php.laravel
      args: ["migrate", "--seed"]
    - name: bash.run
      command: "custom-post-setup-command"
      condition:
        file_exists: ".special-file"

  # Or completely override preset defaults
  override: false

# Cleanup steps to run when removing worktrees
cleanup:
  - name: herd
  - name: bash.run
    command: "echo 'Consider cleaning {{ .DB_DATABASE }}'"
    condition:
      env_exists: DB_CONNECTION

# Project-specific tool versions
tools:
  php:
    version_file: ".php-version"  # Herd-style version file
  node:
    version_file: ".nvmrc"
```

**Configuration Lookup:**
Configuration is inherited from the project root. Worktrees can add or override settings:
- Worktree-specific steps append to preset defaults
- Use `override: true` to completely replace preset steps

**Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `preset` | string | Project preset name (laravel, php) |
| `default_branch` | string | Default branch for new worktrees |
| `scaffold.steps` | list | Additional scaffold steps |
| `scaffold.override` | bool | Replace preset defaults entirely |
| `cleanup` | list | Cleanup steps on worktree removal |
| `tools.*.version_file` | string | File containing tool version |

---

### Worktree-Local Configuration (`arbor.yaml` in worktree)

Each worktree can have its own `arbor.yaml` file for worktree-specific settings. These settings are scoped to the individual worktree and do not affect other worktrees.

**Location:** Inside the worktree directory (sibling to project files like `.env`, `composer.json`).

**Purpose:** Store worktree-local data that persists across scaffold runs, such as:
- Database suffix for this worktree's databases
- Worktree-specific environment overrides
- Custom variables for template substitution

**Structure:**
```yaml
# Worktree-local settings (stored in worktree/arbor.yaml)
db_suffix: "happy_sunset"  # Auto-generated for database naming
```

**Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `db_suffix` | string | Suffix for database naming (auto-generated) |

**Usage in Templates:**
Worktree config values are available in template substitutions:
```yaml
scaffold:
  steps:
    - name: env.write
      key: DB_DATABASE
      value: "{{ .SiteName }}_{{ .DbSuffix }}"  # Uses worktree's db_suffix
```

---

### Global Configuration (`arbor.yaml`)

Located in platform-specific config directory. Defines global defaults.

**Structure:**
```yaml
# Default branch when no project config exists
default_branch: main

# Detected tools and their availability
detected_tools:
  gh: true
  herd: true
  php: true
  composer: true
  npm: true

# Detected tool versions (for display/validation)
tools:
  gh:
    path: /usr/local/bin/gh
    version: "2.49.0"
  php:
    path: /Applications/Herd.app/Contents/Resources/bin/php
    version: "8.3.0"
  composer:
    path: /usr/local/bin/composer
    version: "2.7.1"
  npm:
    path: /usr/local/bin/npm
    version: "10.4.0"

# Scaffold defaults
scaffold:
  parallel_dependencies: true  # Run composer + npm install in parallel
  interactive: false           # Default to non-interactive mode
```

**Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `default_branch` | string | Global default branch |
| `detected_tools.*` | bool | Tool availability flags |
| `tools.*.path` | string | Path to tool binary |
| `tools.*.version` | string | Tool version |
| `scaffold.parallel_dependencies` | bool | Parallel package installs |
| `scaffold.interactive` | bool | Interactive mode default |

---

### Preset Configuration

Presets define default scaffold and cleanup steps for project types. They are built-in but can be extended via project config.

**Laravel Preset:**
```yaml
preset: laravel

scaffold:
  steps:
    - name: php.composer
      args: ["install"]
      condition:
        file_exists: "composer.lock"
    - name: php.composer
      args: ["update"]
      condition:
        not:
          file_exists: "composer.lock"
    - name: file.copy
      from: ".env.example"
      to: ".env"
    - name: php.laravel
      args: ["key:generate", "--no-interaction"]
      condition:
        env_file_missing: "APP_KEY"
    - name: db.create
      condition:
        env_file_contains:
          file: ".env"
          key: "DB_CONNECTION"
    - name: env.write
      key: "DB_DATABASE"
      value: "{{ .SanitizedSiteName }}_{{ .DbSuffix }}"
      condition:
        env_file_contains:
          file: ".env"
          key: "DB_CONNECTION"
    - name: node.npm
      args: ["ci"]
      condition:
        file_exists: "package-lock.json"
    - name: php.laravel
      args: ["migrate:fresh", "--seed", "--no-interaction"]
    - name: node.npm
      args: ["run", "build"]
      condition:
        file_exists: "package-lock.json"
    - name: php.laravel
      args: ["storage:link", "--no-interaction"]
    - name: herd
      args: ["link", "--secure", "{{ .SiteName }}"]

cleanup:
  - name: herd
  - name: db.destroy
```

**Generic PHP Preset:**
```yaml
preset: php

scaffold:
  steps:
    - name: php.composer
      args: ["install"]

cleanup: []
```

---

## Scaffold Steps

Steps are namespaced by language and tool using dot notation: `language.tool.command`

### Step Configuration

```yaml
- name: php.laravel
  args: ["migrate", "--fresh", "--seed"]
  condition:
    file_exists: "artisan"
    file_contains:
      file: "composer.json"
      pattern: "laravel/framework"
  priority: 30
  enabled: true
```

**Step Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Step identifier (e.g., `php.composer`, `file.copy`) |
| `args` | list | Arguments to pass to command |
| `condition` | map | Execution conditions |
| `priority` | int | Execution order (lower = earlier) |
| `enabled` | bool | Enable/disable step |
| `command` | string | For bash.run step |
| `from`/`to` | string | For file.copy step |

### Step Interface

```go
type ScaffoldStep interface {
    Name() string
    Run(ctx Context, opts StepOptions) error
    Priority() int
    Condition(ctx Context) bool
}
```

### Parallel Execution

Steps with the same priority execute in parallel when independent:

**Parallel-safe pairs:**
- `php.composer` + `node.npm` (dependency installation)
- Independent file operations

**Sequential requirements:**
- `node.npm` with `run` args requires earlier `node.npm` with `install`
- `php.laravel` requires `php.composer` to have run first
- `herd` requires PHP environment setup

### Built-in Steps

#### Binary Steps
Binary steps execute the corresponding tool with configured arguments. The step name is the tool identifier.

| Step | Binary | Description |
|------|--------|-------------|
| `php` | `php` | Runs PHP with args |
| `php.composer` | `composer` | Runs composer with args (e.g., `install`, `update`) |
| `php.laravel` | `php artisan` | Runs artisan commands |
| `node.npm` | `npm` | Runs npm with args (e.g., `ci`, `run build`) |
| `node.yarn` | `yarn` | Runs yarn with args |
| `node.pnpm` | `pnpm` | Runs pnpm with args |
| `node.bun` | `bun` | Runs bun with args |
| `herd` | `herd` | Runs herd with args (e.g., `link --secure`, `unlink`) |

#### File Operations
| Step | Description |
|------|-------------|
| `file.copy` | Copies files from `from` to `to` |
| `env.read` | Read key from .env file and store as context variable |
| `env.write` | Write or update key=value in .env file |
| `env.copy` | Copy environment variables between files |

#### Database Steps
| Step | Description |
|------|-------------|
| `db.create` | Create database with random {adjective}_{noun} suffix |
| `db.destroy` | Drop databases matching suffix pattern (cleanup) |

#### Generic Steps
| Step | Description |
|------|-------------|
| `bash.run` | Runs arbitrary bash command |
| `command.run` | Runs arbitrary command |

**Bash Step Example:**
```yaml
- name: bash.run
  command: |
    echo "Custom setup for {{ .Branch }}"
    ./vendor/bin/pint
```

### Conditional Execution

Steps can declare conditions for execution:

```yaml
condition:
  file_exists: "package.json"
  file_has_script: "build"
  command_exists: "herd"
  os: [darwin]
  env_exists: DB_CONNECTION
  not:
    file_exists: ".skip-scaffold"
```

**Supported Conditions:**
| Condition | Description |
|-----------|-------------|
| `file_exists` | File or directory exists |
| `file_contains` | File contains pattern |
| `file_has_script` | package.json has script |
| `command_exists` | Command available in PATH |
| `os` | Operating system matches |
| `env_exists` | Environment variable is set |
| `not` | Negates conditions |

---

## Presets

Presets define default scaffold and cleanup steps for project types. They are explicitly configured in `arbor.yaml`.

### Preset Interface

```go
type Preset interface {
    Name() string
    Detect(path string) bool  // Used for suggestions only
    DefaultSteps() []ScaffoldStep
    CleanupSteps() []ScaffoldStep
}
```

### Initial Presets

#### Laravel Preset
**Detection (for suggestions):**
- `artisan` file exists
- `composer.json` contains `laravel/framework`

**Default Steps:**
1. `php.composer install` (if composer.lock exists) or `php.composer update`
2. `file.copy .env.example → .env`
3. `php.laravel key:generate` (if APP_KEY missing)
4. `db.create` (if DB_CONNECTION in .env)
5. `env.write DB_DATABASE` with generated name
6. `node.npm ci` (if package-lock.json exists)
7. `php.laravel migrate:fresh --seed`
8. `node.npm run build` (if package-lock.json exists)
9. `php.laravel storage:link`
10. `herd link --secure {{ .SiteName }}`

**Cleanup Steps:**
1. `herd` (runs unlink automatically)
2. `db.destroy` (removes worktree-specific databases)

#### Generic PHP Preset
**Detection (for suggestions):**
- `composer.json` exists

**Default Steps:**
1. `php.composer` with args `["install"]`

**Cleanup Steps:**
1. None by default

### Preset Configuration Example

```yaml
# arbor.yaml
preset: laravel

scaffold:
  steps:
    # Disable specific step from preset
    - name: node.npm
      args: ["run", "build"]
      enabled: false

    # Add custom step
    - name: bash.run
      command: "./vendor/bin/pint"
```

### Example Configurations

#### Database Setup Workflow
```yaml
scaffold:
  steps:
    # Create database with generated name (auto-detects mysql/pgsql from .env)
    - name: db.create
      condition:
        env_file_contains:
          file: .env
          key: DB_CONNECTION

    # Write database name to .env
    - name: env.write
      key: DB_DATABASE
      value: "{{ .SiteName }}_{{ .DbSuffix }}"

    # Run migrations
    - name: php.laravel
      args: ["migrate:fresh", "--no-interaction"]

    # Set domain based on worktree path
    - name: env.write
      key: APP_DOMAIN
      value: "app.{{ .Path }}.test"

    # Generate Passport keys
    - name: php.laravel
      args: ["passport:keys", "--no-interaction"]

cleanup:
  steps:
    # Cleanup databases when worktree is removed
    - name: db.destroy
      # type: mysql  # optional, auto-detected from DB_CONNECTION
```

#### Bun Integration Workflow
```yaml
scaffold:
  steps:
    - name: node.bun
      args: ["install"]

    - name: node.bun
      args: ["run", "build"]

    - name: node.bun
      args: ["run", "dev"]
```

#### Environment Variable Chain
```yaml
scaffold:
  steps:
    # Read existing value and store as custom variable
    - name: env.read
      key: DB_DATABASE
      store_as: OriginalDb

    # Create new database with different name
    - name: db.create

    # Write new database name
    - name: env.write
      key: DB_DATABASE
      value: "{{ .SiteName }}_{{ .DbSuffix }}"

    # Use both old and new in migration
    - name: php.laravel
      args: ["db:seed", "--class=TestSeeder", "--database={{ .OriginalDb }}"]

cleanup:
  steps:
    - name: db.destroy
```

---

## Tool Version Detection

Arbor detects available tools and their versions. Global detection serves as defaults; project-specific versions take precedence.

### Detection Strategy

1. **Global detection** (via `arbor install`):
   - Scans PATH for known tools
   - Records path and version
   - Stores in global config

2. **Project-specific detection**:
   - Herd: Check `.php-version` file
   - nvm: Check `.nvmrc` file
   - pyenv: Check `.python-version` file

3. **Version priority**:
   ```
   Project-specific > Global default > System PATH
   ```

### Detected Tools

```yaml
tools:
  gh:
    available: true
    version: "2.49.0"
    path: /usr/local/bin/gh
  herd:
    available: true
    version: "1.0.0"
    path: /Applications/Herd.app
  php:
    available: true
    version: "8.3.0"
    path: /Applications/Herd.app/Contents/Resources/bin/php
    project_version: "8.2.0"  # From .php-version
  composer:
    available: true
    version: "2.7.1"
  npm:
    available: true
    version: "10.4.0"
```

---

## Worktree Structure

```
project/
├── .bare/                    # Bare git repository
│   ├── config
│   ├── heads/
│   ├── objects/
│   └── refs/
├── .git                      # Worktree marker: gitdir: ./.bare
├── arbor.yaml                # Project configuration
├── main/                     # Default branch worktree
│   ├── artisan
│   ├── composer.json
│   ├── package.json
│   └── ...
├── feature-x/                # Feature branch worktree
├── feature-y/                # Feature branch worktree
└── bugfix-z/                 # Bugfix branch worktree
```

**Path Rules:**
- Worktrees are siblings of `.bare`
- Branch names sanitised: `/` → `-`
- Custom paths accepted but sanitised same way

---

## GitHub CLI Integration

### Detection Strategy

Uses efficient method in order:

1. Quick `command -v gh` check (POSIX) or `Get-Command gh` (PowerShell)
2. If available → use `gh repo clone <repo>`
3. If not available → use direct git URL

**Why this order?**
- `command -v` is a shell builtin (near-instant)
- Trying `gh repo clone` first would spawn a process that fails fast anyway
- Avoids double-process overhead when `gh` is missing

### GH Short Format Support

```bash
arbor init michaeldyrynda/arbor    # GH short format
arbor init arbor                   # Assumes current user/org
```

**Detection logic:**
- Contains `/` but not `@` or `:` → GH short format
- Otherwise → direct git URL

---

## UI and Output Format

Arbor uses the Charmbracelet library suite for rich terminal output with colors, styling, and interactive elements.

### Output Libraries

- **Lipgloss**: Styled text, colors, borders, and table formatting
- **Log**: Structured logging with emoji prefixes and color-coded levels
- **Huh/Spinner**: Interactive loading spinners for long-running operations

### Visual Indicators

**Status Symbols:**
| Symbol | Meaning | Usage |
|--------|---------|-------|
| ✓ | Success | Completed operations, successful steps |
| ✗ | Error | Failed operations, errors |
| ⚠ | Warning | Warnings, non-fatal issues |
| ℹ | Info | Informational messages |
| → | Step | Current/ongoing step indicator |
| ● | Current | Current worktree indicator |
| ★ | Main | Main branch indicator |
| ○ | Active | Active (not merged) worktree |

**Color Scheme:**
| Color | Usage |
|-------|-------|
| Green (#4CAF50) | Primary accent, headers |
| Light Green (#66BB6A) | Success states |
| Orange (#FFA726) | Warnings |
| Red (#EF5350) | Errors |
| Blue (#29B6F6) | Info, code |
| Gray (#9E9E9E) | Muted text, summaries |

### Output Functions

```go
// Success messages with checkmark
ui.PrintSuccess("Worktree created")

// Error messages with X
ui.PrintError("Failed to create worktree")

// Warning messages with triangle
ui.PrintWarning("Directory already exists")

// Info messages with info icon
ui.PrintInfo("Using preset: laravel")

// Step indicator
ui.PrintStep("Running composer install...")

// Styled success with path
ui.PrintSuccessPath("Created", "/path/to/worktree")

// Error with hint
ui.PrintErrorWithHint("Command failed", "Check your internet connection")

// Spinner for long operations
ui.RunWithSpinner("Installing dependencies...", func() error {
    return runInstall()
})
```

### Table Output

Worktree listings use styled tables with:
- Bordered table layout with rounded corners
- Color-coded headers
- Bold highlighting for current worktree row
- Status badges with appropriate colors

---

## Error Handling

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

### Rollback

On failure during scaffold:
1. Log error with context
2. Rollback completed steps if possible
3. Exit with appropriate code

### Dry Run

Use `--dry-run` flag to preview operations without executing:

```bash
arbor init arbor --dry-run
arbor work feature-x --dry-run
arbor remove feature-x --dry-run
```

---

## Testing Strategy

### Technology Stack

- **Language**: Go 1.21+
- **CLI Framework**: Cobra
- **Config**: Viper (YAML)
- **Testing**: standard library + testify

### Unit Tests

**Coverage Areas:**
- Path sanitisation (`/` → `-`)
- Config loading/saving (project + global arbor.yaml)
- Condition evaluation
- Branch name validation
- Step execution conditions

**Test Files:**
- `internal/config/config_test.go`
- `internal/config/project_test.go`
- `internal/config/global_test.go`
- `internal/scaffold/step_test.go`
- `internal/utils/path_test.go`

### Integration Tests

**Coverage Areas:**
- Full worktree creation/teardown cycles
- Config file generation (arbor.yaml)
- Multi-platform compatibility
- Preset application

**Test Files:**
- `cmd/arbor/init_test.go`
- `cmd/arbor/work_test.go`
- `cmd/arbor/remove_test.go`

### E2E Tests

**Scenarios:**
1. Laravel project init and worktree creation
2. Worktree removal with cleanup steps
3. Prune merged worktrees
4. Conditional step execution

---

## Implementation

### Directory Structure

```
arbor/
├── cmd/
│   └── arbor/
│       └── main.go
├── internal/
│   ├── cli/
│   │   ├── root.go
│   │   ├── init.go
│   │   ├── work.go
│   │   ├── remove.go
│   │   ├── prune.go
│   │   └── install.go
│   ├── config/
│   │   ├── config.go
│   │   ├── project.go
│   │   ├── global.go
│   │   └── preset.go
│   ├── git/
│   │   ├── worktree.go
│   │   ├── bare.go
│   │   └── detect.go
│   ├── scaffold/
│   │   ├── manager.go
│   │   ├── step.go
│   │   ├── executor.go
│   │   └── conditions.go
│   ├── presets/
│   │   ├── preset.go
│   │   ├── laravel.go
│   │   └── php.go
│   └── utils/
│       ├── path.go
│       ├── os.go
│       └── exec.go
├── arbor.yaml
├── go.mod
├── go.sum
└── README.md
```

### Key Interfaces

```go
// Preset definition
type Preset interface {
    Name() string
    Detect(path string) bool
    DefaultSteps() []ScaffoldStep
    CleanupSteps() []ScaffoldStep
}

// Scaffold step execution
type ScaffoldStep interface {
    Name() string
    Run(ctx Context, opts StepOptions) error
    Priority() int
    Condition(ctx Context) bool
}

// Worktree operations
type WorktreeManager interface {
    Create(path, branch, baseBranch string) error
    Remove(branch string, cleanup bool) error
    List() ([]Worktree, error)
    Prune() ([]PrunedWorktree, error)
}

// Configuration
type Config interface {
    LoadProject(path string) error
    LoadGlobal() error
    SaveProject(path string) error
    GetDefaultBranch() string
    SetDefaultBranch(branch string)
    GetPreset() string
    SetPreset(preset string)
}

// Tool detection
type ToolDetector interface {
    Detect() (ToolReport, error)
    DetectVersion(tool string, versionFile string) (string, error)
}
```

---

## Implementation Phases

### Phase 1: Core Infrastructure
- [x] Project scaffolding (Go modules, cobra, viper)
- [x] Config loading/saving (project + global arbor.yaml)
- [x] Git worktree operations (create, list, remove)
- [x] Basic CLI commands (init, work, remove)
- [x] Path sanitisation utilities
- [x] Exit code definitions

**Learnings (Phase 1):**
- Using Viper for config management works well for both project and global arbor.yaml files
- Git worktree operations are straightforward with exec.Command (no need for go-git library)
- Path sanitisation is simple string replacement but critical for proper directory structure
- Test-driven development with testify provides good coverage for utility functions
- Cobra's PersistentFlags work well for global flags like --dry-run and --verbose
- Global config location uses XDG_CONFIG_HOME with HOME/.config fallback (cross-platform)
- exec.Command with variadic args requires careful handling when building command arrays
- Development occurs inside worktrees - not in the bare repository parent directory
- Directory structure: `.ai/` for AI/planning files, `.gitignore` excludes `.ai/plans/` contents
- Worktree setup: `git worktree add main main` creates the main worktree from bare repo

---

### Phase 2: Scaffold System
- [x] Step interface and executor
- [x] Parallel execution engine
- [x] Condition evaluation
- [x] Built-in steps:
  - [x] php (binary step)
  - [x] php.composer (binary step)
  - [x] php.laravel (binary step)
  - [x] node.npm (binary step)
  - [x] node.yarn (binary step)
  - [x] node.pnpm (binary step)
  - [x] node.bun (binary step)
  - [x] herd (binary step)
  - [x] file.copy
  - [x] bash.run
  - [x] command.run
  - [x] env.read
  - [x] env.write
  - [x] env.copy
  - [x] db.create
  - [x] db.destroy

**Learnings (Phase 2):**
- The Step interface with Name(), Run(), Priority(), and Condition() methods provides a clean, extensible foundation for scaffold steps
- Priority-based execution allows steps with the same priority to run in parallel, enabling parallel `composer install` and `npm install`
- Condition evaluator supports nested conditions (not, and, or patterns) via interface{} type assertions
- Using mapstructure/v2 (already in dependencies via viper) for YAML config decoding worked well
- Initial bug: successful step execution wasn't adding results to the results slice - fixed by adding the result after successful Run()
- Steps are defined in the same package (scaffold) rather than a subpackage to avoid import cycles with ScaffoldContext
- The executor uses a mutex to protect results access, which is important for parallel step execution
- Dry-run mode skips step.Run() but still adds results to track what would have been executed
- Condition checks are performed before running each step, allowing conditional execution based on file existence, environment variables, OS, etc.
- **Limitation**: `Condition` only checks if a binary exists in PATH. It does not handle step dependencies. For example, if `composer` is not available, `php.composer` is skipped but `php.laravel` will still run and fail because `vendor/autoload.php` doesn't exist. Phase 3 (presets) needs to address this by either:
  1. Adding explicit step dependencies
  2. Having conditions that check for previous step artifacts (e.g., `file_exists: "vendor/autoload.php"`)
  3. Implementing a step dependency graph where steps fail fast if prerequisites aren't met

---

### Phase 3: Laravel Preset
- [x] Preset interface
- [x] Laravel preset implementation
- [x] Generic PHP preset
- [x] Config integration
- [x] Init preset prompt

**Learnings (Phase 3):**
- Preset interface with Name(), Detect(), DefaultSteps(), CleanupSteps() provides clean abstraction
- Preset detection uses file presence and content matching (artisan file, composer.json with laravel/framework)
- Helper functions in steps package (ComposerInstall, Artisan, HerdLink, etc.) make preset step creation clean
- Init command auto-detects preset after cloning, with --interactive flag for manual selection
- Preset manager centralizes registration and detection logic
- Cleanup steps include `herd` (unlink) and `db.destroy` for Laravel
- Tests verify detection logic and step composition for each preset

---

### Phase 4: Interactive & Polish
- [x] `arbor work` interactive branch selection
- [x] `arbor prune` command
- [x] `arbor install` command
- [x] Cleanup steps for remove
- [x] Dry-run mode
- [x] Comprehensive testing

**Learnings (Phase 4):**
- Interactive branch selection uses numbered menu (fzf integration possible for enhancement)
- `FindBarePath` helper searches parent directories for `.bare` marker
- `ListBranches` handles `+` prefix for branches checked out in other worktrees
- `IsMerged` uses `git merge-base --is-ancestor` for efficient merge status checking
- Tool detection in `arbor install` parses version output from multiple tools (gh, php, composer, npm, herd)
- Cleanup steps run before worktree removal via scaffold manager
- Test fixtures require proper git repo initialization (commit before cloning to bare)
- Branch ancestry: a commit is an ancestor of itself, so new branches are "merged" immediately

---

### Phase 5: Distribution
- [x] Multi-platform builds
- [ ] Homebrew formula (deferred)
- [x] Release automation

**Learnings (Phase 5):**
- GitHub Actions `ci.yml` runs tests on ubuntu, macos, windows with race detector
- GitHub Actions `release.yml` builds binaries for linux/amd64, darwin/amd64, windows/amd64
- Release workflow triggers on `v*.*.*` tags, creates GitHub release with artifacts
- `actions/upload-artifact` and `actions/download-artifact` handle multi-platform artifacts
- `softprops/action-gh-release` creates GitHub releases with auto-generated notes
- `.goreleaser.yml` provided for advanced release management (optional, GoReleaser can be used)
- Build command: `CGO_ENABLED=0 GOOS=<os> GOARCH=amd64 go build`
- Platform mapping: windows-latest→windows, macos-latest→darwin, ubuntu-latest→linux
- Release process: tag → tests run → builds create → GitHub release published

---

## Future Considerations

### Potential Features (v2.0+)

1. **Additional Presets**: Symfony, Vite, Node.js, Python
2. **Plugin Loading**: Go plugins (.so) for custom presets
3. **Remote Worktrees**: Worktrees on remote servers via SSH
4. **Template Variables**: `{{ .Branch }}`, `{{ .Date }}`, `{{ .SiteName }}`, `{{ .RepoName }}`, `{{ .Path }}`, `{{ .RepoPath }}`, `{{ .DbSuffix }}`, `{{ .VarName }}` in steps
5. **GitHub Integration**: Auto-create PRs when merging
6. **TUI**: Interactive terminal UI
7. **Telemetry**: Anonymous usage statistics

### Configuration Expansion

Future config options:
- `hooks.pre_*` and `hooks.post_*` for custom scripts
- `integrations.herd.path` for Herd configuration
- `integrations.dnsmasq.hosts` for DNS management
