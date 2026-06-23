package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tooppoo/git-kura/internal/tools"
)

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
	hooksDir := tools.PreCommitHooksDir(commonDir)
	// Simulate an interrupted install: pending metadata, managed wrapper present,
	// and core.hooksPath already switched to the managed dir.
	writeWrapperForTest(t, filepath.Join(hooksDir, "pre-commit"))
	git(t, repo, "config", "--local", "core.hooksPath", hooksDir)
	entry := tools.MetadataEntry{
		Component:         tools.PreCommitComponentID,
		ManagedMode:       tools.ManagedModeConfig,
		ComponentMetadata: tools.PreCommitMeta{InstallState: tools.PreCommitStatePending, PreviousLocalHooksPathState: "unset"}.ToMap(),
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	persistToolsEntry(t, repo, entry)

	deps := preCommitDeps(t)
	out, err := runToolsCLI(t, repo, deps, "uninstall", "pre-commit")
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if _, ok := gitConfigLocal(t, repo, "core.hooksPath"); ok {
		t.Fatal("core.hooksPath should be unset after cleaning up a pending install")
	}
	if _, err := os.Stat(tools.PreCommitManagedRoot(commonDir)); !os.IsNotExist(err) {
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
	if _, err := os.Stat(tools.PreCommitManagedRoot(filepath.Join(repo, ".git"))); !os.IsNotExist(err) {
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
	if meta.PreviousHooksPathState != "set" || !tools.SamePathSafe(meta.PreviousHooksPathValue, globalHooks) {
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
