#!/bin/sh
# Build the git-kura tools release archive and sidecar manifest.
#
# Usage: scripts/build-tools-archive.sh <version> [output-dir]
#
# Produces:
#   <output-dir>/git-kura-tools_<version>.tar.gz   -- the tools archive
#   <output-dir>/git-kura-tools_<version>.json      -- the sidecar manifest
#
# The archive contains:
#   manifest.json              -- component/file/checksum map
#   claude-skill/SKILL.md      -- installable Claude Code skill
#   codex-skill/SKILL.md       -- installable Codex skill
#
# Checksums are computed with sha256sum (or shasum -a 256 on macOS).

set -e

VERSION=${1:?Usage: $0 <version> [output-dir]}
OUTPUT_DIR=${2:-.}

ARCHIVE_NAME="git-kura-tools_${VERSION}.tar.gz"
SIDECAR_NAME="git-kura-tools_${VERSION}.json"
ARCHIVE_PATH="${OUTPUT_DIR}/${ARCHIVE_NAME}"
SIDECAR_PATH="${OUTPUT_DIR}/${SIDECAR_NAME}"

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_ROOT=$(cd "${SCRIPT_DIR}/.." && pwd)

# Portable sha256: try sha256sum (Linux), fall back to shasum -a 256 (macOS).
sha256_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d' ' -f1
    else
        shasum -a 256 "$1" | cut -d' ' -f1
    fi
}

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# Copy skill sources into the staging tree.
mkdir -p "${TMP}/claude-skill" "${TMP}/codex-skill"
cp "${REPO_ROOT}/tools/skills/claude/SKILL.md" "${TMP}/claude-skill/SKILL.md"
cp "${REPO_ROOT}/tools/skills/codex/SKILL.md"  "${TMP}/codex-skill/SKILL.md"

# Compute per-file checksums (archive-relative path → sha256).
CLAUDE_SUM=$(sha256_file "${TMP}/claude-skill/SKILL.md")
CODEX_SUM=$(sha256_file  "${TMP}/codex-skill/SKILL.md")

# Write the archive-internal manifest.
cat > "${TMP}/manifest.json" <<EOF
{
  "schemaVersion": 1,
  "components": {
    "claude-skill": {
      "files": {
        "claude-skill/SKILL.md": "${CLAUDE_SUM}"
      }
    },
    "codex-skill": {
      "files": {
        "codex-skill/SKILL.md": "${CODEX_SUM}"
      }
    }
  }
}
EOF

# Create the archive with deterministic entry order.
mkdir -p "${OUTPUT_DIR}"
(cd "${TMP}" && tar czf - manifest.json claude-skill/SKILL.md codex-skill/SKILL.md) \
    > "${ARCHIVE_PATH}"

# Compute the archive checksum for the sidecar manifest.
ARCHIVE_SUM=$(sha256_file "${ARCHIVE_PATH}")

cat > "${SIDECAR_PATH}" <<EOF
{
  "archiveName": "${ARCHIVE_NAME}",
  "archiveChecksum": "${ARCHIVE_SUM}",
  "checksumAlgorithm": "sha256",
  "version": "${VERSION}"
}
EOF

echo "Created ${ARCHIVE_PATH}"
echo "Created ${SIDECAR_PATH}"
