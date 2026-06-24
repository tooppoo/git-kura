# JSON Schema reference

This document is the entry point for all JSON Schema used by git-kura.

It explains which schemas are stable external contracts, which fields are observable but not primary contract targets, and which schemas govern internal persistence only.

For a general overview of the output formats and when to use `--json`, `--toon`, or human output, see [output-format.md](output-format.md).

## Schema classification

git-kura schemas are grouped into three classes.

| Class | Stability | External use |
|---|---|---|
| **Public contract** | Stable. Breaking changes require a version bump and ADR. | External scripts, tools, AI agents may depend on these. |
| **Limited contract** | Best-effort stable for documented fields. | AI agents may read documented fields. Direct schema validation is not recommended. |
| **Internal contract** | May change without notice. | Must not be used by external tools. |

The classification policy is recorded in [ADR 20260624T114339Z](adr/20260624T114339Z_json-schema-classification-and-placement.md).

## Note on `internal/` paths

All schema files in this repository live under `internal/`.
The `internal/` prefix is a Go module boundary: it prevents external Go packages from importing the package.
It does not restrict the JSON Schema *files* from being stable external contracts.
Public contract schemas under `internal/` are treated as stable output contracts regardless of their Go package path.

## Public contract schemas

These schemas define the stable machine-readable output for `--json` output.
External scripts, tools, and AI agents may depend on them.
TOON output (`--toon`) is generated from the same envelope and data as JSON, but TOON whitespace, line order, and field rendering are not part of the stable contract.

### Common envelope

The common envelope wraps every `--json` response.

| File | GitHub view | Raw URL (`main`) |
|---|---|---|
| `internal/output/schema/envelope.schema.json` | [view](https://github.com/tooppoo/git-kura/blob/main/internal/output/schema/envelope.schema.json) | `https://raw.githubusercontent.com/tooppoo/git-kura/main/internal/output/schema/envelope.schema.json` |

Tag-based raw URL example (v0.0.5):

```
https://raw.githubusercontent.com/tooppoo/git-kura/v0.0.5/internal/output/schema/envelope.schema.json
```

The `main`-based URL always points to the latest commit on `main` and may reflect unreleased changes.
The tag-based URL points to the schema as it was at a specific release.
Use the tag-based URL when you need to pin validation to a known release.

### Command-specific data schemas

Each command's success `data` payload is defined by its own schema, nested under the envelope's `data` field.

| Command | File | GitHub view | Raw URL (`main`) |
|---|---|---|---|
| `git kura get` | `internal/cli/schema/get_data.schema.json` | [view](https://github.com/tooppoo/git-kura/blob/main/internal/cli/schema/get_data.schema.json) | `https://raw.githubusercontent.com/tooppoo/git-kura/main/internal/cli/schema/get_data.schema.json` |
| `git kura open` | `internal/cli/schema/commands/open.schema.json` | [view](https://github.com/tooppoo/git-kura/blob/main/internal/cli/schema/commands/open.schema.json) | `https://raw.githubusercontent.com/tooppoo/git-kura/main/internal/cli/schema/commands/open.schema.json` |
| `git kura close` | `internal/cli/schema/commands/close.schema.json` | [view](https://github.com/tooppoo/git-kura/blob/main/internal/cli/schema/commands/close.schema.json) | `https://raw.githubusercontent.com/tooppoo/git-kura/main/internal/cli/schema/commands/close.schema.json` |
| `git kura ls` | `internal/cli/schema/commands/ls.schema.json` | [view](https://github.com/tooppoo/git-kura/blob/main/internal/cli/schema/commands/ls.schema.json) | `https://raw.githubusercontent.com/tooppoo/git-kura/main/internal/cli/schema/commands/ls.schema.json` |
| `git kura seal claim` | `internal/cli/schema/commands/seal_claim.schema.json` | [view](https://github.com/tooppoo/git-kura/blob/main/internal/cli/schema/commands/seal_claim.schema.json) | `https://raw.githubusercontent.com/tooppoo/git-kura/main/internal/cli/schema/commands/seal_claim.schema.json` |
| `git kura seal unclaim` | `internal/cli/schema/commands/seal_unclaim.schema.json` | [view](https://github.com/tooppoo/git-kura/blob/main/internal/cli/schema/commands/seal_unclaim.schema.json) | `https://raw.githubusercontent.com/tooppoo/git-kura/main/internal/cli/schema/commands/seal_unclaim.schema.json` |
| `git kura seal test` | `internal/cli/schema/commands/seal_test.schema.json` | [view](https://github.com/tooppoo/git-kura/blob/main/internal/cli/schema/commands/seal_test.schema.json) | `https://raw.githubusercontent.com/tooppoo/git-kura/main/internal/cli/schema/commands/seal_test.schema.json` |
| `git kura seal ls` | `internal/cli/schema/commands/seal_ls.schema.json` | [view](https://github.com/tooppoo/git-kura/blob/main/internal/cli/schema/commands/seal_ls.schema.json) | `https://raw.githubusercontent.com/tooppoo/git-kura/main/internal/cli/schema/commands/seal_ls.schema.json` |
| `git kura seal doctor` | `internal/cli/schema/commands/seal_doctor.schema.json` | [view](https://github.com/tooppoo/git-kura/blob/main/internal/cli/schema/commands/seal_doctor.schema.json) | `https://raw.githubusercontent.com/tooppoo/git-kura/main/internal/cli/schema/commands/seal_doctor.schema.json` |

Tag-based raw URLs follow the same pattern as the envelope example above.

## Limited contract

These structures appear in `--json` output but are not the primary stable contract target.
AI agents may read the documented fields.
Direct schema validation tooling should not depend on these structures.
Deleting, renaming, or changing the semantics of a documented field here requires a PR description or ADR entry.

### `warnings[].details`

`warnings[].details` is a command-specific object present on warnings that carry additional context.

| Command | Warning code | AI-agent-readable fields |
|---|---|---|
| `git kura open` (dry-run) | `open-dry-run-conflict` | `details.conflicts[].type` (`worktree-path`, `branch`, or `metadata`), `details.conflicts[].path`, `details.conflicts[].branch` |

### `error.details`

`error.details` is a command-specific object present on execution failures.

| Command | AI-agent-readable fields |
|---|---|
| `git kura close` | `details.phase` (failed stage), `details.partialResult` (what completed before failure), `details.storeError.status`, `details.storeError.path` |
| `git kura seal claim` | `details.phase`, `details.paths[].path`, `details.paths[].status`, `details.paths[].ownerKey` (when `status` is `owned-by-other`), `details.conflicts[].path`, `details.conflicts[].ownerKey`, `details.storeError.status`, `details.storeError.path` |
| `git kura seal unclaim` | `details.phase`, `details.paths[].path`, `details.paths[].status`, `details.paths[].ownerKey` (when `status` is `owned-by-other`), `details.conflicts[].path`, `details.conflicts[].ownerKey`, `details.storeError.status`, `details.storeError.path` |
| `git kura seal test` | `details.reason` (why the current key could not be resolved), `details.repositoryRoot`, `details.metadataPath` |

### Diagnostic command business-failure payloads

`git kura seal test` and `git kura seal doctor` produce a business result (`ok:true`) that can be a pass or a failure, separate from execution failure (`ok:false`).

| Command | AI-agent-readable fields |
|---|---|
| `git kura seal test` | `data.passed` (boolean), `data.results[].path`, `data.results[].status`, `data.results[].claimedBy` |
| `git kura seal doctor` | `data.healthy` (boolean), `data.findings[].path`, `data.findings[].message` |

Undocumented fields in `details` or `data` are not limited contract and must not be relied upon.

## Internal contract schemas

These schemas govern the on-disk persistence format that git-kura reads and writes under `.git/kura/`.
They are not external integration targets.
External tools must not depend on these schemas.
They may change without public notice.

Changes that affect persisted state require documenting migration, compatibility, and recovery policy within the git-kura project.
Such changes are versioned by the relevant state schema's own `schemaVersion` field, not by the output envelope `schemaVersion`.

| Purpose | File |
|---|---|
| Worktree metadata (stored at `<git-common-dir>/kura/meta/worktrees/<key>.json`) | `internal/worktree/schema/metadata.schema.json` |
| Seal store (stored at `<git-common-dir>/kura/seals/paths.json`) | `internal/seal/schema/seal_store.schema.json` |
| Tools install metadata | `internal/tools/schema/tools_metadata.schema.json` |
| Pre-commit component metadata | `internal/tools/schema/pre_commit_meta.schema.json` |

No GitHub view links or raw URLs are provided for these schemas because they are not intended for external validation.
