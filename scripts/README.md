# scripts/

This directory contains scripts for repository maintenance, release operations, CI support, and local maintainer operations.

## Subdirectories

| Directory | Purpose |
|---|---|
| `dev/` | Local development environment setup |
| `test/` | Test runner helpers |
| `release/` | Release operation scripts |

## Placement rules

- Release operation scripts go in `scripts/release/`.
- Scripts in `scripts/` are maintainer-facing and are not part of the public `git kura` CLI API.
- Scripts in `scripts/` are not distributed as tool assets via `git kura tools` unless explicitly moved to `tools/` or redesigned as tool assets.

## What does not belong here

Scripts that are tool assets — files intended to be installed into a user repository via `git kura tools install` — belong in `tools/`, not here.

A script that assists with building or distributing tool assets (such as `build-tools-archive.sh`) is a release operation helper, not a tool asset itself, and belongs here in `scripts/`.

## Decision rule

When deciding whether a file belongs in `scripts/` versus `tools/`, ask: is this a tool asset that `git kura tools install` will deliver into a user repository?

- Yes → it belongs in `tools/`.
- No → it belongs here or in `internal/`.

## Further reading

- Responsibility boundary between `tools/` and `scripts/`: [docs/internal/repository-layout.md](../docs/internal/repository-layout.md)
- Maintainer release operation workflow: [docs/internal/release-operations.md](../docs/internal/release-operations.md)
