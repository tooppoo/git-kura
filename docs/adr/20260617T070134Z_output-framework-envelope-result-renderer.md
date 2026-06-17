# Output framework: envelope, result, and renderer

- Status: Accepted
- Created: 2026-06-17T07:01:34Z

## Context

git-kura is introducing stable, machine-readable `--json` output across its commands, with JSON as the canonical structured-output format and human output derived from the same structured result (see issue #49).
Rolling that out one command at a time only works if there is a shared foundation first.
Without it, each command would invent its own envelope shape, its own way of branching between human and JSON output, and its own schema, and the formats would drift.

This ADR records the foundation introduced by issue #60: the common envelope, the structured result and error intermediate representations, the renderer abstraction, the `main` / `run` / renderer responsibility boundary, and the placement of the envelope JSON Schema.
It does not migrate any command to the framework; `get` and `open --dry-run` are migrated in a follow-up issue, and command-specific `data` / `error.details` schemas are added per command later.

This decision belongs in an ADR because it defines a machine-readable output contract, a schema, and an architectural boundary that future commands and contributors must follow rather than re-decide.

## Decision

### Common envelope

Structured (`--json`) output must use a single common envelope with these fields: `ok`, `command`, `schemaVersion`, `data`, `warnings`, and `error`.

- `ok` is a boolean indicating success.
- `command` is the canonical command identifier.
- `schemaVersion` uses that exact spelling (not `schema_version`), matching the existing `schemaVersion` field used elsewhere, and is an integer that versions the envelope shape itself.
- `data` is the command-specific success payload and is a JSON object.
- `warnings` is always an array, on both success and failure.
- `error` is the machine-readable error.

`data` and `error` are mutually exclusive and selected by `ok`:

- When `ok` is `true`, `data` must be present and `error` must be absent.
- When `ok` is `false`, `error` must be present and `data` must be absent.

The envelope is validated against `cmd/git-kura/schema/envelope.schema.json`.
The schema enforces the field set, the `ok`-driven exclusivity of `data` and `error`, `warnings` always being an array, `schemaVersion` being the current version, and `command` being one of the canonical identifiers.
Command-specific `data` and `error.details` schemas are layered on top of this envelope in follow-up issues; the envelope schema constrains `data` and `error.details` only as objects.

Success example:

```json
{
  "ok": true,
  "command": "get",
  "schemaVersion": 1,
  "data": { "key": "60" },
  "warnings": []
}
```

Failure example:

```json
{
  "ok": false,
  "command": "seal.claim",
  "schemaVersion": 1,
  "warnings": [],
  "error": { "code": "seal-conflict", "message": "path is already claimed" }
}
```

### Canonical command identifiers

The `command` field must use a canonical, dot-separated identifier so subcommands stay grouped under their command, for example `get`, `open`, `seal.claim`, and `guard.acquire`.
The Go constants in `allCommands` are the source of truth, and the schema's `command` enum must list exactly those values; a test cross-checks the two so they cannot drift.

### Result, error, and warning intermediate representations

A structured-output command execution must not assemble display strings on stdout/stderr directly.
It must instead build a structured `Result` on success or return a `CommandError` on failure, and a renderer turns that intermediate representation into output.

- `Result` carries the `command`, the command-specific `data`, and `warnings`.
- `CommandError` is the intermediate representation of a failure and carries the `command`, the hyphen-case `code`, the human `message`, the process `exitCode`, optional machine-readable `details`, and `warnings`.
- `Warning` carries a hyphen-case `code` and a human `message`.

`error.code` uses the same hyphen-case reason tokens already used by the stderr error output and the exit-code names, so the JSON error code and the existing human error stay aligned.
`CommandError` is what lets the existing stderr error output be preserved in non-JSON mode while the same failure becomes an error envelope in JSON mode.

### Renderer abstraction

Rendering is done through a `Renderer` interface with a human implementation and a JSON implementation.

- A renderer must accept the `io.Writer` for stdout and the `io.Writer` for stderr, so tests can substitute buffers for the process streams.
- A renderer must not depend on command-specific data types.
  It treats `data` and `error.details` as opaque values.
  Human formatting of `data` is delegated to a `HumanRenderable` interface that command-specific data implements, so the renderer never needs to know the concrete type.
- The human renderer writes command-specific success text to stdout and writes warnings and errors to stderr, preserving the existing human behavior.
- The JSON renderer writes the schema-valid common envelope to stdout for both success and failure, and validates the envelope against the schema before emitting it.

### main / run / renderer responsibility boundary

- `main` parses `os.Args`, invokes `run`, and maps the resulting exit code onto the process.
- `run` dispatches to the command, decides the output mode from the parsed flags, and hands the resulting `Result` or `CommandError` to the selected renderer.
- The renderer owns all stdout/stderr writing for structured output.

A failed structured-output command must be turned into output by the renderer, not by `main`'s generic stderr print.
Once a command builds a `CommandError`, the renderer writes the error envelope (JSON mode) or the stderr message (human mode), and `main` must not then print the same error again, or it would append a human stderr message after the JSON error envelope.
The required behavior is therefore: a failure whose output the renderer has already written carries only an exit code to `main`, and `main` exits with that code without writing anything further to stderr for it.
`main`'s existing behavior for unrendered errors (a plain error or an `exitError`) is unchanged: those are still printed to stderr and mapped to their exit code.

This issue establishes the contract and the renderer error path (`Renderer.RenderError` consuming a `CommandError`).
The mechanism that lets `main` exit with the right code without re-printing — for example an exit-code-only sentinel returned after rendering — is introduced together with the first command migrated onto the framework, in the same change that wires `main`.
It is deliberately not shipped before then, so the framework never exposes an error helper that `main` does not yet honor.

### Scalar output stays separate

Scalar output (`get --path`, `get --branch`, `get --root`) is shell-substitution input, for example `cd "$(git kura get <key> --path)"`.
It is intentionally not routed through this framework and keeps printing a single bare value.
Scalar output and `--json` must not be combined; requesting both is a usage error, so a scalar request never produces an envelope.

### Replacing the existing printJSON

The existing `printJSON(worktreeJSON)` path emits a bare command-specific object rather than the common envelope.
The migration approach is to replace it by having `get` (and `open --dry-run`) build a `Result` whose `data` is the worktree object and render it through the JSON renderer, so the output becomes a common envelope.
That migration is intentionally deferred to the follow-up issue, because it changes the observed output of those commands; this issue only establishes the framework they migrate onto.

## Alternatives Considered

### Per-command envelopes and ad hoc JSON

Letting each command define its own top-level JSON shape was rejected.
It would make `ok` / `error` / `warnings` handling inconsistent across commands, prevent a single envelope schema, and make agent and script consumers special-case every command.

### Renderers that switch on concrete command data types

Having the renderer branch on each command's data type (for example a type switch over every command result) was rejected.
It would couple the renderer to every command and force the renderer to change whenever a command is added.
Delegating human formatting to a `HumanRenderable` interface keeps the renderer generic.

### Reusing the existing exitError as the only error type

`exitError` carries an exit code and a wrapped error but no machine-readable code or details, so it cannot by itself populate an error envelope.
Rather than overload it, `CommandError` is introduced as the richer intermediate representation; wiring command execution onto it is part of the per-command migration.

### Embedding warnings in data, or omitting them when empty

Making `warnings` optional or nesting it inside `data` was rejected.
A consumer would then have to handle both a missing field and an empty array.
`warnings` is always present as an array on both success and failure so consumers can read it unconditionally.

## Output Contract

- The envelope shape and its `ok`-driven `data` / `error` exclusivity are a stable contract validated by `cmd/git-kura/schema/envelope.schema.json`.
- `schemaVersion` versions the envelope shape; command-specific payload schemas version independently.
- `error.code` and `warnings[].code` are hyphen-case tokens; `error.code` matches the existing stderr reason tokens and exit-code names.
- Scalar output is not part of this contract and remains a single bare value.

## Consequences

### Positive Consequences

- Every structured command shares one envelope, one schema, and one renderer path, so JSON consumers and agents can rely on a uniform shape.
- The renderer is decoupled from command-specific types, so adding a command does not require changing the renderer.
- Human and JSON output derive from the same structured result, keeping them in sync.
- Tests can substitute buffers for stdout/stderr and assert envelope conformance through a shared schema-validation helper.

### Negative Consequences

- Structured-output commands must build a `Result` or `CommandError` instead of printing directly, which is more ceremony than a bare `fmt.Println`.
- The envelope adds wrapping fields around each command's payload, so JSON output is larger than a bare object.

### Neutral Consequences

- Scalar output and `--toon` are deliberately outside this framework; scalar output stays a bare value and `--toon` is handled separately.
- The framework is introduced unused by production commands in this issue; commands migrate onto it in follow-up issues.
