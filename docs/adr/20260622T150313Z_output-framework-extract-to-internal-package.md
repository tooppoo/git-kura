# Output framework: extract to internal/output package

- Status: Accepted
- Created: 2026-06-22T15:03:13Z

## Context

[20260617T070134Z_output-framework-envelope-result-renderer.md](20260617T070134Z_output-framework-envelope-result-renderer.md) established the output framework — envelope, `Result`, `CommandError`, `Warning`, `Renderer`, and the envelope JSON Schema — inside `package main` in `cmd/git-kura/output.go`.

As the framework grew to serve `--json`, `--toon`, and human rendering across every command, keeping it in `package main` created two problems.
First, it could not be unit-tested and measured in isolation: tests in `package main` blended framework coverage into the CLI package's coverage signal, and the framework could not be imported from a hypothetical future second binary.
Second, it mixed a stable, machine-readable output contract with CLI flag wiring, making it hard to see the boundary between "what the output format guarantees" and "how the CLI binds flags to the output mode."

This ADR records the decision made in issue #82: extract the output framework to `internal/output`.

## Decision

### Package extraction

The output framework must live in `internal/output` (file `internal/output/output.go`), not in `package main`.

All types, functions, and constants that were previously unexported within `package main` and form the output contract must be exported from `internal/output`:

- Types: `Command`, `RenderMode`, `Warning`, `Envelope`, `Result`, `CommandError`, `HumanRenderable`, `Renderer`
- Functions: `SelectRenderer`, `NormalizeWarnings`, `EncodeEnvelope`, `WriteEnvelope`
- Constants: `RenderHuman`, `RenderJSON`, `RenderTOON`, `SchemaVersion`
- Slices: `AllCommands`
- Command identifier constants: `CommandGet`, `CommandOpen`, `CommandClose`, `CommandLs`, `CommandSealClaim`, `CommandSealUnclaim`, `CommandSealTest`, `CommandSealLs`, `CommandSealDoctor`

Command identifier constants are transitional residents of `internal/output`; they may move to a dedicated package (e.g., `internal/cli`) in a later issue once a cleaner home is established.

The helper `writeTOONEnvelope` remains unexported within `internal/output` because it is an implementation detail of the TOON renderer and is not part of the output contract.

### Envelope schema relocation

The envelope JSON Schema must move from `cmd/git-kura/schema/envelope.schema.json` to `internal/output/schema/envelope.schema.json`.

This co-locates the schema with the code that uses it for runtime validation (`//go:embed schema/envelope.schema.json` in `internal/output/output.go`) and avoids a cross-package embed dependency from `internal/output` back into `cmd/git-kura/schema/`.

The schema's `$id` must be updated to reflect the new canonical path:
`https://raw.githubusercontent.com/tooppoo/git-kura/main/internal/output/schema/envelope.schema.json`

Command-specific `data` and `error.details` schemas (e.g., `get_data.schema.json`) remain outside the output framework package because they are consumed by CLI-level code and tests, not by the framework package itself. They were later moved to `internal/cli/schema/` when the CLI shell was extracted from `cmd/git-kura`.

### Coverage requirement

`internal/output` must have its own focused test file (`internal/output/output_test.go`) so that the package's framework behavior is covered without relying on the `cmd/git-kura` test suite. The project's 90% coverage threshold remains enforced on total repository coverage by `make coverage`.

## Alternatives Considered

### Leave framework in package main, add build-tag-gated export

Adding a build tag to expose framework internals for testing was rejected.
It would complicate the build and leave the framework un-importable from any future binary.

### Move framework to a public (non-internal) package

A public package (e.g., `pkg/output`) was considered but rejected.
git-kura is a single-binary CLI tool with no public library consumers.
`internal/output` signals that the API is project-private, which is the correct guarantee.

## Consequences

### Positive Consequences

- The output framework can be tested independently with its own coverage measurement.
- The boundary between "output contract" and "CLI wiring" is visible in the package structure.
- A future second binary in this module could import `internal/output` without touching `cmd/git-kura`.

### Negative Consequences

- All callers in `cmd/git-kura` (main, seal, seal_path) must add an import and prefix each framework symbol with `output.`.
- The envelope schema `$id` changes, which may affect consumers who use the `$id` to fetch or dereference the schema.

### Neutral Consequences

- The envelope schema content and shape are unchanged; only its location and `$id` move.
- The command identifier constants remain in `internal/output` for now rather than an ideal permanent home.
