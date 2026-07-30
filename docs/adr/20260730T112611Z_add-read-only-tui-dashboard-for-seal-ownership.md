
# Adopt a key-based read-only TUI dashboard for seal ownership

- Status: Accepted
- Created: 2026-07-30T11:26:11Z

## Context

`git kura seal` provides advisory path management that detects conflicts before multiple agents or worktrees start editing the same path, stopping work before a merge conflict can occur.

As the number of concurrent worktrees grows, individual CLI outputs such as `seal ls` make it hard to grasp, repository-wide, which managed worktree key currently claims which paths. With more than a few concurrent agents or worktrees, users need a single view of seal ownership.

Static HTML reports or a Web UI could visualize the same data, but they add generate, open, and regenerate steps that leave the CLI-centered development flow, and they are heavier than the initial requirement demands.

git-kura's purpose is to stop work before conflicts happen, not to resolve conflicts after the fact or to manage worktrees centrally. A management console centered on merge conflict state, worktree detail, or merge and close operations would widen the responsibility too far. `docs/design.md` lists "TUI Git client" as a non-goal; a read-only seal ownership viewer does not provide Git operations and stays inside the seal coordination responsibility, but the boundary is worth recording explicitly.

The primary user question is "which worktree seals what". In git-kura the key is the source of truth for a worktree, so the key, not the path or a directory basename, is the right primary display unit.

This decision affects public CLI behavior and project scope, so it is recorded as an ADR. See issue [#137](https://github.com/tooppoo/git-kura/issues/137).

## Decision

- git-kura must provide `git kura dashboard`, a TUI that runs inside the terminal.
- The dashboard must use the managed worktree key as the primary display identifier and group claimed repository-relative paths under it.
- The display set must be the union of open managed worktree keys and keys holding claims in the seal store.
- Open keys with zero claims must be displayed.
- Keys that hold claims but have no open managed worktree must be displayed and flagged as `orphaned claims`, not hidden.
- Detailed worktree metadata such as worktree path, branch, commit, or dirty state must not appear in the initial version.
- The dashboard must prioritize a single overview of all keys over per-worktree detail screens.
- The dashboard must provide scrolling, per-key expand and collapse, and a filter.
- The filter must be a case-insensitive substring match over keys and claimed paths. A key match shows the whole group with all claimed paths. A path match shows the owner key with only the matched paths and auto-expands that group while the filter is active. Clearing the filter must restore the expand and collapse state from before the filter was applied.
- The initial version must be read-only: no `seal claim`, `seal unclaim`, merge, close, or branch operations from the dashboard.
- Snapshot reads and periodic polling must never acquire the seal store writer lock (`paths.lock`), so the dashboard never delays `seal claim`, `seal unclaim`, or `close`.
- A failed initial load must be shown inside the TUI with a manual retry. A failed reload after a prior success must keep the previous snapshot, mark it stale, and show the last successful load time.
- Seal store read or schema validation failures, invalid stored paths, non-normalized paths, and `duplicate-canonical-path` entries must be distinguished from healthy state and must never render as normal claims.
- When stdin or stdout is not an interactive terminal, the dashboard must fail explicitly without emitting terminal escape sequences.
- The terminal state must be restored on every exit path: normal quit, `Ctrl-C`, and rendering errors.
- The dashboard must target macOS, Linux, and Windows like the rest of git-kura.
- Snapshot collection, aggregation, filtering, and sorting must be separated from TUI rendering so they are testable without a terminal, and polling must be drivable in tests without real-time sleeps.

## Alternatives Considered

### Path-based flat listing

A flat path list suits "who owns this path" lookups, but the main question is "what does each worktree seal", which would require scanning repeated key cells. The reverse lookup is covered by the filter instead.

### Showing all claims of the owner key on a path match

Keeping the whole owner group visible preserves the owner's work scope, but buries the matched path among unrelated claims. The initial version narrows to matched paths; clearing the filter shows the full group.

### Showing only seal claims

Open worktrees with zero claims would disappear, so the view would not be an overview of all concurrent work.

### Showing only open worktrees

Orphaned claims whose worktree is gone would be hidden, exactly the ownership that silently blocks other worktrees.

### Static HTML report or Web dashboard

Leaves the CLI-centered flow and is heavier to implement and operate than the initial requirement justifies.

### Worktree management console

Including merge, close, branch operations, or conflict analysis would widen git-kura's responsibility and the initial scope too far.

### One-shot CLI listing only

Suitable for a single snapshot, but continuous monitoring during concurrent work would require repeated manual re-runs.

## Consequences

### Positive Consequences

- The claim scope of each key and the whole-repository picture stay visible even with many concurrent worktrees.
- Path filtering excludes unrelated claims, so reverse lookup of an owner is direct.
- Orphaned claims and store integrity violations are hard to overlook.
- The dashboard never mutates the seal store, so the existing CLI operation model is unchanged.
- Snapshot logic separated from rendering keeps the feature automatically testable.

### Negative Consequences

- Work cannot be completed from the dashboard; state changes require the existing CLI in each worktree or another terminal.
- Between polls the display can lag behind the actual seal state.
- During a path filter, the owner's non-matching claims are hidden; seeing the full claim scope requires clearing the filter.
- Cross-platform terminal behavior and restore paths need dedicated verification.

## Non-Goals

- Per-worktree detail screens, worktree path, branch, commit, or dirty state display.
- Git diff or commit history display.
- Merge conflict prediction, detection, or visualization.
- State-changing operations from the dashboard.
- Agent execution or progress monitoring.
- Web UI, static HTML reports, graph visualization, or remote repository monitoring.
