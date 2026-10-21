# Project Environments

Anvil prepares and runs language-agnostic development environments for each
worktree. Everything here is **declared** in the committed `anvil.yaml`; what
Anvil **observed** (completed steps, identity, allocations) lives in the
worktree's `.anvil.local` or Anvil's runtime directory, never in `anvil.yaml`.

Lifecycle:

| Concept | Command | What it does |
|---------|---------|--------------|
| Prepare | `anvil ensure` | Provision directories, install dependencies, configure resources. Idempotent and resumable. |
| Run | `anvil run`, `anvil start` | Execute commands, or managed development processes, inside a prepared environment. |
| Check | `anvil check` | Run named validation workflows on demand and record trustworthy results. |

## Top-level keys

```yaml
env:                      # exported to preparation, run, checks and processes
  RUST_LOG: info
  APP_URL: http://localhost:${ANVIL_PORT_WEB}
secret_env: [STRIPE_KEY]  # never shown in JSON (names containing TOKEN, SECRET, PASSWORD, KEY… are redacted automatically)
components: [...]
```

`env` values expand `${VAR}` against Anvil's variables first (see below),
then the inherited environment. Names are case-sensitive.

## Components

A component is one independently prepared part of the repository.

```yaml
components:
  - name: desktop              # [a-z0-9][a-z0-9._-]*
    path: apps/desktop         # worktree-relative, default "."
    uses: [node, tauri]        # stack capabilities (below)
    requires: [pnpm]           # extra commands that must be on PATH
    platforms: [darwin, linux] # GOOS filter; omitted = all (swift defaults to darwin)
    opt_in: false              # true = only with `anvil ensure --component desktop`
    on_missing: fail           # fail (default) | skip when requirements/directory are missing
    when: {file_exists: package.json}   # scaffold condition, relative to path
    env: {VITE_PORT: "${ANVIL_PORT_WEB}"}
    override: false            # true = drop capability default commands
    prepare:                   # run after capability defaults
      - run: pnpm tauri info
        inputs: [package.json, pnpm-lock.yaml]  # relative to path
        outputs: [node_modules]
        always: false
        env: {}
        when: {}
        platforms: [darwin]
```

Components compile into ordinary `command.run` scaffold steps (with `dir`,
`env`, `inputs`, `outputs`), so preparation uses the same executor, recording
and resume behavior as every other step. Requirements are checked for every
applicable component **before** any step runs; one missing command refuses
the whole preparation unless the component says `on_missing: skip`. Naming a
component with `--component` always makes its requirements mandatory.

### Capabilities

Detection is lockfile-based and conservative; without a recognized manifest a
capability only contributes its requirements.

| Capability | Detects | Default command | Inputs → outputs | Environment |
|------------|---------|-----------------|------------------|-------------|
| `node` | `pnpm-lock.yaml`, `bun.lock(b)`, `yarn.lock` (+`.yarnrc.yml` → Berry), `package-lock.json`, `package.json` | `pnpm install --frozen-lockfile`, `bun install --frozen-lockfile`, `yarn install --immutable` / `--frozen-lockfile`, `npm ci`, `npm install` | manifest + lockfile + Yarn config → `node_modules` (Yarn PnP: `.pnp.cjs`) | `node_modules/.bin` on PATH |
| `rust` | `Cargo.toml` (+`Cargo.lock`) | `cargo fetch [--locked]` | `Cargo.toml`, `Cargo.lock`, `rust-toolchain(.toml)` | — |
| `python` | `uv.lock`, `poetry.lock`, `requirements.txt` | `uv sync --frozen`, `poetry install --no-interaction`, venv + pip | lockfile → `.venv` | `VIRTUAL_ENV`, `.venv/bin` (`.venv/Scripts` on Windows) on PATH |
| `go` | `go.mod` | `go mod download` | `go.mod`, `go.sum` | — |
| `php` | `composer.lock` | `composer install --no-interaction` | `composer.json`, `composer.lock` → `vendor` | `vendor/bin` on PATH |
| `tauri` | `src-tauri/Cargo.toml` | `cargo fetch` in `src-tauri` | Cargo inputs under `src-tauri` | — |
| `swift` | `Package.swift` | `swift package resolve` | `Package.swift`, `Package.resolved` | defaults to `platforms: [darwin]` |

Yarn Berry reads `nodeLinker` from the component's `.yarnrc.yml` (or
`YARN_NODE_LINKER`). PnP records `.pnp.cjs` plus explicitly enabled loader/data
files; the `node-modules` and `pnpm` linkers record `node_modules`. Both
`.yarnrc.yml` and legacy `.yarnrc` participate in the install input digest.

Anvil does not build projects by default (no `cargo build`, `tauri build`,
`swift build`); declare such commands in `prepare` when you want them.

The exported preparation environment is hashed into step identities; changing
project env values invalidates completion records without storing their raw
values in local state. Toolchain versions are not part of a step's identity —
use `anvil ensure --refresh` after upgrading a toolchain.

The Laravel/PHP presets are unchanged and keep running through `scaffold`
steps; components may be combined with a preset in one repository.

## Exported variables

| Variable | Meaning |
|----------|---------|
| `ANVIL=1` | Running under Anvil |
| `ANVIL_WORKTREE_ID` | Stable worktree identity (`wt_…`) |
| `ANVIL_WORKTREE_PATH`, `ANVIL_WORKTREE_NAME`, `ANVIL_BRANCH`, `ANVIL_PROJECT` | Worktree location and naming |
| `ANVIL_INSTANCE` | Identifier-safe slug (`[a-z0-9-]`, ≤40 chars) for namespacing bundle IDs, LaunchAgent labels, keychain services, sockets |
| `ANVIL_DB_DATABASE`, `ANVIL_TEST_DB_DATABASE` | Recorded database names when known (informational; `anvil run` never sets `DB_DATABASE`) |
| `ANVIL_COMPONENT`, `ANVIL_COMPONENT_DIR` | With `--component` and during that component's preparation |

## `anvil run`

```bash
anvil run feature-x -- cargo test
anvil run feature-x --component eval -- python score.py
anvil run -- npm test                    # inside a worktree: keeps the current directory
anvil run feature-x --require-ready -- make lint
```

`--` is required. The child replaces Anvil (exit status passes through; 127 =
not found, 126 = not executable). `run` is read-only and works whether or not a
testing database exists. `anvil exec` keeps its database-specific semantics.
With `--component --require-ready`, the selected component must have satisfied
requirements and matching preparation records for its current applicable steps,
including their declared inputs and outputs. Prepare opt-in components explicitly
with `anvil ensure --component NAME` before requiring them to be ready.

## Runtime resources

Each prepared worktree owns a runtime directory **outside** the working tree,
under `$ANVIL_RUNTIME_ROOT`, else `$XDG_STATE_HOME/anvil`, else
`~/.local/state/anvil` (`%LOCALAPPDATA%\anvil` on Windows):
`<root>/worktrees/<worktree id>/`, with an `owner.json` naming the worktree ID
and path.

```yaml
resources:
  directories: [index-out]       # extra writable dirs → ANVIL_DIR_INDEX_OUT
  ports:
    - name: web                  # ANVIL_PORT_WEB
      env: VITE_PORT             # optional alias
  shared:
    - name: models               # ANVIL_SHARED_MODELS
      path: ~/.cache/seyk/models # absolute, ~ or ${VAR}
      env: SEYK_MODEL_DIR
      required: true             # ensure refuses (before effects) when missing
```

| Variable | Directory |
|----------|-----------|
| `ANVIL_RUNTIME_DIR` | the worktree's runtime root |
| `ANVIL_STATE_DIR`, `ANVIL_TMP_DIR`, `ANVIL_CACHE_DIR`, `ANVIL_LOG_DIR`, `ANVIL_ARTIFACTS_DIR` | standard writable directories |
| `ANVIL_DIR_<NAME>` | declared extra directories |
| `ANVIL_SHARED_<NAME>` | declared shared resources (used in place) |
| `ANVIL_PORT_<NAME>` | allocated ports |

Map them onto your tools in `env`, e.g. `TMPDIR: ${ANVIL_TMP_DIR}` or
`CARGO_TARGET_DIR: ${ANVIL_CACHE_DIR}/target`.

**Ports.** `anvil ensure` (and `anvil start`) reserves one port per declared
name in a machine-wide registry (`<root>/ports.json`) under a cross-process
lock. Reservations are stable per worktree identity, unique across all
worktrees, and only chosen when currently bindable on `127.0.0.1` (range
`20000-29999`, override with `ANVIL_PORT_RANGE=LOW-HIGH`). Reservations of
worktrees that no longer exist (or whose identity changed) are reclaimed.
A reservation is an advisory agreement among Anvil processes, **not** a
guarantee: an unrelated program can still bind the port between allocation
and your service starting, so services should bind strictly (for example Vite
`--strictPort`) and fail fast. Status output reports whether something is
currently listening.

**Shared resources** are never copied, written or deleted by Anvil. Read-only
use is a convention Anvil cannot enforce.

## Managed processes

```yaml
processes:
  - name: web
    component: desktop          # working directory and component env
    command: pnpm dev --port "$VITE_PORT" --strictPort
    env: {BROWSER: none}
    stop_timeout: 10s           # SIGTERM → SIGKILL
```

`anvil start [WORKTREE] [--process NAME]` runs each process detached through
`sh -c` in its own session/process group, with the worktree environment and
output appended to `$ANVIL_LOG_DIR/<name>.log`. The group leader is a small
Anvil supervisor that stays alive until every process in the group has
exited, so a command that backgrounds its work and returns (`server &`) is
still `already_running` on the next start. The leader PID is recorded in
`.anvil.local` together with the kernel start time (Linux `/proc/<pid>/stat`
start ticks + boot ID; macOS `kern.proc.pid` start time). `anvil stop` signals
the group only while the leader PID still has that start identity. A recycled
PID is reported as `not_signaled_pid_reused`, and a surviving group whose
leader is gone as `not_signaled_unverifiable` (its ID may now belong to an
unrelated session); neither is ever signaled; nothing is ever killed because it
listens on a recorded port. Managed processes are unsupported on Windows and
other platforms without a verifiable identity (`unsupported_platform`).

## Cleanup

`anvil remove`, `prune` and `unlink --clean` additionally:

1. inspect owned resources before any effect (`--dry-run` lists processes to
   stop, ports to release and the runtime directory to delete),
2. stop verified owned processes first (failure keeps the worktree),
3. after the worktree is removed, release its ports and delete its runtime
   directory only when `owner.json` names this worktree's verified ID and is
   unchanged since step 1 (a worktree moved with `git worktree move` keeps its
   ID, so its old path in `owner.json` still matches).

Shared resources, files outside the runtime directory and other worktrees'
resources are never touched. A worktree whose `.anvil.local` was copied from
another directory never cleans up the original's resources.

## Platform notes

Command steps run through `sh -c`; on Windows they require a POSIX shell on
PATH, as before. Keychain entries, application identifiers, LaunchAgents and
singleton helpers cannot be isolated generically: applications must read
`ANVIL_INSTANCE`, `ANVIL_STATE_DIR` and `ANVIL_PORT_*` to namespace them (see
`examples/mixed-desktop`). This is development resource isolation, not a
security sandbox: processes run with your user's full permissions.
