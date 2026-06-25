# tools/

This directory contains the source files for tool assets distributed by `git kura tools`.

## What belongs here

Only source files for tool assets that are ultimately installed as repository-local helper components via `git kura tools install` belong in this directory.

Current tool asset sources:

- `skills/claude/` — Claude Code skill installed by `git kura tools install claude-skill`
- `skills/codex/` — Codex skill installed by `git kura tools install codex-skill`

## What does not belong here

**`tools/` is not a general-purpose script or implementation directory.** The following must not be placed here:

- `git kura tools` command implementation — that lives in the normal source tree under `internal/`
- Distribution, verification, install, uninstall, or status logic — those are command implementations, not tool assets
- Repository maintenance scripts — use `scripts/`
- Release operation scripts — use `scripts/release/`
- CI support scripts — use `scripts/`
- Local maintainer operation scripts — use `scripts/`

## Decision rule

When deciding whether a file belongs in `tools/`, ask: will this file be distributed and installed into a user repository as a tool asset by `git kura tools install`?

- Yes → it may belong here.
- No → it belongs in `internal/`, `scripts/`, or elsewhere.

## Further reading

- Responsibility boundary between `tools/` and `scripts/`: [docs/internal/repository-layout.md](../docs/internal/repository-layout.md)
- Tool asset distribution specification: [docs/internal/tools-asset-distribution.md](../docs/internal/tools-asset-distribution.md)
