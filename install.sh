#!/bin/sh
# Review this script before running it.
# This generated installer is provided as-is, without any warranty.
# The target repository, release assets, and installed software remain subject to their own licenses.
set -u

if [ "${DEBUG:-}" = "1" ]; then
  set -x
fi

#
# Effective installer configuration:
#   generator.name: installerer
#   generator.sourceUrl: https://github.com/tooppoo/installerer
#   owner: tooppoo
#   repo: git-kura
#   binary.name: git-kura
#   binary.pathInArchive: git-kura
#   archive.format: tar.gz
#   archive.nameTemplate: {repo}_{version}_{os}_{arch}.tar.gz
#   archive.osCase: capitalized
#   checksum.fileName: checksums.txt
#   checksum.algorithm: sha256
#   defaults.installDir: $HOME/.local/bin
#   targets: linux/x86_64, linux/aarch64, darwin/x86_64, darwin/aarch64

OWNER='tooppoo'
REPO='git-kura'
BINARY_NAME='git-kura'
BINARY_PATH_IN_ARCHIVE='git-kura'
CHECKSUM_FILE_NAME='checksums.txt'
# shellcheck disable=SC2088 # a leading '~' here is a literal default, expanded manually by resolve_install_dir, not by the shell
DEFAULT_INSTALL_DIR='$HOME/.local/bin'
INSTALL_DIR=
ARCHIVE_FORMAT='tar.gz'
ARCHIVE_SUFFIX='.tar.gz'
LF='
'
CR=$(printf '\r')

main() {
  version=
  install_dir_raw=$DEFAULT_INSTALL_DIR
  saw_version=0
  saw_install_dir=0
  saw_requirements=0
  saw_check_requirements=0

  while [ "$#" -gt 0 ]; do
    case "$1" in
      --help)
        usage
        exit 0
        ;;
      --version)
        [ "$#" -ge 2 ] || fail "--version requires a value"
        version=$2
        saw_version=1
        shift 2
        ;;
      --install-dir)
        [ "$#" -ge 2 ] || fail "--install-dir requires a value"
        install_dir_raw=$2
        saw_install_dir=1
        shift 2
        ;;
      --requirements)
        saw_requirements=1
        shift
        ;;
      --check-requirements)
        saw_check_requirements=1
        shift
        ;;
      *)
        usage >&2
        fail "unknown argument: $1"
        ;;
    esac
  done

  if [ "$saw_requirements" -eq 1 ] || [ "$saw_check_requirements" -eq 1 ]; then
    if [ "$saw_version" -eq 1 ] || [ "$saw_install_dir" -eq 1 ]; then
      fail "--requirements/--check-requirements must not be combined with --version/--install-dir"
    fi
    [ "$saw_requirements" -eq 0 ] || print_requirements
    if [ "$saw_check_requirements" -eq 1 ]; then
      check_requirements
      exit $?
    fi
    exit 0
  fi

  [ "$version" != "latest" ] || fail "--version latest is ambiguous; omit --version for latest install"
  INSTALL_DIR=$(resolve_install_dir "$install_dir_raw")
  [ -n "$INSTALL_DIR" ] || fail "install directory must not be empty"
  validate_binary_path_in_archive "$BINARY_PATH_IN_ARCHIVE"
  check_runtime_dependencies

  if [ -n "$version" ]; then
    install_pin "$version"
  else
    install_latest
  fi
}

fail() {
  printf '%s\n' "installerer: ${1-unexpected failure}" >&2
  [ "$#" -eq 0 ] || shift
  for detail in "$@"; do
    printf ' %s\n' "$detail" >&2
  done
  exit 1
}

log() {
  printf '%s\n' "$*" >&2
}

usage() {
  printf '%s\n' "usage: $0 [--version <version>] [--install-dir <dir>]"
  printf '%s\n' "       $0 --requirements [--check-requirements]"
  printf '%s\n' "       $0 --check-requirements"
  printf '%s\n' "       $0 --help"
  printf '%s\n' ''
  printf '%s\n' 'options:'
  printf '%s\n' '  --version <version>     Install a specific release tag instead of latest.'
  printf '%s\n' '  --install-dir <dir>     Install into <dir> instead of the default install directory.'
  printf '%s\n' '  --requirements          Print the runtime requirements for this installer.'
  printf '%s\n' '  --check-requirements    Check whether runtime requirements are satisfied.'
  printf '%s\n' '  --help                  Show this help message.'
  printf '%s\n' ''
  printf '%s\n' 'local execution examples:'
  printf '%s\n' '  sh install.sh'
  printf '%s\n' '  sh install.sh --version v0.1.2'
  printf '%s\n' '  sh install.sh --install-dir "$HOME/bin"'
  printf '%s\n' '  sh install.sh --version v0.1.2 --install-dir "$HOME/bin"'
}

print_requirements() {
  printf '%s\n' 'Runtime requirements for this installer:'
  printf '%s\n' ''
  printf '%s\n' 'Runtime premise:'
  printf '%s\n' '- POSIX-compatible sh: this installer is executed under a POSIX-compatible sh runtime'
  printf '%s\n' ''
  printf '%s\n' 'Required commands:'
  printf '%s\n' '- uname: Detects the host OS and architecture.'
  printf '%s\n' '- mktemp: Creates a private temporary workspace for download and extraction.'
  printf '%s\n' '- rm: Cleans up temporary files.'
  printf '%s\n' '- mkdir: Creates the install directory and the archive extraction directory.'
  printf '%s\n' '- cp: Copies the extracted binary into the install directory.'
  printf '%s\n' '- mv: Installs the binary atomically.'
  printf '%s\n' '- chmod: Makes the installed binary executable.'
  printf '%s\n' '- curl: Downloads release files from GitHub release assets.'
  printf '%s\n' '- awk: Encodes URL path segments, looks up checksum entries, and scans the archive listing.'
  printf '%s\n' '- grep: Validates archive filenames.'
  printf '%s\n' '- od: Encodes URL path segments safely.'
  printf '%s\n' '- tr: Encodes URL path segments, canonicalizes OS names, and normalizes expected checksums to lowercase.'
  printf '%s\n' '- cut: Encodes URL path segments safely.'
  printf '%s\n' '- tar: Lists and extracts tar.gz archives.'
  printf '%s\n' '- sha256sum or shasum: Verifies SHA-256 checksums.'
  printf '%s\n' ''
  printf '%s\n' 'Network:'
  printf '%s\n' '- HTTPS access to GitHub release assets: downloads the archive and checksum file from GitHub Releases'
  printf '%s\n' ''
  printf '%s\n' 'Filesystem:'
  printf '%s\n' '- Write permission to the install directory: the installer writes the binary into the install directory'
}

check_requirements() {
  status=0
  printf '%s\n' "Checking runtime requirements..."
  printf '\n'
  printf '%s\n' "Runtime premise:"
  printf '%s\n' '- POSIX-compatible sh: this installer is executed under a POSIX-compatible sh runtime'
  printf '\n'
  if command -v 'uname' >/dev/null 2>&1; then
    printf 'ok: %s\n' 'uname'
  else
    printf 'missing: %s\n' 'uname'
    status=1
  fi
  if command -v 'mktemp' >/dev/null 2>&1; then
    printf 'ok: %s\n' 'mktemp'
  else
    printf 'missing: %s\n' 'mktemp'
    status=1
  fi
  if command -v 'rm' >/dev/null 2>&1; then
    printf 'ok: %s\n' 'rm'
  else
    printf 'missing: %s\n' 'rm'
    status=1
  fi
  if command -v 'mkdir' >/dev/null 2>&1; then
    printf 'ok: %s\n' 'mkdir'
  else
    printf 'missing: %s\n' 'mkdir'
    status=1
  fi
  if command -v 'cp' >/dev/null 2>&1; then
    printf 'ok: %s\n' 'cp'
  else
    printf 'missing: %s\n' 'cp'
    status=1
  fi
  if command -v 'mv' >/dev/null 2>&1; then
    printf 'ok: %s\n' 'mv'
  else
    printf 'missing: %s\n' 'mv'
    status=1
  fi
  if command -v 'chmod' >/dev/null 2>&1; then
    printf 'ok: %s\n' 'chmod'
  else
    printf 'missing: %s\n' 'chmod'
    status=1
  fi
  if command -v 'curl' >/dev/null 2>&1; then
    printf 'ok: %s\n' 'curl'
  else
    printf 'missing: %s\n' 'curl'
    status=1
  fi
  if command -v 'awk' >/dev/null 2>&1; then
    printf 'ok: %s\n' 'awk'
  else
    printf 'missing: %s\n' 'awk'
    status=1
  fi
  if command -v 'grep' >/dev/null 2>&1; then
    printf 'ok: %s\n' 'grep'
  else
    printf 'missing: %s\n' 'grep'
    status=1
  fi
  if command -v 'od' >/dev/null 2>&1; then
    printf 'ok: %s\n' 'od'
  else
    printf 'missing: %s\n' 'od'
    status=1
  fi
  if command -v 'tr' >/dev/null 2>&1; then
    printf 'ok: %s\n' 'tr'
  else
    printf 'missing: %s\n' 'tr'
    status=1
  fi
  if command -v 'cut' >/dev/null 2>&1; then
    printf 'ok: %s\n' 'cut'
  else
    printf 'missing: %s\n' 'cut'
    status=1
  fi
  if command -v 'tar' >/dev/null 2>&1; then
    printf 'ok: %s\n' 'tar'
  else
    printf 'missing: %s\n' 'tar'
    status=1
  fi
  if command -v 'sha256sum' >/dev/null 2>&1 || command -v 'shasum' >/dev/null 2>&1; then
    printf 'ok: %s\n' 'sha256sum or shasum'
  else
    printf 'missing: %s\n' 'sha256sum or shasum'
    status=1
  fi
  printf '\n'
  printf '%s\n' "Not checked:"
  printf '%s\n' '- HTTPS access to GitHub release assets: downloads the archive and checksum file from GitHub Releases'
  printf '%s\n' '- Write permission to the install directory: the installer writes the binary into the install directory'
  printf '\n'
  if [ "$status" -eq 0 ]; then
    printf '%s\n' "All checkable requirements are satisfied."
  else
    printf '%s\n' "Some checkable requirements are missing."
  fi
  return "$status"
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

check_runtime_dependencies() {
  require_command 'uname'
  require_command 'mktemp'
  require_command 'rm'
  require_command 'mkdir'
  require_command 'cp'
  require_command 'mv'
  require_command 'chmod'
  require_command 'curl'
  require_command 'awk'
  require_command 'grep'
  require_command 'od'
  require_command 'tr'
  require_command 'cut'
  case "$ARCHIVE_FORMAT" in
    tar.gz) require_command 'tar' ;;
    zip) require_command 'unzip' ;;
    *) fail "unsupported archive format: $ARCHIVE_FORMAT" ;;
  esac
  if command -v 'sha256sum' >/dev/null 2>&1; then
    CHECKSUM_COMMAND='sha256sum'
  elif command -v 'shasum' >/dev/null 2>&1; then
    CHECKSUM_COMMAND='shasum'
  else
    fail "sha256sum or shasum is required"
  fi
}

url_encode_segment() {
  value=$1
  encoded=
  hex_bytes=$(LC_ALL=C printf '%s' "$value" | od -An -tx1 -v | tr -d ' \n')

  while [ -n "$hex_bytes" ]; do
    byte=$(printf '%s' "$hex_bytes" | cut -c 1-2)
    hex_bytes=$(printf '%s' "$hex_bytes" | cut -c 3-)
    case "$byte" in
      2d) encoded="$encoded-" ;;
      2e) encoded="$encoded." ;;
      5f) encoded="$encoded"_ ;;
      7e) encoded="$encoded~" ;;
      30) encoded="$encoded"0 ;;
      31) encoded="$encoded"1 ;;
      32) encoded="$encoded"2 ;;
      33) encoded="$encoded"3 ;;
      34) encoded="$encoded"4 ;;
      35) encoded="$encoded"5 ;;
      36) encoded="$encoded"6 ;;
      37) encoded="$encoded"7 ;;
      38) encoded="$encoded"8 ;;
      39) encoded="$encoded"9 ;;
      41) encoded="$encoded"A ;;
      42) encoded="$encoded"B ;;
      43) encoded="$encoded"C ;;
      44) encoded="$encoded"D ;;
      45) encoded="$encoded"E ;;
      46) encoded="$encoded"F ;;
      47) encoded="$encoded"G ;;
      48) encoded="$encoded"H ;;
      49) encoded="$encoded"I ;;
      4a) encoded="$encoded"J ;;
      4b) encoded="$encoded"K ;;
      4c) encoded="$encoded"L ;;
      4d) encoded="$encoded"M ;;
      4e) encoded="$encoded"N ;;
      4f) encoded="$encoded"O ;;
      50) encoded="$encoded"P ;;
      51) encoded="$encoded"Q ;;
      52) encoded="$encoded"R ;;
      53) encoded="$encoded"S ;;
      54) encoded="$encoded"T ;;
      55) encoded="$encoded"U ;;
      56) encoded="$encoded"V ;;
      57) encoded="$encoded"W ;;
      58) encoded="$encoded"X ;;
      59) encoded="$encoded"Y ;;
      5a) encoded="$encoded"Z ;;
      61) encoded="$encoded"a ;;
      62) encoded="$encoded"b ;;
      63) encoded="$encoded"c ;;
      64) encoded="$encoded"d ;;
      65) encoded="$encoded"e ;;
      66) encoded="$encoded"f ;;
      67) encoded="$encoded"g ;;
      68) encoded="$encoded"h ;;
      69) encoded="$encoded"i ;;
      6a) encoded="$encoded"j ;;
      6b) encoded="$encoded"k ;;
      6c) encoded="$encoded"l ;;
      6d) encoded="$encoded"m ;;
      6e) encoded="$encoded"n ;;
      6f) encoded="$encoded"o ;;
      70) encoded="$encoded"p ;;
      71) encoded="$encoded"q ;;
      72) encoded="$encoded"r ;;
      73) encoded="$encoded"s ;;
      74) encoded="$encoded"t ;;
      75) encoded="$encoded"u ;;
      76) encoded="$encoded"v ;;
      77) encoded="$encoded"w ;;
      78) encoded="$encoded"x ;;
      79) encoded="$encoded"y ;;
      7a) encoded="$encoded"z ;;
      *) encoded="$encoded%$(printf '%s' "$byte" | tr 'abcdef' 'ABCDEF')" ;;
    esac
  done

  printf '%s' "$encoded"
}

is_valid_git_tag() {
  tag=$1
  case "$tag" in
    ""|latest|/*|*/|*.|@|*//*|*..*|*@\{*|*~*|*^*|*:*|*\?*|*\**|*\[*|*\\*) return 1 ;;
    *"$CR"*|*"$LF"*) return 1 ;;
  esac
  if LC_ALL=C printf '%s' "$tag" | grep -q '[[:cntrl:][:space:]]'; then
    return 1
  fi
  old_ifs=$IFS
  IFS=/
  set -- $tag
  IFS=$old_ifs
  for segment do
    case "$segment" in
      ""|.*|*.lock) return 1 ;;
    esac
  done
  return 0
}

# A Git tag may legitimately contain '/' (e.g. "release/v1.2.3"), but a value
# extracted from a checksum-index archive filename cannot: '/' would split it
# across path segments. installerer treats such tags as unsupported for
# {version} extraction (issue #111) even though --version pinning still
# accepts them.
is_filename_unsafe_tag() {
  value=$1
  case "$value" in
    */*|*\\*) return 0 ;;
  esac
  if LC_ALL=C printf '%s' "$value" | grep -q '[[:cntrl:][:space:]]'; then
    return 0
  fi
  return 1
}
resolve_expected_release_tag() {
  index_path=$1
  prefix=$2
  suffix=$3
  match=
  match_count=0
  while IFS= read -r line || [ -n "$line" ]; do
    set -f
    set -- $line
    set +f
    filename=$2
    [ -n "$filename" ] || continue
    case "$filename" in
      "$prefix"*"$suffix")
        if [ "$filename" != "$match" ]; then
          match_count=$((match_count + 1))
          match=$filename
        fi
        ;;
    esac
  done < "$index_path"
  [ "$match_count" -gt 0 ] \
    || fail "no release asset in $CHECKSUM_FILE_NAME matches the configured archive filename template"
  [ "$match_count" -eq 1 ] \
    || fail "ambiguous: multiple release assets in $CHECKSUM_FILE_NAME match the configured archive filename template"
  candidate=${match#"$prefix"}
  candidate=${candidate%"$suffix"}
  is_valid_git_tag "$candidate" || fail "extracted release tag is not a valid Git tag: $candidate"
  is_filename_unsafe_tag "$candidate" && fail "extracted release tag is not safe as a filename: $candidate"
  printf '%s' "$candidate"
}

validate_archive_asset_name() {
  name=$1
  [ -n "$name" ] || fail "archive filename is empty"
  case "$name" in
    */*|*\\*) fail "archive filename contains a path separator: $name" ;;
  esac
  if LC_ALL=C printf '%s' "$name" | grep '[[:cntrl:][:space:]]' >/dev/null; then
    fail "archive filename contains whitespace or control characters: $name"
  fi
  case "$name" in
    *"$ARCHIVE_SUFFIX") ;;
    *) fail "archive filename does not end with $ARCHIVE_SUFFIX: $name" ;;
  esac
}

validate_binary_path_in_archive() {
  path=$1
  [ -n "$path" ] || fail "binary.pathInArchive is empty"
  case "$path" in
    -*) fail "binary.pathInArchive must not start with a hyphen: $path" ;;
    /*|*/|*\\*) fail "binary.pathInArchive must be a relative file path: $path" ;;
  esac
  old_ifs=$IFS
  IFS=/
  set -- $path
  IFS=$old_ifs
  for segment do
    case "$segment" in
      ""|.|..) fail "binary.pathInArchive contains an unsafe path segment: $path" ;;
    esac
  done
}

resolve_install_dir() {
  raw=$1
  # shellcheck disable=SC2088 # '$HOME'/'~' below are literal glob prefixes matched against $raw, then expanded manually via $HOME; the shell never expands them itself
  case "$raw" in
    '$HOME') printf '%s' "$HOME" ;;
    '$HOME/'*) printf '%s/%s' "$HOME" "${raw#\$HOME/}" ;;
    '~') printf '%s' "$HOME" ;;
    '~/'*) printf '%s/%s' "$HOME" "${raw#\~/}" ;;
    /*) printf '%s' "$raw" ;;
    *) printf '%s' "$raw" ;;
  esac
}

detect_target() {
  raw_os=$(uname -s)
  raw_arch=$(uname -m)
  os=$(printf '%s' "$raw_os" | tr '[:upper:]' '[:lower:]')
  arch=$raw_arch

  case "$os" in
    linux) os=linux ;;
    darwin) os=darwin ;;
    *) fail "unsupported OS: $os" \
      "detected uname: $raw_os $raw_arch" \
      "recognized operating systems: linux, darwin" \
      "configured targets: linux/x86_64, linux/aarch64, darwin/x86_64, darwin/aarch64" ;;
  esac

  case "$arch" in
    x86_64) arch=x86_64 ;;
    aarch64|arm64) arch=aarch64 ;;
    *) fail "unsupported architecture: $arch" \
      "detected uname: $raw_os $raw_arch" \
      "recognized architectures: x86_64, aarch64" \
      "recognized uname -m values: x86_64, aarch64, arm64" \
      "configured targets: linux/x86_64, linux/aarch64, darwin/x86_64, darwin/aarch64" ;;
  esac

  case "$os/$arch" in
    linux/x86_64) ;;
    linux/aarch64) ;;
    darwin/x86_64) ;;
    darwin/aarch64) ;;
    *) fail "unsupported target: $os/$arch" \
      "detected uname: $raw_os $raw_arch" \
      "configured targets: linux/x86_64, linux/aarch64, darwin/x86_64, darwin/aarch64" ;;
  esac

  printf '%s %s\n' "$os" "$arch"
}

resolve_asset_arch_label() {
  canonical_os=$1
  canonical_arch=$2

  case "$canonical_os/$canonical_arch" in
    linux/x86_64) asset_arch_label='x86_64' ;;
    linux/aarch64) asset_arch_label='arm64' ;;
    darwin/x86_64) asset_arch_label='x86_64' ;;
    darwin/aarch64) asset_arch_label='arm64' ;;
    *) fail "unsupported target: $canonical_os/$canonical_arch" ;;
  esac

  printf '%s\n' "$asset_arch_label"
}

render_archive_asset_name() {
  version=$1
  os=$2
  asset_arch_label=$3
  case "$os" in
    linux) os=Linux ;;
    darwin) os=Darwin ;;
  esac
  target="${os}_${asset_arch_label}"
  printf '%s' "$REPO" '_' "$version" '_' "$os" '_' "$asset_arch_label" '.tar.gz'
  printf '\n'
}

render_archive_asset_name_prefix() {
  os=$1
  asset_arch_label=$2
  case "$os" in
    linux) os=Linux ;;
    darwin) os=Darwin ;;
  esac
  target="${os}_${asset_arch_label}"
  printf '%s' "$REPO" '_'
  printf '\n'
}

render_archive_asset_name_suffix() {
  os=$1
  asset_arch_label=$2
  case "$os" in
    linux) os=Linux ;;
    darwin) os=Darwin ;;
  esac
  target="${os}_${asset_arch_label}"
  printf '%s' '_' "$os" '_' "$asset_arch_label" '.tar.gz'
  printf '\n'
}

curl_download() {
  url=$1
  output_path=$2
  label=$3
  log "installerer: requesting $url"
  curl -fsSL "$url" -o "$output_path" || fail "failed to download $label"
}

is_valid_sha256_hex() {
  value=$1
  [ "${#value}" -eq 64 ] || return 1
  # An explicit character list rather than [[:xdigit:]] or an a-f range: both a
  # named class and a bracket range are locale-dependent, and this must accept
  # exactly the same characters on every host.
  case "$value" in
    *[!0123456789ABCDEFabcdef]*) return 1 ;;
  esac
  return 0
}

# Runs before the archive download, so a checksum file that cannot verify
# anything costs no transfer and a malformed value is reported as itself
# instead of resurfacing later as a mismatch.
resolve_expected_checksum() {
  expected_checksum=$(awk -v name="$archive_asset_name" '$2 == name { print $1; found=1; exit } END { if (!found) exit 1 }' "$checksum_path") \
    || fail "checksum entry not found for $archive_asset_name"
  # Defence in depth against a later change to the lookup above rather than a
  # reachable input: under awk's default field splitting a record whose $2
  # equals a non-empty name always has a non-empty $1, and an empty asset name
  # is rejected before this runs.
  [ -n "$expected_checksum" ] || fail "checksum entry not found for $archive_asset_name"
  # The rejected value is Release content and is deliberately not echoed back;
  # the asset name and the expected shape are what a maintainer acts on.
  is_valid_sha256_hex "$expected_checksum" \
    || fail "malformed checksum for $archive_asset_name: expected 64 hexadecimal characters"
  # Normalizing once here, not per backend, is what lets verify_sha256 compare
  # bytes whichever command the host provides. The result is re-validated
  # instead of trusting the assignment's status, which reports only tr's: a
  # printf that failed while tr succeeded on empty input would replace the
  # just-validated value with an empty one and surface later as a mismatch
  # instead of naming the real cause.
  expected_checksum=$(printf '%s' "$expected_checksum" | tr 'ABCDEF' 'abcdef')
  is_valid_sha256_hex "$expected_checksum" \
    || fail "failed to normalize the expected checksum for $archive_asset_name"
}

# Both backends compute a digest and compare it below rather than delegating to
# sha256sum -c: -c reports a read or compute failure with the same nonzero
# status as a real mismatch, so the backend nearly every Linux host selects
# would report a broken checksum command as a tampered download. Comparing here
# costs nothing, because resolve_expected_checksum guarantees $expected_checksum
# is 64 lowercase hex and both commands emit lowercase.
verify_sha256() {
  # Neither command is piped into awk: a pipeline reports only its last
  # command's status, so the compute failure would be masked and reported as a
  # mismatch — the same error-class conflation the expected-value side avoids.
  # Both take -- because $archive_path is $tmpdir-derived and a relative TMPDIR
  # beginning with '-' would otherwise turn the path into an option.
  case "$CHECKSUM_COMMAND" in
    sha256sum)
      actual_checksum=$(sha256sum -- "$archive_path") \
        || fail "failed to compute archive checksum"
      ;;
    shasum)
      actual_checksum=$(shasum -a 256 -- "$archive_path") \
        || fail "failed to compute archive checksum"
      ;;
    *)
      fail "checksum command was not initialized"
      ;;
  esac
  # Both commands print "<digest>  <path>", so %% removing the longest match
  # from the first space onward leaves the digest. This POSIX expansion replaces
  # the awk the shasum branch used to pipe into, so observing the exit status
  # above costs no extra process. Both escape the line and prefix it with a
  # backslash when the path holds a newline or backslash; that form is not
  # decoded here, exactly as the replaced awk did not, and $archive_path only
  # takes it if TMPDIR itself does.
  actual_checksum=${actual_checksum%% *}
  [ "$actual_checksum" = "$expected_checksum" ] || fail "archive checksum mismatch"
}

list_archive_entries() {
  case "$ARCHIVE_FORMAT" in
    tar.gz)
      tar -tzf "$archive_path" > "$listing_path" \
        || fail "failed to list tar.gz archive entries"
      ;;
    zip)
      unzip -Z1 "$archive_path" > "$listing_path" \
        || fail "failed to list zip archive entries"
      ;;
    *)
      fail "unsupported archive format: $ARCHIVE_FORMAT"
      ;;
  esac
}

require_unique_archive_entry() {
  # Two counts, because the name the runtime demands and the names an extraction
  # would accept are not the same set. Exact counts entries stored under exactly
  # the configured path; total also counts entries that only spell it
  # differently, because BSD tar matches those for the configured path while GNU
  # tar matches none of them. Counting exact names alone would let an archive
  # holding two such spellings pass as unique and then let extraction order pick
  # the winner on macOS — the ambiguity this check exists to reject.
  #
  # The normalization mirrors what libarchive's matcher does, as a rule rather
  # than a list of examples: it skips a "." component anywhere in the name, not
  # only a leading one, and collapses redundant slashes. It does not strip a
  # leading "/", resolve "..", or fold case, and neither does this — all three
  # tools already refuse to match those, and folding case would over-reject an
  # archive holding two legitimately distinct names. A trailing "/" is left
  # alone too: libarchive treats it as a directory marker whatever the entry's
  # type, so such an entry extracts as a directory and is caught by the
  # regular-file test below instead.
  #
  # The listing is redirected in rather than passed as an operand: an operand of
  # the form identifier=value is a variable assignment to awk, not a file. It is
  # also not piped into awk, because a pipeline reports only awk's status, so a
  # listing command that died partway through would surface as a missing or
  # unique entry instead of as itself.
  #
  # Every comparison concatenates "" first. Values awk received through -v and
  # fields that look numeric are numeric strings, so a plain == between two of
  # them compares numerically: without the concatenation a configured path of
  # "100" would equal an entry named "1e2" and differ from an entry named
  # "100.0" only numerically. The path reaches awk exclusively through -v, never
  # through the program text, and -v's escape expansion is safe because
  # validate_binary_path_in_archive already rejected backslashes.
  #
  # A GNU tar listing escapes a newline inside an entry name, while BSD tar and
  # unzip print it raw and so split one entry across two records. That can only
  # add records, so it may reject a unique archive or count a name that no
  # extraction will find; it can never hide a second real entry.
  list_archive_entries
  archive_entry_counts=$(awk -v path="$BINARY_PATH_IN_ARCHIVE" '
    {
      name = $0
      gsub(/\/\/+/, "/", name)
      while (name ~ /^\.\//) name = substr(name, 3)
      # Looped, not a single gsub: the "/./" in "a/././b" overlap, so one pass
      # leaves "a/./b" behind. Each pass shortens the name, so this terminates.
      while (name ~ /\/\.\//) gsub(/\/\.\//, "/", name)
      sub(/\/\.$/, "", name)
      if (($0 "") == (path "")) {
        exact = exact + 1
        total = total + 1
      } else if ((name "") == (path "")) {
        total = total + 1
      }
    }
    END { print (exact + 0) " " (total + 0) }
  ' < "$listing_path") \
    || fail "failed to scan the archive listing for $BINARY_PATH_IN_ARCHIVE"
  # A count string with no space at all would survive both expansions unchanged
  # and read as "1 1" when awk printed only "1", the one malformed shape that
  # would proceed rather than fail.
  case "$archive_entry_counts" in
    *' '*) ;;
    *) fail "failed to scan the archive listing for $BINARY_PATH_IN_ARCHIVE" ;;
  esac
  exact_entry_matches=${archive_entry_counts% *}
  total_entry_matches=${archive_entry_counts#* }
  # String comparisons, not -eq: a count awk failed to produce would make an
  # arithmetic test error out mid-condition, and every branch here has to end in
  # either a proven-unique entry or a fail.
  [ "$total_entry_matches" != "0" ] || fail "archive entry not found: $BINARY_PATH_IN_ARCHIVE"
  [ "$total_entry_matches" = "1" ] \
    || fail "archive contains $total_entry_matches entries matching $BINARY_PATH_IN_ARCHIVE: expected exactly one"
  [ "$exact_entry_matches" = "1" ] \
    || fail "archive stores $BINARY_PATH_IN_ARCHIVE under an equivalent path spelling, not under that exact name"
}

extract_archive() {
  require_unique_archive_entry
  mkdir -p "$extract_dir" || fail "failed to create extract directory"
  case "$ARCHIVE_FORMAT" in
    tar.gz)
      tar -xzf "$archive_path" -C "$extract_dir" -- "$BINARY_PATH_IN_ARCHIVE" \
        || fail "failed to extract $BINARY_PATH_IN_ARCHIVE from tar.gz archive"
      ;;
    zip)
      unzip -q "$archive_path" "$BINARY_PATH_IN_ARCHIVE" -d "$extract_dir" \
        || fail "failed to extract $BINARY_PATH_IN_ARCHIVE from zip archive"
      ;;
    *)
      fail "unsupported archive format: $ARCHIVE_FORMAT"
      ;;
  esac

  # A symlink is rejected before the regular-file test so that a link is
  # reported as a link rather than as whatever its target happens to be, and so
  # a link pointing at a regular file outside $extract_dir cannot pass.
  extracted_binary="$extract_dir/$BINARY_PATH_IN_ARCHIVE"
  [ ! -L "$extracted_binary" ] || fail "archive binary entry must not be a symlink: $BINARY_PATH_IN_ARCHIVE"
  [ -f "$extracted_binary" ] || fail "archive binary entry is not a regular file: $BINARY_PATH_IN_ARCHIVE"
}

cleanup() {
  if [ -n "${install_tmp:-}" ]; then
    rm -f "$install_tmp"
  fi
  if [ -n "${tmpdir:-}" ]; then
    rm -rf "$tmpdir"
  fi
}

cleanup_on_signal() {
  cleanup
  exit 1
}

install_binary() {
  mkdir -p -- "$INSTALL_DIR" || fail "failed to create install directory: $INSTALL_DIR"

  install_tmp=$(mktemp -- "$INSTALL_DIR/.$BINARY_NAME.tmp.XXXXXX") \
    || fail "failed to create temporary install file in $INSTALL_DIR"

  cp -- "$extracted_binary" "$install_tmp" \
    || fail "failed to copy binary to temporary install path"

  # -- must precede 755: BSD chmod (macOS) stops option parsing at the first
  # operand, so "chmod 755 -- ..." treats -- as a filename, not a terminator.
  chmod -- 755 "$install_tmp" \
    || fail "failed to set binary mode"

  mv -- "$install_tmp" "$INSTALL_DIR/$BINARY_NAME" \
    || fail "failed to place binary in install directory"

  install_tmp=
}

download_and_install() {
  archive_url=$1
  checksum_url=$2
  archive_asset_name=$3
  curl_download "$checksum_url" "$checksum_path" "checksum file"
  resolve_expected_checksum
  curl_download "$archive_url" "$archive_path" "archive"
  verify_sha256
  extract_archive
  install_binary
  log "installed $BINARY_NAME to $INSTALL_DIR/$BINARY_NAME"
}

install_latest() {
  target=$(detect_target) || exit 1
  set -- $target
  os=$1
  arch=$2
  asset_arch_label=$(resolve_asset_arch_label "$os" "$arch") || exit 1
  trap cleanup EXIT
  trap cleanup_on_signal HUP INT TERM
  tmpdir=$(mktemp -d) || fail "failed to create temporary directory"
  archive_path="$tmpdir/archive"
  checksum_path="$tmpdir/checksums"
  checksum_index_path="$tmpdir/checksums_index"
  listing_path="$tmpdir/listing"
  extract_dir="$tmpdir/extract"
  owner_path=$(url_encode_segment "$OWNER")
  repo_path=$(url_encode_segment "$REPO")
  checksum_index_path_segment=$(url_encode_segment "$CHECKSUM_FILE_NAME")
  checksum_index_url="https://github.com/$owner_path/$repo_path/releases/latest/download/$checksum_index_path_segment"
  curl_download "$checksum_index_url" "$checksum_index_path" "checksum index"
  prefix=$(render_archive_asset_name_prefix "$os" "$asset_arch_label")
  suffix=$(render_archive_asset_name_suffix "$os" "$asset_arch_label")
  resolved_version=$(resolve_expected_release_tag "$checksum_index_path" "$prefix" "$suffix") || exit 1
  log "installerer: resolved latest version $resolved_version"
  archive_asset_name=$(render_archive_asset_name "$resolved_version" "$os" "$asset_arch_label")
  validate_archive_asset_name "$archive_asset_name"
  version_path=$(url_encode_segment "$resolved_version")
  archive_path_segment=$(url_encode_segment "$archive_asset_name")
  checksum_path_segment=$(url_encode_segment "$CHECKSUM_FILE_NAME")
  archive_url="https://github.com/$owner_path/$repo_path/releases/download/$version_path/$archive_path_segment"
  checksum_url="https://github.com/$owner_path/$repo_path/releases/download/$version_path/$checksum_path_segment"
  download_and_install "$archive_url" "$checksum_url" "$archive_asset_name"
}

install_pin() {
  pinned_version=$1
  is_valid_git_tag "$pinned_version" || fail "--version must be a valid Git tag and must not be latest"
  target=$(detect_target) || exit 1
  set -- $target
  os=$1
  arch=$2
  asset_arch_label=$(resolve_asset_arch_label "$os" "$arch") || exit 1
  trap cleanup EXIT
  trap cleanup_on_signal HUP INT TERM
  tmpdir=$(mktemp -d) || fail "failed to create temporary directory"
  archive_path="$tmpdir/archive"
  checksum_path="$tmpdir/checksums"
  listing_path="$tmpdir/listing"
  extract_dir="$tmpdir/extract"
  archive_asset_name=$(render_archive_asset_name "$pinned_version" "$os" "$asset_arch_label")
  validate_archive_asset_name "$archive_asset_name"
  owner_path=$(url_encode_segment "$OWNER")
  repo_path=$(url_encode_segment "$REPO")
  version_path=$(url_encode_segment "$pinned_version")
  archive_path_segment=$(url_encode_segment "$archive_asset_name")
  checksum_path_segment=$(url_encode_segment "$CHECKSUM_FILE_NAME")
  archive_url="https://github.com/$owner_path/$repo_path/releases/download/$version_path/$archive_path_segment"
  checksum_url="https://github.com/$owner_path/$repo_path/releases/download/$version_path/$checksum_path_segment"
  download_and_install "$archive_url" "$checksum_url" "$archive_asset_name"
}

main "$@"

