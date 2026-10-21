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
- Argument and flag errors (unknown flags, extra arguments) also produce one
  `invalid_arguments` envelope when `--json` appears anywhere before `--`.
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

`--dry-run` returns `invalid_arguments` (exit 2) before any changes.

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
`error.code: "prepare_failed"` (with `details.failed_step`), exit status 6 and
the partial `data`. The error message only names the failing step: step errors
embed command output, which goes to stderr and is never written to JSON or
`.anvil.local`.

A step that runs again (`--refresh`, changed definition, declared environment,
inputs or outputs)
first has its completion record removed, so a failed or interrupted rerun can
never leave the earlier success in place. `env.read` (which sets template
variables without `store_as`) always runs, like `store_as` producers. Step
directories and declared `inputs`/`outputs` must resolve inside the worktree,
including through symlinks; escapes are refused before any effect.
