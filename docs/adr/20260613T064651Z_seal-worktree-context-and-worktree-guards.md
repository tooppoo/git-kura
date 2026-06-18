# ADR: Derive seal context from managed worktrees and separate worktree guards from path seals

Status: Partially superseded by [20260614T002323Z_supersede-legacy-seal-command-model.md](20260614T002323Z_supersede-legacy-seal-command-model.md) and [20260618T000000Z_remove-worktree-guard.md](20260618T000000Z_remove-worktree-guard.md). Created: 2026-06-13T06:46:51Z

> **Partially superseded.** Removing `seal enter`, deriving the current key from the managed worktree, the `claim` / `unclaim` semantics, and path seals as cross-worktree conflict detection are still current. Superseded: retaining `seal add` / `seal remove` as deprecated aliases (they were removed outright), `GIT_KURA_SEAL_KEY` as a migration-time current-key mechanism (it is no longer consulted), and the command name `seal check` (implemented as `seal test`).
> The worktree guard section (section 5 and associated rationale, implementation notes, and documentation impact) is superseded by [20260618T000000Z_remove-worktree-guard.md](20260618T000000Z_remove-worktree-guard.md). `git kura guard` has been removed from git-kura core.
> Still deferred (**not** superseded): `seal test --staged`. See [20260614T002323Z_supersede-legacy-seal-command-model.md](20260614T002323Z_supersede-legacy-seal-command-model.md).

## Context

`git-kura seal` exists to detect conflicts before merge time when multiple agents work on multiple tasks in parallel.

The primary conflict we want to detect is a cross-task file conflict:

* agent A works on task `issue-20` in one managed worktree;
* agent B works on task `issue-31` in another managed worktree;
* both agents intend to modify the same repository-relative path;
* the conflict should be detected before editing or before committing, not only during merge.

The previous seal model used `git kura seal enter <key>` to establish a process-local seal context. That command injected the current key through `GIT_KURA_SEAL_KEY` into a child shell or child process. This worked for interactive shell sessions and long-lived child processes, but it is fragile for agent workflows.

Many agents execute shell/tool calls in fresh processes. In that model, a key injected into one child shell is not available to later tool invocations. As a result, agents would have to remember and repeat the current key from conversation context, which is exactly the kind of implicit state git-kura should avoid.

There is also a separate problem: multiple agents may try to use the same managed worktree at the same time. In that case, they are operating on the same working tree and index. Path seals cannot distinguish those agents if they share the same task key, because they are not cross-task conflicts.

These two problems should be handled separately:

* path seals detect cross-worktree file conflicts;
* worktree guards prevent same-worktree multi-agent use.

## Decision

We will redesign the seal workflow around managed worktree identity.

### 1. Remove `git kura seal enter`

We will remove `git kura seal enter`.

Seal context must no longer be established by entering a process-local shell session or by injecting `GIT_KURA_SEAL_KEY` into descendant processes.

The normal workflow is no longer:

```sh
git kura seal enter issue-20
git kura seal add src/foo.go
```

Instead, an agent or user should move into the managed worktree and run seal commands there:

```sh
cd "$(git kura get issue-20 --path)"
git kura seal claim src/foo.go
```

`GIT_KURA_SEAL_KEY` may remain temporarily as an internal compatibility mechanism during migration, but it must not be documented as the primary workflow.

### 2. Derive the current seal key from the current managed worktree

Seal and guard commands will derive the current task key from the current git-kura managed worktree.

A managed worktree already has a stable identity: it is created for a specific key, has a deterministic path, and has metadata recorded by git-kura. Therefore, the current key should be resolved from the current worktree instead of from process-local environment variables or conversation context.

For example:

```sh
git kura seal claim src/foo.go
```

When executed inside the managed worktree for `issue-20`, this command claims `src/foo.go` for `issue-20`.

Agents should not normally pass the key manually.

If both a worktree-derived key and `GIT_KURA_SEAL_KEY` are present, git-kura must verify that they match. If they do not match, the command must fail.

### 3. Replace `seal add/remove` with `seal claim/unclaim`

We will replace the old `seal add` / `seal remove` terminology with `seal claim` / `seal unclaim`.

The old names may remain temporarily as compatibility aliases, but documentation and agent skills should use the new names.

Migration policy:

* introduce `git kura seal claim <path...>` as the preferred command;
* introduce `git kura seal unclaim <path...>` as the preferred command;
* keep `git kura seal add <path...>` as a deprecated alias for `claim`;
* keep `git kura seal remove <path...>` as a deprecated alias for `unclaim`;
* emit deprecation warnings for `add` / `remove`;
* remove the aliases in a later release after documentation and skills have migrated.

The term `claim` better expresses the intended semantics: the current task claims ownership of repository-relative paths before editing them.

### 4. Use path seals for cross-worktree file conflicts

Path seals are repository-wide claims over repository-relative paths.

They prevent different task keys from modifying the same file in parallel.

Example:

```sh
# In the issue-20 worktree
git kura seal claim src/foo.go

# In the issue-31 worktree
git kura seal claim src/foo.go
# => fail: src/foo.go is already claimed by issue-20
```

Path seals are keyed by task/worktree identity, not by individual agent identity.

The intended commands are:

```sh
git kura seal claim <path...>
git kura seal unclaim <path...>
git kura seal check <path...>
git kura seal check --staged
git kura seal ls [<key>]
```

`seal check --staged` provides a commit-time safety net. Even if an agent forgets to claim a file before editing, staged files can still be checked before commit.

### 5. Add cooperative worktree guards for same-worktree multi-agent exclusion

A worktree guard is a cooperative lease over a managed worktree.

It prevents multiple agents from using the same worktree at the same time.

The intended commands are:

```sh
git kura guard acquire
git kura guard release
git kura guard status
```

The guard key is derived from the current managed worktree. The agent does not pass the task key manually.

Example workflow:

```sh
cd "$(git kura get issue-20 --path)"

git kura guard acquire
git kura seal claim src/foo.go
# edit files
git kura seal check --staged
git kura guard release
```

If another agent has already acquired a guard for the same worktree, `git kura guard acquire` must fail.

This is a cooperative guard, not a mandatory OS-level lock. It protects workflows that follow the git-kura protocol. It does not prevent a process that ignores git-kura from using the same worktree.

### 6. Do not introduce `kura enter` / `kura leave` in the core CLI

Directory navigation should remain the responsibility of the shell.

The README may continue to document scripts based on:

```sh
cd "$(git kura get <key> --path)"
```

Future shell integrations may provide convenience functions, but they should not be part of the core command model for this decision.

## Rationale

### Current worktree identity is more reliable than process-local context

Agents may lose conversation context, may execute shell commands in fresh processes, or may be resumed after context compaction.

If the current key is derived from the current managed worktree, agents can recover their task context from git-kura-managed filesystem state.

This avoids requiring agents to remember and repeat the task key for every seal operation.

### Path conflicts and same-worktree contention are different problems

Path seals solve this problem:

```text
different worktrees / different task keys / same repository-relative path
```

Worktree guards solve this problem:

```text
same worktree / multiple agents / same working tree and index
```

Trying to solve both with a single `seal enter` session concept made the design harder to explain and harder to use.

### Agent launch wrappers are insufficient

A command wrapper such as:

```sh
git kura guard run <key> -- claude
```

can guard a long-lived agent process, but it requires the human or tool launching the agent to remember to use the wrapper.

It also only protects the duration of the wrapped command. If used around short-lived shell commands, it degenerates into command-level exclusion rather than worktree-level exclusion.

The desired model is instead:

```text
the agent enters a worktree,
then the agent itself declares that it is now using that worktree.
```

Therefore, explicit `guard acquire` / `guard release` commands better match the intended workflow.

### Keep v0 simple

A fully robust worktree session system would require agent identities, release tokens, PID tracking, heartbeats, TTL renewal, stale cleanup, and ownership-aware release semantics.

For v0, we intentionally avoid that complexity.

The worktree guard is a cooperative lease:

* one guard record per managed worktree;
* atomic acquire;
* explicit release;
* status inspection;
* stale detection;
* no mandatory heartbeat;
* no per-agent file ownership;
* no OS-level enforcement.

## Consequences

### Positive

* Agents no longer need to remember the current task key from conversation context.
* Seal commands become shorter and less error-prone.
* Cross-worktree file conflicts are detected through path seals.
* Same-worktree multi-agent usage can be rejected through worktree guards.
* `seal` and `guard` have distinct responsibilities.
* Core CLI does not need shell-level `enter` / `leave` behavior.
* Agent skills can describe a simple protocol: acquire guard, claim files, check staged files, release guard.

### Negative

* Worktree guards are cooperative. A process that ignores git-kura can still use the worktree.
* `guard release` may be called incorrectly unless stricter ownership is added later.
* Stale guards can remain after abnormal termination.
* The current worktree-to-key resolution must be implemented carefully and must fail safely when the current directory is not inside a managed worktree.
* Existing `seal enter` and `GIT_KURA_SEAL_KEY`-based workflows need migration.
* Existing `seal add/remove` workflows need migration to `claim/unclaim`.

### Neutral / accepted trade-offs

* v0 does not support multiple independent agents safely editing different files inside the same worktree.
* v0 does not attempt to detect all possible lost-update scenarios inside one worktree.
* v0 treats same-worktree multi-agent coordination as a worktree-level exclusion problem, not a file-level scheduling problem.
* v0 does not attempt to enforce worktree guards at the operating-system level.

## Alternatives considered

### Keep `seal enter <key>`

Rejected.

`seal enter <key>` is not only rejected as the primary model; it will be removed.

It made seal context process-local, which is incompatible with agent workflows where shell invocations may not share environment.

It also conflated two separate concerns:

* identifying the current task key;
* guarding same-worktree concurrent use.

The current task key should be derived from the current managed worktree, and same-worktree concurrent use should be handled by `git kura guard acquire` / `git kura guard release`.

### Use `GIT_KURA_SEAL_KEY=<key>` prefixes

Rejected as a primary workflow.

This requires the agent to remember the key and bypasses the worktree-derived context model.

It may remain temporarily for compatibility or internal migration, but it should not be recommended in normal documentation or agent skills.

### Add `seal run <key> -- <command>`

Rejected as the primary workflow.

It can provide a safer one-shot wrapper than raw environment variables, but it still requires the key to be repeated. It reintroduces the problem of relying on conversation or prompt context for task identity.

### Add `guard run <key> -- <agent-command>`

Rejected as the primary workflow.

It only works if the human or tool launches the agent through git-kura. The desired model is for already-running agents to acquire a guard when they begin using a worktree.

It may be reconsidered later as a convenience command, but not as the core guard mechanism.

### Add `kura enter <key>` and `kura leave`

Rejected for the core CLI.

A normal CLI process cannot change the parent shell's current directory. Directory navigation should be provided by documented shell snippets or optional shell integration.

### Track per-agent file ownership inside the same worktree

Rejected for v0.

This would require agent identity, per-agent claims, release semantics, stale cleanup, and conflict rules inside a single working tree.

That complexity is not justified for v0.

Instead, v0 uses worktree-level exclusion for same-worktree multi-agent coordination.

## Implementation notes

### Current key resolution

Seal and guard commands should resolve the current managed worktree before performing operations.

A command should fail if:

* the current directory is not inside a git-kura managed worktree;
* the managed worktree metadata is missing or inconsistent;
* both a worktree-derived key and `GIT_KURA_SEAL_KEY` are present but differ.

### Path normalization

Path seals should use repository-relative paths.

Commands should normalize input paths before storing or comparing claims.

A command should fail safely when a path cannot be resolved as a repository-relative path inside the current managed worktree.

### Guard storage

A guard record may be stored under the git common directory, for example:

```text
<git-common-dir>/kura/guards/worktrees/<worktree-id>.json
```

The record should include at least:

```json
{
  "key": "issue-20",
  "worktreePath": "/absolute/path/to/worktree",
  "createdAt": "2026-06-13T00:00:00Z",
  "updatedAt": "2026-06-13T00:00:00Z"
}
```

Additional fields such as owner, hostname, process ID, or TTL may be added later, but should not be required for the v0 design.

### Guard acquire

`git kura guard acquire` should:

* resolve the current managed worktree;
* derive the current key from that worktree;
* attempt to create a guard record atomically;
* fail if an active guard already exists for the same worktree;
* fail safely if the existing guard cannot be parsed or inspected.

### Guard release

`git kura guard release` should:

* resolve the current managed worktree;
* remove the guard record for that worktree;
* warn or no-op if no guard exists.

For v0, release does not require a token.

This is an accepted trade-off. Adding release tokens would make ownership stricter, but it would also require agents to persist and remember another piece of state.

### Guard status

`git kura guard status` should show whether the current managed worktree is guarded.

It should include at least:

* key;
* worktree path;
* created time;
* updated time, if present;
* stale status, if stale detection is implemented.

### Stale guard handling

For v0, stale guard handling should be conservative.

If `guard acquire` finds a stale guard, it should fail with a clear message and instruct the user to inspect or release the stale guard explicitly.

Example:

```text
error: this worktree is guarded by a stale record
hint: run `git kura guard status`
hint: run `git kura guard release --stale` if the previous agent is no longer using it
```

Automatic stale cleanup should be avoided unless the stale criteria are very clear.

### Compatibility

`seal add` and `seal remove` may remain temporarily as deprecated aliases.

Suggested mapping:

```text
seal add    -> seal claim
seal remove -> seal unclaim
```

Compatibility aliases should emit warnings so documentation and agent skills can migrate.

`seal enter` should be removed rather than kept as a compatibility alias, because it represents the old process-local context model.

## Documentation impact

Agent skills should instruct agents to:

```sh
git kura guard acquire
git kura seal claim <path...>
git kura seal check --staged
git kura guard release
```

They should not instruct agents to pass task keys manually for normal seal operations.

README should continue to document directory movement using:

```sh
cd "$(git kura get <key> --path)"
```

Optional shell integrations may be provided later, but they are outside this ADR.
