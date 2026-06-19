# seal ls --json uses a unified claim shape for project-wide and key-filtered output

- Status: Accepted
- Created: 2026-06-18T14:55:59Z

## Context

`git kura seal ls` lists path claims from the repository-wide seal store. Without arguments it lists every claim across all keys (project-wide). With an explicit key argument it lists only the claims held by that key.

When adding `--json` output to `seal ls` as part of the common output framework (issue #62), two structural questions arose: whether project-wide and key-filtered output should use different JSON shapes, and whether key-filtered claim items should omit the `key` field since every item would share the same key.

## Decision

`git kura seal ls --json` and `git kura seal ls --json <key>` both return a `data` object with the same shape.

Project-wide listing:

```json
{
  "filterKey": null,
  "claims": [
    { "key": "51", "path": "src/foo.go" }
  ]
}
```

Key-filtered listing:

```json
{
  "filterKey": "51",
  "claims": [
    { "key": "51", "path": "src/foo.go" }
  ]
}
```

- `filterKey` must be `null` when no key argument is given.
- `filterKey` must be the key string when a key argument is given.
- `claims` must be an array of claim objects; it must be `[]` when no matching claims exist, never `null`.
- Each claim object must always include both `key` and `path`, even in key-filtered output where every `key` matches `filterKey`.
- `path` must be a repository-root relative path with `/` separators.
- `claims` must be sorted by key, then by path, matching the sort order of human-readable output.

## Alternatives Considered

### Use separate shapes for project-wide and key-filtered output

Project-wide output would return `{"claims": [{"key": "51", "path": "..."}]}` and key-filtered output would return `{"key": "51", "claims": [{"path": "..."}]}`.

This was rejected because it forces callers to branch on the presence or absence of a filter key before parsing claims. A unified shape means callers always treat `claims[]` identically.

### Omit `key` from each claim item in key-filtered output

Since every claim item in a key-filtered response shares the same `filterKey`, the `key` field is technically redundant.

This was rejected because it introduces a structural difference between project-wide and key-filtered responses that complicates processing. It also means a consumer cannot treat each claim as a self-contained record without reading `filterKey` first. The redundancy is accepted in exchange for shape stability.

## Consequences

### Positive Consequences

- Both modes share a single JSON Schema definition (`seal_ls.schema.json`).
- Callers process `claims[]` identically regardless of whether a filter key was given.
- `filterKey` makes the scope of the listing explicit and machine-readable.
- Each claim item is self-contained and can be processed without referring to the envelope.

### Negative Consequences

- In key-filtered output, `key` inside each claim item always equals `filterKey`, which is redundant.

### Neutral Consequences

- The human-readable and JSON output share the same sort order (by key, then by path).
