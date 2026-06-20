package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
