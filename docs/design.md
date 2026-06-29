# Design

Kura is a conflict-aware keyed worktree coordinator for Git.

Its core responsibility is intentionally narrow: given a stable key such as an issue, ticket, or task number, Kura creates, resolves, and removes a deterministic Git worktree, and manages cooperative path seal claims that surface conflicting edits across tasks before implementation, commit, or merge.

## Design principles

### 1. The key is the source of truth

Kura should not require users or agents to remember worktree paths manually. The task key should be enough.

```sh
git kura get 51 --path
```

### 2. Output should be script-friendly

Commands that return values should be usable in shell scripts without extra formatting.

```sh
cd "$(git kura get 51 --path)"
```

For structured output, use JSON:

```sh
git kura get 51 --json
```

For AI prompts, Kura provides [TOON](https://github.com/toon-format/toon):

```sh
git kura get 51 --toon
```

### 3. Early conflict detection

A task claims the repository-relative paths it intends to edit before changing them; conflicting claims from different tasks are detected immediately, not at merge time.

Path seals are cooperative: they protect workflows that follow the git-kura protocol. They do not prevent a process that ignores git-kura from editing claimed paths.

The pre-commit hook (`git kura tools install pre-commit`) provides a final safety net by checking staged files against the seal store at commit time, even if `seal claim` was skipped.

### 4. Kura should stay small

Kura should not become an AI session manager, TUI Git client, pull request orchestrator, or issue tracker client.

Those tools may integrate with Kura, but Kura itself should remain focused on keyed worktree lifecycle management and path seal coordination.

### 5. Safety over convenience

Removing a worktree should be conservative.

Kura should check for conditions such as:

* uncommitted changes
* untracked files
* missing worktree paths
* branch/worktree mismatches
* dirty submodules, if applicable

When in doubt, Kura should stop and explain what must be resolved manually.

## Non-goals

Kura does not aim to:

* manage AI coding sessions
* create or review pull requests
* replace GitHub CLI, GitLab CLI, or other issue tracker tools
* provide a full Git TUI
* infer the correct worktree from natural language
* classify or evaluate task priority

Kura manages the mapping between a key and a Git worktree, and the cooperative path seal claims that make cross-task file conflicts detectable before merge.
It is not a general-purpose conflict resolver: resolving detected conflicts remains the responsibility of the task owner or agent.

See [state-management.md](state-management.md) for how Kura stores local worktrees and metadata.

## Platform support

Kura supports macOS, Linux, and Windows.

Path handling uses platform-aware APIs. Git branch names and filesystem paths are treated as distinct:

```txt
branch: issue/51
path:   <repo>-issue-51  (platform path separator)
```
