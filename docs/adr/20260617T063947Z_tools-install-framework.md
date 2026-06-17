# Tools install/uninstall framework

- Status: Accepted
- Created: 2026-06-17T06:39:47Z

## Context

git-kura ships auxiliary helpers — a pre-commit hook, Claude and Codex skills — that users should be able to install into a repository, remove, and inspect. Rather than build each helper as a bespoke command, we want one framework that every component plugs into, so install/uninstall/status behavior, metadata, locking, output, and asset verification are defined once.

This decision needs an ADR because it establishes durable contracts: a new public command surface (`git kura tools`), a persistent metadata schema, a release-asset distribution and verification model, and destructive-operation safety rules. Each component's concrete install logic is delivered separately (pre-commit in #55, the skills in #56), so the framework must stand on its own and be testable without those components.

Constraints that shaped the design:

* Tool assets must not be embedded in the binary in v0; they ship as separate GitHub Release artifacts.
* An install must verify what it downloads before extracting it, and must bind to the binary's own release version rather than `latest`.
* `status` must work offline and never depend on network availability.
* Uninstall must never destroy user-modified files or externally changed config.

## Decision

Add `git kura tools status [component...]`, `git kura tools install <component...>`, and `git kura tools uninstall <component...>`, with `--all` for install/uninstall. There is no `tools list`: `status` with no component lists every registered component. `install`/`uninstall` require at least one component or `--all`, `--all` may not be combined with explicit names, and `status` does not accept `--all`. Unknown components and these argument violations are usage errors (exit code 2).

Components are registered in a registry that maps an ID to an implementation exposing `install`, `uninstall`, and `status`. The production registry recognizes `pre-commit`, `claude-skill`, and `codex-skill`. Framework tests inject a test-only registry of fixture components, so the framework is exercised end to end without the production components and without any temporary dummy component in the production registry.

Every operation returns a shared result model: component, release version, source asset, destination, action, managed flag, and reason. The action enum is shared but limited per command — `install` emits `created`/`updated`/`skipped`/`failed`, `uninstall` emits `removed`/`not-installed`/`skipped`/`failed`, and `status` emits `installed`/`not-installed`/`skipped`/`failed`. When multiple components are requested, every component is processed even if one fails; the command exits non-zero when at least one component failed, while `skipped` and `not-installed` are not failures.

Tool assets are distributed as a per-release archive `git-kura-tools_<version>.tar.gz` with a sidecar manifest `git-kura-tools_<version>.json`. `install` resolves the binary's official release version, fetches the sidecar manifest and archive for the same release tag (never `latest`), verifies the archive checksum against the sidecar manifest before extracting, and caches the verified, extracted asset under `<git-common-dir>/kura/tools/cache/<version>/`. A cache is reused only when its recorded release version, archive name, archive checksum, and checksum algorithm all match the sidecar manifest. Only `sha256` is supported in v0; any other algorithm, or a checksum mismatch, fails the install. `go install` and source builds have no injected release version and therefore cannot download assets. Signature verification is out of scope for v0.

Install metadata lives at `<git-common-dir>/kura/tools/installed.json` with a lock at `installed.lock`. It records, per component, a schema version, source asset id, release version, release asset name, destination path, installed version, checksum, managed mode (`file` or `config`), component-specific metadata, and timestamps. `install`/`uninstall` hold the lock across the whole read-modify-write and write atomically; a lock that cannot be acquired, or metadata that cannot be read/parsed/validated, aborts the destructive operation without changing anything. Uninstall removes a managed file only when its current checksum matches the recorded one, reverts a managed config value only when it still equals the value git-kura set, and otherwise leaves it in place with a `skipped` reason.

## Alternatives Considered

### Embed tool assets in the binary

Embedding the assets would remove the download, checksum, and cache machinery and let install work offline. It was not selected because it couples asset content to the binary release, bloats the binary, and prevents updating assets without shipping a new binary; the sidecar-verified release artifact keeps assets versioned with — but separate from — the binary.

### Separate `list` command and per-command action enums

A dedicated `tools list` plus action enums tailored to each command was considered. It was rejected to keep the surface small: folding the component listing into `status` avoids a near-duplicate command, and a single shared action enum (constrained per command) keeps the result model and future machine-readable output uniform.

## Consequences

### Positive Consequences

- One framework defines install/uninstall/status, metadata, locking, output, and asset verification, so each component only implements its own file/config logic.
- Assets are verified before extraction and bound to the binary's exact release, closing the obvious supply and version-skew risks for v0.
- Uninstall is safe by construction: user-modified files and externally changed config are never destroyed.

### Negative Consequences

- `install` requires network access and an official release binary, so it cannot run from `go install` or source builds.
- Reusing exit code 5 for the tools metadata lock timeout overloads the original "seal lock timeout" meaning; the stderr reason token (`tools-metadata-lock-timeout:`) disambiguates it.

### Neutral Consequences

- `--json` / `--toon` output for tools commands is deferred to a later change.
- The production components are placeholders until #55 and #56 land; their IDs resolve, but `install` reports that installation is not yet implemented.
