# Supersede the legacy seal command model

- Status: Accepted
- Created: 2026-06-14T00:23:23Z

## Context

The seal feature was designed across four ADRs that were written while the command model was still changing:

1. [20260611T114623Z_use-centralized-seal-store.md](20260611T114623Z_use-centralized-seal-store.md) — the centralized `paths.json` / `paths.lock` store and writer lock.
2. [20260611T114624Z_limit-seal-targets-to-repository-relative-files.md](20260611T114624Z_limit-seal-targets-to-repository-relative-files.md) — repository-relative path constraints and normalization.
3. [20260613T064651Z_seal-worktree-context-and-worktree-guards.md](20260613T064651Z_seal-worktree-context-and-worktree-guards.md) — worktree-derived current key, `claim` / `unclaim`, worktree guards.
4. [20260612T170922Z_seal-command-current-context-and-scope.md](20260612T170922Z_seal-command-current-context-and-scope.md) — the read-only-vs-mutation / current-dependent-vs-independent scope rules.

Each of these ADRs still carries a correct, durable decision, but each one also describes parts of an earlier command model that the implemented seal contract no longer follows:

- `seal add` / `seal remove` were renamed to `seal claim` / `seal unclaim`, and the old names were removed outright rather than kept as deprecated aliases (in contrast to the migration policy ADR 3 proposed).
- The current seal key is now derived solely from the active git-kura managed worktree. `GIT_KURA_SEAL_KEY` no longer participates in current-key resolution at all (it is unrelated to the seal store lock timeout, which is configured via `git config kura.sealLockTimeoutMs`).
- The validation command described as `seal check` in ADR 3 is implemented as `seal test`.
- `seal enter`, `seal session ls`, and `seal session clean` belonged to the session-local model and were withdrawn when that model was replaced.
  `seal doctor` was specified but never implemented; it was not part of the session model and is deferred, not withdrawn.

So far this drift was recorded only implicitly: ADR 4 carries an inline "Partially superseded" note, ADRs 1 and 2 still read as `Accepted` with no pointer, and ADR 3 reads as fully `Accepted` even though parts of it were replaced. A reader landing on any one of these ADRs cannot reliably tell which clauses are current. The implementation map added in [#37](https://github.com/tooppoo/git-kura/issues/37) restates the current range per item, but a maintainer-facing map is not the right place to *make* the supersession authoritative — that belongs in an ADR.

This ADR records the supersession explicitly so each prior ADR can carry a `Partially superseded by` pointer to a single, precise statement of what is still current and what is replaced.

## Decision

### 1. Current seal command contract

The current, implemented seal contract is:

```sh
git kura seal claim <path...>     # take ownership; requires a current key
git kura seal unclaim <path...>   # release ownership; requires a current key
git kura seal test <path...>      # validate against current key; requires a current key
git kura seal ls [<key>]          # repository-wide inspection; current-independent
```

- The current key is derived **only** from the active git-kura managed worktree. No environment variable participates in current-key resolution.
- `seal claim` / `seal unclaim` / `seal test` are current-dependent and must fail when there is no valid current key.
- `seal ls` is repository-wide and must not use the current key as its default scope; a narrower scope must be requested with an explicit `<key>` argument.

This contract is the union of the still-current decisions of all four prior ADRs. It does not change them; it states which of their clauses remain in force.

### 2. What each prior ADR keeps

The following decisions remain current and authoritative:

- ADR 1: the centralized `<git-common-dir>/kura/seals/paths.json` + `paths.lock` layout, the `O_CREATE|O_EXCL` writer lock, atomic temp-file rename writes, the lock timeout, and the conflict / lock-timeout exit codes.
- ADR 2: all repository-relative path constraints and normalization rules, isolated in `normalizeSealPath`.
- ADR 3: removing `seal enter`; deriving the current key from the active managed worktree; the `claim` / `unclaim` semantics and terminology; path seals as cross-worktree file-conflict detection; and not adding `kura enter` / `kura leave` to the core CLI.
- ADR 4: the classification rule itself — mutation and context-validation commands are current-dependent, inspection commands are current-independent and repository-wide — as applied to `seal test` and `seal ls`.

### 3. What this ADR supersedes

The following clauses are superseded and **must not** be read as current design:

- **Command names `seal add` / `seal remove`** (ADR 1 context and `add/remove` acquire/validate flow; ADR 2 throughout; ADR 4 examples). Superseded by `seal claim` / `seal unclaim`.
- **Retaining `seal add` / `seal remove` as deprecated aliases** (ADR 3 section 3, migration policy, and Compatibility). The old names were removed outright; there are no compatibility aliases.
- **`GIT_KURA_SEAL_KEY` as a current-key mechanism**, including "may remain temporarily as an internal compatibility mechanism" (ADR 3 sections 1–2) and every clause in ADR 4 that conditions behavior on `GIT_KURA_SEAL_KEY` being set, unset, or invalid. The current key is worktree-derived; this variable is not consulted.
- **The command name `seal check`** (ADR 3 section 4). The implemented per-path validation command is `seal test`.
- **`seal enter`, `seal session ls`, and `seal session clean`** (ADR 4 inspection/maintenance sections, belonging to the session-local model in the superseded session ADR). These were withdrawn when the session-local model was replaced by worktree-derived context and worktree guards. (`seal doctor` is a separate matter — it was never tied to the session model and is deferred, not withdrawn; see section 4.)

### 4. What is deferred, not superseded

Some clauses of ADRs 3 and 4 remain the intended future design and are **not** superseded — they are simply not implemented yet:

- `seal test --staged` (the commit-time staged-file safety net). `seal test` currently rejects `--all` / `--unsealed` / `--staged` rather than silently ignoring them, leaving room to add `--staged` later.
- `seal doctor` (the repository-wide seal-store integrity check, ADR 4). It was specified but never implemented. Unlike `seal session ls` / `seal session clean`, it was not part of the session-local model, so it is deferred rather than superseded.

When these are implemented, they should follow ADRs 3 and 4, and the implementation map should be extended accordingly.

> **Worktree guards (`guard acquire` / `guard release` / `guard status`) are no longer deferred future design.** They were implemented and subsequently removed in [20260618T000000Z_remove-worktree-guard.md](20260618T000000Z_remove-worktree-guard.md). The guard section of ADR 3 is superseded by that ADR. A guard-equivalent feature may be reconsidered only when the stale-guard detection and unit-level concept conditions defined in the removal ADR are both satisfied.

### 5. Status updates to prior ADRs

The `Status` of ADRs 1, 2, 3, and 4 is updated to `Partially superseded by` this ADR. Their decisions and rationale are not rewritten; only the status line and a pointer note are added, per the ADR update rules.

## Consequences

### Positive Consequences

- A reader landing on any of the four prior ADRs is pointed to a single, authoritative statement of what is current.
- The current seal contract is stated in one place rather than reconstructed from four documents.
- The distinction between *superseded* (replaced) and *deferred* (still intended, unbuilt) is explicit, so `--staged` is not mistaken for an abandoned idea. Worktree guards were subsequently implemented and removed; see [20260618T000000Z_remove-worktree-guard.md](20260618T000000Z_remove-worktree-guard.md) for the conditions under which a guard-equivalent feature may be reconsidered.

### Negative Consequences

- There is now one more ADR to consult, and the prior ADRs must be read together with this one to know which clauses still apply.

### Neutral Consequences

- This ADR records no new implementation decision; it consolidates and makes explicit a supersession that had already happened in code.
- The maintainer-facing [cmd/git-kura/IMPLEMENTATION_MAP.md](../../cmd/git-kura/IMPLEMENTATION_MAP.md) continues to map each item to its implementation, schema, and tests; this ADR is the authoritative source for the supersession it references.
