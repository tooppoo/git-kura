# Seal commands: context and scope

This document explains the *concepts* behind `git kura seal` — how the commands are classified and why. For the command reference (usage, arguments, examples), see the seal sections in [commands.md](../commands.md).

`git kura seal` uses a *seal key* to represent the working context of a process or agent. The current seal key is derived from the git-kura managed worktree the command runs in: each worktree is created by `git kura open <key>`, so the key of the worktree you are in is the current key. This makes the context survive the fresh shell invocations that agent workflows make, without relying on process-local state. See [`docs/adr/20260613T064651Z_seal-worktree-context-and-worktree-guards.md`](../adr/20260613T064651Z_seal-worktree-context-and-worktree-guards.md).

Seal commands are classified by their effect and by whether their meaning depends on the current seal key. This asymmetry is an intentional design decision: read-only is not the deciding factor — semantic dependence on the working context is. See [`docs/adr/20260612T170922Z_seal-command-current-context-and-scope.md`](../adr/20260612T170922Z_seal-command-current-context-and-scope.md) for the full rationale.

> Of these, `seal claim`, `seal unclaim`, `seal test`, `seal ls`, and `seal doctor` are implemented in the current release. `seal session ls` / `seal session clean` belonged to the session-local model that has since been withdrawn. See [`docs/adr/20260614T002323Z_supersede-legacy-seal-command-model.md`](../adr/20260614T002323Z_supersede-legacy-seal-command-model.md).

## Project scope

The *project scope* is the seal state associated with the Git common dir resolved from the current working directory. It is the state shared by all worktrees of that repository. Current-independent inspection commands operate on this project scope by default.

## Current-dependent commands

These commands are semantically tied to the active work context and require a valid current seal key. If the command is not run inside a git-kura managed worktree, or that worktree's metadata is missing or inconsistent, they fail.

| Command | Effect |
|---------|--------|
| `git kura seal claim <file...>` | claim paths for the current key (mutation) |
| `git kura seal unclaim <file...>` | release the current key's claim (mutation) |
| `git kura seal test <file...>` | context-validation (read-only) |

`seal test` is read-only, but it answers whether the given files may be handled in the *current* working context, so it is grouped with the mutation commands and requires a current key. With a valid current key, unsealed files and files claimed by the current key are allowed, while files claimed by another key are rejected.

## Current-independent inspection commands

These commands inspect the project scope by default and must **not** derive a current key from the worktree. Running them from inside a managed worktree produces the same project-wide result as running them from the main checkout. A narrower key scope must be requested explicitly (for example `git kura seal ls <key>`).

| Command | Notes |
|---------|-------|
| `git kura seal ls` | lists project-wide path seals; ignores the current key |
| `git kura seal doctor` | validates project-wide seal store integrity; ignores the current key |

`seal doctor` is project-wide and read-only: it validates the shared path seal store, must not modify seal state, must not take `paths.lock`, and does not provide `seal doctor --fix`. It reports malformed store structure, invalid schema version, invalid stored paths, and normalized-path duplicates as a store integrity failure rather than as a current-key conflict.

> `seal session ls` and `seal session clean` previously belonged to this classification, operating on repository-level session records. The session-local model they served has been withdrawn (see the [supersession ADR](../adr/20260614T002323Z_supersede-legacy-seal-command-model.md)), so there is currently no maintenance command for same-worktree coordination. Agents should use `git status --short` before starting work to surface unexpected shared state.
