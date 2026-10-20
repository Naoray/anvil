---
name: anvil-agent
description: Use when working in repositories managed by Anvil, a git worktree manager for agentic development. Covers worktree commands and workflow, scaffold configuration, Yerd and Herd local site drivers, isolated application and test databases, read-only command execution with anvil exec, safe cleanup, and development conventions for AI coding agents including Codex and Claude Code.
---

# Anvil Agent

Anvil manages git worktrees for agentic development. Use this skill when a repository has `anvil.yaml`, references Anvil in `AGENTS.md`, or the user asks you to create, inspect, open, sync, scaffold, or remove Anvil worktrees.

## Core Commands

```bash
anvil link <repo>          # Link a repository for worktree management
anvil work <branch>        # Create or checkout a worktree
anvil ensure <branch> --json  # Create if needed and prepare idempotently (resumes after failure)
anvil list                 # List linked worktrees
anvil info <branch>        # Print the path to a worktree
anvil scaffold <branch>    # Run scaffold steps for a worktree
anvil exec -- <command>    # Run with the worktree test database exported
anvil run <wt> -- <command>  # Run in the worktree environment (no database required)
anvil start <wt> / anvil stop <wt>  # Managed dev processes (ports in $ANVIL_PORT_*)
anvil check fast --json    # Named checks with provenance; --list shows whether results are current
anvil sync                 # Sync current worktree with upstream
anvil remove <branch>      # Remove a worktree
anvil remove <branch> --dry-run  # Preview database and worktree cleanup
anvil remove <branch> --keep-db  # Remove the worktree but retain databases
anvil prune                # Remove merged worktrees
```

## Workflow

1. Create isolated feature work with `anvil work feature/name`.
2. Move into the worktree path from the command output or `anvil info feature-name`.
3. Implement and test inside the worktree.
4. In Laravel worktrees, run test and pre-push commands through `anvil exec -- <command>` so they use the worktree's isolated test database.
5. Wait for user review before committing.
6. Preview cleanup with `anvil remove feature-name --dry-run`, then clean up with `anvil remove feature-name` or `anvil prune` after the work is landed.

## Deterministic Worktrees

For unattended commands, use `--no-interactive` or a nonempty `CI` environment
variable. `anvil scaffold WORKTREE --force` also suppresses Anvil prompts,
including first-run setup under a PTY. Supply the target or run inside it;
unresolved selection fails. `--quiet` keeps prompts enabled. Prompt suppression
does not authorize database resets or bypass ownership checks.

`anvil work REMOTE/BRANCH` creates local BRANCH at the selected remote-tracking
commit and tracks that remote. Exact local names win; ambiguous unqualified
remote names require qualification. Use `refs/remotes/REMOTE/BRANCH` for a remote
whose short name collides with a local branch. Existing locals retain their
commit/upstream. For new branches, `--base` changes the starting commit and
`--no-track` disables new tracking. Anvil does not fetch or push automatically.

Worktree files, indexes and HEADs are separate. Git refs and repository config
remain shared; account for that before changing refs or Git settings.

## Reuse Dependencies

Opt in with `anvil work feature/next --warm-from feature/current`. Select an exact
registered source branch, folder name or path in the same linked repository.
The target branch/`--base` remains authoritative. `--warm-from` requires scaffold;
`--copy-mode auto|copy` requires a warm source. Auto uses native cloning where
supported with ordinary-copy fallback; copy always makes ordinary copies.
Inspect the reported `cloned`, `copied` or `cold` result for each install.

Only verified `vendor` and `node_modules` trees are reused, independently. A prior
successful locked install, matching inputs/config/toolchain/platform, and intact
contents/modes/links are required. Missing or incompatible provenance goes cold.
Warm hits skip the installer; later build/setup steps still run. Do not claim
startup speedup or physical storage savings from clone success.

Eligible commands are plain `npm ci` (complete v2/v3 integrity lock) and scriptless
`composer install`. Extra installer arguments, install hooks/plugins,
local/workspace dependencies, native node addons and nonstandard Composer paths
stay cold. npm shrinkwrap or alternate `COMPOSER` manifests stay cold. Normal
Laravel Composer scripts keep Composer cold; npm may still warm. Composer root
versions/source refs must match; hits rebuild target autoload without plugins or
scripts.

`.anvil.local` stores provenance alongside the target's own DB state. Never copy
source `.env`, `.anvil.local`, Git metadata, borrowed bindings, ownership, caches
or live databases. A warm source grants no database authority. Files/indexes
remain independent; Git refs/config are shared. Use normal discovery and cleanup
commands for these native worktrees.

## Runtime Scaffold Conditions

`work`, `scaffold`, `remove` and `prune` load the selected target's `anvil.yaml`,
including when its commit differs from the invoking checkout. Missing settings retain linked
project defaults; the invoking checkout's scaffold YAML is not substituted.

Use an earlier `store_as` result to guard a later step:

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
        context_var: {name: IsWorktree, value: "true"}
```

Values are literal, case-sensitive strings evaluated when the step executes.
Absent variables never match, even an empty expected value. Quote boolean-looking
values. Siblings are ANDed; `not` negates its operand and ANDs with siblings.
Guards apply to all registered steps, retaining intrinsic eligibility.

Unknown or malformed conditions, including disabled later steps and cleanup
steps excluded by `--keep-db`, fail before scaffold state or step mutations.
Preflight runs before producers, so it cannot consume their future results.
Arbitrary configured shell commands remain user code; guards provide no generic
shell sandbox.

## Config Files

- Repository config: `anvil.yaml`
- Local worktree state: `.anvil.local` (generated and gitignored; contains database ownership/binding state and dependency provenance)
- Global config: `~/.config/anvil/anvil.yaml`

Do not rewrite `.anvil.local` casually. Anvil preserves unknown fields and uses its ownership records to decide which databases cleanup may drop.

## Local Site Driver

Set the preferred Laravel site driver globally:

```yaml
# ~/.config/anvil/anvil.yaml
site_driver: yerd
```

Supported values are `yerd` and `herd`. When unset, Anvil prefers Yerd when it is on `PATH`, then Herd, and otherwise defaults to Yerd. Set `site_driver: herd` to preserve the existing Herd workflow explicitly.

For Yerd, the Laravel preset runs separate commands:

```bash
yerd link <site>
yerd secure <site>
yerd unlink <site>  # cleanup
```

Yerd is not flag-compatible with Herd. Do not translate `herd link --secure` by replacing only the binary name. Anvil uses the same derived site name for scaffold and cleanup, including the default branch.

When `site_driver: yerd` is selected, Anvil also delegates server-backed
database operations to Yerd while retaining its own worktree naming and
ownership records:

```bash
yerd service start <mysql|mariadb|postgres>  # when installed but stopped
yerd db create <service> <database>
yerd db list <service>
yerd db drop <service> <database>            # cleanup
```

A missing required Yerd service fails scaffolding. Direct host, port,
username, and password overrides are not valid on the Yerd path.

When `site_driver: herd` is selected, Anvil prepares the matching Herd SQL
service and uses Herd-provided database binaries for the logical database
lifecycle:

```bash
herd services:start <mysql|mariadb|postgresql>
mysql|mariadb ...  # create, list, and drop
createdb|psql|dropdb ...  # PostgreSQL lifecycle
```

Herd connection values come from the worktree `.env` or explicit database
step arguments. Passwords are passed through process environment variables,
not command arguments. Anvil does not embed MySQL or PostgreSQL clients.

## Migration and Borrowing Policy

The Laravel preset copies `.env.example` only when `.env` is missing; its
`env.write` steps still update their selected keys. Newly created owned
application databases migrate/seed; existing or borrowed/shared applications
skip by default. Testing stays existence-only unless explicitly selected.

- Use repeatable `--db-action application=migrate`, `testing=skip`, or
  `EXACT_NAME=migrate`. Target YAML `scaffold.database_actions` uses the same
  selectors and `skip`/`migrate` values. CLI replaces the identical selector;
  overlapping selectors fail.
- Only `--reset-database EXACT_NAME` authorizes a built-in reset/seed of an
  existing exact owned database. Roles, new databases and borrowed/shared
  targets cannot be reset. Force, CI and prompt suppression grant no reset consent.
- Each migration receives its own target environment and uncached Laravel config
  path without temporarily rewriting `.env`. Use `db.migrate` in custom plans;
  explicit action/reset flags append it if omitted.
- `--borrow-app-db WORKTREE` on work/scaffold shares a registered owner's managed
  application database, including its data and schema. Writes affect the owner
  and all borrowers. Use isolated application data when writers need independence.
- Borrowers keep their own suffix/testing database/history. Credentials and source
  testing records are not copied. Bindings survive re-scaffold; chains, self,
  unavailable sources and service/engine/endpoint mismatches fail. An application
  db.create must actually apply the binding. Re-scaffold requires the source;
  read-only exec can export a recorded identity after owner keep-db removal.
- Newly owned server records include a verified `endpoint` (driver/service/host/port).
  Historical actions use that identity, and Yerd listener metadata must match
  before SQL availability checks and Artisan run. Never infer reset/borrow
  authority for legacy records from today's `.env` or global site driver.
  Unknown-identity records support safe skips/exports; use `--keep-db` for removal
  and manage those legacy databases separately.
- Owner cleanup with active borrowers refuses before effects even under force.
  `--keep-db` permits removal while preserving data; borrowers never adopt
  ownership. Borrower cleanup only drops owned records. Malformed/unreadable
  sibling state blocks destructive cleanup. Prune/unlink order borrowers first.
  `unlink --clean` preserves unregistered files.
- SQLite uses workspace-relative paths (default `database/database.sqlite`),
  exclusive missing-file creation and preserves existing rows. Absolute,
  traversal, symlink, special, URI and `:memory:` paths are rejected. No server
  testing record or live SQLite copying is provided; exec's SQLite refusal remains.

Built-in policy does not sandbox arbitrary custom shell migration commands.

## Test Database Isolation

The Laravel preset creates and records an application database plus an empty testing database:

```yaml
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

Use `anvil exec` for local test commands instead of invoking them bare. It is read-only: it resolves the recorded databases, exports `DB_DATABASE` and `ANVIL_TEST_DB_DATABASE` for the testing database plus `ANVIL_DB_DATABASE` when the application database is known, then runs the child without modifying databases or `.anvil.local`.

- Child exit codes pass through. Exit `127` means missing; `126` means found but not executable; `1` is an Anvil resolution error.
- `anvil remove <worktree> --dry-run` performs live enumeration but no mutation and prints the exact owned application, testing, and parallel-worker databases it would drop.
- `anvil remove <worktree> --keep-db` removes the worktree while preserving every database.
- Unreadable or invalid sibling state blocks destructive cleanup. Force does not bypass borrower protection; use `--keep-db` to preserve data during removal.
- Pre-v1.8 worktrees may lack a testing record. Do not assume one or let `anvil exec` create it; scaffold a testing-role `db.create` step or create a fresh worktree.

## Laravel Scaffold Cache

After the Laravel preset creates or updates `.env`, it clears the PHPStan/Larastan result cache when `vendor/bin/phpstan` exists. Projects without that binary are skipped. Account for this command when diagnosing scaffold failures or writing preset tests.

## Development Rules

- Use TDD for behavior changes: write a failing test, confirm it fails, then implement.
- Run focused tests first, then broader tests before handoff.
- Run `golangci-lint run ./...` before commit when available.
- Handle errors explicitly; do not ignore returned errors.
- Do not commit until the user has reviewed and approved the changes.
