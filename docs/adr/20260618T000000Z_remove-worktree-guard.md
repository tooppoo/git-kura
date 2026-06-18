# Remove worktree guard from git-kura core

- Status: Accepted
- Created: 2026-06-18T00:00:00Z
- Supersedes: [20260613T064651Z_seal-worktree-context-and-worktree-guards.md](20260613T064651Z_seal-worktree-context-and-worktree-guards.md) (guard section only)

## Context

`git kura guard` was introduced as a cooperative worktree guard: a lease over a single managed worktree that prevents two agents from starting work in the same worktree at the same time, where they would share one working tree and index.

In practice, the v0 guard design has the following unresolved problems:

**Stale guard detection.** The guard record carries no owner identity, release token, heartbeat, PID, or `updatedAt` field. A record that exists is always treated as an active guard. This means a guard left behind by an abnormal agent exit cannot be distinguished from a live guard. Users must discover and manually release stale guards, with no tooling to help them decide when it is safe to do so.

**Intended follow-up work blocked by guard.** Same-worktree sequential work — a human resuming a task, a follow-up agent continuing after the original one completes — is indistinguishable from same-worktree concurrent work. The guard blocks both equally, forcing an explicit `guard release` step even when no contention exists.

**Weak ownership.** `guard release` requires no token. Any agent inside the worktree can release a guard that another agent holds. The exclusion contract is cooperative only, so the v0 guard does not add meaningful safety over simply running `git status --short` before starting work.

**Maintenance cost without benefit.** Making the guard optional or advisory does not solve the stale detection problem; it just shifts the decision burden onto callers while retaining the same implementation and documentation cost.

Because the v0 guard design cannot distinguish intended sequential work from concurrent contention, and cannot distinguish a live guard from a stale record, it is not suitable as a core git-kura feature.

## Decision

Remove `git kura guard` from git-kura core.

The following are removed:

- The `git kura guard` top-level command and all subcommands (`acquire`, `release`, `status`).
- The guard record schema (`guard_record.schema.json`).
- The guard-specific exit code (`exitGuardConflict`, code 8). The numeric value 8 must not be reused for another purpose.
- The guard command constants from the output framework (`guard.acquire`, `guard.release`, `guard.status`).
- All guard-related tests and fixtures.
- Guard documentation in `docs/commands.md`, `README.md`, agent skills, and ADRs.

**Same-worktree write coordination is not removed as a concern.** It is removed as a git-kura core mechanism. After removal:

- Cross-worktree path conflicts continue to be detected by `git kura seal`.
- Same-worktree shared working tree and index state is not enforced by git-kura core.
- Independent write work must use separate keys and separate worktrees.
- Work added to the same worktree is performed with the understanding that the working tree and index are shared.
- Agents should run `git status --short` before starting work and before leaving the worktree to surface unexpected shared state.

## Future Reinstatement Conditions

A guard-equivalent feature may be reconsidered only when both of the following are fully defined:

### 1. Stale guard problem

At a minimum, the following must be defined before reintroducing a guard:

- Which fields the guard record must carry: owner identity, release token, heartbeat, `updatedAt`, `expiresAt`, reason.
- The stale detection criteria.
- Who may release a stale guard, and under what conditions.
- The safety condition for releasing a guard without incorrectly releasing an active one.
- The recovery procedure after abnormal agent exit.
- Whether automatic cleanup is adopted, or explicit release only.

### 2. Unit-level concept

At a minimum, the following must be defined before reintroducing a guard:

- How a work unit smaller than a key is defined.
- Whether the canonical unit is conversation, run, unit, or activity.
- How an agent acquires, holds, and restores its own unit identity.
- Whether a worktree-global implicit current unit is used, or avoided.
- The relationship between a unit and the seal store, the index, and commit boundaries.
- How stale units are handled.

Until both conditions are satisfied, guard-equivalent features remain out of scope for git-kura core.

## Alternatives Considered

### Downgrade guard to optional or advisory

Rejected.

Making the guard optional does not fix the stale detection problem. The implementation and documentation cost remain. An optional guard that callers may choose to ignore or skip provides weaker guarantees than the current design, not stronger ones.

### Add release tokens to the v0 guard

Rejected as a near-term fix.

Release tokens address the ownership problem but not the stale detection problem. A token that no living process holds is still indistinguishable from an active token held by an agent that has not yet run `guard release`. Stale detection remains unsolved until expiry or heartbeat semantics are defined.

### Replace guard with a PID-based liveness check

Rejected.

PIDs are not portable across process restarts, containers, or remote agents. A PID-based check only works when the guard holder and the checker share a process namespace, which cannot be assumed in agent workflows.

## Consequences

### Positive Consequences

- Removes a feature that blocked intended sequential work without providing meaningful contention detection.
- Removes stale record confusion: guards that cannot be cleared except by manual `guard release` no longer exist.
- Reduces the surface of git-kura core.
- Agent skills become simpler: `git status --short` replaces the guard acquire/release steps.

### Negative Consequences

- Same-worktree concurrent use by two uncoordinated agents is no longer detectable by git-kura core. Agents must coordinate at the workflow level or by using separate keys.
- The numeric exit code 8 is retired. Scripts that test for exit code 8 (`guard-active:`) will need to be updated if guard is reintroduced with a new code.

### Neutral Consequences

- Cross-worktree path conflicts continue to be handled by `git kura seal` without change.
- The condition for reintroducing a guard is now documented explicitly, rather than left as an implicit future task.
