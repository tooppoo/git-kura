package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/internal/tools"
)

func TestSkillInstallCreatesFile(t *testing.T) {
	cases := []struct {
		name        string
		componentID string
		content     []byte
		deps        func(*testing.T, []byte) toolsDeps
		dest        func(string) string
	}{
		{
			name:        "claude",
			componentID: tools.ClaudeSkillComponentID,
			content:     []byte("claude skill content"),
			deps: func(t *testing.T, content []byte) toolsDeps {
				return skillDeps(t, content, []byte("codex skill content"))
			},
			dest: claudeSkillDest,
		},
		{
			name:        "codex",
			componentID: tools.CodexSkillComponentID,
			content:     []byte("codex skill content"),
			deps: func(t *testing.T, content []byte) toolsDeps {
				return skillDeps(t, []byte("claude skill content"), content)
			},
			dest: codexSkillDest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := toolsTestRepo(t)
			deps := tc.deps(t, tc.content)

			out, err := runToolsCLI(t, repo, deps, "install", tc.componentID)
			if err != nil {
				t.Fatalf("install: %v\n%s", err, out)
			}
			if !strings.Contains(out, "created") {
				t.Fatalf("first install should report created:\n%s", out)
			}

			dest := tc.dest(repo)
			assertPathExists(t, dest)
			got, readErr := os.ReadFile(dest)
			if readErr != nil {
				t.Fatalf("read installed file: %v", readErr)
			}
			if string(got) != string(tc.content) {
				t.Fatalf("installed content = %q, want %q", got, tc.content)
			}
		})
	}
}

func TestSkillInstallIsIdempotent(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content v1")
	deps := skillDeps(t, content, []byte("codex"))

	// First install
	if _, err := runToolsCLI(t, repo, deps, "install", tools.ClaudeSkillComponentID); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Second install with same content
	out, err := runToolsCLI(t, repo, deps, "install", tools.ClaudeSkillComponentID)
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
		registry: mustToolsRegistry(t, tools.NewClaudeSkillComponent()),
		version:  "1.0.0",
		fetcher:  deps1,
	}
	if _, err := runToolsCLI(t, repo, d1, "install", tools.ClaudeSkillComponentID); err != nil {
		t.Fatalf("install v1: %v", err)
	}

	// Install v2 (different content → updated)
	deps2 := skillFetcherForVersion(t, "2.0.0", v2, []byte("codex"))
	d2 := toolsDeps{
		registry: mustToolsRegistry(t, tools.NewClaudeSkillComponent()),
		version:  "2.0.0",
		fetcher:  deps2,
	}
	out, err := runToolsCLI(t, repo, d2, "install", tools.ClaudeSkillComponentID)
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

	out, err := runToolsCLI(t, repo, deps, "install", tools.ClaudeSkillComponentID)
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
	if _, err := runToolsCLI(t, repo, deps, "install", tools.ClaudeSkillComponentID); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Modify the installed file
	dest := claudeSkillDest(repo)
	appendFile(t, dest, "\nuser addition")

	// Re-install should fail
	out, err := runToolsCLI(t, repo, deps, "install", tools.ClaudeSkillComponentID)
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
	out, err := runToolsCLI(t, worktreePath, deps, "install", tools.ClaudeSkillComponentID)
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

	if _, err := runToolsCLI(t, repo, deps, "install", tools.ClaudeSkillComponentID); err != nil {
		t.Fatalf("install: %v", err)
	}

	store, err := tools.ReadMetadata(installedJSONPath(repo))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}

	entry, ok := store.Components[tools.ClaudeSkillComponentID]
	if !ok {
		t.Fatal("metadata entry should exist after install")
	}
	if entry.Checksum != tools.SHA256Hex(content) {
		t.Fatalf("metadata checksum = %q, want %q", entry.Checksum, tools.SHA256Hex(content))
	}
	if entry.ManagedMode != tools.ManagedModeFile {
		t.Fatalf("metadata managedMode = %q, want %q", entry.ManagedMode, tools.ManagedModeFile)
	}
	if entry.DestinationPath != claudeSkillDest(repo) {
		t.Fatalf("metadata destinationPath = %q, want %q", entry.DestinationPath, claudeSkillDest(repo))
	}
}

func TestSkillInstallHoldsLock(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content")
	deps := skillDeps(t, content, []byte("codex"))

	// Acquire the metadata lock manually before install.
	_, lockFile, err := tools.MetadataPaths(repo)
	if err != nil {
		t.Fatalf("metadata paths: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(lockFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(lockFile) }()

	// Install should fail with lock timeout (or general error if timeout=0)
	_, installErr := runToolsCLI(t, repo, deps, "install", tools.ClaudeSkillComponentID)
	if installErr == nil {
		t.Fatal("install should fail when lock is held")
	}
}

func TestSkillInstallBlockedWhenMetadataDeletedButFileExists(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content")
	deps := skillDeps(t, content, []byte("codex"))

	// Install
	if _, err := runToolsCLI(t, repo, deps, "install", tools.ClaudeSkillComponentID); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Remove metadata entry, simulating lost metadata
	store, err := tools.ReadMetadata(installedJSONPath(repo))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	delete(store.Components, tools.ClaudeSkillComponentID)
	if err := tools.WriteMetadata(installedJSONPath(repo), store); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	// Install should fail: file exists but no metadata → unmanaged
	out, installErr := runToolsCLI(t, repo, deps, "install", tools.ClaudeSkillComponentID)
	requireToolsExit(t, installErr, exitGeneralError)
	if !strings.Contains(out, "unmanaged") {
		t.Fatalf("install with deleted metadata and existing file should fail as unmanaged:\n%s", out)
	}
}
