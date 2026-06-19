# Claude / Codex skill components install to the representative root

- Status: Accepted
- Created: 2026-06-18T00:00:01Z

## Context

`git kura tools install claude-skill` / `codex-skill` installs a SKILL.md file that teaches an AI agent how to use git-kura safely. The file must be placed where the target agent reads it:

- Claude Code: `<repo>/.claude/skills/git-kura/SKILL.md`
- Codex: `<repo>/.agents/skills/git-kura/SKILL.md`

git-kura supports linked worktrees (created with `git kura open`), where `git rev-parse --show-toplevel` returns the managed worktree path rather than the main repository root. If `tools install` placed skill files relative to the current worktree root, they would land inside the git common-dir subtree (e.g. `.git/kura/worktrees/<key>/`) and disappear when the worktree is closed.

Additionally, `git kura guard` was removed in #68, so the skill content must not reference `guard acquire`, `guard release`, or `guard status`.

## Decision

Claude and Codex skill components share the same implementation pattern (`skillComponent`), differing only in component ID, archive source path, and destination directory.

The installation target is the **representative root** — the stable repository root that git-kura associates with the git common dir. When the current working directory is the main (non-managed) worktree, the representative root is the current repo root. When it is a managed worktree, the representative root is recovered from the kura metadata that recorded the original repository root at `git kura open` time.

Representative root resolution follows this algorithm:

1. Compute `worktreesBase = <commonDir>/kura/worktrees`.
2. If the current repo root is not directly under `worktreesBase`, it is the representative root.
3. If the current repo root is directly under `worktreesBase`, read `<commonDir>/kura/meta/worktrees/<key>.json` and return `repositoryRoot`.
4. Validate the resolved path: it must exist, be a directory, and have the same common dir.

Destination paths:

```
<representative-root>/.claude/skills/git-kura/SKILL.md   (claude-skill)
<representative-root>/.agents/skills/git-kura/SKILL.md   (codex-skill)
```

`codex-skill` uses `.agents/skills` (the existing agent skill convention) rather than `.codex/skills`.

Representative root resolution errors are categorised by reason token so scripts can distinguish them:

| Reason | Condition |
|--------|-----------|
| `not-in-git-repository` | Run outside a git repository (exit code 8) |
| `missing-repository-metadata` | Inside a managed worktree but kura metadata is absent |
| `representative-root-missing` | Resolved path does not exist |
| `representative-root-not-directory` | Resolved path is a file |
| `representative-root-common-dir-mismatch` | Resolved path has a different git common dir |

The first category is a **repository context error** (exit code 8). The remainder are **state errors** (exit code 1).

The skill content must not include `git kura guard acquire`, `guard release`, or `guard status`, because the guard command was removed in #68.

## Alternatives Considered

### Use `filepath.Dir(commonDir)` as the representative root

`filepath.Dir(commonDir)` gives the parent of `.git`, which is the main worktree root for typical repositories. This avoids reading metadata and works even when no kura metadata exists.

It was not selected because:
- It fails for non-standard git directory layouts (e.g. `--separate-git-dir`, submodules) where `filepath.Dir(commonDir)` does not equal the repo root.
- The kura metadata approach is consistent with `git kura get <key> --root`, which also reads `repositoryRoot` from kura metadata and is already validated by existing tests.

### Install to current worktree root unconditionally

Simpler to implement, but skill files placed inside `.git/kura/worktrees/<key>/` are lost when the worktree is closed.

## Consequences

### Positive Consequences

- Skill files are placed in a stable location that survives worktree lifecycle operations.
- Claude and Codex skill installation shares one implementation pattern, reducing duplication.
- The representative root concept and resolution errors are clearly documented and testable.

### Negative Consequences

- `tools install` from a managed worktree requires kura metadata for that worktree to exist and be parseable.

### Neutral Consequences

- Exit code 8 is defined for repository context errors, re-using the number previously reserved for the (now-removed) guard command.
- Codex skill is installed under `.agents/skills`, not `.codex/skills`, to match the existing agent skill directory convention in this repository.
