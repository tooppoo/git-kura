# Diagnostic commands separate execution status from inspection result in structured output

- Status: Accepted
- Created: 2026-06-19T15:00:00Z

## Context

git-kura outputs JSON via a common envelope provided by the output framework (see [output framework ADR](20260617T070134Z_output-framework-envelope-result-renderer.md)).

For read-only diagnostic commands such as `seal test` and `seal doctor`, two distinct concepts exist:

- Whether the command itself could be executed
- Whether the inspected target satisfied the condition / was healthy

Conflating these makes it difficult for JSON consumers to tell whether `ok: false` means "the command could not be executed" or "the inspection ran but the target failed."

Issue #71 introduced this distinction explicitly into the design when migrating `seal test` and `seal doctor` to the output framework.

## Decision

In the structured output of diagnostic commands, the common envelope's `ok` field represents whether command execution succeeded.

The inspection result is placed in command-specific data.

```
Cannot execute the inspection itself:
  ok: false
  error: {...}

Inspection ran but target failed / is unhealthy:
  ok: true
  data.passed: false   (seal test)
  or
  data.healthy: false  (seal doctor)
```

### Application to `seal test`

`seal test` resolves the current key and determines the seal state of a path.

- Current key resolution failure (not-inside-git-repository / not-in-managed-worktree / metadata-missing / metadata-inconsistent): `ok: false`
- Invalid path (absolute / outside repository / cannot be normalized): `ok: false`
- Path claimed by another key: `ok: true`, `data.passed: false`
- Missing path (a not-yet-created path inside the repository): `ok: true`, reported as a business result in `data.results[]`

`missing-path` is a business result representing a not-yet-created path and is not an input contract violation. It is treated as `safe: true`.

Absolute paths, paths outside the repository, and paths that cannot be normalized are input contract violations that prevent a diagnostic target from being constructed, so they are not business results.

### Application to `seal doctor`

`seal doctor` is treated as a repository-wide inspection. It does not depend on the current key.

- Malformed store (store file unreadable / JSON parse failure / schema validation failure): `ok: false`, `error.code: seal-doctor-error`
- Store is readable but has integrity violations: `ok: true`, `data.healthy: false`, `data.findings[]`

JSON consumers use `ok` to determine whether doctor could run, and `data.healthy` to determine store health.

### Alignment of reason tokens

`error.code` is aligned with stderr reason tokens.

JSON-only error codes are not created as a rule.

`current-key-unresolved` for `seal test` current key resolution failures is treated as a formal reason token.

- Non-JSON stderr also includes `current-key-unresolved`
- JSON `error.code` is also `current-key-unresolved`
- `error.details.reason` holds the detailed classification: `not-inside-git-repository` / `not-in-managed-worktree` / `metadata-missing` / `metadata-inconsistent`

### Preservation of exit codes

Exit codes follow the existing contract even when `--json` is specified.

- A conflict in `seal test` (`data.passed: false`) uses exit code 6 even when the JSON envelope has `ok: true`
- A malformed store in `seal doctor` uses exit code 7 (`ok: false`)
- An integrity violation in `seal doctor` uses exit code 7 (`ok: true` but `data.healthy: false`)

JSON consumers must read command-specific data in addition to `ok`.

## Alternatives Considered

### Expressing conflict / unhealthy as `ok: false`

Expressing a `seal test` conflict or `seal doctor` integrity violation as `ok: false` eliminates the distinction between "the command could not run" and "the target did not satisfy the condition." JSON consumers would have to determine the error type from `error.code`, mixing execution failure and business failure into the same axis.

### Changing exit codes to align JSON and non-JSON

Making `ok: true` + `data.passed: false` + exit 0 would break compatibility with existing CLI scripts. Exit codes are part of the stable output contract, so existing values are preserved.

## Output Contract

- `ok` is a stable contract field representing command execution success or failure.
- `data.passed` / `data.healthy` are command-specific inspection result fields.
- Exit codes follow the existing contract even when `--json` is specified.
- `error.code` is a hyphen-case token aligned with stderr reason tokens.
- `current-key-unresolved` is the formal reason token for `seal test` current key resolution failures.

## Consequences

### Positive Consequences

- The meaning of `ok: false` becomes unambiguous (execution failed, not "inspection failed").
- Diagnostic results become easier to handle in a machine-readable way.
- The responsibilities of command-specific schemas are clearly defined.
- Divergence between human output and JSON output is minimized (`error.code` aligns with stderr reason tokens).

### Negative Consequences

- Even with a non-zero exit code, the JSON envelope may have `ok: true`.
- JSON consumers must read command-specific data in addition to `ok`.
- Validation via command-specific schemas and representative fixtures becomes important.

### Neutral Consequences

- The business result (`passed`/`failed`) and execution status (`ok`) in `seal test` can vary independently.
- Future diagnostic commands such as `tools doctor` or `config doctor` can follow the same principle.
