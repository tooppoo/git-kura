#!/usr/bin/env sh
# Walkthrough test for git-kura.
#
# This script drives the real `git kura` command through representative
# end-to-end scenarios against a throwaway git repository. It favours coverage
# of the common path over exhaustive variation. It is written for POSIX sh
# (no bash-specific syntax) and targets Unix-like systems; Windows is out of
# scope.
#
# Prerequisites: `git kura` must resolve on PATH. In CI this is arranged by
# running `make build` and prepending `./bin` to PATH. For local runs use
# `make walkthrough`, which does the same and then invokes this script.
#
# Read top-down: `main` describes the walkthrough; the helpers it relies on are
# defined below it and `main` is invoked at the end of the file.

set -eu

# main runs the whole walkthrough. It relies on the helpers defined further
# down (step/pass/fail, the gk runner, the expect_* assertions, seal_present)
# and is invoked at the end of the file, after everything it needs exists.
main() {
	#-- 1. PATH availability --------------------------------------------

	step "git kura --version"
	gk "." --version
	expect_rc 0 "git-kura on PATH responds to --version"

	#-- 2. throwaway repository -----------------------------------------

	WORK="$(mktemp -d)"
	REPO="$WORK/repo"
	mkdir -p "$REPO"
	cd "$REPO"
	git init -q
	git config user.email walkthrough@example.com
	git config user.name "git-kura walkthrough"
	for f in file1.txt file2.txt file3.txt file4.txt file5.txt; do
		printf 'content of %s\n' "$f" >"$REPO/$f"
	done
	git add .
	git commit -qm "initial commit"

	#-- 3. create multiple worktrees ------------------------------------

	step "create multiple worktrees"
	gk "$REPO" open alpha
	expect_rc 0 "open worktree alpha"
	gk "$REPO" open beta
	expect_rc 0 "open worktree beta"
	ALPHA="$(cd "$REPO" && git kura get alpha)"
	BETA="$(cd "$REPO" && git kura get beta)"

	#-- 4. seal claim ---------------------------------------------------

	step "seal claim succeeds without conflict"
	gk "$ALPHA" seal claim file1.txt file2.txt
	expect_rc 0 "alpha claims file1.txt and file2.txt"

	step "seal claim conflicts across worktrees"
	gk "$BETA" seal claim file1.txt
	expect_rc 6 "beta cannot claim file1.txt already claimed by alpha"
	expect_stderr_contains "seal-conflict" "conflict carries the seal-conflict reason token"
	expect_stderr_contains "already claimed" "conflict message reports the file is already claimed"

	gk "$BETA" seal claim file3.txt
	expect_rc 0 "beta claims the unclaimed file3.txt"

	#-- 5. seal ls ------------------------------------------------------

	step "seal ls reflects current claims"
	seal_present alpha file1.txt "seal ls lists alpha file1.txt"
	seal_present alpha file2.txt "seal ls lists alpha file2.txt"
	seal_present beta file3.txt "seal ls lists beta file3.txt"

	#-- 6. seal test ----------------------------------------------------

	step "seal test detects conflicts and clean paths"
	gk "$BETA" seal test file1.txt
	expect_rc 6 "seal test reports a conflict for file1.txt"
	expect_stderr_contains "seal-conflict" "seal test conflict carries the reason token"

	gk "$BETA" seal test file3.txt
	expect_rc 0 "seal test passes for a file beta already claims"

	gk "$BETA" seal test file4.txt
	expect_rc 0 "seal test passes for an unclaimed file"

	#-- 7. seal unclaim -------------------------------------------------

	step "seal unclaim frees a path for another worktree"
	gk "$ALPHA" seal unclaim file1.txt
	expect_rc 0 "alpha unclaims file1.txt"

	gk "$BETA" seal claim file1.txt
	expect_rc 0 "beta can claim file1.txt once alpha unclaimed it"

	gk "$BETA" seal claim file2.txt
	expect_rc 6 "beta still cannot claim file2.txt, which alpha did not unclaim"
	expect_stderr_contains "seal-conflict" "still-claimed file2.txt reports the conflict token"

	#-- 8. seal doctor --------------------------------------------------

	step "seal doctor validates a healthy store"
	gk "$REPO" seal doctor
	expect_rc 0 "seal doctor succeeds on a healthy store"

	step "seal doctor rejects a corrupted store"
	STORE="$REPO/.git/kura/seals/paths.json"
	cp "$STORE" "$STORE.bak"
	printf 'this is not valid json\n' >"$STORE"
	gk "$REPO" seal doctor
	expect_nonzero "seal doctor fails on a corrupted store"
	expect_stderr_contains "seal-doctor-error" "doctor reports the seal-doctor-error token"
	mv "$STORE.bak" "$STORE"

	#-- 9. close (happy path only) -------------------------------------

	step "close: open and claim a throwaway worktree"
	gk "$REPO" open gamma
	expect_rc 0 "open worktree gamma"
	GAMMA="$(cd "$REPO" && git kura get gamma)"
	gk "$GAMMA" seal claim file5.txt
	expect_rc 0 "gamma claims file5.txt"

	GAMMA_META="$REPO/.git/kura/meta/worktrees/gamma.json"
	if [ ! -d "$GAMMA" ]; then
		fail "precondition: gamma worktree directory should exist before close"
	fi
	if [ ! -f "$GAMMA_META" ]; then
		fail "precondition: gamma metadata should exist before close"
	fi

	step "close removes the worktree directory and metadata"
	gk "$REPO" close gamma
	expect_rc 0 "close gamma"
	if [ -d "$GAMMA" ]; then
		fail "gamma worktree directory still exists after close"
	fi
	pass "gamma worktree directory removed"
	if [ -f "$GAMMA_META" ]; then
		fail "gamma metadata still exists after close"
	fi
	pass "gamma metadata removed"

	step "close releases the closed worktree's seals"
	gk "$REPO" seal ls
	expect_rc 0 "seal ls runs after close"
	if grep -E "^gamma[[:space:]]+file5.txt\$" "$GK_OUT_FILE" >/dev/null; then
		dump_last
		fail "gamma seal on file5.txt is still present after close"
	fi
	pass "gamma seal on file5.txt released after close"

	#-- done ------------------------------------------------------------

	step "walkthrough complete"
	printf '\nAll walkthrough scenarios passed.\n'
}

#-- output helpers ----------------------------------------------------------

step() { printf '\n=== %s ===\n' "$*"; }
pass() { printf 'PASS: %s\n' "$*"; }
fail() { printf 'WALKTHROUGH FAILED: %s\n' "$*" >&2; exit 1; }

dump_last() {
	printf -- '--- stdout ---\n' >&2
	[ -f "$GK_OUT_FILE" ] && cat "$GK_OUT_FILE" >&2
	printf -- '--- stderr ---\n' >&2
	[ -f "$GK_ERR_FILE" ] && cat "$GK_ERR_FILE" >&2
}

#-- command runner ----------------------------------------------------------
# GK_RC / GK_OUT_FILE / GK_ERR_FILE hold the result of the most recent run.

GK_RC=0
GK_OUT_FILE=""
GK_ERR_FILE=""

# gk <dir> <args...>: run `git kura <args>` in <dir>, capturing exit code,
# stdout and stderr. Never aborts the script even when the command fails, so
# the caller can assert on the captured exit code.
gk() {
	gk_dir="$1"
	shift
	if [ -z "$GK_OUT_FILE" ]; then
		GK_OUT_FILE="$(mktemp)"
		GK_ERR_FILE="$(mktemp)"
	fi
	if ( cd "$gk_dir" && git kura "$@" ) >"$GK_OUT_FILE" 2>"$GK_ERR_FILE"; then
		GK_RC=0
	else
		GK_RC=$?
	fi
}

expect_rc() {
	want="$1"
	desc="$2"
	if [ "$GK_RC" -eq "$want" ]; then
		pass "$desc (exit $GK_RC)"
	else
		dump_last
		fail "$desc: expected exit $want, got $GK_RC"
	fi
}

expect_nonzero() {
	desc="$1"
	if [ "$GK_RC" -ne 0 ]; then
		pass "$desc (exit $GK_RC)"
	else
		dump_last
		fail "$desc: expected non-zero exit, but the command succeeded"
	fi
}

expect_stderr_contains() {
	needle="$1"
	desc="$2"
	if grep -q "$needle" "$GK_ERR_FILE"; then
		pass "$desc (stderr contains \"$needle\")"
	else
		dump_last
		fail "$desc: stderr does not contain \"$needle\""
	fi
}

# seal_present <key> <path> <desc>: assert `seal ls` lists key/path.
seal_present() {
	sp_key="$1"
	sp_path="$2"
	sp_desc="$3"
	gk "$REPO" seal ls
	expect_rc 0 "seal ls runs while checking: $sp_desc"
	if grep -E "^${sp_key}[[:space:]]+${sp_path}\$" "$GK_OUT_FILE" >/dev/null; then
		pass "$sp_desc"
	else
		dump_last
		fail "$sp_desc: expected \"$sp_key $sp_path\" in seal ls"
	fi
}

#-- cleanup -----------------------------------------------------------------

WORK=""
cleanup() {
	cd /
	if [ -n "$WORK" ] && [ -d "$WORK" ]; then
		chmod -R u+w "$WORK" 2>/dev/null || true
		rm -rf "$WORK"
	fi
	[ -n "$GK_OUT_FILE" ] && rm -f "$GK_OUT_FILE" "$GK_ERR_FILE"
}
trap cleanup EXIT INT TERM

main "$@"
