package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeSkillInstallCreatesFile(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("claude skill content")
	deps := skillDeps(t, content, []byte("codex skill content"))

	out, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID)
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "created") {
		t.Fatalf("first install should report created:\n%s", out)
	}

	dest := claudeSkillDest(repo)
	assertPathExists(t, dest)
	got, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("read installed file: %v", readErr)
	}
	if string(got) != string(content) {
		t.Fatalf("installed content = %q, want %q", got, content)
	}
}

func TestCodexSkillInstallCreatesFile(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("codex skill content")
	deps := skillDeps(t, []byte("claude skill content"), content)

	out, err := runToolsCLI(t, repo, deps, "install", codexSkillComponentID)
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "created") {
		t.Fatalf("first install should report created:\n%s", out)
	}

	dest := codexSkillDest(repo)
	assertPathExists(t, dest)
	got, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("read installed file: %v", readErr)
	}
	if string(got) != string(content) {
		t.Fatalf("installed content = %q, want %q", got, content)
	}
}

func TestSkillInstallIsIdempotent(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content v1")
	deps := skillDeps(t, content, []byte("codex"))

	// First install
	if _, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Second install with same content
	out, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID)
	if err != nil {
		t.Fatalf("second install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "skipped") {
		t.Fatalf("re-install with same checksum should be skipped:\n%s", out)
	}
}

func TestSkillInstallUpdatesOldVersion(t *testing.T) {
	repo := toolsTestRepo(t)
	v1 := []byte("skill content v1")
	v2 := []byte("skill content v2")

	// Install v1
	deps1 := skillFetcherForVersion(t, "1.0.0", v1, []byte("codex"))
	d1 := toolsDeps{
		registry: newToolsRegistry(newClaudeSkillComponent()),
		version:  "1.0.0",
		fetcher:  deps1,
	}
	if _, err := runToolsCLI(t, repo, d1, "install", claudeSkillComponentID); err != nil {
		t.Fatalf("install v1: %v", err)
	}

	// Install v2 (different content → updated)
	deps2 := skillFetcherForVersion(t, "2.0.0", v2, []byte("codex"))
	d2 := toolsDeps{
		registry: newToolsRegistry(newClaudeSkillComponent()),
		version:  "2.0.0",
		fetcher:  deps2,
	}
	out, err := runToolsCLI(t, repo, d2, "install", claudeSkillComponentID)
	if err != nil {
		t.Fatalf("install v2: %v\n%s", err, out)
	}
	if !strings.Contains(out, "updated") {
		t.Fatalf("install with changed content should report updated:\n%s", out)
	}

	dest := claudeSkillDest(repo)
	got, _ := os.ReadFile(dest)
	if string(got) != string(v2) {
		t.Fatalf("file content should be v2 after update, got %q", got)
	}
}

func TestSkillInstallRejectsUnmanagedFile(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content")
	deps := skillDeps(t, content, []byte("codex"))

	// Create an unmanaged file at the destination
	dest := claudeSkillDest(repo)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dest, "user-created content")

	out, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID)
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(out, "failed") {
		t.Fatalf("install with unmanaged file should fail:\n%s", out)
	}
	if !strings.Contains(out, "unmanaged") {
		t.Fatalf("failure reason should mention unmanaged:\n%s", out)
	}
}

func TestSkillInstallRejectsUserModifiedFile(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content")
	deps := skillDeps(t, content, []byte("codex"))

	// Install cleanly
	if _, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Modify the installed file
	dest := claudeSkillDest(repo)
	appendFile(t, dest, "\nuser addition")

	// Re-install should fail
	out, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID)
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(out, "failed") {
		t.Fatalf("install with user-modified file should fail:\n%s", out)
	}
}

func TestSkillInstallFromManagedWorktree(t *testing.T) {
	repo := toolsTestRepo(t)

	// Commit a file so worktree open succeeds
	commitFile(t, repo, "init.txt", "initial\n")

	// Open a managed worktree
	worktreePath := openManagedWorktree(t, repo, "wt-skill-test")

	content := []byte("skill for worktree install test")
	deps := skillDeps(t, content, []byte("codex"))

	// Install from inside the managed worktree — skill must land in repo root
	out, err := runToolsCLI(t, worktreePath, deps, "install", claudeSkillComponentID)
	if err != nil {
		t.Fatalf("install from worktree: %v\n%s", err, out)
	}
	if !strings.Contains(out, "created") {
		t.Fatalf("install from worktree should create file:\n%s", out)
	}

	// File must be at the representative root (repo), not inside the worktree
	assertPathExists(t, claudeSkillDest(repo))
	assertPathMissing(t, claudeSkillDest(worktreePath))
}

func TestSkillInstallPersistsMetadata(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content")
	deps := skillDeps(t, content, []byte("codex"))

	if _, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID); err != nil {
		t.Fatalf("install: %v", err)
	}

	store, err := readToolsMetadata(installedJSONPath(repo))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}

	entry, ok := store.Components[claudeSkillComponentID]
	if !ok {
		t.Fatal("metadata entry should exist after install")
	}
	if entry.Checksum != sha256hex(content) {
		t.Fatalf("metadata checksum = %q, want %q", entry.Checksum, sha256hex(content))
	}
	if entry.ManagedMode != managedModeFile {
		t.Fatalf("metadata managedMode = %q, want %q", entry.ManagedMode, managedModeFile)
	}
	if entry.DestinationPath != claudeSkillDest(repo) {
		t.Fatalf("metadata destinationPath = %q, want %q", entry.DestinationPath, claudeSkillDest(repo))
	}
}

func TestSkillInstallHoldsLock(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content")
	deps := skillDeps(t, content, []byte("codex"))

	// Acquire the metadata lock manually before install
	storeFile, lockFile, err := toolsMetadataPaths(repo)
	if err != nil {
		t.Fatalf("metadata paths: %v", err)
	}
	_ = storeFile
	release, err := acquireToolsLock(lockFile, 0)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer release()

	// Install should fail with lock timeout (or general error if timeout=0)
	_, installErr := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID)
	if installErr == nil {
		t.Fatal("install should fail when lock is held")
	}
}

func TestSkillInstallBlockedWhenMetadataDeletedButFileExists(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content")
	deps := skillDeps(t, content, []byte("codex"))

	// Install
	if _, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Remove metadata entry, simulating lost metadata
	store, err := readToolsMetadata(installedJSONPath(repo))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	delete(store.Components, claudeSkillComponentID)
	if err := writeToolsMetadata(installedJSONPath(repo), store); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	// Install should fail: file exists but no metadata → unmanaged
	out, installErr := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID)
	requireToolsExit(t, installErr, exitGeneralError)
	if !strings.Contains(out, "unmanaged") {
		t.Fatalf("install with deleted metadata and existing file should fail as unmanaged:\n%s", out)
	}
}
