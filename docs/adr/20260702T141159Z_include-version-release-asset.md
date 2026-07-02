# Include VERSION in Validated Release Assets

- Status: Accepted
- Created: 2026-07-02T14:11:59Z

## Context

[20260702T050859Z_goreleaser-artifacts-before-release.md](20260702T050859Z_goreleaser-artifacts-before-release.md) established that the release workflow runs GoReleaser with `release --clean --skip=publish`, validates local artifacts, and hands the validated file list to `softprops/action-gh-release`.

The GoReleaser configuration also attaches repository-root `VERSION` through `release.extra_files`. The release workflow writes the GitHub ref name into this file before running GoReleaser, but `--skip=publish` means GoReleaser may not include that extra file in `artifacts.json`. Without validator support, `VERSION` can be omitted from the validated upload list even though it is part of the intended release asset set.

This changes the validated release asset contract, so it needs an ADR rather than only an implementation note.

## Decision

This ADR supersedes [20260702T050859Z_goreleaser-artifacts-before-release.md](20260702T050859Z_goreleaser-artifacts-before-release.md).

The GitHub Release asset set must include `VERSION` in addition to platform archives, `checksums.txt`, `checksums.txt.sigstore.json`, archive SBOMs, the tools archive, and the tools sidecar manifest.

The `release-asset` plan payload must include the expected `VERSION` filename. The validator must allowlist `VERSION`, validate that the file exists, and validate that its trimmed content equals the target version, including the leading `v`.

When `VERSION` is absent from GoReleaser's `artifacts.json` because `release.extra_files` are not emitted under `--skip=publish`, the validator may promote repository-root `VERSION` into the upload candidate metadata. The validator may also continue promoting the tools archive and tools sidecar manifest from the configured tools output directory when they are absent from `artifacts.json`.

## Consequences

### Positive Consequences

- The validated upload list matches the release assets configured in GoReleaser.
- Downstream consumers can rely on a release-attached `VERSION` file whose content matches the tag.
- The pre-upload validation gate remains deterministic even when `release.extra_files` are missing from `artifacts.json`.

### Negative Consequences

- Release validation now depends on repository-root `VERSION` being present before validation runs.
- A mismatch between `VERSION` and the target tag fails release validation even if every GoReleaser-generated artifact is otherwise valid.

### Neutral Consequences

- `checksums.txt` still covers only the platform archives.
- `VERSION` is uploaded by `softprops/action-gh-release` through the same validated file list as other release assets.
