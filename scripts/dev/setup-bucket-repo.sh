#!/bin/sh
set -eu

# This script prepares the Scoop bucket repository used by git-kura release support.
#
# It is intentionally limited to environment setup:
#
# - clone the bucket repository when it is missing
# - verify an existing directory before trusting it
# - avoid destructive recovery or state normalization
#
# Release operations such as manifest update, commit, push, and PR creation belong to
# the release support script itself, not to devcontainer setup. Keeping this script
# conservative prevents an automatic container setup from modifying maintainer work.

TARGET_DIR="${GIT_KURA_SCOOP_BUCKET_DIR:-$HOME/catalog-scoop-bucket}"
REPO_URL="${GIT_KURA_SCOOP_BUCKET_URL:-https://github.com/tooppoo/catalog-scoop-bucket.git}"
EXPECTED_REPO="${GIT_KURA_SCOOP_BUCKET_REPO:-tooppoo/catalog-scoop-bucket}"
EXPECTED_MANIFEST="bucket/git-kura.json"

main() {
  ensure_git_available

  if [ -e "$TARGET_DIR" ]; then
    verify_bucket_repo "$TARGET_DIR"
    exit 0
  fi

  clone_bucket_repo "$TARGET_DIR"
}

info() {
  printf '%s\n' "$*" >&2
}

warn() {
  printf 'warning: %s\n' "$*" >&2
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

to_lower() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

canonical_dir() {
  [ -d "$1" ] || return 1
  (
    cd "$1"
    pwd -P
  )
}

# GitHub repository URLs can appear in several equivalent forms depending on how a
# maintainer cloned the repository. The release setup only cares about the canonical
# owner/repository identity, not the transport form.
normalize_github_repo() {
  url="$1"

  case "$url" in
    git@github.com:*)
      path="${url#git@github.com:}"
      ;;
    ssh://git@github.com/*)
      path="${url#ssh://git@github.com/}"
      ;;
    https://github.com/*)
      path="${url#https://github.com/}"
      ;;
    http://github.com/*)
      path="${url#http://github.com/}"
      ;;
    *)
      return 1
      ;;
  esac

  path="${path%.git}"

  while [ "${path%/}" != "$path" ]; do
    path="${path%/}"
  done

  case "$path" in
    */*)
      owner="${path%%/*}"
      repo="${path#*/}"
      ;;
    *)
      return 1
      ;;
  esac

  # A repository identity is exactly owner/repository. Nested paths would mean the
  # remote URL is not the repository root expected by this setup.
  case "$repo" in
    */*)
      return 1
      ;;
  esac

  [ -n "$owner" ] || return 1
  [ -n "$repo" ] || return 1

  to_lower "$owner/$repo"
}

ensure_git_available() {
  command -v git >/dev/null 2>&1 || die "git is required but was not found"
}

verify_bucket_repo() {
  target="$1"
  expected_repo_normalized="$(to_lower "$EXPECTED_REPO")"

  [ -d "$target" ] || die "target exists but is not a directory: $target"

  if ! git -C "$target" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    die "target directory is not a git worktree: $target"
  fi

  root="$(git -C "$target" rev-parse --show-toplevel)"
  target_abs="$(canonical_dir "$target")"
  root_abs="$(canonical_dir "$root")"

  # The bucket path is later passed to release support as a repository root.
  # Accepting a subdirectory would make path-sensitive checks and manifest updates
  # harder to reason about.
  if [ "$target_abs" != "$root_abs" ]; then
    die "target directory is not the repository root: $target"
  fi

  origin_url="$(git -C "$target" remote get-url origin 2>/dev/null || true)"
  [ -n "$origin_url" ] || die "origin remote is missing in bucket repository: $target"

  actual_repo="$(normalize_github_repo "$origin_url" || true)"
  [ -n "$actual_repo" ] || die "origin remote is not a supported GitHub URL: $origin_url"

  # Existing directories are trusted only when they clearly point to the expected
  # bucket repository. This avoids accidentally running release support against an
  # unrelated checkout with a similar path.
  if [ "$actual_repo" != "$expected_repo_normalized" ]; then
    die "unexpected origin repository: expected=$EXPECTED_REPO actual=$actual_repo"
  fi

  # The git-kura manifest is the concrete integration point between the main
  # repository release and the Scoop bucket. Its absence means the checkout is not
  # usable for the intended release workflow.
  if [ ! -f "$target/$EXPECTED_MANIFEST" ]; then
    die "expected Scoop manifest is missing: $target/$EXPECTED_MANIFEST"
  fi

  status="$(git -C "$target" status --porcelain)"
  if [ -n "$status" ]; then
    # Dirty state is not repaired here. A devcontainer setup script should not decide
    # whether local bucket changes are disposable.
    warn "bucket repository has local changes; setup will not modify it: $target"
  fi

  info "Scoop bucket repository is ready: $target"
}

clone_bucket_repo() {
  target="$1"
  parent="$(dirname "$target")"

  mkdir -p "$parent"
  git clone -- "$REPO_URL" "$target"

  # Cloned repositories are verified through the same path as existing repositories
  # so that first-run and repeated setup have the same trust boundary.
  verify_bucket_repo "$target"
}

main "$@"
