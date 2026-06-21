#!/bin/sh
# Installer for git-kura
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/tooppoo/git-kura/main/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/tooppoo/git-kura/main/install.sh | sh -s -- --version v0.0.2
#   curl -fsSL https://raw.githubusercontent.com/tooppoo/git-kura/main/install.sh | sh -s -- --install-dir "$HOME/bin"
#   curl -fsSL https://raw.githubusercontent.com/tooppoo/git-kura/main/install.sh | sh -s -- --require-signature
set -eu

REPO="tooppoo/git-kura"
BINARY="git-kura"
DEFAULT_INSTALL_DIR="${HOME}/.local/bin"

# ── argument parsing ──────────────────────────────────────────────────────────

VERSION=""
INSTALL_DIR="${DEFAULT_INSTALL_DIR}"
REQUIRE_SIGNATURE=0

while [ $# -gt 0 ]; do
    case "$1" in
        --version)
            [ $# -ge 2 ] || { printf 'Error: --version requires a value\n' >&2; exit 1; }
            VERSION="$2"
            shift 2
            ;;
        --install-dir)
            [ $# -ge 2 ] || { printf 'Error: --install-dir requires a value\n' >&2; exit 1; }
            INSTALL_DIR="$2"
            shift 2
            ;;
        --require-signature)
            REQUIRE_SIGNATURE=1
            shift
            ;;
        *)
            printf 'Unknown argument: %s\n' "$1" >&2
            exit 1
            ;;
    esac
done

# ── platform detection ────────────────────────────────────────────────────────

raw_os="$(uname -s)"
case "$raw_os" in
    Linux*)  OS="Linux" ;;
    Darwin*) OS="Darwin" ;;
    *)
        printf 'Unsupported OS: %s\n' "$raw_os" >&2
        exit 1
        ;;
esac

raw_arch="$(uname -m)"
case "$raw_arch" in
    x86_64|amd64)  ARCH="x86_64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)
        printf 'Unsupported architecture: %s\n' "$raw_arch" >&2
        exit 1
        ;;
esac

# ── version resolution ────────────────────────────────────────────────────────

if [ -z "$VERSION" ]; then
    printf 'Resolving latest release...\n'
    release_api="https://api.github.com/repos/${REPO}/releases/latest"
    VERSION="$(curl -fsSL "$release_api" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')"
    if [ -z "$VERSION" ]; then
        printf 'Failed to resolve latest release version from GitHub API. Specify --version vX.Y.Z to avoid depending on latest release resolution.\n' >&2
        exit 1
    fi
fi

printf 'Installing %s %s (%s/%s)...\n' "$BINARY" "$VERSION" "$OS" "$ARCH"

# ── download ──────────────────────────────────────────────────────────────────

ARCHIVE="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

printf 'Downloading %s...\n' "$ARCHIVE"
curl -fsSL -o "${TMP_DIR}/${ARCHIVE}" "${BASE_URL}/${ARCHIVE}"

printf 'Downloading checksums.txt...\n'
curl -fsSL -o "${TMP_DIR}/checksums.txt" "${BASE_URL}/checksums.txt"

# ── checksum verification ─────────────────────────────────────────────────────

printf 'Verifying checksum...\n'

checksum_line="$(grep " ${ARCHIVE}$" "${TMP_DIR}/checksums.txt" || true)"
if [ -z "$checksum_line" ]; then
    printf 'Checksum for %s not found in checksums.txt\n' "$ARCHIVE" >&2
    exit 1
fi
expected_hash="$(printf '%s' "$checksum_line" | awk '{print $1}')"

if command -v sha256sum > /dev/null 2>&1; then
    actual_hash="$(sha256sum "${TMP_DIR}/${ARCHIVE}" | awk '{print $1}')"
elif command -v shasum > /dev/null 2>&1; then
    actual_hash="$(shasum -a 256 "${TMP_DIR}/${ARCHIVE}" | awk '{print $1}')"
else
    printf 'Neither sha256sum nor shasum is available; cannot verify checksum\n' >&2
    exit 1
fi

if [ "$actual_hash" != "$expected_hash" ]; then
    printf 'Checksum mismatch for %s\n' "$ARCHIVE" >&2
    printf '  expected: %s\n' "$expected_hash" >&2
    printf '  actual:   %s\n' "$actual_hash" >&2
    exit 1
fi

printf 'Checksum verified.\n'

# ── optional cosign verification ─────────────────────────────────────────────

if command -v cosign > /dev/null 2>&1; then
    printf 'cosign found; downloading signature bundle...\n'
    sig_bundle="${TMP_DIR}/checksums.txt.sigstore.json"
    if curl -fsSL -o "$sig_bundle" "${BASE_URL}/checksums.txt.sigstore.json" 2>/dev/null; then
        printf 'Verifying signature with cosign...\n'
        cosign verify-blob \
            --bundle "$sig_bundle" \
            --certificate-identity-regexp "https://github.com/${REPO}/.*" \
            --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
            "${TMP_DIR}/checksums.txt"
        printf 'Signature verified.\n'
    else
        if [ "$REQUIRE_SIGNATURE" -eq 1 ]; then
            printf 'Signature bundle not available; --require-signature is set\n' >&2
            exit 1
        fi
        printf 'Signature bundle not available; skipping cosign verification.\n'
    fi
elif [ "$REQUIRE_SIGNATURE" -eq 1 ]; then
    printf 'cosign is not installed; --require-signature is set\n' >&2
    exit 1
fi

# ── extract and install ───────────────────────────────────────────────────────

printf 'Extracting %s...\n' "$BINARY"
tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "${TMP_DIR}" "$BINARY"

mkdir -p "$INSTALL_DIR"
cp "${TMP_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
chmod +x "${INSTALL_DIR}/${BINARY}"

printf '%s installed to %s/%s\n' "$BINARY" "$INSTALL_DIR" "$BINARY"

# ── PATH check ────────────────────────────────────────────────────────────────

case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) ;;
    *)
        printf '\nNOTE: %s is not on your PATH.\n' "$INSTALL_DIR"
        printf 'Add the following to your shell profile:\n'
        printf '  export PATH="%s:$PATH"\n' "$INSTALL_DIR"
        ;;
esac

# ── verify installation ───────────────────────────────────────────────────────

if "${INSTALL_DIR}/${BINARY}" -h > /dev/null 2>&1; then
    printf '\n%s %s installed successfully.\n' "$BINARY" "$VERSION"
else
    printf '\nWARNING: Verification failed. Run '"'"'%s/%s -h'"'"' to check.\n' \
        "$INSTALL_DIR" "$BINARY"
fi
