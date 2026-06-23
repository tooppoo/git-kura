package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/internal/tools"
)

func installCtx(repo, commonDir string) tools.InstallContext {
	return tools.InstallContext{
		Context:        tools.Context{RepoRoot: repo, CommonDir: commonDir},
		ReleaseVersion: fixtureVersion,
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
	if !tools.SamePathSafe(got, hooksDir) {
		t.Fatalf("core.hooksPath = %q, want %q", got, hooksDir)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("core.hooksPath must be absolute, got %q", got)
	}

	meta, ok := readPreCommitMeta(t, repo)
	if !ok || meta.InstallState != tools.PreCommitStateInstalled {
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
	if !ok || !tools.SamePathSafe(got, managedHooksDir(repo)) {
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
	if got := tools.ResolvePreviousPreCommitPath(meta, repo, filepath.Join(repo, ".git")); !tools.SamePathSafe(got, prevHook) {
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
	relMeta := tools.PreCommitMeta{PreviousHooksPathState: "set", PreviousHooksPathValue: "relhooks"}
	if got := tools.ResolvePreviousPreCommitPath(relMeta, "/install-wt", commonDir); got != filepath.Join("/install-wt", "relhooks", "pre-commit") {
		t.Fatalf("install-wt resolution = %q", got)
	}
	if got := tools.ResolvePreviousPreCommitPath(relMeta, "/linked-wt", commonDir); got != filepath.Join("/linked-wt", "relhooks", "pre-commit") {
		t.Fatalf("linked-wt resolution = %q, want it resolved against the linked worktree", got)
	}

	// An absolute value is used as-is regardless of worktree.
	absMeta := tools.PreCommitMeta{PreviousHooksPathState: "set", PreviousHooksPathValue: "/abs/hooks"}
	if got := tools.ResolvePreviousPreCommitPath(absMeta, "/whatever", commonDir); got != filepath.Join("/abs/hooks", "pre-commit") {
		t.Fatalf("absolute resolution = %q", got)
	}

	// Unset falls back to the common dir's default hooks directory.
	unsetMeta := tools.PreCommitMeta{PreviousHooksPathState: "unset"}
	if got := tools.ResolvePreviousPreCommitPath(unsetMeta, "/wt", commonDir); got != filepath.Join(commonDir, "hooks", "pre-commit") {
		t.Fatalf("unset resolution = %q", got)
	}

	// The managed dir itself is guarded against (recursion).
	managedMeta := tools.PreCommitMeta{PreviousHooksPathState: "set", PreviousHooksPathValue: tools.PreCommitHooksDir(commonDir)}
	if got := tools.ResolvePreviousPreCommitPath(managedMeta, "/wt", commonDir); got != "" {
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

	o := tools.PreCommitComponent{}.Install(installCtx(repo, commonDir))
	if o.Result.Action != tools.ActionFailed || !strings.Contains(o.Result.Reason, "pending metadata") {
		t.Fatalf("expected pending-metadata failure, got %+v", o.Result)
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
	hooksRoot := tools.PreCommitManagedRoot(commonDir)
	if err := os.MkdirAll(filepath.Dir(hooksRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksRoot, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	o := tools.PreCommitComponent{}.Install(installCtx(repo, commonDir))
	if o.Result.Action != tools.ActionFailed || !strings.Contains(o.Result.Reason, "managed wrapper") {
		t.Fatalf("expected wrapper-write failure, got %+v", o.Result)
	}
	// Pending metadata must have been cleaned up on this failure.
	if _, ok := readPreCommitMeta(t, repo); ok {
		t.Fatal("pending metadata should be removed after wrapper-write failure")
	}
}
