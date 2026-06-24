---
name: git-kura
description: Develop and review the git-kura repository using git-kura itself. Use when Codex works on this repo, implements features or fixes, reviews changes, updates docs, runs tests, or needs to choose the correct Kura-managed worktree from a task key, GitHub issue number, branch, PR, or stable development slug.
---

# git-kura Development

Use this skill when working on the `git-kura` repository itself.

The core rule is simple: make the task key the source of truth. Do not choose
worktree paths by hand when a Kura-managed worktree can be used.

```txt
task key -> branch -> worktree path
```

## Key Selection

Choose a stable, shell-safe key before changing files.

- Use the GitHub issue number for issue work, without `#`.
- Use an explicitly provided key when the user gives one.
- For non-issue work, derive a short slug from the task, such as
  `installer-script` or `json-output`.
- For review work, use the implementation target's key. If only a branch or PR
  is provided, inspect existing Kura worktrees before deciding.

Treat keys as opaque identifiers. Do not assume Kura contacts GitHub, parses
issue trackers, or resolves PR metadata.

## Development Workflow

For implementation, docs, tests, or refactors, always follow these steps in
order. Never start editing before claiming the files you intend to change.

1. Open the Kura worktree at the start of the task:

   ```sh
   git kura open <key>
   ```

   If `git kura open <key>` reports that the branch or worktree already exists,
   do not create an alternate worktree. Reuse the existing one.

2. Move into the worktree:

   ```sh
   cd "$(git kura get <key>)"
   ```

3. Check the worktree state before starting work:

   ```sh
   git status --short
   ```

   Review uncommitted changes and the index. If the worktree has unexpected
   changes from a prior session, clarify with the user before proceeding.
   git-kura does not enforce same-worktree exclusion; use `git status --short`
   to detect shared state manually.

4. List the files you plan to change, then claim all of them:

   ```sh
   git kura seal claim <files...>
   ```

   Claims are repository-root relative and require each path to be an existing
   file (not a directory). For files you will create, create them first (for
   example with `touch`) and then claim them.

5. If the claim conflicts, stop and report. A cross-key conflict makes
   `seal claim` exit with code 6 and print `seal-conflict:` along with the key
   that already claims each path. Report the conflicting files and the owning
   key, then pause the task. Do not unclaim another key's paths or edit around
   the conflict.

6. If there is no conflict, make the actual changes. Edit only claimed paths.
   If the set of files to change grows, claim the new files before editing
   them.

7. After review, create the PR only when you are told to. Do not push or open a
   PR before review or before being asked.

8. When told to merge, release every path you claimed:

   ```sh
   git kura seal unclaim <files...>
   ```

   Use `git kura seal ls <key>` to confirm which paths the key still claims.

9. Check the worktree state before leaving:

   ```sh
   git status --short
   ```

   Confirm there are no unexpected uncommitted changes before tearing down.

10. Tear down the worktree:

    ```sh
    cd "$(git kura get <key> --root)"   # back to the repository root
    git kura close <key>                 # delete worktree and branch (after safety check)
    git pull                             # update main
    ```

Before finishing, report the key, worktree path, branch, changed files, and
checks run.

## Review Workflow

For reviews:

1. Identify the target key. If uncertain, run:

   ```sh
   git kura ls
   ```

2. Resolve and enter the target worktree:

   ```sh
   git kura get <key> --toon
   cd "$(git kura get <key>)"
   ```

3. Confirm the target:

   ```sh
   git status --short
   git branch --show-current
   git log --oneline --decorate -n 10
   ```

4. Review from inside that worktree.
5. In review mode, do not edit files unless the user explicitly asks for fixes.
   If fixes are requested, run `git status --short` and claim the files before
   making changes, following the implementation workflow's seal rules.

Lead review responses with bugs, regressions, missing tests, and safety risks.
If no issues are found, say so and name the checks performed.

## Repository Shape

- CLI entrypoint: `cmd/git-kura/main.go`.
- CLI tests and integration helpers: `cmd/git-kura/*_test.go`.
- Worktree path, metadata, and state helpers: `internal/worktree/`.
- Git command wrappers: `internal/gitutil/`.
- Structured output schemas: common envelope schema in `internal/output/schema/envelope.schema.json` plus command-specific data schemas in `internal/cli/schema/` (for example `get_data.schema.json` and `commands/open.schema.json`).
- User docs: `README.md` and `docs/`.
- ADRs: `docs/adr/`.

Keep Kura small. It manages deterministic mappings between keys, branches,
worktrees, and local metadata. Avoid turning it into an issue tracker client,
AI session manager, PR tool, or general Git UI.

## Design Invariants

- A key maps deterministically to branch name and worktree path.
- Local state lives under Git's common directory at `<git-common-dir>/kura/`.
- Worktrees live under `<git-common-dir>/kura/worktrees/<key>`.
- Metadata lives under `<git-common-dir>/kura/meta/worktrees/<key>.json`.
- Scalar outputs are script-friendly and contain only the requested value.
- JSON is the compatibility contract for structured output.
- TOON is prompt-friendly output generated from the same metadata model.
- Structured output must be schema-valid and must not guess missing
  creation-time metadata such as `baseBranch`.
- Unsafe keys must be rejected without changing Git refs or the filesystem.
- Cleanup must be conservative and must not discard user changes.

When behavior changes, update the docs and tests that describe the same
contract.

## Validation

Use the smallest check that gives confidence while developing:

```sh
go test ./...
```

Before finishing broad or user-facing changes, prefer:

```sh
make check
```

The `Makefile` targets are:

- `make fmt` runs `gofmt -w` on Go files.
- `make fmt-check` verifies formatting.
- `make vet` runs `go vet ./...`.
- `make test` runs `go test ./...`.
- `make coverage` enforces the coverage threshold.
- `make vuln` runs `go tool govulncheck ./...`.
- `make build` builds `./bin/git-kura`.
- `make check` runs formatting, vet, coverage, and vulnerability checks.

If `govulncheck` or another tool is unavailable, report that explicitly and
run the remaining checks.

## Safety Rules

- Do not implement directly in the repository root when a Kura worktree can be
  used.
- Do not guess Kura worktree paths manually.
- Do not use `cd ../...` to find another task worktree.
- Do not edit a file before claiming it with `git kura seal claim`.
- Do not unclaim or override another key's seal to work around a conflict;
  report the conflict and stop instead.
- Do not run `git kura close <key>` unless the user asks for cleanup.
- Do not close a dirty worktree without explicit user confirmation.
- Do not force-delete branches, metadata, or worktree directories.
- Do not alter unrelated user changes in this repository or any Kura worktree.

## Reporting Templates

For implementation:

```md
**Implementation Result**
Key: <key>
Worktree: <path>
Branch: <branch>

Changed: <files>
Verified: <commands>
Notes: <risks or follow-ups>
```

For review:

```md
**Review Result**
Target: <key>, <path>, <branch>

Findings:
Verification:
Notes:
```
