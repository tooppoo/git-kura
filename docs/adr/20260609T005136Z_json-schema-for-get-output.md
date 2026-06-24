# JSON Schema for `git kura get --json` Output

- Status: Superseded by [20260624T114339Z_json-schema-classification-and-placement.md](20260624T114339Z_json-schema-classification-and-placement.md)
- Created: 2026-06-09T00:51:36Z

## Context

`git kura get --json` is intended as the stable, machine-readable output format for scripts and AI agents.

For that contract to be trustworthy, it must be formally specified and enforced — not just documented in prose. Prose documentation can drift from the implementation without any signal; a machine-readable schema can be checked automatically.

There are two audiences for the output specification:

```txt
machines:  scripts, tools, tests, external integrations
humans:    developers reading docs or building integrations
```

Both need reliable access to the schema.

## Decision

The JSON output of `git kura get --json` is formally defined by a JSON Schema (draft 2020-12) located at:

```txt
cmd/kura/schema/output.schema.json
```

The schema is embedded in the binary at build time using `//go:embed`. The `--json` output path validates its own output against the embedded schema before printing. If the output does not conform, the command exits with an error.

```go
//go:embed schema/output.schema.json
var outputSchemaJSON []byte

var outputSchema = mustCompileOutputSchema()
```

The schema is compiled once at startup from the embedded bytes. It is not fetched at runtime and requires no network access or external file.

The same `outputSchema` variable is used directly in integration tests, so the test and production code share a single schema source.

The `github.com/santhosh-tekuri/jsonschema/v6` library is used for validation. It is compiled into the binary and adds no runtime dependency for users.

Human-readable documentation in `docs/output-format.md` links to the schema file rather than re-specifying the fields in prose.

## Schema Location

The schema is placed in `cmd/kura/schema/` rather than `docs/` or `testdata/` for the following reasons:

- `//go:embed` cannot reference paths outside the package directory tree, so `docs/` is not accessible from `cmd/kura/`
- `testdata/` implies test-only infrastructure, which would misrepresent its role as the authoritative production contract
- `cmd/kura/schema/` signals that the schema is part of the implementation, is embedded in the binary, and governs runtime behavior

## Alternatives Considered

### Prose documentation only

The field types and constraints could be described only in `docs/output-format.md` as a Markdown table.

This was not selected because prose can silently diverge from the implementation. There is no mechanism to detect a mismatch until an external caller breaks.

### Runtime validation in tests only

The schema could be used only in integration tests, not in the binary itself.

This was not selected because test-only validation provides a weaker contract. It detects drift during `go test` but not in production builds or when tests are not run. Runtime self-validation makes the guarantee unconditional.

### Schema in `docs/`

Placing the schema in `docs/output-format.schema.json` would keep it alongside the prose documentation and make it publicly accessible at a stable URL.

This was not selected for the initial implementation because `//go:embed` paths must be within the package tree. Embedding from `docs/` would require either duplicating the file or restructuring the package layout. The current layout keeps the single authoritative file where it can be both embedded and read.

This decision may be revisited if a stable public URL for the schema becomes a requirement.

### Schema in `testdata/`

Go convention places test fixtures in `testdata/`. This would allow `//go:embed testdata/output.schema.json` from the same package.

This was not selected because `testdata/` implies the schema is test infrastructure. The schema governs the runtime contract of the binary, not only test assertions.

## Consequences

### Positive Consequences

- The output contract is machine-readable and versioned alongside the implementation.
- The binary self-validates its output; schema drift is detected at runtime.
- Tests use the same schema as production code. There is no separate "test schema" that can diverge.
- Adding or removing fields without updating the schema is caught immediately.
- External tools can reference the schema file directly for their own validation.

### Negative Consequences

- The binary embeds the schema and its validator library, increasing binary size slightly.
- The `--json` output path now makes additional allocations for schema validation on every invocation.
- Any change to the schema is a breaking change if it removes or renames a required field.

### Neutral Consequences

- `docs/output-format.md` no longer re-specifies the field table. It links to the schema instead.
- `schemaVersion: 1` is pinned as a `const` in the schema. Incompatible changes must increment the version.
