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
| `node` | `pnpm-lock.yaml`, `bun.lock(b)`, `yarn.lock` (+`.yarnrc.yml` → Berry), `package-lock.json`, `package.json` | `pnpm install --frozen-lockfile`, `bun install --frozen-lockfile`, `yarn install --immutable` / `--frozen-lockfile`, `npm ci`, `npm install` | manifest + lockfile (+ `.yarnrc.yml`) → `node_modules`, or `.pnp.cjs` for Yarn Plug'n'Play (Berry's default `nodeLinker`) | `node_modules/.bin` on PATH |
| `rust` | `Cargo.toml` (+`Cargo.lock`) | `cargo fetch [--locked]` | `Cargo.toml`, `Cargo.lock`, `rust-toolchain(.toml)` | — |
| `python` | `uv.lock`, `poetry.lock`, `requirements.txt` | `uv sync --frozen`, `poetry install --no-interaction`, venv + pip | lockfile → `.venv` | `VIRTUAL_ENV`, `.venv/bin` (`.venv\Scripts` on Windows) on PATH |
| `go` | `go.mod` | `go mod download` | `go.mod`, `go.sum` | — |
| `php` | `composer.lock` | `composer install --no-interaction` | `composer.json`, `composer.lock` → `vendor` | `vendor/bin` on PATH |
| `tauri` | `src-tauri/Cargo.toml` | `cargo fetch` in `src-tauri` | Cargo inputs under `src-tauri` | — |
| `swift` | `Package.swift` | `swift package resolve` | `Package.swift`, `Package.resolved` | defaults to `platforms: [darwin]` |

Anvil does not build projects by default (no `cargo build`, `tauri build`,
`swift build`); declare such commands in `prepare` when you want them. Toolchain
versions are not part of a step's identity — use `anvil ensure --refresh` after
upgrading a toolchain. The declared environment is: changing a project `env`
value, or a component or command `env` value after expansion, reruns the steps
that receive it. Only a digest is recorded, never the values; `PATH` is left
out so shell differences do not invalidate preparation.

Component and prepare-command `env` values may reference `${ANVIL_COMPONENT}`
and `${ANVIL_COMPONENT_DIR}`. Conditions (`when`, `platforms`) of every
component are validated before any effect, including components skipped on
this machine.

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
not found, 126 = not executable). COMMAND is looked up on the exported `PATH`
(and `PATHEXT` on Windows), so component-local tools such as
`node_modules/.bin` or `.venv/bin` win over global ones; a relative path is
resolved against the child's directory.

`--require-ready` refuses (`not_prepared`, exit 6) unless worktree preparation
is complete; with `--component` the component's requirements must also be met
and each of its applicable steps must have a current completion record (an
opt-in component needs `anvil ensure --component NAME` first). Without
`--require-ready` these are warnings on stderr. `run` is read-only and works whether or not a
testing database exists. `anvil exec` keeps its database-specific semantics.

## Platform notes

Command steps run through `sh -c`; on Windows they require a POSIX shell on
PATH, as before. Keychain entries, application identifiers, LaunchAgents and
singleton helpers cannot be isolated generically: applications must read
`ANVIL_INSTANCE` (and, with resource management, Anvil's directories and
ports) to namespace them.
