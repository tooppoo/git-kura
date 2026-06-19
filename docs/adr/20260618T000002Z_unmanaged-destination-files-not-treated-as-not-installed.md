# Unmanaged destination files are not treated as not-installed

- Status: Accepted
- Created: 2026-06-18T00:00:02Z

## Context

`git kura tools` components install files into user-visible locations such as agent skill directories. A component may have no install metadata while its destination file already exists. This can happen when:

- the user created the file manually before running `tools install`,
- install metadata was deleted or lost, or
- a previous non-managed installation left a file behind.

If this state is treated as plain `not-installed`, install logic may overwrite a user-controlled file. That would violate the tools framework safety policy that unmanaged or user-modified files must not be overwritten.

## Decision

When a component metadata entry is absent, `status` must still inspect the destination file path before returning `not-installed`.

If the metadata entry is absent **and** the destination file exists, the file is treated as **unmanaged**.

In that case, `status` reports an unmanaged destination file state rather than plain `not-installed`. The reason is:

```
unmanaged-file-exists
```

This state has priority over plain `not-installed`. The `status` action is `not-installed` (the component is not under git-kura management) but the reason distinguishes it from true absence.

`install` must not overwrite the destination file in this state, and must fail instead.

Only when both the metadata entry and the destination file are absent may `status` report plain `not-installed`.

This rule applies to all file-managed tools components, not only to Claude / Codex skill files.

## Alternatives Considered

### Treat absent metadata as not-installed unconditionally

Simpler to implement, and avoids the extra filesystem check. This was rejected because it would silently overwrite user-created files, violating the safety contract established by the tools framework.

### Require explicit `--force` to overwrite unmanaged files

This approach protects users but adds complexity to the CLI surface. Because the typical resolution (delete the unmanaged file, then install) is simple enough to communicate in the error message, a `--force` flag was not added.

## Consequences

### Positive Consequences

- User-managed files are protected from accidental overwrite.
- Metadata absence and filesystem absence are not conflated.
- `status` distinguishes unmanaged presence from true non-installation.

### Negative Consequences

- Component implementations must check destination file existence before returning `not-installed`, adding a small amount of boilerplate.

### Neutral Consequences

- `install` with an unmanaged destination file fails with a message containing `unmanaged-file-exists`, which scripts can match.
- This rule is documented and enforced in the `skillComponent` implementation; future file-managed components should follow the same pattern.
