# Seal mutation status codes

`git kura seal claim` and `git kura seal unclaim` use an all-or-nothing
preflight phase that classifies every input path before writing to the seal
store. This document lists every status code that can appear in the `paths`
array of a JSON response.

## Success statuses (ok:true)

These statuses appear in the `data.paths` array of a successful response.

| Status | Command | Meaning |
|---|---|---|
| `claimed` | claim | Path was newly added to the seal store for the current key. |
| `already-owned` | claim | Path was already claimed by the current key. No change was made (idempotent). |
| `released` | unclaim | Path was removed from the seal store. |
| `not-claimed` | unclaim | Path was not in the seal store. No change was made (idempotent). |

## Preflight error statuses (ok:false, phase: "preflight")

These statuses appear in the `error.details.paths` array when the command
fails. At least one blocking status is present; non-blocking statuses reflect
what would have happened had the operation succeeded.

| Status | Blocking | Command | Meaning |
|---|---|---|---|
| `would-claim` | No | claim | Path would have been newly claimed if no blocking paths were present. |
| `already-owned` | No | claim | Path is already claimed by the current key (same as success). |
| `would-release` | No | unclaim | Path would have been released if no blocking paths were present. |
| `not-claimed` | No | unclaim | Path is not in the seal store (same as success). |
| `owned-by-other` | **Yes** | claim, unclaim | Path is claimed by a different key. |
| `duplicate` | **Yes** | claim, unclaim | Path normalizes to the same store key as an earlier argument (see `duplicateOf`). |
| `invalid-path` | **Yes** | claim | Path does not exist, is a directory, or cannot be stat-checked. |
| `outside-repository` | **Yes** | claim, unclaim | Path is absolute or resolves outside the repository root. |

### `duplicateOf` field

When a path item has status `duplicate`, the item also carries a `duplicateOf`
field: a zero-based index into the `paths` array identifying the first
occurrence of the same normalized path in the input.

### `ownerKey` field

When a path item has status `owned-by-other`, the item also carries an
`ownerKey` field: the key that currently holds the claim on that path.

## Preflight error details shape

When the command fails during preflight, `error.details` includes:

- `phase`: `"preflight"`
- `currentKey`: the current key derived from the managed worktree
- `paths[]`: every input path with its preflight status (see table above)
- `conflicts[]`: one entry per `owned-by-other` path, each with `path`, `ownerKey`, `requestedKey`
- `duplicates[]`: one entry per `duplicate` path, each with `path`, `firstIndex`, `duplicateIndex`

## Store-level errors (ok:false)

When the seal store itself cannot be read or written, the response carries
`error.details.phase` and `error.details.storeError`. No `paths` array is
present because the failure is not attributable to a specific input path.

| Phase | `storeError.status` | Meaning |
|---|---|---|
| `preflight` | — | Setup before store access failed (key resolution, repository lookup, lock acquisition). `storeError` is absent. |
| `read-store` | `store-read-error` | The seal store file could not be read from disk. |
| `validate-store` | `store-validation-error` | The seal store file was read but failed JSON Schema validation (malformed or hand-edited). |
| `write-store` | `store-write-error` | Writing the updated store failed. |

`storeError.path` is the store file path when available. Example:

```json
{
  "phase": "validate-store",
  "storeError": {
    "status": "store-validation-error",
    "path": ".git/kura/seals.json"
  }
}
```
