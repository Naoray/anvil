# Anvil Agent API

Anvil's agent-facing commands share one machine-readable contract so coding
agents and orchestrators (Claude Code, Codex, Solo, Teyos) never parse
human-oriented terminal output.

## Envelope (`anvil.agent/v1`)

Every command run with `--json` writes **exactly one** JSON document to stdout:

```json
{
  "schema": "anvil.agent/v1",
  "command": "ensure",
  "ok": true,
  "mutating": true,
  "data": { },
  "warnings": []
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `schema` | string | Contract version. Additive fields keep the version; renamed, removed or redefined fields bump it (one deliberate exception: `status --all` entry `lease`/`prepare` became objects within v1 — see the compatibility note under `anvil status --all --json`). |
| `command` | string | The command that produced the document. |
| `ok` | bool | `true` only when the operation fully succeeded. Incomplete preparation is never `ok`. |
| `mutating` | bool | Whether the command may change worktrees, state or processes. Read-only commands never write `.anvil.local`. |
| `data` | object | Command result. May be present on failure to describe partial progress. |
| `error` | object | Present when `ok` is `false`: `{code, message, details?}`. |
| `warnings` | array | Nonfatal diagnostics; always an array. |

Rules:

- Diagnostics, progress and child process output go to **stderr**. stdout
  carries only the envelope, with no ANSI escapes.
- `--json` never prompts. Agent commands never use partial name matching.
- Argument and flag errors (unknown flags, extra arguments) also produce one
  `invalid_arguments` envelope when `--json` appears anywhere before `--` on a
  command that supports `--json`. The root command does not: `anvil --version
  --json` is a plain flag error; use `anvil version --json`.
- Mutating commands without a side-effect-free mode refuse `--dry-run` with
  `invalid_arguments` before any effect; inspect with read-only commands.
- Secrets are never included. Preparation errors use a classified summary;
  failing child output is available only on stderr, never in JSON.

## Exit codes and error codes

| Exit | Error codes |
|------|-------------|
| 0 | success |
| 1 | `internal_error`, `unsupported_platform`, `check_failed` (unless the child status is passed through) |
| 2 | `invalid_arguments`, `ambiguous_worktree`, `expensive_check_requires_opt_in` |
| 3 | `worktree_not_found`, `check_not_found`, `process_not_found`, `component_not_found` |
| 4 | `git_failed` |
| 5 | `project_not_found`, `config_invalid` |
| 6 | `prepare_failed`, `not_prepared` |
| 7 | `resource_busy`, `lease_held`, `lease_stale`, `lease_not_owned`, `cleanup_blocked` |

Codes are stable and never reused for a different meaning.

## Commands

| Command | Mutating | Purpose |
|---------|----------|---------|
| `anvil ensure [WT] --json` | yes | Create if needed (branch, or `--review COMMIT`), record role/task, acquire lease, provision resources, prepare idempotently |
| `anvil context [WT] --json` | no | Identity, metadata, lease, environment (redacted), components, resources, processes, checks, platform capabilities |
| `anvil status [WT] --json` / `--all` | no | Readiness, Git state and pin match, lease, processes, ports, check currency, cleanup preview |
| `anvil check --list --json` | no | Checks with latest result and currency |
| `anvil check [CHECK...] --json` | yes | Run checks, record provenance |
| `anvil slots --json` | no | Machine-wide check slots: capacity, load, holders, queue |
| `anvil start` / `anvil stop [WT] --json` | yes | Managed processes |
| `anvil lease acquire\|renew\|release\|break [WT] --json` | yes | Advisory leases |
| `anvil lease show [WT] --json` | no | Lease state |
| `anvil remove WT --json` (`--force` or `--dry-run`) | yes | Remove a worktree with cleanup, or preview the plan |
| `anvil adopt [WT] \| --all --json` | yes (`--dry-run`) | Record identity (and optionally a lease) for existing worktrees without scaffolding |
| `anvil prune candidates --json [--output FILE]` | no | Per-worktree prune candidacy with reasons, blockers and evidence (never fetches unless `--fetch`; never rewrites indexes) |
| `anvil prune apply --from FILE --json` (`--force` or `--dry-run`) | yes | Recheck and remove the file's candidates |
| `anvil run [WT] -- CMD` | no (state) | Exec in the worktree environment; child stdio and exit status are the child's own |
| `anvil version --json` | no | Version, commit, build date, Go version and platform |

Read-only commands never write `.anvil.local`, allocate ports or create
directories. `check --list` and `status` run only the declared toolchain probe
and shared-resource identity commands to decide currency; they read the
directory identity cache when present but never create or write it (only
`anvil check` execution does). Mutating commands are not transactional across
all resources, but each state change is atomic and validated before effects.

`run` has no JSON mode by design: it replaces itself with the child (execve on
Unix), so stdout/stderr belong to the child. Use `check` for recorded,
structured outcomes.

## Worktree identity

Agent commands resolve `WORKTREE` exactly by folder name, branch, absolute path
or Anvil worktree ID. The ID (`wt_` + 16 hex digits) is assigned on first
preparation, stored in `.anvil.local` with the canonical path, and is the only
key Anvil uses for ownership. Branch names are not unique identifiers.

```json
"worktree": {
  "id": "wt_3f9c0a1b2c3d4e5f",
  "identity_status": "ok",
  "name": "feature-auth",
  "path": "/Users/me/worktrees/app/feature-auth",
  "branch": "feature/auth",
  "detached": false,
  "head": "<40-hex commit>",
  "managed": true
}
```

`identity_status` is `ok`, `missing` (never prepared), `unknown` (identity
could not be read, for example a missing directory or a bare entry), `moved`
(path changed; the ID is kept on the next `ensure` or `adopt`) or `copied`
(another directory holds the same ID; the copy receives a new ID and drops the
original's lease and process records on the next `ensure` or `adopt`, and is
never treated as their owner).

`managed` is `true` iff Anvil holds a valid identity for this path
(`identity_status` `ok` or `moved`). It is `false` for `missing` (never
prepared or adopted: for example the main checkout or a plain
`git worktree add` worktree, until `anvil ensure` adopts it) and for `copied`,
and when the identity cannot be read (with a warning). Decide whether a
worktree is Anvil's from `managed`, never from its name or location.

Every payload that describes a worktree (`ensure`, `context`, `status` and
each `status --all` entry) uses this `worktree` object, the same `lease`
object (see [Leases](#leases)) and the same readiness semantics (see
[`anvil status`](#anvil-status---json)).

## `anvil ensure [WORKTREE] --json` (mutating)

`--dry-run` returns `invalid_arguments` (exit 2) before any changes.

```json
"data": {
  "worktree": { "...": "see above" },
  "created": false,
  "ready": true,
  "lease": {"state": "none"},
  "prepare": {
    "status": "complete",
    "ready": true,
    "plan_digest": "<sha256>",
    "steps": [
      {"index": 0, "name": "php.composer", "key": "php.composer#…#1", "result": "up_to_date"},
      {"index": 1, "name": "db.create", "key": "db.create#…#1", "result": "ran"},
      {"index": 2, "name": "file.copy", "key": "file.copy#…#1", "result": "skipped", "reason": "condition not met"}
    ]
  }
}
```

Step `result` is `ran`, `up_to_date`, `skipped` (with `reason`), `failed` or
`not_reached`. `prepare.status` is `complete`, `failed`, `running`,
`interrupted` (a previous run died; the next `ensure` resumes) or `unprepared`.
`ready` is `true` only when status is `complete`. `data.ready` has the same
semantics as `status` and `context`: preparation refuses unmet component
requirements and missing required shared resources, and with
`--skip-prepare` they are evaluated read-only (a reason is added to
`warnings`; when they cannot be evaluated, `ready` is `false` with a
warning). A failure returns `ok: false`,
`error.code: "prepare_failed"` (with `details.failed_step`), exit status 6 and
the partial `data`. The error message only names the failing step: step errors
embed command output, which goes to stderr and is never written to JSON or
`.anvil.local`.

A step that runs again (`--refresh`, changed definition, declared environment,
inputs or outputs)
first has its completion record removed, so a failed or interrupted rerun can
never leave the earlier success in place. `env.read` (which sets template
variables without `store_as`) always runs, like `store_as` producers.
`db.migrate` runs on every ensure; for the worktree's owned testing database it
runs a non-destructive `migrate`, never `migrate:fresh` unless the one-off
`--db-action testing=fresh` flag selects it (`database_actions` in `anvil.yaml`
rejects `fresh`), so re-ensuring is safe while checks use that database. A
`fresh` rebuild is refused while a check of the worktree is running. Step
directories and declared `inputs`/`outputs` must resolve inside the worktree,
including through symlinks; escapes are refused before any effect.

## `anvil check [CHECK...] --json` (mutating) and `anvil check --list --json` (read-only)

Run:

```json
"data": {
  "worktree": { },
  "results": [
    {
      "schema": "anvil.check-result/v1",
      "run_id": "20261020T201500.123456789Z-1a2b3c4d",
      "check": "fast",
      "status": "passed",
      "exit_code": 0,
      "started_at": "…", "finished_at": "…", "duration_ms": 812, "queued_ms": 0,
      "worktree": {"id": "wt_…", "path": "…", "branch": "feature/x"},
      "identity": {
        "git": {"head": "<sha>", "tree": "<tree sha>", "branch": "feature/x", "dirty": false, "dirty_paths": 0, "state_digest": "<sha256>"},
        "definition_digest": "…", "toolchain": [{"command": "cargo --version", "output": "cargo 1.82.0", "exit_code": 0}],
        "toolchain_digest": "…", "environment_names": ["ANVIL_WORKTREE_ID", "…"], "environment_digest": "…",
        "inputs": [], "inputs_digest": "…",
        "resources": [{"name": "models", "path": "/…/models", "state": "present", "method": "file", "source": "SHA256SUMS", "digest": "…"}],
        "resources_digest": "…", "platform": {"os": "darwin", "arch": "arm64"},
        "identity_digest": "…",
        "content_environment_digest": "…", "content_toolchain_digest": "…", "content_digest": "…"
      },
      "commands": [{"run": "cargo test --lib", "dir": "…", "exit_code": 0, "duration_ms": 800, "log": "…/command-00.log"}],
      "artifacts": [{"path": "…", "sha256": "…", "size": 123}],
      "locks": [], "anvil_version": "…", "run_dir": "…",
      "producer": {"worktree_id": "wt_…", "path": "…", "role": "implementation", "lease_owner": "codex"},
      "reused": false,
      "assessment": {"current": true, "passing_and_current": true, "stale_reasons": [], "reused_from": null}
    }
  ]
}
```

A non-passing check sets `ok: false`, `error.code: "check_failed"` with
`details.{check,status,run_id}`, and exits with the failing command's status
(124 timeout, 130 interrupted). Interrupted while waiting for a resource lock,
nothing runs: `check_failed`, exit 130 and no result. A check cannot start
while its worktree is being removed
(`cleanup_blocked`). Child output never appears in stdout with
`--json`; it streams to stderr and is kept in the logs.

`identity.resources[]` lists only the shared resources the check declares
(sorted by name; `[]` and an empty `resources_digest` when none, which keeps
the identity digest of earlier versions). `state` is `present` or `absent`
(missing optional resource, empty `digest`). If a declared resource cannot be
identified (missing required resource, failing or timed-out identity command,
unreadable file or tree), `anvil check` runs nothing and returns
`check_failed` (exit 1) with `details.reason: "identity_unavailable"`,
`details.{check,resource}` and a message containing
`identity unavailable: shared resource "<name>": …`. Without
`details.reason`, `check_failed` means a check ran and did not pass (or was
interrupted). With several checks, the first identity error aborts the
remaining checks; results of checks that already finished are in `data`. `--list` and `status`
report `stale_reasons: ["identity unavailable: …"]`. Changed resources add one
stale reason each: `shared resource "<name>" changed`, `… appeared` or
`… disappeared`. Resource identities are machine-wide: identity commands run
in the resource directory with the inherited process environment only (minus
the review role's `env_unset` and any inherited `ANVIL_*` worktree variables
except `ANVIL_RUNTIME_ROOT`), never worktree variables, so two worktrees compute
the same resource identity unless the command's output depends on variables
removed by `roles.review.env_unset`. The directory identity cache is
best-effort: an unreadable cache is ignored (the next `anvil check` run rebuilds it) and a cache that cannot be written
is reported as a warning, never as an identity failure. Results recorded before this field existed decode with
`resources: null`.

List: `data.checks[]` has `name`, `description`, `expensive`, `reusable`,
`locks`, `commands`, `latest` (a result or `null`), `assessment`
(`current`, `passing_and_current`, `stale_reasons[]`, `reused_from`),
`lock_holders[]` and
`slots` (`null` for checks that take no slot, otherwise `{weight, capacity,
in_use, queued, holders[], queue[], queue_busy}`: machine-wide counts plus this
check's own slot holders and queue entries, shaped as in `anvil slots`). `status`
reports the same `reused_from` per check. When
`queue_busy` is true, `queued`, `holders` and `queue` are `null` because the
snapshot is unavailable.

### Machine-wide check slots

Checks marked `expensive: true` (weight 1) or declaring `slots: N` (weight N)
share one semaphore across every project and worktree on the machine, under
`<runtime root>/slots/`. Capacity comes from the global config
(`~/.config/anvil/anvil.yaml`: `checks: {slots: 2, max_load: 6.0, load_wait: 2m}`),
overridden by `ANVIL_CHECK_SLOTS`, `ANVIL_CHECK_MAX_LOAD` and
`ANVIL_CHECK_LOAD_WAIT`; the default capacity is `max(2, CPUs/4)` and load
gating is off unless `max_load` is set. A check whose weight exceeds the capacity
fails before anything runs with `config_invalid` (exit 5).

Capacity is enforced per runtime root: processes that share
`ANVIL_RUNTIME_ROOT` (by default one per user) share the slots; another user
or root has its own. Each participant takes slots among the first `capacity`
slot files it is configured with, so if participants are configured with
different capacities the effective machine-wide cap is the largest of them.
Keep `checks.slots` and `ANVIL_CHECK_SLOTS` consistent.

- **Order.** Waiters are admitted strictly head-of-line: higher
  `--priority N` first, then arrival. Only the head may take slots, so a
  request never overtakes an earlier request of equal or higher priority,
  even when it would fit. Responsive tickets come first: a ticket whose owner
  has not heartbeated for 30s queues behind them until it polls again.
- **Load.** With `max_load` set, the head waits (`waiting_for: "load"`) while
  the 1-minute load average exceeds it. `load_wait` (default 2m) bounds each
  check's load wait, measured from when it queued; when it elapses the check
  proceeds with a warning, because slot capacity
  still bounds concurrency. Where the load average is unavailable (not Linux
  or macOS), gating is skipped with a warning and never blocks.
- **Waiting.** `--slot-wait D` bounds the whole wait; the default `0` waits
  until interrupted. This differs from `--lock-wait`, whose `0` fails
  immediately. On timeout nothing runs: `resource_busy` (exit 7) with
  `details.{waiting_for, position, queued, capacity, in_use, holders[], queue_busy}`. If the queue lock is unavailable at timeout, `queue_busy` is true, `holders` is null, and `in_use` is still counted from the slot locks.
  Ctrl-C or SIGTERM while queued removes the ticket and behaves like any
  interruption before execution (`check_failed`, exit 130, no result).
  While queued, `check NAME: waiting for slot (position 2 of 3)` lines go to
  stderr (suppressed by `--quiet`). A queued check keeps holding its
  worktree's shared lifecycle gate and the check's active lock, so the
  worktree cannot be removed (`cleanup_blocked`) and the same check cannot
  start again there while it waits.
- **Nesting.** Commands of a check that holds a slot receive
  `ANVIL_CHECK_SLOT_HOLDER=<check>`. A slot-gated `anvil check` started there
  would wait for capacity its own ancestor holds, which never frees at full
  capacity, so with the default unbounded `--slot-wait` it is refused before
  queueing with `config_invalid` (exit 5) naming both checks. An explicit
  `--slot-wait D` queues as usual and fails with `resource_busy` when the
  bound elapses.
- **Queue state.** `queue.json` only orders waiters. If it is lost or
  malformed it is reset with a warning; capacity stays enforced by the slot
  lock files. Writes are atomic and synced.
- **Crash safety.** A weight-w run holds w lock files; the supervisor of each
  command inherits them like resource locks, so capacity is freed only when
  the run and its process group are gone, and always when they die. Every
  participant deletes queue tickets whose PID exited or now has a different
  kernel start identity. A live ticket that stops heartbeating for 30s (a
  stopped or wedged process) queues behind responsive tickets but keeps its
  ticket, and regains its place when it polls again.
- **Reuse.** `--reuse` is assessed (under the resource locks) before queueing;
  a hit executes nothing and takes no slot. On a miss, one non-blocking slot
  attempt is made while those locks are still held; if it succeeds the
  assessment stands (toolchain probes run once). Otherwise the locks are
  released, the check queues, and it is reassessed after admission.
- **Timing.** Results carry `queued_ms`, the slot queue wait (`0` for
  ungated checks); `started_at` and `duration_ms` exclude it.

Every result records its `producer`: the worktree ID and path that recorded
it, its `role` (when set) and the `owner` of its active lease (when leased).
Results recorded before producers existed decode with `producer: null`. Roles
and lease owners are declared metadata, not authentication.

### Reuse across worktrees

`--reuse` returns a passing result instead of running a check declared
`reusable: true`, from one of two places:

1. This worktree's latest result, when its full identity (`identity_digest`)
   is current and its artifacts are intact (unchanged since 1.x).
2. Otherwise, a clean passing result another worktree of the same project
   (same canonical Git common directory) recorded at the same **content
   identity** (`identity.content_digest`).

The content identity covers HEAD's `tree` (not the commit, so the same
content on another commit matches) and a clean checkout, the check definition
(`definition_digest`: commands, inputs, toolchain and resource declarations),
`inputs_digest` (paths relative to the worktree or `${ANVIL_RUNTIME_DIR}`),
`content_toolchain_digest` (toolchain probe output with the worktree path,
runtime directory and the other values below normalized; the exact
`toolchain_digest` keeps the raw output), the platform, `resources_digest`
and `content_environment_digest`. It is empty for a dirty checkout: a dirty
producer never feeds the index and a dirty consumer never reuses another
worktree's result. A run whose commands change the working tree (the state
re-read after the run differs from the recorded one) is not offered to other
worktrees either; a warning says so.

Content outside Git is **not** part of the identity: ignored or untracked
dependency trees (`vendor/`, `node_modules/`), generated `.env` files, build
caches and anything else Git does not track. Declare the files that determine
them, such as lockfiles and manifests (`composer.lock`, `package-lock.json`,
`Cargo.lock`, a generated `.env`), as `inputs`, and add `toolchain` probes
(`node --version`, `php -v`) for tools resolved outside the tree.

`content_environment_digest` covers each command's directory and its
declared and Anvil-set variables (project, component, resource, database
and command `env` values). Inherited process variables count only when they
are in this short default list of toolchain-affecting variables, and are
set: `PATH` (entries inside the worktree normalized), `GOFLAGS`, `GOOS`,
`GOARCH`, `CGO_ENABLED`, `NODE_OPTIONS`, `NODE_ENV`, `RUSTFLAGS`,
`CARGO_BUILD_TARGET`, `PHP_INI_SCAN_DIR` and `CI`. `PATH` must match exactly
apart from entries inside the worktree, so agents that prepend per-session
directories to `PATH` do not share results; a miss reporting "no producer
recorded at this content identity" usually means the environment, `PATH` or
toolchain differ — compare the `content_*_digest` fields of both identities.
They change what common
toolchains build or how test runners behave, while agent and session
variables (`CLAUDE_SESSION_ID`, terminal and shell state) do not and are
ignored. A check extends the list with `identity_env: [NAME]`; every other
inherited variable is ignored, so prefer declaring values in `env` and
recording tool versions with `toolchain` probes. Secret values stay
placeholders. Worktree-specific Anvil values are normalized before hashing;
each one is safe because it names or isolates the worktree rather than
describing what was validated:

| Value | Placeholder | Matching |
|-------|-------------|----------|
| worktree path (`ANVIL_WORKTREE_PATH`, command dirs) | `${ANVIL_WORKTREE}` | at path boundaries; inputs and artifacts are already worktree-relative |
| runtime directory (`ANVIL_RUNTIME_DIR` and the standard dirs below it) | `${ANVIL_RUNTIME_DIR}` | at path boundaries |
| `ANVIL_WORKTREE_ID`, `ANVIL_INSTANCE` | `${ANVIL_WORKTREE_ID}`, `${ANVIL_INSTANCE}` | whole identifiers |
| `ANVIL_WORKTREE_NAME`, `ANVIL_BRANCH` | `${ANVIL_WORKTREE_NAME}`, `${ANVIL_BRANCH}` | that variable only (names are not unique) |
| each reserved port `ANVIL_PORT_<NAME>` (and its `env` alias) | `${ANVIL_PORT_<NAME>}` | whole numbers, e.g. inside `APP_URL` |
| `ANVIL_DB_DATABASE`, `ANVIL_TEST_DB_DATABASE` | `${ANVIL_DB_DATABASE}`, `${ANVIL_TEST_DB_DATABASE}` | whole identifiers, so `DB_DATABASE` or DSNs derived from them match |

Only the consumer's own values are rewritten: a declared value naming
another worktree's path stays literal. `ANVIL_WORKTREE_NAME` and
`ANVIL_BRANCH` are replaced only as whole variables, so a declared value
derived from them (`APP_NAME: demo-${ANVIL_WORKTREE_NAME}`) differs between
worktrees and prevents reuse; derive such values from `${ANVIL_INSTANCE}`
instead. Database normalization assumes each
worktree's database comes from the same declared preparation; a check whose
outcome depends on undeclared database contents must not be `reusable`.

Whenever a reusable check passes on a clean checkout, Anvil writes an index
entry `<runtime root>/reuse/<sha256(common dir)>/<check>/<content digest>/<worktree id>.json`
pointing at that run's `result.json`. A lookup revalidates every entry and
skips it, with a reason, unless: the result lies inside
`<runtime root>/worktrees/<producer id>/`; it passed, was not itself reused,
and its recorded identity was clean and recomputes to the same content digest;
the producer is still a registered worktree of this repository whose
`.anvil.local` identity has the same ID; the trust policy admits the producer;
and every recorded artifact still has its digest. A failed validation is a
miss (a `check <name> not reused: this worktree: …; other worktrees: …`
warning, then the check runs), never an error; with no entry at all the
reason is `no producer recorded at this content identity`. An entry whose
result file no longer exists (pruned run) is removed by the lookup, and
removing a worktree (`remove`, `prune`, `unlink` runtime cleanup) removes all
of its entries. Index changes take a per-check index lock (a leaf lock;
lookups read entries without it because entries are replaced atomically).

Trust model: the index and the results it points at are trusted as written by
the same user's Anvil processes. Validation guards against stale state and
honest mistakes, not against a user forging records; roles, lease owners and
the trust policy below are a coordination aid for honest agents, not a
security boundary.

A cross-worktree hit runs nothing and writes a record into the consumer's own
store: the consumer's identity, `status: "passed"`, no `commands`, the
producer's `artifacts`, the consumer as `producer`, and
`reused_from: {worktree_id, path, role, lease_owner, run_id, check}` naming the
producing run. `check --list` and `status` in the consumer therefore report it
current with `assessment.reused_from`; a later `--reuse` there returns that
record (if the policy still admits its producer).

Where `reused_from` appears: run output (`check --reuse`) carries
`assessment.reused_from` whenever `reused` is true, including same-worktree
reuse, where it names this worktree's own earlier run; it is `null` for
executed runs. `check --list` and `status` show the stored record, so they
report `reused_from` only for consumer records reused from another worktree
(same-worktree reuse writes no new record and shows `null` there).

Trust policy: `--reuse-trust same-worktree|same-role|any` (default
`roles.<role>.reuse_trust`, else `any` for every role, so a fresh review
worktree reuses an author's pass) and `--reuse-exclude-role ROLE`
(repeatable) added to `roles.<role>.reuse_exclude_roles`. `same-role`
compares the recorded producer role with this worktree's role (unlabelled
worktrees match each other). Exclusions never apply to a worktree's own
results. The flag replaces the configured trust level but cannot remove
configured exclusions. Invalid values return `invalid_arguments`.

```yaml
roles:
  review:
    reuse_trust: any                       # same-worktree | same-role | any
    reuse_exclude_roles: [implementation]  # independent reviewers rerun author results
checks:
  fast:
    reusable: true
    identity_env: [RUSTFLAGS]              # inherited values that matter
```

Check identities fingerprint each resolved command directory and effective
non-secret environment, including component values, expanded command overrides
and inherited process variables (the content identity used across worktrees
excludes inherited variables; see above). Execution receives the same snapshot. Values
are never stored. Names marked by `secret_env` or Anvil's secret-name heuristic
contribute a placeholder, so changing only a secret does not invalidate reuse.

Declared inputs and artifacts must remain inside the worktree or runtime
root. Symlinks below those roots (including ancestor directories and links
inside input directories) are rejected with a diagnostic; they cannot be reused.

Dirty submodules and nested repositories are rejected when their recursive
state cannot be identified from the parent checkout. Clean them before running
or reusing checks; two unidentifiable dirty states never share a passing result.

Artifact patterns must match regular files. Directory artifacts fail the run
with a diagnostic and disable reuse; use a file glob such as `bundle/*` instead.

Execution identity and reuse assessment are computed only after all declared
resource locks are held. Inputs produced by a preceding lock holder are included
in that identity, which commands receive as `ANVIL_CHECK_IDENTITY`.

Checks (including reuse), ensure and scaffold hold a shared worktree lifecycle
gate from startup through completion. Remove, prune and unlink cleanup take the
exclusive gate before any safety inspection and retain it through deletion and
runtime release. A conflict fails closed (`cleanup_blocked`, exit 7). Cleanup
also retains the preparation lock and refuses an existing preparation holder.
Gate and preparation lock files live in a per-user directory outside the
worktree and its Git directory. Gate files are never deleted.

Lock order is lifecycle gate → preparation lock → repository database lock →
check active lock → machine-wide check slots → resource locks sorted by name →
run lock → `.anvil.local` state lock. The reuse index lock is a leaf lock:
nothing else is acquired while it is held. A queued check holds no resource lock;
the slot queue file lock is held only briefly and never while waiting. One
exception: on a `--reuse` miss a gated check, while still holding its resource
locks, waits up to 2s for the queue file lock and makes one non-blocking slot
attempt (queued behind earlier requests like any ticket); this cannot deadlock
because a queue-lock holder never waits on resource or slot locks. Never upgrade a shared gate; acquire multiple worktree gates in
canonical path order. Lease mutations must use the same shared gate.

On Unix, checks and toolchain probes run beneath a separate supervisor which
inherits the lifecycle, active, slot, resource and run locks. Parent death closes a
heartbeat, terminates the process group, and retains exclusion until the group
is gone. Commands must keep descendants in that group rather than daemonizing.
A group whose termination cannot be established keeps the locks (fail closed).
Windows check execution and toolchain probes are refused until job-object
supervision provides the same complete-lifetime guarantee.

## `anvil ensure` role, task and lease flags

```bash
anvil ensure feature/x --role implementation --task-id SOLO-7 --task-url https://… --pr 42
anvil ensure --review <commit> [NAME]          # role review, pinned_commit, detached
anvil ensure feature/x --lease-owner codex --lease-ttl 2h --session-id S [--takeover-stale]
```

`data.metadata`: `{role, pinned_commit, task: {id, url, pr}, created_at}`.
Recorded roles and pins are never changed implicitly (`invalid_arguments`).
A review worktree keeps its role on every later `ensure` (by name, path, ID or
from inside it, with or without `--review`), so its minimal preparation and
`roles.review.components` scope always apply. `--review` never adopts an
existing worktree without role metadata (a branch worktree, the main worktree
or one made by `work`/`scaffold`): choose another name. Reusing a review
worktree requires its checkout to still be detached at the pinned commit;
otherwise `ensure` refuses with `invalid_arguments` and changes nothing.
`roles.review.components` limits the plan before requirements are evaluated:
an excluded component's missing tools or directory never block a review.
`data.lease`: see below; `token` is present only when this call issued it.

## `anvil remove WORKTREE --json` (mutating)

`WORKTREE` is required (exact folder, branch, path or ID). Because `--json`
never prompts, removal requires `--force`; otherwise `invalid_arguments`
(exit 2) before any effect. `--dry-run` needs neither confirmation nor
`--force` and changes no worktree, branch, `.anvil.local`, runtime directory,
port, process or database; it still takes the lifecycle, preparation and
database coordination locks.

```json
"data": {
  "worktree": { },
  "dry_run": true,
  "removed": false,
  "stop_processes": [{"name": "web", "state": "running", "pid": 123, "command": "…", "log": "…"}],
  "release_ports": [{"port": 20431, "name": "web", "worktree_id": "wt_…", "worktree_path": "…"}],
  "remove_runtime_dir": "…",
  "databases": {"action": "drop", "names": ["app_x", "app_x_test"], "targets": ["app_x", "app_x_test", "app_x_test_1"]},
  "branch": {"name": "feature/x", "delete": false, "deleted": false}
}
```

`databases.names` are the records in `.anvil.local`; `targets` are what
`db.destroy` dropped or, in a dry run, would drop, including enumerated
parallel-worker databases. A dry run never starts a database service: if it is
not running, `targets` holds only the recorded names and a warning says
`parallel-worker databases not enumerated: service not running`.
`databases.action` is `drop` (a `db.destroy` cleanup step is configured; its
condition is not evaluated for the plan), `preserve` (`--keep-db`) or
`untouched`; `suffix` appears only when legacy suffix-based cleanup applies: the
worktree has a `db_suffix`, no exact database records, and `action` is `drop`.
`branch.delete` reflects `--delete-branch`. Errors:
`worktree_not_found` (3), `lease_held` (7), `cleanup_blocked` (7: checks or
preparation running, a borrower, unreadable or malformed state, a locked
worktree, a locked registered worktree that is absent). An unlocked registered
sibling whose directory no longer exists is skipped with a warning suggesting
`git worktree prune` or `git worktree repair`.

## `anvil adopt [WORKTREE] --json` / `--all` (mutating)

Records the Anvil identity of existing worktrees without any scaffolding or
preparation, exactly as `ensure` would (`EnsureIdentity` semantics), and with
`--lease-owner` acquires or renews a lease through the same code as `ensure`.
`--dry-run` predicts every action and lease conflict and writes nothing.

```json
"data": {
  "dry_run": false,
  "worktrees": [
    {"worktree": { }, "action": "adopted", "reason": "identity recorded", "lease": {"state": "none"}},
    {"worktree": { }, "action": "skipped", "reason": "main checkout (name it explicitly to adopt it)", "lease": null},
    {"worktree": { }, "action": "unchanged", "reason": "…", "lease": { }, "error": {"code": "lease_held", "message": "…"}}
  ]
}
```

`action` is `adopted` (no identity → new ID), `unchanged` (identity matches),
`updated` (moved: ID and lease kept), `reassigned` (copied: new ID; the
original's `lease` and `processes` records are dropped from the copy) or
`skipped` (bare, missing directory, or the main checkout with `--all`).
An active lease of another owner is never replaced: the entry carries
`error`, and the command fails with `lease_held` (7; `lease_stale` without
`--takeover-stale`), `details.lease` naming the holder. With several failures
the first one's code is used. `--all` together with `WORKTREE` is
`invalid_arguments`.

## `anvil prune candidates --json` (read-only)

`data` is the candidate document (also written to `--output FILE`):

```json
"data": {
  "schema": "anvil.prune-candidates/v1",
  "generated_at": "2026-10-21T12:00:00Z",
  "repository": {"common_dir": "/src/app/.git", "project": "app", "project_path": "/src/app", "default_branch": "main", "merge_target": "origin/main"},
  "settings": {"recent_seconds": 86400, "protected": ["release/*"], "fetched": false, "pr_lookup": true, "strict_processes": false, "allow_uninspectable": [], "allow_ignored": [".env"]},
  "worktrees": [
    {
      "worktree": { },
      "kind": "branch",
      "candidate": false,
      "reasons": ["branch merged into origin/main", "all commits exist on a remote"],
      "blockers": ["1 tracked change(s)"],
      "warnings": [],
      "evidence": {
        "merge": {"state": "merged", "target": "origin/main"},
        "pr": {"state": "merged", "number": 42, "base": "main", "merged_at": "…", "head_oid": "…", "source": "gh"},
        "remote": {"state": "pushed", "unpushed_commits": 0},
        "tracked": {"count": 1},
        "untracked": {"count": 0, "sample": [], "excluded": ["node_modules"]},
        "ignored": {"count": 1, "allowed": 1, "sample": ["storage/"], "newest_at": "…", "excluded": ["node_modules", "vendor", ".anvil.local"]},
        "lease": {"state": "none"},
        "protected": {"protected": false},
        "activity": {"state": "idle", "latest_at": "…", "latest_source": "files", "threshold_seconds": 86400,
                     "sources": {"head_reflog": "…", "files": "…", "lease_renewed": "…", "check_result": "…", "ignored_files": "…"},
                     "running_checks": [], "warnings": []},
        "open_files": {"method": "procfs", "holders": [{"pid": 4242, "command": "vim", "via": ["fd"], "path": "…"}],
                       "skipped_other_users": 0, "uninspectable_own": [], "foreign_namespace_own": []}
      }
    }
  ]
}
```

`kind` is `branch`, `main`, `bare`, `detached`, `locked`, `missing` or
`default_branch`; only `branch` worktrees get `evidence` and can be candidates.
`candidate` is `true` only when `blockers` is empty. Every evidence `error`,
`unknown` state or unsupported method is a blocker
(`evidence unavailable: <what>: …`), never proof of inactivity. Git runs with
`--no-optional-locks`, so the scan never rewrites an index. `merge.state`
is `merged`, `not_merged` or `unknown`; `not_merged` is accepted only when the
PR is `merged` and its `head_oid` equals the local branch tip (squash/rebase
merges). `pr.state` is `open` (blocker, any base), `merged` (into the default
branch), `merged_other_base` (never treated as merged), `closed`, `none` or
`unavailable` (`reason`; not a blocker). `ignored` lists gitignored paths
(ignored directories collapsed, e.g. `storage/`) outside `node_modules`,
`vendor` and `.anvil.local`; `count` paths not allowed by `--allow-ignored` or
`prune.allow_ignored` (a glob matching the relative path or a parent
directory) block, `allowed` counts the rest. `remote.state` is `pushed`,
`unpushed` (blocker) or `unknown`. `activity.state` is `recent` (blocker),
`idle` or `unknown`; `--recent` must be at least `10m` (`invalid_arguments`
otherwise), so the check cannot be disabled. A missing HEAD reflog adds
`activity.warnings` ("reflog unavailable") and does not block. `open_files.method` is `procfs` (Linux:
`/proc/<pid>/{cwd,root,exe,fd/*,maps}`), `lsof` (macOS) or `unsupported`.
Own processes that cannot be inspected (`uninspectable_own`) block unless named
by `--allow-uninspectable NAME`, which exempts every process with that command
name (`comm`, truncated to 15 characters and settable by the same user) in
every worktree. **Known gap:** other users' processes that cannot be inspected
only warn by default: `skipped_other_users` counts them on Linux and is `null`
on macOS, where `lsof` cannot see them at all (a warning is emitted on every
scan). A root-owned container (Docker, Sail) or another account using the
worktree is therefore not detected unless `--strict-processes` is given, which
blocks on them (always on macOS: "other users' processes cannot be inspected on
this platform"). The permissive default exists because system daemons are
always uninspectable for non-root users. Processes in another mount namespace
(containers, snap/flatpak apps) cannot have their paths mapped: own ones are
listed in `foreign_namespace_own` and block ("runs in another mount
namespace") unless named by `--allow-uninspectable`; other users' are counted
in `skipped_other_users`; kernel threads are ignored. Containers sharing a
worktree should hold a lease. Activity sources also include `preparation`
(the preparation record's start/finish); rewriting `.anvil.local` (adopting,
lease bookkeeping) is not activity. Agents should use `prune candidates`/`prune apply` rather than legacy
`anvil prune --force`, which only checks merge state and leases. Nothing is fetched unless `--fetch`. Worktrees without an `ok` identity
are blocked until `anvil adopt`.

## `anvil prune apply --from FILE --json` (mutating)

Without `--dry-run`, `--force` is required (like `remove --json`; it never
prompts); otherwise `invalid_arguments` (2) before any effect, also in human
mode. Each result carries `warnings` from the latest evaluation.

`FILE` is the candidate document or a `prune candidates --json` envelope.
A wrong schema, malformed file, invalid pattern or recent window, a path or
worktree ID listed twice, a `--recent` below `10m` or a `common_dir` other than
the current project's is `invalid_arguments` (2) before any effect.

```json
"data": {
  "dry_run": false,
  "from": "candidates.json",
  "repository": { },
  "settings": { },
  "results": [
    {"worktree": { }, "outcome": "removed"},
    {"worktree": { }, "outcome": "skipped", "code": "blocked", "reason": "blocked by current evidence: …", "blockers": ["active lease by \"codex\" (task \"\") until …"]},
    {"worktree": { }, "outcome": "skipped", "code": "target_mismatch", "reason": "… is not a registered worktree of this repository"}
  ]
}
```

`outcome` is `removed`, `would_remove` (`--dry-run`), `skipped` or `failed`.
Skip codes: `not_candidate` (the file did not mark it), `target_mismatch`
(path not absolute or not canonical, not a registered worktree of this
repository, or worktree ID differs), `changed` (branch or HEAD changed since
generation), `blocked` (current evidence has blockers), `lease_held` /
`cleanup_blocked` (refused by the removal lifecycle before any effect).
Evidence is collected again before removal and once more under the exclusive
lifecycle gate (after the lease check, before stopping processes or running
cleanup steps). Settings are this invocation's flags, strengthened by the
file's (`protected` combined, the larger `recent_seconds`, `strict_processes`
if either, `allow_ignored` only where both allow it); `allow_uninspectable` is
never taken from the file.
`--ignore-lease` is never applied. If any removal `failed`, the command fails
with the first failure's code. Residual race: a non-Anvil process that starts
using the worktree after the final recheck and before `git worktree remove`
is not detected; Anvil's own users are excluded by the lifecycle gate.

## Leases

```json
"lease": {
  "state": "active",
  "owner": "codex", "task_id": "SOLO-7", "session_id": "S",
  "acquired_at": "…", "renewed_at": "…", "expires_at": "…", "ttl_seconds": 7200,
  "previous": {"owner": "claude", "how": "taken_over_stale", "expires_at": "…", "ended_at": "…"},
  "token": "lt_…"
}
```

`state` is `active`, `stale` (not renewed before `expires_at`) or `none`.
`context`, `status`, `status --all` and `ensure` report this object without
`token` (which appears only in the response that issued it). `state` is always
present; when the lease record cannot be read, `status --all` reports
`{"state": "unknown"}` with a warning (single-worktree commands fail with
`internal_error`), never `none`.
Acquire, renew, release and break are atomic read-modify-writes under the
worktree's state lock; concurrent acquisitions have exactly one winner. Only
the token's SHA-256 is stored. `lease_held` (active, other owner),
`lease_stale` (needs `--takeover-stale`) and `lease_not_owned` (wrong token or
no lease) carry the conflicting lease in `error.details.lease`. A task ID never
authorizes renew/release. `anvil remove` (and `prune`/`unlink --clean`)
refuse an active lease with `lease_held` unless `--ignore-lease`; a stale lease
only warns.
`prune --ignore-lease` and `unlink --clean --ignore-lease` apply the same explicit
override to every selected worktree. `--force` alone does not bypass leases.
Lease mutations, `ensure`, preparation and checks hold the shared per-worktree
lifecycle gate. Cleanup holds it exclusively through Git removal and resource
release. A mutation during cleanup fails with `cleanup_blocked` and leaves state
unchanged. After taking the gate, CLI callers revalidate the target's Git
directory, registration and HEAD before claim writes or starting checks.
The lock order is lifecycle, preparation, repository database lifecycle, check
active, check slots, sorted resources, run, then local state (with the
non-blocking `--reuse` slot attempt described above as the only exception). Lifecycle and
preparation gates live outside the worktree and Git directory and remain after removal, so
concurrent callers use the same lock even during deletion.

## `anvil slots --json` (read-only)

```json
"data": {
  "dir": "…/anvil/slots",
  "capacity": 2, "in_use": 1,
  "load": {"supported": true, "value": 3.12, "max": 6, "wait_seconds": 120},
  "holders": [{"slot": 0, "weight": 1, "pid": 4242, "check": "bench", "worktree": "…", "worktree_id": "wt_…", "run_id": "…", "priority": 0, "since": "…"}],
  "queue": [{"position": 1, "waiting_for": "slot", "priority": 5, "weight": 2, "check": "e2e", "worktree": "…", "worktree_id": "wt_…", "pid": 4310, "enqueued_at": "…", "heartbeat": "…", "responsive": true}],
  "queue_busy": false
}
```

Machine-wide and project-independent. `load.max` is `0` when load gating is
off; `load.value` is `null` (and `supported` false) where the load average is
unavailable, and `load.error` then says why (omitted otherwise). `queue` is in admission order and omits tickets of dead or
recycled PIDs; only position 1 can be `waiting_for: "load"`. Holders are
reported only while their slot lock is held. The view creates nothing.
`queue_busy: true` (with a warning) means the queue lock stayed busy: `in_use`
is still exact, probed from the slot locks, but `queued`, `holders` and
`queue` are `null` (unavailable), never reported as empty. The `slots` objects
of `check --list` and `status` carry the same `queue_busy` flag.

## `anvil status --json`

```json
"data": {
  "worktree": {}, "ready": true, "metadata": {}, "lease": {},
  "git": {"head": "…", "branch": "…", "dirty": false, "dirty_paths": 0, "state_digest": "…", "pinned_commit_matches": true},
  "prepare": {"status": "complete", "ready": true, "failed_step": "", "error": "", "finished_at": "…"},
  "processes": [{"name": "web", "state": "running", "pid": 123, "command": "…", "log": "…"}],
  "ports": [{"port": 20431, "name": "web", "worktree_id": "wt_…", "listening": true}],
  "checks": [{"name": "fast", "status": "passed", "run_id": "…", "current": true, "passing_and_current": true, "stale_reasons": []}],
  "slots": {"capacity": 2, "in_use": 1, "queued": 1, "holders": [], "queue": [{"position": 1, "waiting_for": "slot", "check": "e2e", "…": "…"}]},
  "cleanup": {
    "allowed": false,
    "blocked_by": ["active lease by \"codex\" (task \"SOLO-7\"); release it or use --ignore-lease"],
    "warnings": [], "stop_processes": [], "release_ports": [], "remove_runtime_dir": "…", "owned_databases": []
  }
}
```

`slots` is the machine-wide check slot `capacity`, `in_use` and `queued`
counts with this worktree's own `holders[]` and `queue[]` entries (shaped as
in `anvil slots`), or `null` with a warning when slot settings are invalid.

`ready` requires complete preparation, satisfied component requirements and
present required shared resources. `prepare.status` is as for `ensure`;
`failed_step`, `error` (a classified summary, never child output) and
`finished_at` are present when recorded. Process `state` is `running`,
`exited`, `reused` (PID recycled — never signaled), `unknown` or `foreign`.

Single `status` fails with `internal_error` when the preparation record
cannot be read. `anvil context --json` reports the same `ready` and `prepare`
next to `worktree`, `metadata` and `lease`, but degrades instead of failing:
an unreadable preparation record is reported as `prepare.status: "unknown"`,
`ready: false` and a warning.

### `anvil status --all --json`

```json
"data": {
  "worktrees": [
    {
      "worktree": {"...": "see Worktree identity", "managed": true},
      "ready": true,
      "role": "implementation",
      "task": {"id": "SOLO-7"},
      "lease": {"state": "active", "owner": "codex", "task_id": "SOLO-7", "expires_at": "…"},
      "prepare": {"status": "complete", "ready": true, "finished_at": "…"},
      "dirty": false
    }
  ]
}
```

Entries use the same `worktree`, `lease` and `ready` semantics as single
`status`. `prepare` is summarized: it carries `status`, `ready`,
`failed_step` and `finished_at` but never `error`, so a list of many
worktrees stays small; read the error with `anvil status WORKTREE`. `ready` is
evaluated (components and shared resources) only for worktrees whose
preparation is complete; why such a worktree is not ready is added to
`warnings` (prefixed with the entry's exact `worktree.path`), and when the evaluation fails (for
example an invalid `anvil.yaml` in that worktree) it is `false` with a
"readiness unavailable" warning while the command still succeeds. A value that cannot be read for a worktree is reported as `unknown`
with a warning, never as absent: `role: "unknown"`, `lease.state: "unknown"`,
`prepare.status: "unknown"`. `dirty` is omitted when the working state cannot
be read.

**Compatibility.** Before this release `status --all` entries reported
`lease` and `prepare` as bare strings. They are now the objects used by
single `status`: read `lease.state` instead of `lease`, and `prepare.status`
instead of `prepare` (values are unchanged). This redefinition would normally
bump the schema; it is deliberately kept under `anvil.agent/v1` to keep one
contract version, and is called out here and in the changelog. `ready` and
`worktree.managed` are additive.

## `anvil version --json`

`anvil version` and `anvil --version` (`-v`) print the same line,
`anvil version X (commit: C, built: D)`, exit 0, never prompt and never run
first-run setup; `version` takes no arguments (`invalid_arguments`, exit 2).
The machine-readable form is `anvil version --json` (read-only);
`anvil --version --json` is not supported.

```json
"data": {"version": "1.13.0", "commit": "<sha>", "build_date": "…", "go_version": "go1.24.4", "os": "darwin", "arch": "arm64"}
```

## Integrations

Solo, Teyos, Claude Code and Codex consume the same commands: an orchestrator
calls `ensure --lease-owner … --task-id …`, hands the worktree ID and lease token
to a worker, polls `status --json`, and calls `check --list --json` to decide
whether results are current. Anvil stores task identifiers only as metadata
and never assigns work, reviews, merges or orchestrates agents. An MCP adapter
can wrap these commands later without changing this contract.
