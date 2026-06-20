
# JSON output for state-changing commands and seal mutation error details

- Status: Accepted
- Created: 2026-06-20T00:00:00Z

## Context

Earlier JSON output support was limited to read-only commands (`get`, `ls`,
`seal ls`, `seal test`, `seal doctor`) and the `open --dry-run` preview. Users
of the JSON interface had no way to capture structured output from the commands
that actually mutate state: `open`, `close`, `seal claim`, and `seal unclaim`.

Two related design questions had to be resolved together:

1. **Which effect fields should appear in the open and close success payload?**
   The caller may want to know whether a worktree was freshly created vs.
   already existed, whether a branch had to be created, whether metadata was
   written, and how many seals were released. These are facts about what the
   command actually did, not inputs to the command.

2. **How should seal claim/unclaim report errors?**
   Both commands already validated every path before writing (all-or-nothing
   preflight). In JSON mode the caller needs to know which path failed and why,
   not just that the command failed. Reporting only the first error (the
   existing human-mode behaviour) loses information.

## Decision

### `open --json` without `--dry-run`

`open --json` (with or without `--dry-run`) succeeds and emits a JSON
envelope. The schema is shared between dry-run and actual open via the
`openDataSchema` variable (the old `openDryRunDataSchema` alias is kept for
backward compatibility in tests).

The actual open payload adds three optional boolean fields that are absent in
dry-run mode:

```json
{
  "createdWorktree": true,
  "createdBranch":   true,
  "createdMetadata": true
}
```

Each field is `true` when this invocation created the resource and `false`
when it already existed (e.g. the worktree directory was already present).

### `close --json`

`close --json` emits a JSON envelope with the following required fields:

```json
{
  "key":               "51",
  "worktreePath":      "/path/to/worktree",
  "branch":            "51",
  "removedWorktree":   true,
  "removedBranch":     true,
  "removedMetadata":   true,
  "releasedSealCount": 0
}
```

Effect fields are `false` (not `true`) when the corresponding resource was
already absent. `releasedSealCount` counts paths removed from the seal store.

On error, `error.details.phase` identifies the stage that failed (see below).

### `seal claim --json` and `seal unclaim --json`

`--json` must appear before the path arguments. The success payload is:

```json
{
  "currentKey": "task-1",
  "paths": [
    { "path": "src/foo.ts", "status": "claimed" },
    { "path": "src/bar.ts", "status": "already-owned" }
  ]
}
```

On error, the response is an ok:false envelope whose `error.details` carries:

```json
{
  "phase": "preflight",
  "currentKey": "task-1",
  "paths": [
    { "path": "src/foo.ts", "status": "would-claim" },
    { "path": "src/bar.ts", "status": "owned-by-other", "ownerKey": "other-task" }
  ],
  "conflicts": [
    { "path": "src/bar.ts", "ownerKey": "other-task", "requestedKey": "task-1" }
  ],
  "duplicates": []
}
```

Every input path appears in `paths` with its preflight classification.
Blocking statuses prevent the write; non-blocking statuses show what would
have happened. `conflicts[]` lists ownership conflicts (one entry per
`owned-by-other` path). `duplicates[]` lists duplicate normalized paths.
See `docs/commands/seal-mutation-status.md` for the full status catalogue.

For store-level failures (`read-store`, `validate-store`, `write-store`),
`phase` identifies the failed stage and `storeError` carries the store-wide
failure details. `read-store` means the file could not be read; `validate-store`
means the file was read but failed JSON Schema validation; `write-store` means
the updated store could not be persisted.

```json
{
  "phase": "validate-store",
  "storeError": {
    "status": "store-validation-error",
    "path": ".git/kura/seals.json"
  }
}
```

### All-or-nothing preflight in JSON mode

The existing all-or-nothing semantic is preserved. The difference in JSON mode
is that the preflight loop collects the classification of every input path
before returning, rather than stopping at the first error. This gives the
caller complete information in a single invocation.

In non-JSON mode the behaviour is unchanged: the first non-conflict path error
is returned immediately; cross-key conflicts are collected and reported
together.

## Alternatives Considered

### Report only the first blocking path in JSON error details

Simple to implement, but callers doing batch operations would need multiple
round-trips to discover all blocking paths. Rejected in favour of full
preflight reporting.

### Separate `open --actual-json` flag

Keeping `--json` exclusively for dry-run and adding a new flag for actual open
avoids a flag meaning change. Rejected because the ambiguity was already
resolved by the precedent set in `close`: `--json` always means "structured
output for whatever this command does". The old restriction was a temporary
limitation, not a design principle.

### Omit `conflicts[]` and `duplicates[]` from error details

Keeping only `paths[]` would be simpler, but callers doing conflict detection
would need to scan `paths` and filter by status. The Issue #63 Finalize comment
explicitly requires `conflicts[]` and `duplicates[]` as top-level fields in
`error.details` for diagnosability, so this alternative was not adopted.

## Consequences

### Positive Consequences

- Callers can capture structured output from every git-kura command, not just
  read-only ones.
- Batch scripts using `seal claim/unclaim --json` see all blocking paths in
  one response instead of one per invocation.
- The open schema is now unified across dry-run and actual open; the effect
  fields tell callers exactly what happened.

### Negative Consequences

- The `open --json` flag now has a different meaning (actual creation) than it
  had before this change (usage error). Any caller that relied on the usage
  error to detect `--json` without `--dry-run` must be updated.

### Neutral Consequences

- `openDryRunDataSchema` is kept as an alias for `openDataSchema` so existing
  tests and documentation that reference it continue to work without changes.
