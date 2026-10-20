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
| `schema` | string | Contract version. Additive fields keep the version; renamed, removed or redefined fields bump it. |
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
- Secrets are never included. Error messages keep only the first line of a
  failing step's error; full command output is not echoed into JSON.

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
| `anvil start` / `anvil stop [WT] --json` | yes | Managed processes |
| `anvil lease acquire\|renew\|release\|break [WT] --json` | yes | Advisory leases |
| `anvil lease show [WT] --json` | no | Lease state |
| `anvil run [WT] -- CMD` | no (state) | Exec in the worktree environment; child stdio and exit status are the child's own |

Read-only commands never write `.anvil.local`, allocate ports or create
directories. `check --list` and `status` run only the declared toolchain probe
commands to decide currency. Mutating commands are not transactional across
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
  "head": "<40-hex commit>"
}
```

`identity_status` is `ok`, `missing` (never prepared), `moved` (path changed;
the ID is kept on the next `ensure`) or `copied` (another directory holds the
same ID; the copy receives a new ID and drops the original's lease and process
records on the next `ensure`, and is never treated as their owner).

## `anvil ensure [WORKTREE] --json` (mutating)

```json
"data": {
  "worktree": { "...": "see above" },
  "created": false,
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
`ready` is `true` only when status is `complete`. A failure returns `ok: false`,
`error.code: "prepare_failed"`, exit status 6 and the partial `data`.

## `anvil check [CHECK...] --json` (mutating) and `anvil check --list --json` (read-only)

Run:

```json
"data": {
  "worktree": { },
  "results": [
    {
      "schema": "anvil.check-result/v1",
      "run_id": "20261020T201500Z-1a2b3c4d",
      "check": "fast",
      "status": "passed",
      "exit_code": 0,
      "started_at": "…", "finished_at": "…", "duration_ms": 812,
      "worktree": {"id": "wt_…", "path": "…", "branch": "feature/x"},
      "identity": {
        "git": {"head": "<sha>", "branch": "feature/x", "dirty": false, "dirty_paths": 0, "state_digest": "<sha256>"},
        "definition_digest": "…", "toolchain": [{"command": "cargo --version", "output": "cargo 1.82.0", "exit_code": 0}],
        "toolchain_digest": "…", "environment_names": ["ANVIL_WORKTREE_ID", "…"], "environment_digest": "…",
        "inputs": [], "inputs_digest": "…", "platform": {"os": "darwin", "arch": "arm64"},
        "identity_digest": "…"
      },
      "commands": [{"run": "cargo test --lib", "dir": "…", "exit_code": 0, "duration_ms": 800, "log": "…/command-00.log"}],
      "artifacts": [{"path": "…", "sha256": "…", "size": 123}],
      "locks": [], "anvil_version": "…", "run_dir": "…",
      "reused": false
    }
  ]
}
```

A non-passing check sets `ok: false`, `error.code: "check_failed"` with
`details.{check,status,run_id}`, and exits with the failing command's status
(124 timeout, 130 interrupted). Child output never appears in stdout with
`--json`; it streams to stderr and is kept in the logs.

List: `data.checks[]` has `name`, `description`, `expensive`, `reusable`,
`locks`, `commands`, `latest` (a result or `null`), `assessment`
(`current`, `passing_and_current`, `stale_reasons[]`) and `lock_holders[]`.

## `anvil ensure` role, task and lease flags

```bash
anvil ensure feature/x --role implementation --task-id SOLO-7 --task-url https://… --pr 42
anvil ensure --review <commit> [NAME]          # role review, pinned_commit, detached
anvil ensure feature/x --lease-owner codex --lease-ttl 2h --session-id S [--takeover-stale]
```

`data.metadata`: `{role, pinned_commit, task: {id, url, pr}, created_at}`.
Recorded roles and pins are never changed implicitly (`invalid_arguments`).
`data.lease`: see below; `token` is present only when this call issued it.

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
Acquire, renew, release and break are atomic read-modify-writes under the
worktree's state lock; concurrent acquisitions have exactly one winner. Only
the token's SHA-256 is stored. `lease_held` (active, other owner),
`lease_stale` (needs `--takeover-stale`) and `lease_not_owned` (wrong token or
no lease) carry the conflicting lease in `error.details.lease`. A task ID never
authorizes renew/release. `anvil remove` (and `prune`/`unlink --clean`)
refuse an active lease with `lease_held` unless `--ignore-lease`; a stale lease
only warns.

## `anvil status --json`

```json
"data": {
  "worktree": {}, "ready": true, "metadata": {}, "lease": {},
  "git": {"head": "…", "branch": "…", "dirty": false, "dirty_paths": 0, "state_digest": "…", "pinned_commit_matches": true},
  "prepare": {"status": "complete", "ready": true, "failed_step": "", "error": "", "finished_at": "…"},
  "processes": [{"name": "web", "state": "running", "pid": 123, "command": "…", "log": "…"}],
  "ports": [{"port": 20431, "name": "web", "worktree_id": "wt_…", "listening": true}],
  "checks": [{"name": "fast", "status": "passed", "run_id": "…", "current": true, "passing_and_current": true, "stale_reasons": []}],
  "cleanup": {
    "allowed": false,
    "blocked_by": ["active lease by \"codex\" (task \"SOLO-7\"); release it or use --ignore-lease"],
    "warnings": [], "stop_processes": [], "release_ports": [], "remove_runtime_dir": "…", "owned_databases": []
  }
}
```

`ready` requires complete preparation, satisfied component requirements and
present required shared resources. Process `state` is `running`, `exited`,
`reused` (PID recycled — never signaled), `unknown` or `foreign`.

## Integrations

Solo, Teyos, Claude Code and Codex consume the same commands: an orchestrator
calls `ensure --lease-owner … --task-id …`, hands the worktree ID and lease token
to a worker, polls `status --json`, and calls `check --list --json` to decide
whether results are current. Anvil stores task identifiers only as metadata
and never assigns work, reviews, merges or orchestrates agents. An MCP adapter
can wrap these commands later without changing this contract.
