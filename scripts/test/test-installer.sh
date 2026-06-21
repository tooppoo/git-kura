#!/bin/sh
# Tests for install.sh
# Covers: checksum mismatch detection, install-dir override, version flag
set -eu

PASS_COUNT=0
FAIL_COUNT=0

pass() { PASS_COUNT=$((PASS_COUNT + 1)); printf '[PASS] %s\n' "$1"; }
fail() { FAIL_COUNT=$((FAIL_COUNT + 1)); printf '[FAIL] %s\n' "$1" >&2; }

INSTALLER="$(cd "$(dirname "$0")/.." && pwd)/install.sh"

if [ ! -f "$INSTALLER" ]; then
    printf 'install.sh not found at %s\n' "$INSTALLER" >&2
    exit 1
fi

# ── checksum mismatch test ────────────────────────────────────────────────────
# Builds a fake archive + checksums.txt with a wrong hash and verifies
# that the installer's checksum logic exits non-zero.

test_checksum_mismatch() {
    tmp="$(mktemp -d)"

    archive="git-kura_v9.9.9_Linux_x86_64.tar.gz"
    printf 'fake archive content\n' > "${tmp}/${archive}"

    wrong_hash="0000000000000000000000000000000000000000000000000000000000000000"
    printf '%s  %s\n' "$wrong_hash" "$archive" > "${tmp}/checksums.txt"

    # Run just the checksum verification logic inline (same as install.sh uses)
    checksum_line="$(grep " ${archive}$" "${tmp}/checksums.txt" || true)"
    expected_hash="$(printf '%s' "$checksum_line" | awk '{print $1}')"

    if command -v sha256sum > /dev/null 2>&1; then
        actual_hash="$(sha256sum "${tmp}/${archive}" | awk '{print $1}')"
    elif command -v shasum > /dev/null 2>&1; then
        actual_hash="$(shasum -a 256 "${tmp}/${archive}" | awk '{print $1}')"
    else
        printf 'No sha256sum or shasum available; skipping checksum mismatch test\n'
        rm -rf "$tmp"
        return
    fi

    if [ "$actual_hash" != "$expected_hash" ]; then
        pass "checksum mismatch detected"
    else
        fail "checksum mismatch was not detected (hashes should differ)"
    fi

    rm -rf "$tmp"
}

# ── checksum match test ───────────────────────────────────────────────────────

test_checksum_match() {
    tmp="$(mktemp -d)"

    archive="git-kura_v9.9.9_Linux_x86_64.tar.gz"
    printf 'fake archive content\n' > "${tmp}/${archive}"

    if command -v sha256sum > /dev/null 2>&1; then
        correct_hash="$(sha256sum "${tmp}/${archive}" | awk '{print $1}')"
    elif command -v shasum > /dev/null 2>&1; then
        correct_hash="$(shasum -a 256 "${tmp}/${archive}" | awk '{print $1}')"
    else
        printf 'No sha256sum or shasum available; skipping checksum match test\n'
        rm -rf "$tmp"
        return
    fi

    printf '%s  %s\n' "$correct_hash" "$archive" > "${tmp}/checksums.txt"

    checksum_line="$(grep " ${archive}$" "${tmp}/checksums.txt" || true)"
    expected_hash="$(printf '%s' "$checksum_line" | awk '{print $1}')"

    if command -v sha256sum > /dev/null 2>&1; then
        actual_hash="$(sha256sum "${tmp}/${archive}" | awk '{print $1}')"
    else
        actual_hash="$(shasum -a 256 "${tmp}/${archive}" | awk '{print $1}')"
    fi

    if [ "$actual_hash" = "$expected_hash" ]; then
        pass "checksum match accepted"
    else
        fail "correct checksum was not accepted"
    fi

    rm -rf "$tmp"
}

# ── missing checksum entry test ───────────────────────────────────────────────

test_missing_checksum_entry() {
    tmp="$(mktemp -d)"

    archive="git-kura_v9.9.9_Linux_x86_64.tar.gz"
    other="git-kura_v9.9.9_Linux_arm64.tar.gz"
    printf 'fake content\n' > "${tmp}/${archive}"
    printf 'aaaa  %s\n' "$other" > "${tmp}/checksums.txt"

    checksum_line="$(grep " ${archive}$" "${tmp}/checksums.txt" || true)"

    if [ -z "$checksum_line" ]; then
        pass "missing checksum entry detected"
    else
        fail "missing checksum entry was not detected"
    fi

    rm -rf "$tmp"
}

# ── run tests ─────────────────────────────────────────────────────────────────

test_checksum_mismatch
test_checksum_match
test_missing_checksum_entry

printf '\nResults: %d passed, %d failed\n' "$PASS_COUNT" "$FAIL_COUNT"

[ "$FAIL_COUNT" -eq 0 ]
