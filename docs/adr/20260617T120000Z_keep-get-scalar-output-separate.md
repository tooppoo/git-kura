# Keep get scalar output separate and define dry-run output modes

- Status: Accepted
- Created: 2026-06-17T12:00:00Z

## Context

Issue #61 migrates `git kura get` and `git kura open --dry-run` onto the common output framework introduced in [20260617T070134Z_output-framework-envelope-result-renderer.md](20260617T070134Z_output-framework-envelope-result-renderer.md).
The framework defines a common JSON envelope (`ok`, `command`, `schemaVersion`, `data`, `warnings`, `error`) and a renderer boundary, but it does not by itself decide which of a command's output modes are routed through that envelope.

`git kura get` has two different output use cases.
The first is structured metadata for tools, integrations, and AI agents, which should use the common JSON envelope.
The second is scalar output for shell composition, where users rely on commands such as `cd "$(git kura get 51 --path)"` and `cd "$(git kura get 51 --root)"` staying a single bare value.
Wrapping those scalar values in JSON would break existing shell substitution.

`git kura open --dry-run` previously printed the planned worktree as a bare JSON object on stdout.
The migration has to decide whether the dry run is human-readable or JSON by default, how `--dry-run` and `--json` interact, and how a dry run reports a condition that would collide at real creation time.

These are durable public-contract decisions about script-facing and agent-facing output, so they are recorded as an ADR rather than left to issue-local discussion.

## Decision

### Scalar output stays separate from the envelope

The following `get` modes must remain scalar output and must not be wrapped in the common JSON envelope:

- `git kura get <key> --path`
- `git kura get <key> --branch`
- `git kura get <key> --root`

`git kura get <key> --json` and `git kura get <key> --format json` must return the common JSON envelope, with the existing worktree metadata nested under `data`.
`--format json` is an alias of `--json` and must produce identical output.

Scalar output and `--json` / `--format json` are mutually exclusive.
Combining them must be treated as a normal usage error, regardless of flag order:

```sh
git kura get 51 --path --json
git kura get 51 --json --path
git kura get 51 --path --format json
git kura get 51 --format json --path
```

These combinations must not be treated as valid JSON requests.
They are handled as ordinary usage errors without a JSON envelope:

- stdout: empty
- stderr: usage error message
- exit code: `2`

A valid `get --json` / `get --format json` request that fails at execution time is different: it must emit an `ok: false` envelope on stdout while preserving the existing exit code.

### open --dry-run output modes

`--dry-run` controls side effects and `--json` controls output format; the two are independent.

- `open --dry-run` on its own must print human-readable output derived from the same structured result as the JSON form, showing at least the planned worktree path, branch, repository root, and base branch.
- `open --dry-run --json` and `open --json --dry-run` must print the common JSON envelope.
- A dry run must not create the worktree, branch, or metadata.

For now `--json` is only supported together with `--dry-run`, because the real creation path is not yet migrated; `open --json` without `--dry-run` is a usage error.

### Dry-run conflicts are warnings, not failures

A dry run inspects, without side effects, the pre-creation conditions that would collide if `open` actually ran: an existing worktree path, an existing branch, and existing metadata.
Finding such a condition must not fail the command.

- command result: success
- exit code: `0`
- JSON envelope: `ok: true`
- the conditions are reported in `warnings[]` with code `open-dry-run-conflict`
- `warnings[].details.conflicts` is an array that may contain `worktree-path`, `branch`, and `metadata` conflict items
- human-readable output also shows the warning

`ok: true` for a dry run means the dry-run evaluation completed, not that creation is guaranteed to succeed.

## Alternatives Considered

### Wrap scalar values in the JSON envelope

Routing `--path` / `--branch` / `--root` through the envelope would make output uniform, but it would make shell substitution verbose and break existing usage, so it was rejected.

### Treat scalar + `--json` as a JSON-mode error envelope

Emitting an error envelope for `get --path --json` would let the mere presence of `--json` override usage-error handling and blur the line between invalid CLI usage and a valid JSON request that fails at runtime, so it was rejected in favor of a plain usage error.

### Keep `open --dry-run` JSON by default

Keeping the old bare-JSON default would have avoided changing observed output, but it would diverge from the framework's principle that human output is the default and JSON is opt-in via `--json`, and it would leave the dry run emitting a bare object instead of the common envelope.

### Treat dry-run conflicts as failures

Returning a non-zero exit for a detected conflict would conflate "this condition would collide at creation time" with "the dry-run evaluation failed", defeating the purpose of a side-effect-free check, so conflicts are reported as warnings instead.

## Consequences

### Positive Consequences

- Shell substitution use cases stay stable and concise.
- Structured consumers get the common envelope for `get --json` and `open --dry-run --json` without forcing every output mode into JSON.
- A dry run can surface likely collisions without failing, so callers can inspect them before committing to creation.

### Negative Consequences

- `open --dry-run` output changes: the bare JSON object is replaced by human-readable output by default and by the common envelope under `--json`.
- CLI parsing must reject scalar output flags combined with `--json` / `--format json` / `--toon` / `--format toon` regardless of order, and reject `open --json` without `--dry-run`.

### Neutral Consequences

- The `get` and `open --dry-run` data payloads share the same worktree shape but are validated by separate command-specific schemas so they can version independently.
- `--toon` was later migrated into the envelope framework (issue #51): `toonRenderer` implements the same `Renderer` interface and writes the full envelope. The scalar-output constraint above was updated accordingly.
