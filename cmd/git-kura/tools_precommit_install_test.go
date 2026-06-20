package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func installCtx(repo, commonDir string) toolsInstallContext {
	return toolsInstallContext{
		toolsContext:   toolsContext{repoRoot: repo, commonDir: commonDir},
		releaseVersion: fixtureVersion,
	}
}

func TestPreCommitInstallSetsHooksPathAndIsIdempotent(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := preCommitDeps(t)

	out, err := runToolsCLI(t, repo, deps, "install", "pre-commit")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "created") {
		t.Fatalf("first install should be created:\n%s", out)
	}

	hooksDir := managedHooksDir(repo)
	wrapper := filepath.Join(hooksDir, "pre-commit")
	assertPathExists(t, wrapper)
	info, err := os.Stat(wrapper)
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("wrapper must be executable: mode=%v err=%v", info.Mode(), err)
	}

	got, ok := gitConfigLocal(t, repo, "core.hooksPath")
	if !ok {
		t.Fatal("core.hooksPath should be set in local config")
	}
	if !samePathSafe(got, hooksDir) {
		t.Fatalf("core.hooksPath = %q, want %q", got, hooksDir)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("core.hooksPath must be absolute, got %q", got)
	}

	meta, ok := readPreCommitMeta(t, repo)
	if !ok || meta.InstallState != preCommitStateInstalled {
		t.Fatalf("metadata installState = %q (ok=%v), want installed", meta.InstallState, ok)
	}
	if meta.PreviousLocalHooksPathState != "unset" {
		t.Fatalf("previousHooksPathState = %q, want unset", meta.PreviousLocalHooksPathState)
	}

	// Re-install is idempotent and does not destroy previous metadata.
	out, err = runToolsCLI(t, repo, deps, "install", "pre-commit")
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if !strings.Contains(out, "skipped") {
		t.Fatalf("second install should be skipped:\n%s", out)
	}
	meta2, _ := readPreCommitMeta(t, repo)
	if meta2.PreviousLocalHooksPathState != "unset" {
		t.Fatalf("re-install must preserve previous metadata, got %q", meta2.PreviousLocalHooksPathState)
	}
}

func TestPreCommitReinstallRepairsInconsistent(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := preCommitDeps(t)
	if _, err := runToolsCLI(t, repo, deps, "install", "pre-commit"); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Break the install: drop core.hooksPath while metadata stays installed.
	git(t, repo, "config", "--local", "--unset", "core.hooksPath")

	out, err := runToolsCLI(t, repo, deps, "install", "pre-commit")
	if err != nil {
		t.Fatalf("repair install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "updated") {
		t.Fatalf("repair should report updated:\n%s", out)
	}
	got, ok := gitConfigLocal(t, repo, "core.hooksPath")
	if !ok || !samePathSafe(got, managedHooksDir(repo)) {
		t.Fatalf("repair should restore core.hooksPath, got %q", got)
	}
	// Previous-hook metadata (captured at first install) must be preserved.
	meta, _ := readPreCommitMeta(t, repo)
	if meta.PreviousLocalHooksPathState != "unset" {
		t.Fatalf("repair must preserve previous metadata, got %q", meta.PreviousLocalHooksPathState)
	}
}

func TestPreCommitInstallCapturesPreviousHook(t *testing.T) {
	repo := toolsTestRepo(t)
	prevDir := filepath.Join(repo, "myhooks")
	if err := os.MkdirAll(prevDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prevHook := filepath.Join(prevDir, "pre-commit")
	if err := os.WriteFile(prevHook, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "config", "--local", "core.hooksPath", prevDir)

	deps := preCommitDeps(t)
	if _, err := runToolsCLI(t, repo, deps, "install", "pre-commit"); err != nil {
		t.Fatalf("install: %v", err)
	}

	meta, ok := readPreCommitMeta(t, repo)
	if !ok {
		t.Fatal("expected metadata")
	}
	if meta.PreviousLocalHooksPathState != "set" || meta.PreviousLocalHooksPathValue != prevDir {
		t.Fatalf("previous local hooks path = %q/%q, want set/%q", meta.PreviousLocalHooksPathState, meta.PreviousLocalHooksPathValue, prevDir)
	}
	if meta.PreviousHooksPathState != "set" || meta.PreviousHooksPathValue != prevDir {
		t.Fatalf("previous effective hooks path = %q/%q, want set/%q", meta.PreviousHooksPathState, meta.PreviousHooksPathValue, prevDir)
	}
	// The previous pre-commit hook resolves to prevHook for this worktree.
	if got := resolvePreviousPreCommitPath(meta, repo, filepath.Join(repo, ".git")); !samePathSafe(got, prevHook) {
		t.Fatalf("resolved previous pre-commit = %q, want %q", got, prevHook)
	}
}

func TestPreCommitInstallStoresRawRelativePreviousHooksPath(t *testing.T) {
	repo := toolsTestRepo(t)
	git(t, repo, "config", "--local", "core.hooksPath", "relhooks")

	deps := preCommitDeps(t)
	if _, err := runToolsCLI(t, repo, deps, "install", "pre-commit"); err != nil {
		t.Fatalf("install: %v", err)
	}
	meta, _ := readPreCommitMeta(t, repo)
	// The raw relative value is stored verbatim, not pre-resolved against the
	// install worktree, so it can be resolved per-worktree at hook runtime.
	if meta.PreviousHooksPathState != "set" || meta.PreviousHooksPathValue != "relhooks" {
		t.Fatalf("previous effective hooks path = %q/%q, want set/relhooks", meta.PreviousHooksPathState, meta.PreviousHooksPathValue)
	}
}

func TestResolvePreviousPreCommitPathPerWorktree(t *testing.T) {
	commonDir := filepath.Join("/repo", ".git")

	// A relative previous value resolves against the worktree the hook runs in.
	relMeta := preCommitMeta{PreviousHooksPathState: "set", PreviousHooksPathValue: "relhooks"}
	if got := resolvePreviousPreCommitPath(relMeta, "/install-wt", commonDir); got != filepath.Join("/install-wt", "relhooks", "pre-commit") {
		t.Fatalf("install-wt resolution = %q", got)
	}
	if got := resolvePreviousPreCommitPath(relMeta, "/linked-wt", commonDir); got != filepath.Join("/linked-wt", "relhooks", "pre-commit") {
		t.Fatalf("linked-wt resolution = %q, want it resolved against the linked worktree", got)
	}

	// An absolute value is used as-is regardless of worktree.
	absMeta := preCommitMeta{PreviousHooksPathState: "set", PreviousHooksPathValue: "/abs/hooks"}
	if got := resolvePreviousPreCommitPath(absMeta, "/whatever", commonDir); got != filepath.Join("/abs/hooks", "pre-commit") {
		t.Fatalf("absolute resolution = %q", got)
	}

	// Unset falls back to the common dir's default hooks directory.
	unsetMeta := preCommitMeta{PreviousHooksPathState: "unset"}
	if got := resolvePreviousPreCommitPath(unsetMeta, "/wt", commonDir); got != filepath.Join(commonDir, "hooks", "pre-commit") {
		t.Fatalf("unset resolution = %q", got)
	}

	// The managed dir itself is guarded against (recursion).
	managedMeta := preCommitMeta{PreviousHooksPathState: "set", PreviousHooksPathValue: preCommitHooksDir(commonDir)}
	if got := resolvePreviousPreCommitPath(managedMeta, "/wt", commonDir); got != "" {
		t.Fatalf("managed dir should be guarded, got %q", got)
	}
}

func TestPreCommitInstallPreflightFailsOnHigherPrecedenceScope(t *testing.T) {
	repo := toolsTestRepo(t)
	// A worktree-scoped value outranks repository local config.
	git(t, repo, "config", "--local", "extensions.worktreeConfig", "true")
	git(t, repo, "config", "--worktree", "core.hooksPath", "/elsewhere")

	deps := preCommitDeps(t)
	out, err := runToolsCLI(t, repo, deps, "install", "pre-commit")
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(out, "higher-precedence") {
		t.Fatalf("expected preflight failure, got:\n%s", out)
	}
	if _, err := os.Stat(managedHooksDir(repo)); !os.IsNotExist(err) {
		t.Fatalf("managed hooks dir must not exist after preflight failure: %v", err)
	}
	if _, err := os.Stat(installedJSONPath(repo)); !os.IsNotExist(err) {
		t.Fatalf("metadata must not exist after preflight failure: %v", err)
	}
}

func TestPreCommitInstallFailsWritingPendingMetadata(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	// Make the tools metadata directory path a regular file so the pending
	// metadata write cannot create or read installed.json.
	toolsDir := filepath.Join(commonDir, "kura", "tools")
	if err := os.MkdirAll(filepath.Dir(toolsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolsDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	o := preCommitComponent{}.install(installCtx(repo, commonDir))
	if o.result.Action != actionFailed || !strings.Contains(o.result.Reason, "pending metadata") {
		t.Fatalf("expected pending-metadata failure, got %+v", o.result)
	}
	if _, ok := gitConfigLocal(t, repo, "core.hooksPath"); ok {
		t.Fatal("core.hooksPath must not be set when pending metadata write fails")
	}
}

func TestPreCommitInstallFailsWritingWrapper(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	// Pre-create the managed hooks root as a file so the wrapper directory
	// cannot be created.
	hooksRoot := preCommitManagedRoot(commonDir)
	if err := os.MkdirAll(filepath.Dir(hooksRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksRoot, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	o := preCommitComponent{}.install(installCtx(repo, commonDir))
	if o.result.Action != actionFailed || !strings.Contains(o.result.Reason, "managed wrapper") {
		t.Fatalf("expected wrapper-write failure, got %+v", o.result)
	}
	// Pending metadata must have been cleaned up on this failure.
	if _, ok := readPreCommitMeta(t, repo); ok {
		t.Fatal("pending metadata should be removed after wrapper-write failure")
	}
}

func TestRollbackReason(t *testing.T) {
	if got := rollbackReason("primary", nil); got != "primary" {
		t.Fatalf("nil rollback err: got %q", got)
	}
	got := rollbackReason("primary", os.ErrPermission)
	if !strings.Contains(got, "rollback also failed") {
		t.Fatalf("rollback failure should be surfaced: %q", got)
	}
}

func TestRollbackPreCommitRestoresConfig(t *testing.T) {
	repo := toolsTestRepo(t)
	managedRoot := preCommitManagedRoot(filepath.Join(repo, ".git"))
	if err := os.MkdirAll(managedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "config", "--local", "core.hooksPath", filepath.Join(repo, "managed"))

	prev := preCommitPrevHook{localState: "set", localValue: filepath.Join(repo, "old")}
	if err := rollbackPreCommit(repo, managedRoot, prev); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	got, _ := gitConfigLocal(t, repo, "core.hooksPath")
	if got != prev.localValue {
		t.Fatalf("core.hooksPath = %q, want %q", got, prev.localValue)
	}
	if _, err := os.Stat(managedRoot); !os.IsNotExist(err) {
		t.Fatalf("managed root should be removed: %v", err)
	}

	if err := rollbackPreCommit(repo, managedRoot, preCommitPrevHook{localState: "unset"}); err != nil {
		t.Fatalf("rollback unset: %v", err)
	}
	if _, ok := gitConfigLocal(t, repo, "core.hooksPath"); ok {
		t.Fatal("core.hooksPath should be unset")
	}
}

func TestResolvePreviousHookLookupRecursionGuard(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	// core.hooksPath already points at git-kura's own managed dir.
	git(t, repo, "config", "--local", "core.hooksPath", preCommitHooksDir(commonDir))

	prev, err := resolvePreviousHookLookup(repo, commonDir)
	if err != nil {
		t.Fatalf("resolvePreviousHookLookup: %v", err)
	}
	// Local restore target must not be ourselves.
	if prev.localState != "unset" {
		t.Fatalf("orphaned local value must be treated as unset, got %q", prev.localState)
	}
	// The captured effective value (the managed dir) must resolve to no previous
	// hook at runtime, preventing recursion.
	meta := preCommitMeta{PreviousHooksPathState: prev.effectiveState, PreviousHooksPathValue: prev.effectiveValue}
	if got := resolvePreviousPreCommitPath(meta, repo, commonDir); got != "" {
		t.Fatalf("managed dir must not chain as previous hook, got %q", got)
	}
}

func TestResolvePreviousHookLookupErrorsOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	if _, err := resolvePreviousHookLookup(dir, filepath.Join(dir, ".git")); err == nil {
		t.Fatal("resolvePreviousHookLookup should error when git config cannot run")
	}
}

func TestResolvePreviousHookLookupOrphanLocalValue(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	// Local already points at git-kura's own managed dir (orphaned install): it
	// must not be recorded as the local restore target.
	git(t, repo, "config", "--local", "core.hooksPath", preCommitHooksDir(commonDir))

	prev, err := resolvePreviousHookLookup(repo, commonDir)
	if err != nil {
		t.Fatalf("resolvePreviousHookLookup: %v", err)
	}
	if prev.localState != "unset" || prev.localValue != "" {
		t.Fatalf("orphaned local value must be treated as unset, got %q/%q", prev.localState, prev.localValue)
	}
}

func TestPersistAndDeleteEntryErrorOnBadStore(t *testing.T) {
	repo := toolsTestRepo(t)
	// Make the tools metadata directory path a file so read/write of
	// installed.json fails.
	toolsDir := filepath.Join(repo, ".git", "kura", "tools")
	if err := os.MkdirAll(filepath.Dir(toolsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolsDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := persistPreCommitEntry(repo, toolsMetadataEntry{Component: preCommitComponentID, ManagedMode: managedModeConfig}); err == nil {
		t.Fatal("persist should fail on an unreadable store")
	}
	if err := deletePreCommitEntry(repo); err == nil {
		t.Fatal("delete should fail on an unreadable store")
	}
}

func TestPersistDeleteEntryNonRepoError(t *testing.T) {
	dir := t.TempDir() // not a git repo — toolsMetadataPaths fails at CommonDir
	if err := persistPreCommitEntry(dir, toolsMetadataEntry{}); err == nil {
		t.Fatal("persistPreCommitEntry should fail outside a git repo")
	}
	if err := deletePreCommitEntry(dir); err == nil {
		t.Fatal("deletePreCommitEntry should fail outside a git repo")
	}
}
