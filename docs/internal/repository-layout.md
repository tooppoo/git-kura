# Repository layout

This document is the authoritative reference for the responsibility boundary between `tools/` and `scripts/` in this repository.

## Directory responsibilities

### `tools/`

`tools/` contains source files for tool assets distributed by `git kura tools`.

A file belongs in `tools/` only if it is a tool asset that `git kura tools install` will ultimately deliver into a user repository as a repository-local helper component.

`tools/` is not a general-purpose implementation directory. The `git kura tools` command implementation lives in the normal source tree (`internal/`), not in `tools/`.

### `scripts/`

`scripts/` contains scripts for repository maintenance, release operations, CI support, and local maintainer operations.

Scripts in `scripts/` are maintainer-facing. They are not part of the public `git kura` CLI API and are not distributed as tool assets via `git kura tools` unless explicitly redesigned as tool assets.

### `internal/`

`internal/` contains the Go source tree for all `git kura` command implementations, including the `git kura tools` framework (install, uninstall, status, asset download, verification, and caching logic).

## Why `tools/` is limited to tool asset sources

Placing release scripts, maintenance scripts, or distribution helpers in `tools/` would conflate two unrelated concerns: the content being distributed and the machinery that distributes it.

`tools/` represents what ships to users. Everything that supports building, testing, or releasing that content belongs elsewhere so that `tools/` remains a clean, auditable snapshot of what users receive.

## Why distribution logic lives in `internal/`, not `tools/`

`git kura tools install` performs asset download, checksum verification, caching, metadata management, and locking. That logic is part of the `git kura` command and belongs in the Go source tree like any other command implementation.

Placing distribution logic in `tools/` would mean distributing the installer alongside the installed assets, creating a circular layout and obscuring the source-of-truth for each concern.

## Why `scripts/` is separate from `tools/`

`scripts/` serves repository maintainers, not users. Release scripts, CI helpers, and development setup scripts are operational tools for the people who maintain git-kura; they are not git-kura features.

Keeping `scripts/` separate from `tools/` ensures that maintainer-facing scripts are never accidentally treated as distributable tool assets.

## Why release support scripts go in `scripts/release/`

Release operation scripts (such as those planned in #105) are maintainer-facing operations, not tool assets. They belong in `scripts/release/` for the same reason that `scripts/build-tools-archive.sh` is in `scripts/`: they assist with releasing git-kura, but they are not content that `git kura tools install` delivers to users.

Grouping release scripts under `scripts/release/` also makes it easy to apply targeted CI permissions and access controls to release operations without affecting other scripts.

## Decision rule

When deciding where a file belongs, apply this test in order:

1. Will `git kura tools install` deliver this file into a user repository as a helper component? → `tools/`
2. Is this `git kura` command implementation or Go business logic? → `internal/`
3. Is this a release, CI, maintenance, or local developer operation? → `scripts/` (release operations under `scripts/release/`)

If none of the above apply, the file probably belongs in `internal/` or should prompt a design discussion.

## Consistency policy

`tools/README.md`, `scripts/README.md`, and this document describe the same boundary from different vantages. When the boundary changes, all three must be updated together. This document is the canonical reference; the README files are summaries for file contributors.

## Related documents

- Tool asset distribution specification: [tools-asset-distribution.md](tools-asset-distribution.md)
- Tools install/uninstall framework ADR: [../adr/20260617T063947Z_tools-install-framework.md](../adr/20260617T063947Z_tools-install-framework.md)
