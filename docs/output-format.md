# Output formats

This document covers the structured output policy for `git kura` commands. It
applies to commands that emit a machine-readable `--json` envelope and a
human-readable default. Scalar output (single-value flags such as `--path`,
`--branch`, `--root`) is outside scope.

## Information classification

Every field in a structured output payload — success `data`, `warnings[].details`, and `error.details` — is assigned one of three classes:

| Class | Meaning |
|---|---|
| **human-required** | Must appear in human output. Contains user-actionable information: affected paths, owners, effect flags, or a next-action hint. |
| **human-conditional** | Shown in human output only when a warning or error condition is present. |
| **machine-only** | Omitted from human output. Includes envelope metadata (`schemaVersion`, `command`, empty arrays) that a human reader does not need. |

### Diagnostic command convention

Some commands (seal test, seal doctor) produce a business result that is either
"passed" or "failed", separate from an execution failure. The two outcomes must
be distinguishable in both human and JSON output:

| Outcome | JSON | Human |
|---|---|---|
| Business pass | `ok:true, data.passed:true` | silent |
| Business failure | `ok:true, data.passed:false`, exit code > 0 | structured message token on **stdout** |
| Execution failure | `ok:false` envelope | error message with different token on **stderr** |

## Per-command output policy

### `git kura get`

`git kura get` supports scalar and structured output.

## Scalar output

Returns a single value, suitable for shell substitution.

```sh
git kura get 51 --path    # absolute path to the worktree
git kura get 51 --branch  # branch name
```

## Structured output

### JSON (`--json` / `--format json`)

JSON is the canonical machine-readable format. Use it for scripts, tools, and integrations. `--format json` is an alias of `--json`.

```sh
git kura get 51 --json
git kura get 51 --format json
git kura open 51 --dry-run --json
```

JSON output uses the common output envelope. The worktree metadata is nested under `data`:

```json
{
  "ok": true,
  "command": "get",
  "schemaVersion": 1,
  "data": {
    "schemaVersion": 1,
    "key": "51",
    "kind": "worktree",
    "branch": "issue/51",
    "worktreePath": "/home/user/projects/myrepo-issue-51",
    "repositoryRoot": "/home/user/projects/myrepo",
    "baseBranch": "main",
    "exists": true,
    "dirty": false
  },
  "warnings": []
}
```

A valid `get --json` request that fails at execution time returns an `ok: false` envelope on stdout with the failure under `error`, while preserving the existing exit code.

Scalar output (`--path` / `--branch` / `--root`) is not wrapped in the envelope and must not be combined with `--json` / `--format json` / `--toon` / `--format toon`; doing so is a usage error (exit code 2).

`git kura open <key> --dry-run --json` uses the common envelope with the planned worktree under `data`, validated by its own command-specific schema. In dry-run data, `baseBranch` is the current branch and both `exists` and `dirty` are `false`. Conditions that would collide at real creation time (an existing worktree path, branch, or metadata) are reported in `warnings[]` under code `open-dry-run-conflict`, with the colliding items in `details.conflicts`; the dry run still succeeds with exit code 0. On its own (without `--json`), `git kura open <key> --dry-run` prints human-readable output instead of an envelope.

### TOON (`--toon` / `--format toon`)

[TOON](https://github.com/toon-format/toon) is a prompt-friendly, AI-oriented format generated from the same envelope as JSON. The top-level structure mirrors the JSON envelope (`ok`, `command`, `schemaVersion`, `data`, `warnings`, and on failure `error`) but is formatted for readability in LLM prompts.

```sh
git kura get 51 --toon
git kura get 51 --format toon
```

Example output (success):

```toon
ok: true
command: get
schemaVersion: 1
data:
  schemaVersion: 1
  key: fizz
  kind: worktree
  branch: fizz
  worktreePath: /workspaces/git-kura/.git/kura/worktrees/fizz
  repositoryRoot: /workspaces/git-kura
  baseBranch: main
  exists: true
  dirty: false
warnings[0]:
```

TOON output is written to **stdout** on both success and failure, matching JSON behaviour. The whitespace, line order, and field rendering are not part of the stable contract — only the JSON envelope schema and the JSON command-data schema are stable. Use TOON when passing worktree context to an LLM prompt or coding agent; use `--json` for scripts and external tooling.

All commands that support `--json` also support `--toon`. The two flags are mutually exclusive; combining them is a usage error.

### `git kura close`

`git kura close <key>` removes a worktree and its associated branch and
metadata. It outputs one summary line followed by per-effect lines.

**Human output** (example):

```
closed: .git/kura/worktrees/task1 (branch: task1)
  removed worktree
  removed branch
  removed metadata
  released 2 seal(s)
```

**Field classification:**

| Field | Class | Notes |
|---|---|---|
| `key` | machine-only | Present in JSON only |
| `worktreePath` | human-required | Identifies what was removed |
| `branch` | human-required | Identifies the removed branch |
| `removedWorktree` | human-required | Shown when true |
| `removedBranch` | human-required | Shown when true |
| `removedMetadata` | human-required | Shown when true |
| `releasedSealCount` | human-required | Shown when > 0 |

**`error.details` field classification** (when execution fails mid-close):

| Field | Class | Notes |
|---|---|---|
| `error.details.phase` | human-required | Names the stage that failed (`preflight`, `read-store`, `validate-store`, `remove-worktree`, `remove-branch`, `release-seals`, `remove-metadata`) |
| `error.details.partialResult` | human-conditional | Mirrors the success `data` shape; shows what completed before the failure |
| `error.details.storeError.status` | human-required when `phase` is `read-store` or `validate-store` | Seal store failure type |
| `error.details.storeError.path` | human-required when `phase` is `read-store` or `validate-store` | Seal store file path |

### `git kura seal claim`

`git kura seal claim <paths...>` registers each path under the current key. One
line is emitted per path.

**Human output** (example):

```
claimed: src/foo.go
already owned: src/bar.go
```

**Field classification:**

| Field | Class | Notes |
|---|---|---|
| `currentKey` | machine-only | Implicit from the worktree context |
| `paths[].path` | human-required | Identifies the file |
| `paths[].status` | human-required | `claimed` or `already owned` |

**`error.details` field classification** (when claim execution fails):

| Field | Class | Notes |
|---|---|---|
| `error.details.phase` | human-required | Names the stage that failed (`preflight`, `read-store`, `validate-store`, `write-store`) |
| `error.details.currentKey` | machine-only | Same as the worktree key; implicit from context |
| `error.details.paths[].path` | human-required when `phase` is `preflight` | Per-input path |
| `error.details.paths[].status` | human-required when `phase` is `preflight` | Per-path disposition: `would-claim`, `already-owned`, `owned-by-other`, `duplicate`, `invalid-path`, `outside-repository` |
| `error.details.paths[].ownerKey` | human-conditional | Present when `status` is `owned-by-other` |
| `error.details.paths[].duplicateOf` | machine-only | Index of the first occurrence; present when `status` is `duplicate` |
| `error.details.conflicts[].path` | human-required when conflict | Path that is already claimed by a different key |
| `error.details.conflicts[].ownerKey` | human-required when conflict | Key that currently owns the path |
| `error.details.conflicts[].requestedKey` | machine-only | Same as `currentKey` |
| `error.details.duplicates[].path` | human-required when duplicate | Path that appears more than once in the input |
| `error.details.duplicates[].firstIndex` | machine-only | Index of first occurrence in the input list |
| `error.details.duplicates[].duplicateIndex` | machine-only | Index of the duplicate in the input list |
| `error.details.storeError.status` | human-required when `phase` is `read-store`, `validate-store`, or `write-store` | Seal store failure type |
| `error.details.storeError.path` | human-required when `phase` is `read-store`, `validate-store`, or `write-store` | Seal store file path |

### `git kura seal unclaim`

`git kura seal unclaim <paths...>` removes a path claim. One line per path.

**Human output** (example):

```
released: src/foo.go
not claimed: src/bar.go
```

**Field classification:**

| Field | Class | Notes |
|---|---|---|
| `currentKey` | machine-only | Implicit from the worktree context |
| `paths[].path` | human-required | Identifies the file |
| `paths[].status` | human-required | `released` or `not claimed` |

**`error.details` field classification** (when unclaim execution fails):

| Field | Class | Notes |
|---|---|---|
| `error.details.phase` | human-required | Names the stage that failed (`preflight`, `read-store`, `validate-store`, `write-store`) |
| `error.details.currentKey` | machine-only | Same as the worktree key; implicit from context |
| `error.details.paths[].path` | human-required when `phase` is `preflight` | Per-input path |
| `error.details.paths[].status` | human-required when `phase` is `preflight` | Per-path disposition: `would-release`, `not-claimed`, `owned-by-other`, `duplicate`, `invalid-path`, `outside-repository` |
| `error.details.paths[].ownerKey` | human-conditional | Present when `status` is `owned-by-other` |
| `error.details.paths[].duplicateOf` | machine-only | Index of the first occurrence; present when `status` is `duplicate` |
| `error.details.conflicts[].path` | human-required when conflict | Path claimed by a key other than the caller |
| `error.details.conflicts[].ownerKey` | human-required when conflict | Key that currently owns the path |
| `error.details.conflicts[].requestedKey` | machine-only | Same as `currentKey` |
| `error.details.duplicates[].path` | human-required when duplicate | Path that appears more than once in the input |
| `error.details.duplicates[].firstIndex` | machine-only | Index of first occurrence in the input list |
| `error.details.duplicates[].duplicateIndex` | machine-only | Index of the duplicate in the input list |
| `error.details.storeError.status` | human-required when `phase` is `read-store`, `validate-store`, or `write-store` | Seal store failure type |
| `error.details.storeError.path` | human-required when `phase` is `read-store`, `validate-store`, or `write-store` | Seal store file path |

### `git kura seal test`

`git kura seal test <paths...>` checks whether paths are claimable by the
current key without modifying the store (read-only).

- **Pass** (`data.passed:true`, exit 0): no human output — nothing actionable.
- **Conflict** (`data.passed:false`, exit 6): stdout contains `seal-conflict:`
  with the path and the key that owns it. Stdout is used because a conflict is
  a business result (`ok:true`), not an execution failure.
- **Execution failure** (exit 1): stderr contains `current-key-unresolved:`
  with the reason; stdout is empty and never contains `seal-conflict:`.

**Field classification:**

| Field | Class | Notes |
|---|---|---|
| `currentKey` | machine-only | |
| `passed` | machine-only | Exit code carries the same signal |
| `results[].path` | human-conditional | Shown on conflict via stdout |
| `results[].status` | human-conditional | Shown on conflict via stdout |
| `results[].claimedBy` | human-required when conflict | Owner key shown on stdout |

**`error.details` field classification** (when execution fails — distinct from conflict):

| Field | Class | Notes |
|---|---|---|
| `error.details.reason` | human-required | Describes why the current key could not be resolved |
| `error.details.repositoryRoot` | human-conditional | Present when the repository root was determined |
| `error.details.metadataPath` | human-conditional | Present when the metadata path could be determined |

### `git kura seal doctor`

`git kura seal doctor` validates the integrity of the entire seal store
(repository-wide, read-only).

- **Healthy** (`data.healthy:true`, exit 0): no human output.
- **Unhealthy** (`data.healthy:false`, exit 7): stdout contains
  `seal-doctor-error:` followed by all finding messages.
- **Execution failure** (exit 1 or 2): stderr error message without `seal-doctor-error:`.

**Field classification:**

| Field | Class | Notes |
|---|---|---|
| `healthy` | machine-only | Exit code carries the same signal |
| `summary.*` | machine-only | Counts; not shown in human output |
| `findings[].severity` | machine-only | Not separately rendered |
| `findings[].code` | machine-only | Not separately rendered |
| `findings[].path` | human-conditional | Embedded within message on stdout when unhealthy |
| `findings[].message` | human-required when unhealthy | Full finding text on stdout via `seal-doctor-error:` |

### `git kura seal ls`

`git kura seal ls` lists all current claims. Human output is a plain list; each
line shows the key and path.

### `git kura ls`

`git kura ls` lists open worktree keys. Human output is a plain list of keys,
one per line.

### `git kura open`

`git kura open <key>` creates a managed worktree.

**Human output** (example):

```
opened: .git/kura/worktrees/key1 (branch: key1)
  created worktree
  created branch
  created metadata
```

**Field classification:**

| Field | Class | Notes |
|---|---|---|
| `key` | machine-only | Present in JSON only |
| `worktreePath` | human-required | First line; callers extract the path |
| `branch` | human-required | Shown in first line |
| `repositoryRoot` | human-conditional | Shown in dry-run only |
| `baseBranch` | human-conditional | Shown in dry-run only |
| `createdWorktree` | human-required | Shown when true |
| `createdBranch` | human-required | Shown when true |
| `createdMetadata` | human-required | Shown when true |

**`warnings[].details` field classification** (code `open-dry-run-conflict`, dry-run only):

| Field | Class | Notes |
|---|---|---|
| `warnings[].details.conflicts[].type` | machine-only | Discriminates `worktree-path`, `branch`, or `metadata` |
| `warnings[].details.conflicts[].path` | human-required when conflict | The colliding worktree path or metadata path |
| `warnings[].details.conflicts[].branch` | human-required when conflict | The colliding branch name |

For `--dry-run` without `--json`, the format shows `worktree path`, `branch`, `repository root`, and `base branch` so the user can review what will be created.

## Schema reference

For the full schema specification — including public contract schemas with GitHub view links and raw URLs, limited-contract fields for AI agents, and internal persistence schemas — see [schema.md](schema.md).

The common envelope schema is at `internal/output/schema/envelope.schema.json` and command-specific data schemas are under `internal/cli/schema/`.
Runtime validation embeds the schemas used by the corresponding packages via `//go:embed`.
