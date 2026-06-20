package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tooppoo/git-kura/internal/gitutil"
)

// --- helpers ---------------------------------------------------------------

// preCommitFetcher builds a fake release fetcher serving a minimal valid tools
// asset. The pre-commit component generates its wrapper itself and reads no
// archive files, so an empty component manifest is enough to pass the framework
// asset-resolution gate.
func preCommitFetcher(t *testing.T) *fakeFetcher {
	t.Helper()
	manifest := archiveManifest{SchemaVersion: 1, Components: map[string]archiveManifestComponent{}}
	archive := makeToolsArchive(t, manifest, nil)
	archiveName := "git-kura-tools_" + fixtureVersion + ".tar.gz"
	sidecar := makeSidecar(t, fixtureVersion, archiveName, checksumAlgorithmSHA256, archive)
	return &fakeFetcher{sidecar: sidecar, archive: archive}
}

func preCommitDeps(t *testing.T) toolsDeps {
	t.Helper()
	return toolsDeps{registry: newToolsRegistry(preCommitComponent{}), version: fixtureVersion, fetcher: preCommitFetcher(t)}
}

func gitConfigLocal(t *testing.T, repo, key string) (string, bool) {
	t.Helper()
	v, ok, err := gitutil.ConfigGetLocal(repo, key)
	if err != nil {
		t.Fatalf("read local config %s: %v", key, err)
	}
	return strings.TrimRight(v, "\n"), ok
}

func readPreCommitMeta(t *testing.T, repo string) (preCommitMeta, bool) {
	t.Helper()
	store, err := readToolsMetadata(installedJSONPath(repo))
	if err != nil {
		t.Fatalf("read tools metadata: %v", err)
	}
	entry, ok := store.Components[preCommitComponentID]
	if !ok {
		return preCommitMeta{}, false
	}
	return preCommitMetaFromEntry(&entry)
}

func managedHooksDir(repo string) string {
	return filepath.Join(repo, ".kura", "tools", "hooks", "_")
}

// --- install ---------------------------------------------------------------

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

func TestPreCommitStatusDetectsModifiedWrapper(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := preCommitDeps(t)
	if _, err := runToolsCLI(t, repo, deps, "install", "pre-commit"); err != nil {
		t.Fatalf("install: %v", err)
	}
	wrapper := filepath.Join(managedHooksDir(repo), "pre-commit")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\necho tampered\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runToolsCLI(t, repo, deps, "status", "pre-commit")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "inconsistent") || !strings.Contains(out, "modified outside git-kura") {
		t.Fatalf("status should detect a modified wrapper:\n%s", out)
	}
}

func TestPreCommitStatusFromManagedWorktreeResolvesKey(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")
	deps := preCommitDeps(t)
	if _, err := runToolsCLI(t, repo, deps, "install", "pre-commit"); err != nil {
		t.Fatalf("install: %v", err)
	}
	wt := openManagedWorktree(t, repo, "alpha")
	out, err := runToolsCLI(t, wt, deps, "status", "pre-commit")
	if err != nil {
		t.Fatalf("status from worktree: %v", err)
	}
	if !strings.Contains(out, "currentKey=alpha") || !strings.Contains(out, "currentKeySource=managed-worktree") {
		t.Fatalf("status should resolve the managed-worktree key:\n%s", out)
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

// --- uninstall -------------------------------------------------------------

func TestPreCommitUninstallRestoresUnset(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := preCommitDeps(t)
	if _, err := runToolsCLI(t, repo, deps, "install", "pre-commit"); err != nil {
		t.Fatalf("install: %v", err)
	}

	out, err := runToolsCLI(t, repo, deps, "uninstall", "pre-commit")
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if !strings.Contains(out, "removed") {
		t.Fatalf("uninstall should report removed:\n%s", out)
	}
	if _, ok := gitConfigLocal(t, repo, "core.hooksPath"); ok {
		t.Fatal("core.hooksPath should be unset after uninstall")
	}
	if _, err := os.Stat(filepath.Join(repo, ".kura", "tools", "hooks")); !os.IsNotExist(err) {
		t.Fatalf("managed hook tree should be gone: %v", err)
	}
	if _, ok := readPreCommitMeta(t, repo); ok {
		t.Fatal("metadata should be removed after uninstall")
	}
}

func TestPreCommitUninstallRestoresPreviousValue(t *testing.T) {
	repo := toolsTestRepo(t)
	prevDir := filepath.Join(repo, "myhooks")
	if err := os.MkdirAll(prevDir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "config", "--local", "core.hooksPath", prevDir)

	deps := preCommitDeps(t)
	if _, err := runToolsCLI(t, repo, deps, "install", "pre-commit"); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := runToolsCLI(t, repo, deps, "uninstall", "pre-commit"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	got, ok := gitConfigLocal(t, repo, "core.hooksPath")
	if !ok || got != prevDir {
		t.Fatalf("core.hooksPath = %q (ok=%v), want %q", got, ok, prevDir)
	}
}

func TestPreCommitUninstallLeavesUserChangedHooksPath(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := preCommitDeps(t)
	if _, err := runToolsCLI(t, repo, deps, "install", "pre-commit"); err != nil {
		t.Fatalf("install: %v", err)
	}
	git(t, repo, "config", "--local", "core.hooksPath", filepath.Join(repo, "other"))

	out, err := runToolsCLI(t, repo, deps, "uninstall", "pre-commit")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !strings.Contains(out, "left untouched") {
		t.Fatalf("uninstall should leave user-changed hooksPath untouched:\n%s", out)
	}
	got, _ := gitConfigLocal(t, repo, "core.hooksPath")
	if got != filepath.Join(repo, "other") {
		t.Fatalf("core.hooksPath was changed to %q", got)
	}
}

func TestPreCommitUninstallCleansUpPendingState(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	hooksDir := preCommitHooksDir(commonDir)
	// Simulate an interrupted install: pending metadata, managed wrapper present,
	// and core.hooksPath already switched to the managed dir.
	if err := writeManagedWrapper(filepath.Join(hooksDir, "pre-commit")); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "config", "--local", "core.hooksPath", hooksDir)
	entry := toolsMetadataEntry{
		Component:         preCommitComponentID,
		ManagedMode:       managedModeConfig,
		ComponentMetadata: preCommitMeta{InstallState: preCommitStatePending, PreviousLocalHooksPathState: "unset"}.toMap(),
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if err := persistPreCommitEntry(repo, entry); err != nil {
		t.Fatal(err)
	}

	deps := preCommitDeps(t)
	out, err := runToolsCLI(t, repo, deps, "uninstall", "pre-commit")
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if _, ok := gitConfigLocal(t, repo, "core.hooksPath"); ok {
		t.Fatal("core.hooksPath should be unset after cleaning up a pending install")
	}
	if _, err := os.Stat(preCommitManagedRoot(commonDir)); !os.IsNotExist(err) {
		t.Fatalf("managed hook tree should be removed: %v", err)
	}
}

func TestPreCommitUninstallRestoresLocalDespiteHigherPrecedence(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := preCommitDeps(t)
	if _, err := runToolsCLI(t, repo, deps, "install", "pre-commit"); err != nil {
		t.Fatalf("install: %v", err)
	}
	// After install, a higher-precedence worktree override shadows the effective
	// value while local config still points at the managed dir.
	git(t, repo, "config", "--local", "extensions.worktreeConfig", "true")
	git(t, repo, "config", "--worktree", "core.hooksPath", filepath.Join(repo, "elsewhere"))

	out, err := runToolsCLI(t, repo, deps, "uninstall", "pre-commit")
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	// The dangling local value must be cleared so removing the override later
	// does not re-activate the now-deleted managed dir.
	if v, ok := gitConfigLocal(t, repo, "core.hooksPath"); ok {
		t.Fatalf("repository-local core.hooksPath should be restored to unset, got %q", v)
	}
	if _, err := os.Stat(preCommitManagedRoot(filepath.Join(repo, ".git"))); !os.IsNotExist(err) {
		t.Fatalf("managed hook tree should be removed: %v", err)
	}
}

func TestPreCommitUninstallDoesNotPersistPreviousGlobalAsLocal(t *testing.T) {
	repo := toolsTestRepo(t)
	// A previous hooks path that comes from GLOBAL config (local unset).
	globalCfg := filepath.Join(t.TempDir(), "gitconfig")
	globalHooks := filepath.Join(repo, "globalhooks")
	if err := os.WriteFile(globalCfg, []byte("[core]\n\thooksPath = "+globalHooks+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalCfg)

	deps := preCommitDeps(t)
	if _, err := runToolsCLI(t, repo, deps, "install", "pre-commit"); err != nil {
		t.Fatalf("install: %v", err)
	}
	meta, _ := readPreCommitMeta(t, repo)
	if meta.PreviousLocalHooksPathState != "unset" {
		t.Fatalf("local scope was unset before install; got state %q", meta.PreviousLocalHooksPathState)
	}
	// The effective previous hooks path (from global) is still captured for
	// chaining, even though the local scope was unset.
	if meta.PreviousHooksPathState != "set" || !samePathSafe(meta.PreviousHooksPathValue, globalHooks) {
		t.Fatalf("previous effective hooks path = %q/%q, want set/%q", meta.PreviousHooksPathState, meta.PreviousHooksPathValue, globalHooks)
	}

	if _, err := runToolsCLI(t, repo, deps, "uninstall", "pre-commit"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	// Uninstall must return local to unset (so the global value shows through
	// again), not write the previous global value into repo-local config.
	if v, ok := gitConfigLocal(t, repo, "core.hooksPath"); ok {
		t.Fatalf("uninstall must not leave a repository-local core.hooksPath, got %q", v)
	}
}

func TestPreCommitUninstallNotInstalled(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := preCommitDeps(t)
	out, err := runToolsCLI(t, repo, deps, "uninstall", "pre-commit")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !strings.Contains(out, "not-installed") {
		t.Fatalf("expected not-installed:\n%s", out)
	}
}

// --- status ----------------------------------------------------------------

func TestPreCommitStatusReportsState(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := preCommitDeps(t)

	out, err := runToolsCLI(t, repo, deps, "status", "pre-commit")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "not-installed") {
		t.Fatalf("status before install should be not-installed:\n%s", out)
	}

	if _, err := runToolsCLI(t, repo, deps, "install", "pre-commit"); err != nil {
		t.Fatalf("install: %v", err)
	}
	out, err = runToolsCLI(t, repo, deps, "status", "pre-commit")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{"installed", "managedHooksPath=", "currentKey=", "bypass: git commit --no-verify"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output missing %q:\n%s", want, out)
		}
	}
}

func TestPreCommitStatusReportsPendingState(t *testing.T) {
	repo := toolsTestRepo(t)
	// A leftover pending entry (e.g. from an interrupted install) must be
	// reported as a non-installed, recoverable state.
	entry := toolsMetadataEntry{
		Component:         preCommitComponentID,
		ManagedMode:       managedModeConfig,
		ComponentMetadata: preCommitMeta{InstallState: preCommitStatePending}.toMap(),
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if err := persistPreCommitEntry(repo, entry); err != nil {
		t.Fatal(err)
	}
	deps := preCommitDeps(t)
	out, err := runToolsCLI(t, repo, deps, "status", "pre-commit")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "installState=pending") || !strings.Contains(out, "skipped") {
		t.Fatalf("status should report the pending state:\n%s", out)
	}
}

func TestPreCommitStatusDetectsInconsistency(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := preCommitDeps(t)
	if _, err := runToolsCLI(t, repo, deps, "install", "pre-commit"); err != nil {
		t.Fatalf("install: %v", err)
	}
	git(t, repo, "config", "--local", "--unset", "core.hooksPath")

	out, err := runToolsCLI(t, repo, deps, "status", "pre-commit")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "inconsistent") {
		t.Fatalf("status should detect inconsistency:\n%s", out)
	}
}

// --- hook execution: current key + seal decision ---------------------------

func stage(t *testing.T, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", rel)
}

func runHook(t *testing.T, dir string, args ...string) error {
	t.Helper()
	var err error
	withWorkingDir(t, dir, func() {
		_, err = captureStdout(t, func() error { return runPreCommitHook(args) })
	})
	return err
}

func TestPreCommitHookAllowsUnclaimedWhenKeyNone(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	stage(t, repo, "free.txt", "free\n")
	// repo itself is not a managed worktree → current key is none; unclaimed
	// staged files are allowed.
	if err := runHook(t, repo); err != nil {
		t.Fatalf("hook should allow unclaimed file with key=none: %v", err)
	}
}

func TestPreCommitHookRejectsClaimedWhenKeyNone(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	wt := openManagedWorktree(t, repo, "owner")
	stage(t, wt, "shared.txt", "x\n")
	withWorkingDir(t, wt, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"shared.txt"}); err != nil {
			t.Fatalf("claim: %v", err)
		}
	})

	stage(t, repo, "shared.txt", "y\n")
	if code := exitCodeOf(runHook(t, repo)); code != exitSealConflict {
		t.Fatalf("expected seal-conflict exit %d, got %d", exitSealConflict, code)
	}
}

func TestPreCommitHookRejectsForeignKeyClaim(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	wtA := openManagedWorktree(t, repo, "alpha")
	wtB := openManagedWorktree(t, repo, "beta")

	stage(t, wtB, "shared.txt", "b\n")
	withWorkingDir(t, wtB, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"shared.txt"}); err != nil {
			t.Fatalf("claim: %v", err)
		}
	})

	stage(t, wtA, "shared.txt", "a\n")
	if code := exitCodeOf(runHook(t, wtA)); code != exitSealConflict {
		t.Fatalf("expected seal-conflict, got exit %d", code)
	}

	stage(t, wtA, "ownfile.txt", "a\n")
	withWorkingDir(t, wtA, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"ownfile.txt"}); err != nil {
			t.Fatalf("claim own: %v", err)
		}
	})
	git(t, wtA, "reset", "shared.txt")
	if err := runHook(t, wtA); err != nil {
		t.Fatalf("hook should allow own-claimed file: %v", err)
	}
}

func TestPreCommitHookHandlesSpecialCharPaths(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	wt := openManagedWorktree(t, repo, "owner")
	weird := "a dir/with space.txt"
	stage(t, wt, weird, "x\n")
	withWorkingDir(t, wt, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{weird}); err != nil {
			t.Fatalf("claim: %v", err)
		}
	})

	stage(t, repo, weird, "y\n")
	if code := exitCodeOf(runHook(t, repo)); code != exitSealConflict {
		t.Fatalf("expected conflict on space-containing path, got exit %d", code)
	}
}

// --- hook execution: previous hook chaining --------------------------------

// writePreCommitMetaForHook installs metadata whose previous effective
// core.hooksPath is prevValue (a directory; may be absolute or relative). The
// previous hook chained at runtime is "<resolved prevValue>/pre-commit".
func writePreCommitMetaForHook(t *testing.T, repo, prevValue string) {
	t.Helper()
	commonDir := filepath.Join(repo, ".git")
	meta := preCommitMeta{
		InstallState:           preCommitStateInstalled,
		PreviousHooksPathState: "set",
		PreviousHooksPathValue: prevValue,
		NewHooksPathValue:      preCommitHooksDir(commonDir),
		WrapperChecksum:        sha256hex([]byte(preCommitWrapperScript)),
	}
	entry := toolsMetadataEntry{
		Component:         preCommitComponentID,
		ManagedMode:       managedModeConfig,
		ComponentMetadata: meta.toMap(),
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if err := persistPreCommitEntry(repo, entry); err != nil {
		t.Fatalf("persist metadata: %v", err)
	}
}

// writePrevHook creates an executable (unless mode says otherwise) pre-commit
// hook at "<dir>/pre-commit" and returns the directory.
func writePrevHook(t *testing.T, dir, script string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pre-commit"), []byte(script), mode); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPreCommitHookChainsPreviousHookFailure(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	dir := writePrevHook(t, filepath.Join(repo, "prevhooks"), "#!/bin/sh\nexit 7\n", 0o755)
	writePreCommitMetaForHook(t, repo, dir)

	stage(t, repo, "free.txt", "free\n")
	if code := exitCodeOf(runHook(t, repo)); code != 7 {
		t.Fatalf("expected previous hook exit 7, got %d", code)
	}
}

func TestPreCommitHookChainsPreviousHookSuccess(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	marker := filepath.Join(repo, "ran")
	dir := writePrevHook(t, filepath.Join(repo, "prevhooks"), "#!/bin/sh\ntouch "+marker+"\nexit 0\n", 0o755)
	writePreCommitMetaForHook(t, repo, dir)

	stage(t, repo, "free.txt", "free\n")
	if err := runHook(t, repo); err != nil {
		t.Fatalf("hook should pass: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("previous hook should have run: %v", err)
	}
}

// TestPreCommitHookChainsPreviousHookPerWorktree is the regression test for the
// linked-worktree resolution finding: a relative previous core.hooksPath must
// chain the hook of the worktree the commit happens in, not the install
// worktree.
func TestPreCommitHookChainsPreviousHookPerWorktree(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	// Previous core.hooksPath is the RELATIVE value "relhooks".
	writePreCommitMetaForHook(t, repo, "relhooks")

	// Linked worktree alpha has its own relhooks/pre-commit that exits 42; the
	// install worktree (repo) deliberately has none.
	wt := openManagedWorktree(t, repo, "alpha")
	writePrevHook(t, filepath.Join(wt, "relhooks"), "#!/bin/sh\nexit 42\n", 0o755)

	stage(t, wt, "free.txt", "free\n")
	if code := exitCodeOf(runHook(t, wt)); code != 42 {
		t.Fatalf("hook in the linked worktree must chain <linked>/relhooks/pre-commit (exit 42), got %d", code)
	}

	// From the install worktree, the same relative value resolves to repo/relhooks
	// which has no hook, so nothing is chained and the commit is allowed.
	stage(t, repo, "free.txt", "free\n")
	if err := runHook(t, repo); err != nil {
		t.Fatalf("install worktree has no relhooks/pre-commit; hook should pass, got %v", err)
	}
}

func TestPreCommitHookSkipsNonExecutablePrevious(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	dir := writePrevHook(t, filepath.Join(repo, "prevhooks"), "#!/bin/sh\nexit 9\n", 0o644)
	writePreCommitMetaForHook(t, repo, dir)

	stage(t, repo, "free.txt", "free\n")
	if err := runHook(t, repo); err != nil {
		t.Fatalf("non-executable previous hook should be skipped, got: %v", err)
	}
}

func TestPreCommitHookSkipsMissingPrevious(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	writePreCommitMetaForHook(t, repo, filepath.Join(repo, "does-not-exist"))
	stage(t, repo, "free.txt", "free\n")
	if err := runHook(t, repo); err != nil {
		t.Fatalf("missing previous hook should be skipped: %v", err)
	}
}

// --- run dispatch ----------------------------------------------------------

func TestToolsRunDispatch(t *testing.T) {
	if err := runToolsRun([]string{"--help"}); err != nil {
		t.Fatalf("help: %v", err)
	}
	if got := exitCodeOf(runToolsRun(nil)); got != exitUsageError {
		t.Fatalf("no args should be usage error, got %d", got)
	}
	if got := exitCodeOf(runToolsRun([]string{"bogus"})); got != exitUsageError {
		t.Fatalf("unknown target should be usage error, got %d", got)
	}
}

func TestRunToolsWithRunSubcommand(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")
	stage(t, repo, "free.txt", "free\n")
	deps := preCommitDeps(t)
	var err error
	withWorkingDir(t, repo, func() {
		_, err = captureStdout(t, func() error { return runToolsWith(deps, []string{"run", "pre-commit"}) })
	})
	if err != nil {
		t.Fatalf("tools run pre-commit: %v", err)
	}
}

// --- unit-level helpers ----------------------------------------------------

func TestEvaluateSealedPaths(t *testing.T) {
	store := sealPathStore{Paths: map[string]sealEntry{
		"a.txt":     {Key: "k1"},
		"dir/b.txt": {Key: "k2"},
	}}
	if c := evaluateSealedPaths(store, sealKeyNone, []string{"a.txt", "free.txt"}); len(c) != 1 {
		t.Fatalf("key none: want 1 conflict, got %d", len(c))
	}
	if c := evaluateSealedPaths(store, "k1", []string{"a.txt", "dir/b.txt"}); len(c) != 1 || c[0].sealedBy != "k2" {
		t.Fatalf("key k1: want 1 conflict by k2, got %v", c)
	}
	if c := evaluateSealedPaths(store, "k1", []string{"./dir/b.txt"}); len(c) != 1 {
		t.Fatalf("want canonicalized match, got %d", len(c))
	}
}

func TestPreCommitHookFailsClosedOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	if code := exitCodeOf(runHook(t, dir)); code != exitGeneralError {
		t.Fatalf("hook outside a repo should fail closed (exit 1), got %d", code)
	}
}

func TestPreCommitHookFailsClosedOnUnreadableMetadata(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	// Make the worktree metadata directory unreadable (a file in its place), so
	// current-key derivation fails and the hook fails closed.
	metaParent := filepath.Join(repo, ".git", "kura", "meta")
	if err := os.MkdirAll(metaParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metaParent, "worktrees"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stage(t, repo, "free.txt", "free\n")
	if code := exitCodeOf(runHook(t, repo)); code != exitGeneralError {
		t.Fatalf("unreadable metadata should fail closed, got exit %d", code)
	}
}

func TestPreCommitHookFailsClosedOnAmbiguousKey(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	wt := openManagedWorktree(t, repo, "alpha")
	// Inject a second metadata file mapping another key to the same worktree
	// root, making the current-key derivation ambiguous.
	dupe := filepath.Join(repo, ".git", "kura", "meta", "worktrees", "beta.json")
	if err := os.WriteFile(dupe, []byte(`{"repositoryRoot":"`+repo+`","baseBranch":"main","worktreePath":"`+wt+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stage(t, wt, "free.txt", "free\n")
	if code := exitCodeOf(runHook(t, wt)); code != exitGeneralError {
		t.Fatalf("ambiguous key should fail closed, got exit %d", code)
	}
}

func TestPreCommitStatusReportsPreviousHookPresence(t *testing.T) {
	repo := toolsTestRepo(t)
	prevDir := filepath.Join(repo, "myhooks")
	if err := os.MkdirAll(prevDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prevDir, "pre-commit"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "config", "--local", "core.hooksPath", prevDir)

	deps := preCommitDeps(t)
	if _, err := runToolsCLI(t, repo, deps, "install", "pre-commit"); err != nil {
		t.Fatalf("install: %v", err)
	}
	out, err := runToolsCLI(t, repo, deps, "status", "pre-commit")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "previousPreCommitExists=true") || !strings.Contains(out, "previousPreCommitExecutable=true") {
		t.Fatalf("status should report the previous hook presence:\n%s", out)
	}
}

func TestDeletePreCommitEntry(t *testing.T) {
	repo := toolsTestRepo(t)
	entry := toolsMetadataEntry{
		Component:         preCommitComponentID,
		ManagedMode:       managedModeConfig,
		ComponentMetadata: preCommitMeta{InstallState: preCommitStatePending}.toMap(),
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if err := persistPreCommitEntry(repo, entry); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if _, ok := readPreCommitMeta(t, repo); !ok {
		t.Fatal("entry should be present after persist")
	}
	if err := deletePreCommitEntry(repo); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := readPreCommitMeta(t, repo); ok {
		t.Fatal("entry should be gone after delete")
	}
	// Deleting an absent entry is a no-op.
	if err := deletePreCommitEntry(repo); err != nil {
		t.Fatalf("delete absent: %v", err)
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

func installCtx(repo, commonDir string) toolsInstallContext {
	return toolsInstallContext{
		toolsContext:   toolsContext{repoRoot: repo, commonDir: commonDir},
		releaseVersion: fixtureVersion,
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

func TestPreCommitMetaFromEntry(t *testing.T) {
	if _, ok := preCommitMetaFromEntry(nil); ok {
		t.Fatal("nil entry should report ok=false")
	}
	if _, ok := preCommitMetaFromEntry(&toolsMetadataEntry{}); ok {
		t.Fatal("entry without componentMetadata should report ok=false")
	}
	m := preCommitMeta{InstallState: preCommitStateInstalled, PreviousLocalHooksPathState: "set"}
	got, ok := preCommitMetaFromEntry(&toolsMetadataEntry{ComponentMetadata: m.toMap()})
	if !ok || got.InstallState != preCommitStateInstalled || got.PreviousLocalHooksPathState != "set" {
		t.Fatalf("round-trip failed: %+v ok=%v", got, ok)
	}
}

func TestPreCommitMetaFromEntryRejectsBadShape(t *testing.T) {
	// installState as a number cannot unmarshal into the string field.
	entry := &toolsMetadataEntry{ComponentMetadata: map[string]any{"installState": 123}}
	if _, ok := preCommitMetaFromEntry(entry); ok {
		t.Fatal("type-mismatched componentMetadata should report ok=false")
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

func TestRunPreviousPreCommitNoMetadata(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	// No installed.json at all → nothing to chain.
	if err := runPreviousPreCommit(repo, commonDir, nil); err != nil {
		t.Fatalf("no metadata should be a no-op, got %v", err)
	}
}

func TestSamePathSafe(t *testing.T) {
	if samePathSafe("", "/x") || samePathSafe("/x", "") {
		t.Fatal("empty inputs must never match")
	}
	if !samePathSafe("/a/b", "/a/b") {
		t.Fatal("identical clean paths should match")
	}
	if samePathSafe("/a/b", "/a/c") {
		t.Fatal("different paths should not match")
	}
}

func TestPreCommitConsistentVariants(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	sum := sha256hex([]byte(preCommitWrapperScript))

	if reason, ok := preCommitConsistent(repo, commonDir, sum); ok || !strings.Contains(reason, "unset") {
		t.Fatalf("unset core.hooksPath: reason=%q ok=%v", reason, ok)
	}

	git(t, repo, "config", "--local", "core.hooksPath", filepath.Join(repo, "elsewhere"))
	if reason, ok := preCommitConsistent(repo, commonDir, sum); ok || !strings.Contains(reason, "not the managed dir") {
		t.Fatalf("non-managed: reason=%q ok=%v", reason, ok)
	}

	// A relative core.hooksPath is resolved against the repo root before the
	// managed-dir comparison.
	git(t, repo, "config", "--local", "core.hooksPath", "relhooks")
	if reason, ok := preCommitConsistent(repo, commonDir, sum); ok || !strings.Contains(reason, "not the managed dir") {
		t.Fatalf("relative non-managed: reason=%q ok=%v", reason, ok)
	}

	// Point at the managed dir but with the wrapper missing.
	git(t, repo, "config", "--local", "core.hooksPath", preCommitHooksDir(commonDir))
	if reason, ok := preCommitConsistent(repo, commonDir, sum); ok || !strings.Contains(reason, "missing") {
		t.Fatalf("missing wrapper: reason=%q ok=%v", reason, ok)
	}
}

func TestRunPreviousPreCommitEmptyPath(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	// Installed metadata with no previous hook recorded → nothing to chain.
	entry := toolsMetadataEntry{
		Component:         preCommitComponentID,
		ManagedMode:       managedModeConfig,
		ComponentMetadata: preCommitMeta{InstallState: preCommitStateInstalled}.toMap(),
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if err := persistPreCommitEntry(repo, entry); err != nil {
		t.Fatal(err)
	}
	if err := runPreviousPreCommit(repo, commonDir, nil); err != nil {
		t.Fatalf("empty previous path should be a no-op, got %v", err)
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

func TestCollectDiagnosticsKeySource(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")
	commonDir := filepath.Join(repo, ".git")

	// Unmanaged worktree root.
	d := collectPreCommitDiagnostics(repo, commonDir, preCommitMeta{})
	if d.currentKeySource != "unmanaged-worktree" {
		t.Fatalf("currentKeySource = %q, want unmanaged-worktree", d.currentKeySource)
	}

	wt := openManagedWorktree(t, repo, "alpha")
	// Inject a duplicate worktree metadata entry → ambiguous.
	dupe := filepath.Join(commonDir, "kura", "meta", "worktrees", "beta.json")
	if err := os.WriteFile(dupe, []byte(`{"repositoryRoot":"`+repo+`","baseBranch":"main","worktreePath":"`+wt+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	d = collectPreCommitDiagnostics(wt, commonDir, preCommitMeta{})
	if d.currentKeySource != "ambiguous" {
		t.Fatalf("currentKeySource = %q, want ambiguous", d.currentKeySource)
	}

	// Replace the worktrees dir with a plain file so os.ReadDir fails with a
	// non-IsNotExist error, exercising the currentKeySource="error" branch.
	worktreesDir := filepath.Join(commonDir, "kura", "meta", "worktrees")
	if err := os.RemoveAll(worktreesDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(worktreesDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d = collectPreCommitDiagnostics(repo, commonDir, preCommitMeta{})
	if d.currentKeySource != "error" {
		t.Fatalf("currentKeySource = %q, want error", d.currentKeySource)
	}
}

func TestPreCommitStatusHooksPathMismatch(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := preCommitDeps(t)
	if _, err := runToolsCLI(t, repo, deps, "install", "pre-commit"); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Enable worktree config and shadow the repository-local value with a
	// worktree-level override, making currentHooksPath differ from effectiveHooksPath.
	git(t, repo, "config", "--local", "extensions.worktreeConfig", "true")
	git(t, repo, "config", "--worktree", "core.hooksPath", filepath.Join(repo, "other"))

	out, err := runToolsCLI(t, repo, deps, "status", "pre-commit")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "hooksPathMismatch=true") {
		t.Fatalf("status should report hooksPathMismatch=true when effective and local values diverge:\n%s", out)
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

func TestRunPreviousPreCommitInvalidMeta(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	// Write a pre-commit entry whose componentMetadata fails the pre-commit schema
	// (installState "bad" is not in the allowed enum). runPreviousPreCommit must
	// silently skip the invalid entry rather than hard-fail.
	entry := toolsMetadataEntry{
		Component:         preCommitComponentID,
		ManagedMode:       managedModeConfig,
		ComponentMetadata: map[string]any{"installState": "bad"},
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if err := persistPreCommitEntry(repo, entry); err != nil {
		t.Fatal(err)
	}
	if err := runPreviousPreCommit(repo, commonDir, nil); err != nil {
		t.Fatalf("invalid meta should be silently skipped, got %v", err)
	}
}

func TestPreCommitSealCheckFailsOnCorruptStore(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")
	stage(t, repo, "free.txt", "free\n")

	storeFile, _, err := pathsSealStore(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storeFile, []byte("{not valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = preCommitSealCheck(storeFile, sealKeyNone, repo, "pre-hook")
	if exitCodeOf(err) != exitGeneralError {
		t.Fatalf("corrupt store should fail closed, got %v", err)
	}
}

func TestRunPreviousPreCommitRecursionGuard(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	hooksDir := preCommitHooksDir(commonDir)
	if err := writeManagedWrapper(filepath.Join(hooksDir, "pre-commit")); err != nil {
		t.Fatal(err)
	}
	// Metadata whose previous core.hooksPath points back at git-kura's own
	// managed dir must be skipped to avoid infinite recursion.
	writePreCommitMetaForHook(t, repo, hooksDir)
	if err := runPreviousPreCommit(repo, commonDir, nil); err != nil {
		t.Fatalf("recursion guard should skip, got %v", err)
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
