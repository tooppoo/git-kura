# Tools asset distribution

This document is the authoritative reference for the current operational specification of how `git kura tools` distributes and installs tool assets.

The design rationale and background decisions are recorded in [../adr/20260617T063947Z_tools-install-framework.md](../adr/20260617T063947Z_tools-install-framework.md). This document describes what the system does; the ADR explains why it was designed that way.

## Tool components and archive-backed assets

`git kura tools` manages three registered components:

| Component ID | Description | Source |
|---|---|---|
| `pre-commit` | Git pre-commit hook | Generated from Go source; not in release archive |
| `claude-skill` | Claude Code skill | Distributed via release archive |
| `codex-skill` | Codex skill | Distributed via release archive |

`pre-commit` is installed from a wrapper script embedded in the git-kura binary itself, not from a file in `tools/` or from the release archive.

`claude-skill` and `codex-skill` are the only components backed by the release archive described in this document.

## Release artifacts

Each git-kura release publishes two tool asset artifacts to the same GitHub Release tag as the binary:

| Artifact | Naming convention |
|---|---|
| Tools archive | `git-kura-tools_<version>.tar.gz` |
| Sidecar manifest | `git-kura-tools_<version>.json` |

The tools archive and its sidecar manifest always belong to the same release tag.

### Archive contents

```
manifest.json              -- component/file/checksum map (schemaVersion 1)
claude-skill/SKILL.md      -- installable Claude Code skill
codex-skill/SKILL.md       -- installable Codex skill
```

The archive-internal `manifest.json` maps each component to its member files and their SHA-256 checksums.

### Sidecar manifest fields

```json
{
  "archiveName": "git-kura-tools_<version>.tar.gz",
  "archiveChecksum": "<sha256-hex>",
  "checksumAlgorithm": "sha256",
  "version": "<version>"
}
```

## Version resolution

`git kura tools install` always resolves the binary's own release version and fetches the tools archive for that exact release tag.

`latest` release is never used. This ensures the installed tool assets correspond to the running binary version and prevents version skew between the binary and its assets.

## Source builds and `go install`

Binaries built from source or installed via `go install` have no injected release version. These builds cannot resolve a release tag and therefore cannot download tool assets. Attempting `git kura tools install` from such a build fails with an appropriate error.

## Checksum verification

Before extracting the downloaded archive, `git kura tools install` verifies the archive checksum against the value in the sidecar manifest.

- Only `sha256` is supported in v0.
- A checksum mismatch or an unsupported algorithm fails the install immediately; nothing is extracted or written.

Signature verification is out of scope for v0.

## Cache

Verified, extracted assets are cached under:

```
<git-common-dir>/kura/tools/cache/<version>/cache.json   -- cache metadata (version, archive name, checksum, algorithm)
<git-common-dir>/kura/tools/cache/<version>/asset/        -- extracted archive contents
```

A cached entry is reused only when all four of the following match the sidecar manifest:

1. Recorded release version
2. Recorded archive name
3. Recorded archive checksum
4. Recorded checksum algorithm

If the cache entry is absent, or if release version, archive name, or checksum algorithm does not match, the archive is re-downloaded and re-verified before use. An archive checksum mismatch is handled differently — see below.

## Cache and sidecar manifest inconsistency

Not all metadata mismatches are treated the same way.

**Archive checksum mismatch**: If the cache records a different archive checksum than the sidecar manifest for the same version, the install fails. git-kura does not use the cache, does not silently replace it, and does not re-download. This is treated as a release asset inconsistency: a release asset is immutable, so a checksum disagreement signals something unexpected has changed.

**Other metadata mismatch** (release version, archive name, or checksum algorithm): The cache entry is treated as a miss. The install proceeds with a fresh download and verification, and the cache is replaced on success.

## Related documents

- Design rationale and background: [../adr/20260617T063947Z_tools-install-framework.md](../adr/20260617T063947Z_tools-install-framework.md)
- Repository layout and directory responsibilities: [repository-layout.md](repository-layout.md)
