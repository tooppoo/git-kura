# Store Kura State in the Git Common Directory

- Status: Accepted
- Created: 2026-06-09T12:00:00Z

## Context

Kura needs durable local state for worktrees and metadata. That state must survive `git clean -fdx`, because metadata such as `baseBranch` cannot always be reconstructed after a worktree is opened.

The previous design stored state in a sibling directory named `<repo>.kura/`. That kept state outside the working tree, but it assumes the repository parent directory is writable. In devcontainers and other managed environments, the repository itself may be writable while its parent directory is not. In those environments, `git kura open <key>` can fail with permission errors while creating `<repo>.kura/`.

## Decision

Kura stores its local state under Git's common directory:

```txt
<git-common-dir>/kura/
  worktrees/
    <key>/
  meta/
    worktrees/
      <key>.json
```

For a normal repository at `/home/user/project`, this resolves to:

```txt
/home/user/project/.git/kura/
```

Kura resolves the common directory with:

```sh
git rev-parse --git-common-dir
```

This supports linked worktrees because the Git common directory may differ from the current worktree's `.git` path.

## Consequences

- Kura no longer requires write permission to the repository parent directory.
- Kura state survives `git clean -fdx` because it lives under Git's internal directory, not in the working tree.
- Cleanup is still local and explicit: removing `<git-common-dir>/kura/` removes Kura-managed worktrees and metadata for that repository.
- Worktree paths are less visible than sibling directories, but they remain deterministic and are discoverable with `git kura get <key> --path`.
