package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tooppoo/git-kura/internal/tools"
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
	entry := tools.MetadataEntry{
		Component:         tools.PreCommitComponentID,
		ManagedMode:       tools.ManagedModeConfig,
		ComponentMetadata: tools.PreCommitMeta{InstallState: tools.PreCommitStatePending}.ToMap(),
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	persistToolsEntry(t, repo, entry)
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

func TestPreCommitMetaFromEntry(t *testing.T) {
	if _, ok := tools.PreCommitMetaFromEntry(nil); ok {
		t.Fatal("nil entry should report ok=false")
	}
	if _, ok := tools.PreCommitMetaFromEntry(&tools.MetadataEntry{}); ok {
		t.Fatal("entry without componentMetadata should report ok=false")
	}
	m := tools.PreCommitMeta{InstallState: tools.PreCommitStateInstalled, PreviousLocalHooksPathState: "set"}
	got, ok := tools.PreCommitMetaFromEntry(&tools.MetadataEntry{ComponentMetadata: m.ToMap()})
	if !ok || got.InstallState != tools.PreCommitStateInstalled || got.PreviousLocalHooksPathState != "set" {
		t.Fatalf("round-trip failed: %+v ok=%v", got, ok)
	}
}

func TestPreCommitMetaFromEntryRejectsBadShape(t *testing.T) {
	// installState as a number cannot unmarshal into the string field.
	entry := &tools.MetadataEntry{ComponentMetadata: map[string]any{"installState": 123}}
	if _, ok := tools.PreCommitMetaFromEntry(entry); ok {
		t.Fatal("type-mismatched componentMetadata should report ok=false")
	}
}
