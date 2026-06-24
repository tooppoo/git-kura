# Store Kura State Outside the Repository Root

- Status: Superseded by [20260609T120000Z_store-kura-state-in-git-common-dir.md](20260609T120000Z_store-kura-state-in-git-common-dir.md)
- Created: 2026-06-09T01:27:12Z

## Context

Kura maps a stable key to a deterministic Git worktree.

The worktree itself is intentionally created outside the repository root. This avoids placing generated worktrees under the primary working tree, where commands such as `git clean -fdx` could remove them.

Kura also needs local state that cannot be reconstructed from Git worktree data alone. In particular, `baseBranch` means the branch from which `git kura open <key>` created the worktree. Once the main worktree moves to another branch, that original value cannot be recovered reliably from the worktree path, branch name, or current repository state.

Storing metadata inside the repository root in an ignored `.git-kura/` directory would create an asymmetric failure mode:

```txt
repo/
  .git-kura/        # removed by git clean -fdx
repo.kura/
  worktrees/
    51/             # survives because it is outside repo/
```

In that case the worktree survives, but the metadata needed for accurate structured output is lost.

Using generic sibling names such as `<repo>.worktrees/` also risks colliding with other tools or user-created directories. Kura-owned local state should be namespaced clearly and should be easy to remove as one unit.

## Decision

Kura stores all Kura-managed local state outside the repository root, under a single sibling directory derived from the repository root.

For a repository at:

```txt
<repo-parent>/<repo>
```

Kura uses:

```txt
<repo-parent>/<repo>.kura/
  worktrees/
    <key>/
  meta/
    worktrees/
      <key>.json
```

The worktree path for a key is:

```txt
<repo-parent>/<repo>.kura/worktrees/<key>
```

The metadata path for a key is:

```txt
<repo-parent>/<repo>.kura/meta/worktrees/<key>.json
```

This keeps all Kura-managed state under one directory that can be inspected, moved, backed up, or removed as a unit.

The worktree path remains deterministic from the repository root and key.

The metadata file is created by `git kura open <key>` after the worktree is created. It records state that must remain stable after creation, including the base branch.

Example metadata:

```json
{
  "schemaVersion": 1,
  "key": "51",
  "kind": "worktree",
  "branch": "kura-51",
  "worktreePath": "/home/user/project.kura/worktrees/51",
  "repositoryRoot": "/home/user/project",
  "baseBranch": "main"
}
```

Runtime fields such as whether the worktree currently exists and whether it is dirty are computed when resolving metadata for output.

Structured output must be generated only from resolved, schema-valid metadata. If Kura cannot construct valid metadata for a key, it must not produce JSON or TOON metadata for that key.

## Missing Metadata

Some values can be reconstructed deterministically:

```txt
key
branch
worktreePath
repositoryRoot
```

Other values cannot be reconstructed reliably:

```txt
baseBranch
```

Therefore, if a worktree exists but its Kura metadata file is missing, Kura should not invent `baseBranch` from the current repository branch.

Expected behavior:

- `git kura get <key> --path` may still print the deterministic worktree path.
- `git kura get <key> --branch` may still print the deterministic branch name.
- `git kura get <key> --json` and `git kura get <key> --toon` should fail unless valid metadata can be loaded or explicitly repaired.
- `git kura open <key>` should refuse to silently recreate metadata for an existing worktree with missing metadata.

A future repair command may allow users to reconstruct missing metadata explicitly:

```sh
git kura repair 51 --base-branch main
```

That repair operation must be explicit because it asks the user to provide information Kura cannot infer safely.

## Alternatives Considered

### Store metadata in `<repo>/.git-kura/`

This would make the state easy to discover from the repository root.

This was not selected because ignored files under the repository root can be removed by `git clean -fdx`. That would allow the worktree to survive while the metadata is lost, including non-reconstructable fields such as `baseBranch`.

### Store worktrees and metadata in separate sibling directories

For example:

```txt
<repo-parent>/<repo>.kura.worktrees/<key>
<repo-parent>/<repo>.kura.meta/worktrees/<key>.json
```

This would namespace both directories under Kura and reduce collision risk compared with `<repo>.worktrees/`.

This was not selected because cleanup is less ergonomic. Users would need to know and remove multiple sibling directories to clear Kura state. A single `<repo>.kura/` directory is easier to reason about and maintain.

### Store worktrees in `<repo>.worktrees/` and metadata in `<repo>.kura/`

For example:

```txt
<repo-parent>/<repo>.worktrees/<key>
<repo-parent>/<repo>.kura/worktrees/<key>.json
```

This was not selected because `<repo>.worktrees/` is a generic name that may collide with other tools or local user conventions. It also splits Kura-owned state across multiple sibling directories.

### Store metadata inside each worktree

For example:

```txt
<repo-parent>/<repo>.kura/worktrees/<key>/.git-kura.json
```

This would keep metadata physically close to the worktree.

This was not selected because the file would live inside a Git working tree and could be removed by `git clean -fdx` run inside that worktree. It would also add Kura-specific files to user worktrees.

### Reconstruct all metadata from Git

Kura could derive the branch name, worktree path, and repository root from the key and Git state.

This was not selected because `baseBranch` is historical creation-time data. It is not recoverable once the repository moves on.

### Omit `baseBranch`

Kura could avoid storing local metadata by removing `baseBranch` from structured output.

This was not selected because `baseBranch` is useful context for scripts, reviewers, and AI coding agents. If Kura exposes it, it must be accurate rather than guessed from current state.

## Consequences

### Positive Consequences

- All Kura-managed local state lives under one directory outside the repository root.
- The state directory is namespaced as `<repo>.kura/`, reducing collision risk with other tools.
- Cleanup is ergonomic: removing `<repo>.kura/` removes Kura worktrees and metadata for that repository.
- `git clean -fdx` in the main repository does not remove Kura worktrees or metadata.
- `baseBranch` remains accurate after the main repository changes branches.
- Structured output can fail honestly when required metadata is missing instead of emitting guessed data.

### Negative Consequences

- Kura creates a sibling directory next to the repository.
- Worktree paths are slightly longer than a flat `<repo>.worktrees/<key>` layout.
- Moving or renaming the repository directory can orphan the sibling state directory unless Kura later provides repair or migration commands.

### Neutral Consequences

- `.gitignore` does not need to ignore `.git-kura/` in the repository root because Kura state is not stored there.
- The metadata path is local machine state and should not be committed.
- Scalar outputs remain deterministic even when metadata is missing, but structured outputs require valid stored metadata.
