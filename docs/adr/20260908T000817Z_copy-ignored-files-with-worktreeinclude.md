# Copy ignored files with `.worktreeinclude` when opening a worktree

- Status: Accepted
- Created: 2026-09-08T00:08:17Z

## Context

[Issue #141](https://github.com/tooppoo/git-kura/issues/141) requests copying local ignored files into newly opened worktrees without introducing a custom bootstrap framework. Git worktree creation does not populate these files. The manifest format, source worktree, overwrite policy, and failure behavior establish public worktree lifecycle rules.

`.worktreeinclude` is a convention used by other tools, not a Git standard. [Claude Code documents](https://code.claude.com/docs/en/worktrees#copy-gitignored-files-into-worktrees) `.gitignore` syntax and copying only files that both match the manifest and are ignored by Git. [RepoPrompt's implementation contract](https://github.com/repoprompt/repoprompt-ce/blob/main/docs/worktrees.md) uses the same eligibility rule and excludes symlinks, non-regular files, and existing destinations. Tool-specific extensions and failure reporting differ.

## Decision

`git kura open <key>` must apply the root `.worktreeinclude` from the worktree where the command runs, after creating the new worktree, branch, and metadata. A linked source worktree uses its own local manifest and files. Missing or empty manifests and unmatched patterns are no-ops. The manifest may be tracked or local, but must be a regular file.

Selection must be the intersection of untracked files matching `.worktreeinclude` and untracked files ignored by Git's standard ignore sources in the source worktree. Git itself evaluates both sets with `git ls-files --others --ignored -z`, using the manifest for one set and `--exclude-standard` for the other. This preserves Git's pattern rules, including excluded-parent behavior, without introducing a separate glob implementation or modifying ignore configuration. Paths with spaces, tabs, and newlines remain intact.

Only regular files may be copied. Directory patterns select eligible descendant files and create needed parent directories; empty directories and submodule contents are outside this file-based selection. Copying must skip existing destination files and symlinks, and must not follow source symlinks or destination parent symlinks. Existing real directories may receive missing children. Filesystem operations are confined to each worktree, `.git` components are rejected, and exclusive file creation prevents overwriting a destination created concurrently. New files use source permissions subject to the umask.

Application errors must fail `open` with exit code `1`, using the existing human, JSON, or TOON error output. The error must identify the retained worktree and direct the user to inspect it and complete copying manually. Kura must retain the branch, worktree, metadata, and any copied files, including partially written files if an I/O error occurs. Copying must not delete user files or roll back worktree creation. Writing metadata before copying keeps the worktree resolvable through `get` after a copy failure.

Successful copying must preserve the existing output schema. The reported `dirty` value must reflect Git status after copying because source-only ignore rules may not exist in the destination. Dry runs must remain free of side effects and do not evaluate or preview the manifest.

Bootstrap scripts, lifecycle hook frameworks, symlink directives, and synchronization of existing worktrees are outside this decision.

## Alternatives Considered

### Define a custom bootstrap script or manifest

The issue explicitly excludes this scope. Reusing the existing Git-pattern convention lets repositories share basic manifests with other tools.

### Implement independent pattern matching

This would duplicate Git's treatment of negation, directory patterns, escaping, and ignore precedence. Delegating selection to Git keeps those semantics tied to the repository's installed Git version. Kura does not emulate tool-specific departures from Git's excluded-parent rule.

### Overwrite targets or roll back after an error

Either can discard checkout-time files or work performed after creation. Preserving existing destinations and retaining partial state follows the issue's safety requirements.

## Consequences

### Positive Consequences

- New worktrees can receive local environment and configuration files through a shared convention.
- Git remains authoritative for tracked-file exclusion, ignored-file eligibility, and pattern interpretation.
- Failed copies leave a managed worktree that users can inspect and repair.

### Negative Consequences

- Symlinks, empty directories, and submodule-local files require manual setup.
- Broad patterns can enumerate and copy substantial amounts of ignored data; no hidden file-count or size limit is imposed.
- Copying is not atomic as a whole, and a failure can leave some files copied or partially written.

### Neutral Consequences

- `.worktreeinclude` applies only when creating a new worktree; it is not a synchronization mechanism.
- Existing metadata and structured output schemas remain unchanged.
