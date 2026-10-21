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
      identity: {file: SHA256SUMS}  # how checks that depend on it identify it
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

A shared resource that checks depend on declares exactly one `identity`
method:

| `identity` | Identity |
|------------|----------|
| `file: SHA256SUMS` | SHA-256 of that file (relative to the resource path; it must resolve inside the resource, including through symlinks) |
| `command: "git rev-parse HEAD"` | SHA-256 of the command's stdout, run with `sh -c` in the resource directory under the same supervision and 15-second deadline as toolchain probes, with the inherited process environment only (no worktree variables, inherited `ANVIL_*` values other than `ANVIL_RUNTIME_ROOT` removed, and review worktrees' `env_unset` applied; resource identities are machine-wide unless the output depends on `env_unset` variables). stderr is ignored; a non-zero exit or timeout is an error |
| `directory: true` | digest of the whole tree: relative paths, type, permission bits, file contents and symlink targets |

`directory` identities cache file digests machine-wide in
`<runtime root>/cache/resource-identity/<sha256 of the canonical path>.json`,
keyed by size, mtime, ctime, inode and mode, so unchanged files are not
re-read; entries whose mtime or ctime is within two seconds of the scan that
hashed them are never cached, so an edit within the filesystem's timestamp
granularity is always seen. Only `anvil check` execution writes the cache
(atomically: temporary file + rename, only when it changed; temporary files
abandoned for over an hour are removed); `check --list` and `status` only read
it. The cache is best-effort: an unreadable, corrupt or foreign cache is
ignored and rebuilt, and a failed write is a warning, never an identity
failure. Where ctime and inode are unavailable (Windows), the cache is
disabled and every file is hashed.
Symlinks inside the tree are not followed: their target text is hashed, and a
link whose fully resolved target lies outside the resource, or does not
exist, fails identification (its target's content could change unseen). Sockets, FIFOs and devices fail identification too.

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

## Checks

Checks are named, ordered lists of existing project commands. Anvil runs them
and records results; it never replaces Cargo, pytest, `go test` or a
benchmark runner.

```yaml
checks:
  fast:
    description: Unit tests
    commands:
      - cargo test --lib                      # string shorthand
      - run: uv run pytest -q
        component: eval                       # component dir + env
        dir: tests                            # relative to the component/worktree
        env: {PYTHONHASHSEED: "0"}
        timeout: 5m
    timeout: 15m
    toolchain: [cargo --version, uv --version]  # output recorded in the identity
    resources: [models]      # shared resources whose identity is recorded
  benchmark:
    expensive: true          # never implicit; needs --allow-expensive
    reusable: true           # allows --reuse of a current passing result
    identity_env: [CUDA_VISIBLE_DEVICES]  # inherited values that matter across worktrees
    locks: [gpu]             # exclusive machine-wide resource
    inputs: ["${ANVIL_ARTIFACTS_DIR}/recordings", eval/contract.yaml]
    artifacts: ["${ANVIL_ARTIFACTS_DIR}/bench/*.json"]
    commands: [...]
```

```bash
anvil check                      # every non-expensive check, in order
anvil check fast quality --json
anvil check --list --json        # read-only: latest result + whether it is current
anvil check benchmark --allow-expensive --lock-wait 30m
anvil check benchmark --allow-expensive --reuse
```

**Provenance.** Every run stores `result.json` (schema
`anvil.check-result/v1`) and per-command logs under
`$ANVIL_RUNTIME_DIR/checks/<name>/runs/<run id>/` (the 20 most recent by
start time are kept, never the one `latest.json` points at; run IDs carry a
nanosecond timestamp). A result records:

- the commit (`identity.git.head`), whether the tree was dirty, how many paths
  differed and `state_digest` — a digest of HEAD plus the exact content of every
  modified, deleted and untracked non-ignored path (`.anvil.local` excluded). A
  dirty tree never shares a digest with its HEAD. Dirty submodules and nested
  repositories fail identification rather than sharing an incomplete digest;
- the check definition digest, toolchain probe output, an environment digest
  and platform. The environment digest covers what each command actually
  receives: Anvil's variables, component and command `env` after expansion and
  the inherited environment. Execution receives the same resolved snapshot.
  Names are listed and values hashed; secret values are excluded (only their presence
  counts), so changing a secret does not make a result stale;
- declared input digests. Inputs must remain inside the worktree or runtime
  directory. Symlinks below either root, including ancestor directories, fail
  identification;
- the identity of each declared shared resource (`identity.resources[]`:
  `name`, `path`, `state` `present`/`absent`, `method`, `source`, `digest`, and
  `resources_digest`). Only resources named in the check's `resources` count;
  every other shared resource can change freely. A check may only name
  resources that declare an `identity` (a presence-only dependency would let a
  changed resource reuse a stale pass). A missing optional resource is a valid
  `absent` identity; a missing `required` one, a failing or timed-out identity
  command, or an unreadable file or tree is an error: `anvil check` refuses
  before running or reusing anything, and `--list`/`status` report
  `identity unavailable: …` (never current). Checks that declare no resources
  keep the identity digest of earlier Anvil versions;
- status (`passed`, `failed`, `timed_out`, `interrupted`, `error`), exit code,
  timings, the commands with their logs, and file artifacts with SHA-256 and size.
  Directory artifacts fail the run and cannot be reused. File patterns such as
  `bundle/*` record the files instead.

**Validity.** `anvil check --list` recomputes the identity and reports
`assessment.current`, `assessment.passing_and_current` and `stale_reasons`
(`commit changed`, `uncommitted changes differ`, `check definition changed`,
`toolchain changed`, `environment changed`, `inputs changed`,
`shared resource "<name>" changed` / `appeared` / `disappeared`,
`platform changed`). Only `passing_and_current` may be treated as a pass.
Ignored files are not part of the working-tree identity; declare them as
`inputs` when results depend on them. A check that modifies tracked files makes
its own result stale — the recorded identity is the state it validated.

**Failure and interruption.** A run is written as `running` before its first
command and holds a lock for its lifetime. A failing command stops the check
and its exit status is passed through. Timeouts kill the command's whole
process group (exit 124). SIGINT/SIGTERM terminate the group and record
`interrupted` (exit 130); an interruption while waiting for a lock returns
exit 130 without starting a command. Toolchain probes run supervised the same
way (own process group, 15-second deadline, bounded output). On macOS and
Linux a separate supervisor inherits the run's locks. Anvil death closes its
heartbeat and terminates the command group. The supervisor retains resource
exclusion, `running` status and cleanup protection until the group is gone.
Commands must keep descendants in that group. Windows checks and toolchain
probes fail closed until job-object supervision can provide this guarantee.
Nothing that did not finish successfully is ever recorded as passing.

**Locks.** `locks` are exclusive across all Anvil processes on the machine
(`<runtime root>/locks/<name>.lock`, holder details in `<name>.holder.json`).
A held lock fails fast with `resource_busy` (exit 7) and the holder's check,
worktree and PID, unless `--lock-wait` is given (negative waits indefinitely;
SIGINT/SIGTERM still end the wait). Locks are taken before the identity is
computed, so the recorded identity is the state the commands ran against.
Locks remain held through supervisor cleanup and are never stolen because
time passed. The same check cannot run twice concurrently in one worktree; different
checks without shared locks run concurrently.

**Reuse and benchmark caching.** `--reuse` returns the latest result instead of
running only when the check is declared `reusable`, the latest result passed,
its complete identity is unchanged, and every recorded artifact still has its
recorded digest. Commands receive `ANVIL_CHECK_IDENTITY`, `ANVIL_GIT_STATE`,
`ANVIL_GIT_HEAD`, `ANVIL_GIT_DIRTY`, `ANVIL_CHECK_RUN_ID` and
`ANVIL_CHECK_RUN_DIR` so an existing runner can key its own outputs (for
example recordings under `$ANVIL_ARTIFACTS_DIR/recordings/$ANVIL_GIT_STATE`).
A scoring check that declares the recordings and its scoring contract as
`inputs` gets a new identity whenever either changes, so recordings can be
rescored under a new contract without re-recording. Anvil does not infer
compatibility beyond these explicit identities (see `examples/gaze-like`).

**Reuse across worktrees.** When this worktree has no current pass, `--reuse`
also accepts a clean passing result another worktree of the same repository
recorded at the same content identity (`identity.content_digest`: HEAD's
`tree`, the definition, inputs, toolchain, platform, shared resources and the
declared environment with worktree paths, IDs, ports and database names
normalized; inherited variables count only for a short toolchain list such as
`PATH`, `GOFLAGS` and `NODE_ENV`, or when named in `identity_env`). Files Git
does not track (`vendor/`, `node_modules/`, generated `.env`) are not part of
it; declare the lockfiles and manifests that determine them as `inputs`. The
result is revalidated against the producer's records (still registered, same
identity, clean, intact artifacts) and recorded in this worktree with
`reused_from`. `roles.<role>.reuse_trust` (`same-worktree`, `same-role`,
`any`; default `any`) and `roles.<role>.reuse_exclude_roles` restrict
producers, as do `--reuse-trust` and `--reuse-exclude-role`. See
[agent-api.md](agent-api.md#reuse-across-worktrees) for the normalization
rules and index layout.

Check `inputs` and `artifacts` must resolve inside the worktree or its runtime
directory. Symlinks are rejected, including links to other files inside those
roots. An unsupported input fails before commands run. An unsupported artifact
fails the run and disables reuse.

Logs contain whatever commands print and are stored with mode 0600 in the
runtime directory; they are referenced by path, never embedded in JSON.

## Cleanup

`anvil remove`, `prune` and `unlink --clean` additionally:

0. refuse (`cleanup_blocked`, exit 7) while checks, ensure or scaffold use the
   worktree. These operations hold one shared lifecycle gate. Cleanup takes it
   exclusively before inspection and retains it through deletion and runtime
   release. New users fail with `cleanup_blocked`. Cleanup also refuses a held
   preparation lock and retains that lock during deletion,
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
