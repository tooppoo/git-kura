# JSON Schema classification and placement policy

- Status: Accepted
- Created: 2026-06-24T11:43:39Z

## Context

git-kura uses JSON Schema (draft 2020-12) in two distinct roles: as machine-readable output contracts that external scripts and AI agents can rely on, and as internal validation schemas that ensure the correctness of persisted state.

These roles have different stability guarantees and different expectations about who may depend on them.

Earlier ADRs established individual schema decisions without a unified classification policy:

- [20260609T005136Z_json-schema-for-get-output.md](20260609T005136Z_json-schema-for-get-output.md) defined the original schema location as `cmd/kura/schema/output.schema.json` and established a single combined schema for `get --json` output.
- [20260617T070134Z_output-framework-envelope-result-renderer.md](20260617T070134Z_output-framework-envelope-result-renderer.md) placed the envelope schema at `cmd/git-kura/schema/envelope.schema.json` as part of the output framework introduction.
- [20260622T150313Z_output-framework-extract-to-internal-package.md](20260622T150313Z_output-framework-extract-to-internal-package.md) moved the envelope schema to `internal/output/schema/envelope.schema.json`.

The schema landscape now spans five locations and multiple audiences.
Without a unified classification, it is unclear to external integrators and AI agents which schemas they may safely depend on, which schemas are observed but not guaranteed, and which schemas are purely internal.

A further complication is the meaning of `internal/` in Go.
`internal/` prevents external Go packages from importing the package.
It does not restrict whether a JSON Schema *file* in that tree may be referenced as a contract by external tools.
Without explicit documentation, callers may either over-depend on internal schemas that can change without notice, or under-use output schemas that are designed for stable external consumption.

## Decision

### Schema classification

Every JSON Schema in this repository is assigned to exactly one of three classes: public contract, limited contract, or internal contract.

#### Public contract

Public contract schemas are the stable machine-readable output contracts for external scripts, tools, and AI agents.

Schemas in this class:

- `internal/output/schema/envelope.schema.json` — the common output envelope
- `internal/cli/schema/get_data.schema.json` — `git kura get` success data
- `internal/cli/schema/commands/open.schema.json` — `git kura open` success data
- `internal/cli/schema/commands/close.schema.json` — `git kura close` success data
- `internal/cli/schema/commands/ls.schema.json` — `git kura ls` success data
- `internal/cli/schema/commands/seal_claim.schema.json` — `git kura seal claim` success data
- `internal/cli/schema/commands/seal_unclaim.schema.json` — `git kura seal unclaim` success data
- `internal/cli/schema/commands/seal_test.schema.json` — `git kura seal test` success data
- `internal/cli/schema/commands/seal_ls.schema.json` — `git kura seal ls` success data
- `internal/cli/schema/commands/seal_doctor.schema.json` — `git kura seal doctor` success data

Policy:

- External integrators and AI agents may depend on these schemas.
- TOON output is generated from the same envelope and data as JSON, but its whitespace, line order, and field rendering are not part of the stable contract. Only the JSON envelope schema and the JSON command-data schemas are stable.
- Breaking changes require a schema version bump, an ADR, and a `docs/schema.md` update.
- `docs/schema.md` must list the GitHub view link and raw URL for each public contract schema.
- `docs/schema.md` must explain the difference between a `main`-based raw URL and a tag-based raw URL.

#### Limited contract

Limited contract schemas are output structures that are externally observable but not intended as the primary stable contract for long-term integration.

Structures in this class:

- `warnings[].details` — command-specific warning detail payloads
- `error.details` — command-specific error detail payloads
- Diagnostic command business-failure payloads (`data.passed`, `data.healthy`, `findings[]`)
- Dry-run warning payloads (e.g. `open-dry-run-conflict`)

Policy:

- AI agents may read fields documented in `docs/schema.md` for these structures.
- git-kura will make reasonable effort to keep documented limited-contract fields stable.
- Deleting, renaming, or changing the semantics of a documented limited-contract field requires a PR description or ADR entry explaining the reason.
- `docs/schema.md` must list the AI-agent-readable fields for each limited-contract structure, but must not provide raw URLs for direct schema validation tooling.
- `docs/schema.md` may include GitHub view links to the schema files.
- Limited-contract structures are not considered for common envelope `schemaVersion` bumps on their own.
- Undocumented `details` fields are not public contract.
- Structures with growing external adoption may be promoted to public contract in a later issue.

#### Internal contract

Internal contract schemas govern persisted state that git-kura reads and writes internally.

Schemas in this class:

- `internal/worktree/schema/metadata.schema.json` — worktree metadata persisted at `<git-common-dir>/kura/meta/worktrees/<key>.json`
- `internal/seal/schema/seal_store.schema.json` — seal store persisted at `<git-common-dir>/kura/seals/paths.json`
- `internal/tools/schema/tools_metadata.schema.json` — tools install metadata
- `internal/tools/schema/pre_commit_meta.schema.json` — pre-commit component metadata

Policy:

- These schemas are not external integration targets.
- External tools must not depend on these schemas.
- The schemas may change without public notice.
- Changes that affect persisted state must document migration, compatibility, and recovery policy. Such changes are versioned by the relevant state schema's own `schemaVersion` field, not by the output envelope `schemaVersion`.
- `docs/schema.md` must explain that these are internal persistence formats and not external dependencies.
- `docs/schema.md` must not include GitHub view links or raw URLs for these schemas.

### Schema placement

Schema files remain co-located with the Go packages that use them for runtime validation (`//go:embed`).

The `internal/` path prefix for Go packages means the package API is project-private; it does not restrict a JSON Schema *file* in that tree from being a stable external contract.
Public contract schemas located under `internal/` are still treated as stable output contracts and must not be broken without the process described above.

The current canonical locations reflect `go:embed` constraints and domain ownership:

- `internal/output/` validates the common envelope; the envelope schema belongs there.
- `internal/cli/` validates command-specific output data; command-specific schemas belong there.
- `internal/worktree/`, `internal/seal/`, `internal/tools/` own their persisted state; their schemas belong there.

Physical relocation of schema files is not in scope for this decision. If relocation becomes desirable, it requires updating all `$id` values, `//go:embed` paths, doc links, and a new ADR.

### Supersession of earlier schema placement ADRs

[20260609T005136Z_json-schema-for-get-output.md](20260609T005136Z_json-schema-for-get-output.md) established `cmd/kura/schema/output.schema.json` as the schema location and treated a single file as the combined output contract. That location no longer exists; the schema has been split into the envelope schema and command-specific data schemas, and all schemas now reside under `internal/`. This ADR supersedes the schema location and single-file decisions in that ADR.

[20260617T070134Z_output-framework-envelope-result-renderer.md](20260617T070134Z_output-framework-envelope-result-renderer.md) placed the envelope schema at `cmd/git-kura/schema/envelope.schema.json`. That placement decision was already superseded by [20260622T150313Z_output-framework-extract-to-internal-package.md](20260622T150313Z_output-framework-extract-to-internal-package.md), which moved the schema to `internal/output/schema/envelope.schema.json`. This ADR additionally records that the new location is canonical and establishes the classification policy that was absent from both prior decisions.

## Alternatives Considered

### Move public contract schemas out of `internal/` to a `schema/` or `docs/` top-level directory

Separating the schema files from their `go:embed` packages would require either symlinks or a duplication mechanism, and would separate the schema from the code that validates against it.
Since `go:embed` requires paths within the package tree, this approach was rejected in the original single-schema ADR and remains rejected here.

### Allow `internal/` schemas to change freely because they are not exported Go APIs

This would mislead external integrators.
The `internal/` path is a Go module boundary, not a JSON Schema stability boundary.
Treating all `internal/` schemas as freely changeable was rejected because the output envelope and command-data schemas are already published output contracts.

### Promote all schemas to a single class

Using a single class for all schemas would either over-constrain internal state schemas or under-specify the output contract stability expected by external callers.
A three-tier classification reflects the actual three audiences: stable external integration, observed-but-non-primary fields, and implementation-internal persistence.

## Consequences

### Positive Consequences

- External integrators and AI agents can identify which schemas are stable contracts and which are not.
- The distinction between Go `internal/` visibility and JSON Schema stability is explicit and documented.
- Limited-contract fields that AI agents need are catalogued in one place (`docs/schema.md`).
- Internal state schemas have a documented change policy that protects existing repository state.

### Negative Consequences

- The classification must be maintained as new schemas are added; a schema without a class assignment is an oversight gap.
- Breaking a public contract schema now requires explicit ADR and version-bump ceremony rather than an informal change.

### Neutral Consequences

- Schema file locations are unchanged.
- `$id` values in all schema files continue to use `https://raw.githubusercontent.com/tooppoo/git-kura/main/...` paths, reflecting the canonical raw URL.
- `docs/schema.md` is introduced as the entry point for all schema-related documentation.
