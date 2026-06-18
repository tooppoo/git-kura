# Output formats

`git kura get` supports scalar and structured output.

## Scalar output

Returns a single value, suitable for shell substitution.

```sh
git kura get 51 --path    # absolute path to the worktree
git kura get 51 --branch  # branch name
```

## Structured output

### JSON (`--json` / `--format json`)

JSON is the canonical machine-readable format. Use it for scripts, tools, and integrations. `--format json` is an alias of `--json`.

```sh
git kura get 51 --json
git kura get 51 --format json
git kura open 51 --dry-run --json
```

JSON output uses the common output envelope. The worktree metadata is nested under `data`:

```json
{
  "ok": true,
  "command": "get",
  "schemaVersion": 1,
  "data": {
    "schemaVersion": 1,
    "key": "51",
    "kind": "worktree",
    "branch": "issue/51",
    "worktreePath": "/home/user/projects/myrepo-issue-51",
    "repositoryRoot": "/home/user/projects/myrepo",
    "baseBranch": "main",
    "exists": true,
    "dirty": false
  },
  "warnings": []
}
```

A valid `get --json` request that fails at execution time returns an `ok: false` envelope on stdout with the failure under `error`, while preserving the existing exit code.

Scalar output (`--path` / `--branch` / `--root`) is not wrapped in the envelope and must not be combined with `--json` / `--format json`; doing so is a usage error (exit code 2).

`git kura open <key> --dry-run --json` uses the common envelope with the planned worktree under `data`, validated by its own command-specific schema. In dry-run data, `baseBranch` is the current branch and both `exists` and `dirty` are `false`. Conditions that would collide at real creation time (an existing worktree path, branch, or metadata) are reported in `warnings[]` under code `open-dry-run-conflict`, with the colliding items in `details.conflicts`; the dry run still succeeds with exit code 0. On its own (without `--json`), `git kura open <key> --dry-run` prints human-readable output instead of an envelope.

### TOON (`--toon` / `--format toon`)

[TOON](https://github.com/toon-format/toon) is a prompt-friendly, AI-oriented format generated from the same metadata model as JSON.

```sh
git kura get 51 --toon
git kura get 51 --format toon
```

Example output:

```toon
schemaVersion: 1
key: fizz
kind: worktree
branch: fizz
worktreePath: /workspaces/git-kura/.git/kura/worktrees/fizz
repositoryRoot: /workspaces/git-kura
baseBranch: main
exists: true
dirty: false
```

Use TOON when passing workspace context to an LLM prompt or coding agent. JSON remains the compatibility contract for external tools; TOON is not a replacement.

## Metadata schema

The common envelope is defined in [`cmd/git-kura/schema/envelope.schema.json`](../cmd/git-kura/schema/envelope.schema.json). The command-specific `data` payloads are defined in [`cmd/git-kura/schema/get_data.schema.json`](../cmd/git-kura/schema/get_data.schema.json) and [`cmd/git-kura/schema/open_dry_run_data.schema.json`](../cmd/git-kura/schema/open_dry_run_data.schema.json). All schemas are embedded in the binary for runtime output validation.
