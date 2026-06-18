package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skillFetcher builds a fake release fetcher serving a minimal tools asset
// containing both skill files with the given content.
func skillFetcher(t *testing.T, claudeContent, codexContent []byte) *fakeFetcher {
	t.Helper()
	return skillFetcherForVersion(t, fixtureVersion, claudeContent, codexContent)
}

func skillFetcherForVersion(t *testing.T, version string, claudeContent, codexContent []byte) *fakeFetcher {
	t.Helper()
	manifest := archiveManifest{
		SchemaVersion: 1,
		Components: map[string]archiveManifestComponent{
			claudeSkillComponentID: {Files: map[string]string{
				claudeSkillArchivePath: sha256hex(claudeContent),
			}},
			codexSkillComponentID: {Files: map[string]string{
				codexSkillArchivePath: sha256hex(codexContent),
			}},
		},
	}
	files := map[string][]byte{
		claudeSkillArchivePath: claudeContent,
		codexSkillArchivePath:  codexContent,
	}
	archive := makeToolsArchive(t, manifest, files)
	archiveName := "git-kura-tools_" + version + ".tar.gz"
	sidecar := makeSidecar(t, version, archiveName, checksumAlgorithmSHA256, archive)
	return &fakeFetcher{sidecar: sidecar, archive: archive}
}

func skillDeps(t *testing.T, claudeContent, codexContent []byte) toolsDeps {
	t.Helper()
	return toolsDeps{
		registry: newToolsRegistry(newClaudeSkillComponent(), newCodexSkillComponent()),
		version:  fixtureVersion,
		fetcher:  skillFetcher(t, claudeContent, codexContent),
	}
}

func claudeSkillDest(repo string) string {
	return filepath.Join(repo, ".claude", "skills", "git-kura", "SKILL.md")
}

func codexSkillDest(repo string) string {
	return filepath.Join(repo, ".agents", "skills", "git-kura", "SKILL.md")
}

// --- install ---------------------------------------------------------------

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

// --- uninstall -------------------------------------------------------------

func TestSkillUninstallRemovesFile(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content")
	deps := skillDeps(t, content, []byte("codex"))

	if _, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID); err != nil {
		t.Fatalf("install: %v", err)
	}

	out, err := runToolsCLI(t, repo, deps, "uninstall", claudeSkillComponentID)
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if !strings.Contains(out, "removed") {
		t.Fatalf("uninstall should report removed:\n%s", out)
	}

	assertPathMissing(t, claudeSkillDest(repo))

	// metadata entry must be removed
	store, err := readToolsMetadata(installedJSONPath(repo))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if _, ok := store.Components[claudeSkillComponentID]; ok {
		t.Fatal("metadata entry should be removed after uninstall")
	}
}

func TestSkillUninstallSkipsUserModifiedFile(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content")
	deps := skillDeps(t, content, []byte("codex"))

	if _, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Modify the installed file
	dest := claudeSkillDest(repo)
	appendFile(t, dest, "\nuser addition")

	out, err := runToolsCLI(t, repo, deps, "uninstall", claudeSkillComponentID)
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if !strings.Contains(out, "skipped") {
		t.Fatalf("uninstall of user-modified file should be skipped:\n%s", out)
	}

	// File must still be present
	assertPathExists(t, dest)
}

func TestSkillUninstallNotInstalledIsNoOp(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := skillDeps(t, []byte("c"), []byte("co"))

	out, err := runToolsCLI(t, repo, deps, "uninstall", claudeSkillComponentID)
	if err != nil {
		t.Fatalf("uninstall without prior install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "not-installed") {
		t.Fatalf("uninstall with no metadata should report not-installed:\n%s", out)
	}
}

func TestSkillUninstallDoesNotRemoveParentDir(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content")
	deps := skillDeps(t, content, []byte("codex"))

	if _, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Add another file in .claude/skills to simulate user content
	otherFile := filepath.Join(repo, ".claude", "skills", "other.md")
	writeFile(t, otherFile, "other user skill")

	if _, err := runToolsCLI(t, repo, deps, "uninstall", claudeSkillComponentID); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	// .claude directory must still exist
	assertPathExists(t, filepath.Join(repo, ".claude"))
	assertPathExists(t, otherFile)
}

// --- status ----------------------------------------------------------------

func TestSkillStatusNotInstalled(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := skillDeps(t, []byte("c"), []byte("co"))

	out, err := runToolsCLI(t, repo, deps, "status", claudeSkillComponentID)
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "not-installed") {
		t.Fatalf("status without install should be not-installed:\n%s", out)
	}
}

func TestSkillStatusUnmanagedFileExists(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := skillDeps(t, []byte("c"), []byte("co"))

	// Create unmanaged file at destination
	dest := claudeSkillDest(repo)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dest, "user skill")

	out, err := runToolsCLI(t, repo, deps, "status", claudeSkillComponentID)
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "unmanaged-file-exists") {
		t.Fatalf("status with unmanaged file should report unmanaged-file-exists:\n%s", out)
	}
	if !strings.Contains(out, "not-installed") {
		t.Fatalf("status with unmanaged file should report not-installed action:\n%s", out)
	}
}

func TestSkillStatusInstalled(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content")
	deps := skillDeps(t, content, []byte("codex"))

	if _, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID); err != nil {
		t.Fatalf("install: %v", err)
	}

	out, err := runToolsCLI(t, repo, deps, "status", claudeSkillComponentID)
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "installed") {
		t.Fatalf("status after install should report installed:\n%s", out)
	}
	if !strings.Contains(out, "true") {
		t.Fatalf("status after install should show managed: true:\n%s", out)
	}
}

func TestSkillStatusAfterUserModification(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content")
	deps := skillDeps(t, content, []byte("codex"))

	if _, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Modify the installed file
	appendFile(t, claudeSkillDest(repo), "\nuser addition")

	out, err := runToolsCLI(t, repo, deps, "status", claudeSkillComponentID)
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "installed") {
		t.Fatalf("modified managed file should still report installed action:\n%s", out)
	}
	if !strings.Contains(out, "false") {
		t.Fatalf("modified managed file should show managed: false:\n%s", out)
	}
}

// --- repository context error ----------------------------------------------

func TestToolsRepositoryContextError(t *testing.T) {
	outside := t.TempDir()
	deps := toolsDeps{
		registry: newToolsRegistry(newClaudeSkillComponent()),
		version:  fixtureVersion,
		fetcher:  &fakeFetcher{},
	}

	_, err := runToolsCLI(t, outside, deps, "status", claudeSkillComponentID)
	requireToolsExit(t, err, exitRepositoryContextError)
}

func TestToolsInstallRepositoryContextError(t *testing.T) {
	outside := t.TempDir()
	content := []byte("skill")
	deps := skillDeps(t, content, content)

	_, err := runToolsCLI(t, outside, deps, "install", claudeSkillComponentID)
	requireToolsExit(t, err, exitRepositoryContextError)
}

// --- representative root resolution ----------------------------------------

func TestResolveRepresentativeRootFromMainWorktree(t *testing.T) {
	repo := toolsTestRepo(t)
	commitFile(t, repo, "init.txt", "initial\n")

	commonDir := filepath.Join(repo, ".git")

	var repRoot string
	withWorkingDir(t, repo, func() {
		var err error
		repRoot, err = resolveRepresentativeRoot(repo, commonDir)
		if err != nil {
			t.Fatalf("resolveRepresentativeRoot: %v", err)
		}
	})
	if !samePathSafe(repRoot, repo) {
		t.Fatalf("representative root = %q, want %q", repRoot, repo)
	}
}

func TestResolveRepresentativeRootFromManagedWorktree(t *testing.T) {
	repo := toolsTestRepo(t)
	commitFile(t, repo, "init.txt", "initial\n")

	worktreePath := openManagedWorktree(t, repo, "reproot-test")
	commonDir := filepath.Join(repo, ".git")

	var repRoot string
	withWorkingDir(t, worktreePath, func() {
		var err error
		repRoot, err = resolveRepresentativeRoot(worktreePath, commonDir)
		if err != nil {
			t.Fatalf("resolveRepresentativeRoot from managed worktree: %v", err)
		}
	})
	if !samePathSafe(repRoot, repo) {
		t.Fatalf("representative root = %q, want %q", repRoot, repo)
	}
}

func TestResolveRepresentativeRootMissingMetadata(t *testing.T) {
	repo := toolsTestRepo(t)
	commitFile(t, repo, "init.txt", "initial\n")

	// Simulate a managed-worktree path without metadata
	commonDir := filepath.Join(repo, ".git")
	fakeWorktreePath := filepath.Join(commonDir, "kura", "worktrees", "orphan")
	if err := os.MkdirAll(fakeWorktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := resolveRepresentativeRoot(fakeWorktreePath, commonDir)
	if err == nil {
		t.Fatal("expected error for managed worktree with missing metadata")
	}
	if !strings.Contains(err.Error(), "missing-repository-metadata") {
		t.Fatalf("error = %q, want it to contain missing-repository-metadata", err.Error())
	}
}

func TestResolveRepresentativeRootMissingDir(t *testing.T) {
	tmp := t.TempDir()
	// Use a path that doesn't exist as the representative root
	missingRoot := filepath.Join(tmp, "nonexistent")
	commonDir := filepath.Join(tmp, ".git")

	_, err := resolveRepresentativeRoot(missingRoot, commonDir)
	if err == nil {
		t.Fatal("expected error for non-existent representative root")
	}
	if !strings.Contains(err.Error(), "representative-root-missing") {
		t.Fatalf("error = %q, want it to contain representative-root-missing", err.Error())
	}
}

func TestResolveRepresentativeRootNotDirectory(t *testing.T) {
	repo := toolsTestRepo(t)
	// Create a regular file and use it as the "root"
	fileAsRoot := filepath.Join(repo, "notadir")
	writeFile(t, fileAsRoot, "x")

	commonDir := filepath.Join(repo, ".git")
	_, err := resolveRepresentativeRoot(fileAsRoot, commonDir)
	if err == nil {
		t.Fatal("expected error when representative root is a file")
	}
	if !strings.Contains(err.Error(), "representative-root-not-directory") {
		t.Fatalf("error = %q, want it to contain representative-root-not-directory", err.Error())
	}
}

func TestResolveRepresentativeRootCommonDirMismatch(t *testing.T) {
	// Two separate git repos — their common dirs differ
	repo1 := toolsTestRepo(t)
	repo2 := toolsTestRepo(t)

	commonDir2 := filepath.Join(repo2, ".git")

	// Use repo1 as representative root but repo2's common dir → mismatch
	_, err := resolveRepresentativeRoot(repo1, commonDir2)
	if err == nil {
		t.Fatal("expected error for common dir mismatch")
	}
	if !strings.Contains(err.Error(), "representative-root-common-dir-mismatch") {
		t.Fatalf("error = %q, want it to contain representative-root-common-dir-mismatch", err.Error())
	}
}

// --- metadata persistence --------------------------------------------------

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

func TestSkillUninstallDeletesMetadataEntry(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content")
	deps := skillDeps(t, content, []byte("codex"))

	if _, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := runToolsCLI(t, repo, deps, "uninstall", claudeSkillComponentID); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	store, err := readToolsMetadata(installedJSONPath(repo))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if _, ok := store.Components[claudeSkillComponentID]; ok {
		t.Fatal("metadata entry should be deleted after uninstall")
	}
}

// --- metadata lock ---------------------------------------------------------

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

// --- both components -------------------------------------------------------

func TestBothSkillsInstallAndUninstall(t *testing.T) {
	repo := toolsTestRepo(t)
	claudeContent := []byte("claude skill content")
	codexContent := []byte("codex skill content")
	deps := skillDeps(t, claudeContent, codexContent)

	// Install both
	out, err := runToolsCLI(t, repo, deps, "install", "--all")
	if err != nil {
		t.Fatalf("install --all: %v\n%s", err, out)
	}

	assertPathExists(t, claudeSkillDest(repo))
	assertPathExists(t, codexSkillDest(repo))

	// Status should show both installed
	out, err = runToolsCLI(t, repo, deps, "status", claudeSkillComponentID, codexSkillComponentID)
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	if strings.Count(out, "installed") < 2 {
		t.Fatalf("both skills should be installed:\n%s", out)
	}

	// Uninstall both
	out, err = runToolsCLI(t, repo, deps, "uninstall", "--all")
	if err != nil {
		t.Fatalf("uninstall --all: %v\n%s", err, out)
	}

	assertPathMissing(t, claudeSkillDest(repo))
	assertPathMissing(t, codexSkillDest(repo))
}

// --- unmanaged-file-exists status priority ---------------------------------

func TestSkillStatusUnmanagedHasPriorityOverNotInstalled(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := skillDeps(t, []byte("c"), []byte("co"))

	// No metadata, but file exists at destination
	dest := codexSkillDest(repo)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dest, "pre-existing codex skill")

	out, err := runToolsCLI(t, repo, deps, "status", codexSkillComponentID)
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	// Must report unmanaged-file-exists, not simply not-installed
	if !strings.Contains(out, "unmanaged-file-exists") {
		t.Fatalf("unmanaged file should take priority over not-installed:\n%s", out)
	}
}

// --- round-trip: install → reinstall after metadata removed ----------------

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

// parseWorktreeMetadata is a local helper for reading just the RepositoryRoot field.
func parseWorktreeMetadata(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read worktree metadata: %v", err)
	}
	var meta struct {
		RepositoryRoot string `json:"repositoryRoot"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parse worktree metadata: %v", err)
	}
	return meta.RepositoryRoot
}
