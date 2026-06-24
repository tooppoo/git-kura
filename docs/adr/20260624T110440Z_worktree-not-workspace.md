# Use worktree, not workspace, as the canonical concept name

- Status: Accepted
- Created: 2026-06-24T11:04:40Z

## Context

During directory reorganization work (issue #79), `internal/workspace` was temporarily proposed as a new package name.
This raised the question of whether `workspace` should become an official concept name in git-kura alongside or above `worktree`.

git-kura's core primitive is the Git worktree: the combination of a checked-out branch and a working directory managed by `git worktree`.
The existing implementation lives in `internal/worktree`.
Introducing `workspace` as a distinct term would create two names for the same thing, and could mislead contributors or AI agents into inferring a richer abstraction (`workspace = worktree + metadata + seal + tool state`) that does not exist in the project.

## Decision

`workspace` is not an official concept in git-kura.

- User-facing documentation, implementation packages, and code comments must use `worktree` when referring to the Git worktree that git-kura manages.
- The package `internal/workspace` must not be created. The existing `internal/worktree` package is maintained and extended.
- The compound concept `workspace = worktree + metadata + seal + tool state` must not be introduced.
- Occurrences of `workspace` as a synonym for `worktree` in existing docs, comments, or code must be replaced with `worktree`.
- The word `workspace` may appear when it refers to an external concept that is not a git-kura worktree: for example, VS Code `workspaceFolder`, Go workspace (`go.work`), devcontainer or cloud IDE environments, or Claude Code project memory. These uses are not in scope for replacement.

## Alternatives Considered

### Introduce workspace as a higher-level abstraction

`workspace` could have been defined to encompass worktree state, metadata, seal state, and active tool context.

This was not selected because the additional abstraction layer adds naming overhead without providing new capability.
All existing features are already expressed cleanly through the `worktree`, `seal`, and metadata concepts.

### Keep both terms with distinct meanings

`worktree` could refer to the raw Git worktree and `workspace` to the git-kura managed view of it.

This was not selected because the distinction is not meaningful to users.
Introducing `workspace` as a separate term for the managed view adds no expressive power; `worktree` already covers that meaning.
Two terms for the same thing increase cognitive load and create inconsistency in documentation and code search.

## Consequences

### Positive Consequences

- Documentation, code, and agent prompts use a single consistent term.
- The `internal/worktree` package remains the authoritative location for worktree logic.
- AI agents are less likely to infer a non-existent abstraction boundary.

### Negative Consequences

- Existing docs and comments that used `workspace` must be updated. This is a one-time cost covered by issue #86.

### Neutral Consequences

- The word `workspace` may still appear in files when it refers to external concepts (VS Code, Go, devcontainers). This is expected and consistent with this ADR.
