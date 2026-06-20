package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestResolveRepresentativeRootMalformedMetadata(t *testing.T) {
	repo := toolsTestRepo(t)
	commitFile(t, repo, "init.txt", "initial\n")

	commonDir := filepath.Join(repo, ".git")
	key := "badmeta"
	worktreePath := filepath.Join(commonDir, "kura", "worktrees", key)
	metaPath := filepath.Join(commonDir, "kura", "meta", "worktrees", key+".json")

	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// Valid JSON but missing repositoryRoot field.
	if err := os.WriteFile(metaPath, []byte(`{"key":"badmeta"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := resolveRepresentativeRoot(worktreePath, commonDir)
	if err == nil {
		t.Fatal("expected error for metadata without repositoryRoot")
	}
	if !strings.Contains(err.Error(), "missing-repository-metadata") {
		t.Fatalf("error = %q, want missing-repository-metadata", err.Error())
	}
}

func TestResolveRepresentativeRootCommonDirError(t *testing.T) {
	// A plain directory (not a git repo) triggers a CommonDir error.
	tmp := t.TempDir()
	commonDir := filepath.Join(tmp, ".git")

	_, err := resolveRepresentativeRoot(tmp, commonDir)
	if err == nil {
		t.Fatal("expected error when CommonDir resolution fails")
	}
	if !strings.Contains(err.Error(), "representative-root-common-dir-mismatch") {
		t.Fatalf("error = %q, want representative-root-common-dir-mismatch", err.Error())
	}
}
