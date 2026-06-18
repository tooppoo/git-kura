# ls --json returns key enumeration only

- Status: Accepted
- Created: 2026-06-18T14:55:56Z

## Context

`git kura ls` lists the open git-kura managed worktrees. Its human-readable output emits one key per line, and `git kura get <key>` is the dedicated command for retrieving detailed worktree metadata (branch name, worktree path, base branch, and so on).

When adding `--json` output to `ls` as part of the common output framework (issue #62), a potential design was to include full worktree metadata in each array element—effectively making `ls --json` a batch version of `get --json`. This would collapse enumeration and detail retrieval into a single command.

## Decision

`git kura ls --json` returns a `data` object whose only field is `keys`: a sorted string array of the open worktree keys.

```json
{
  "keys": ["51", "62"]
}
```

- `keys` must contain every open git-kura managed worktree key.
- `keys` must be alphabetically sorted, matching the order of human-readable output.
- `keys` must be an empty array (`[]`) when no worktrees are open, never `null`.
- `ls --json` must not include worktree metadata such as branch name, worktree path, or base branch.
- Callers that need per-key details must invoke `git kura get <key> --json` for each key.

## Alternatives Considered

### Include full worktree metadata in each element

`ls --json` could return an array of objects identical to or derived from the `get --json` payload.

This was rejected because it would blur the responsibility boundary between `ls` (enumerate) and `get` (inspect one key). The combined payload would also grow in size and schema surface with every new field added to `get --json`, making the two commands harder to evolve independently.

### Return a map from key to metadata

`ls --json` could return `{"51": {...}, "62": {...}}` rather than a flat list.

This was rejected for the same responsibility reasons, and additionally because a JSON object with dynamic string keys is harder to process in typed languages and harder to validate with JSON Schema.

## Consequences

### Positive Consequences

- `ls` stays responsible for enumeration only; `get` stays responsible for per-key detail.
- Human-readable and JSON output carry the same semantics (a list of keys, nothing more).
- The JSON payload is small and stable regardless of how many fields `get` grows.
- Scripts and AI agents can enumerate keys with `ls --json` and fetch details selectively with `get --json`.

### Negative Consequences

- A caller that needs details for all open worktrees must issue one `get --json` call per key; there is no single-round-trip way to retrieve all metadata.

### Neutral Consequences

- The `ls.schema.json` command-specific schema has a single required field, which makes it a minimal schema.
