package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestSkillUninstallWhenFileAlreadyDeleted(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content")
	deps := skillDeps(t, content, []byte("codex"))

	if _, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID); err != nil {
		t.Fatalf("install: %v", err)
	}

	if err := os.Remove(claudeSkillDest(repo)); err != nil {
		t.Fatal(err)
	}

	out, err := runToolsCLI(t, repo, deps, "uninstall", claudeSkillComponentID)
	if err != nil {
		t.Fatalf("uninstall with already-deleted file: %v\n%s", err, out)
	}
	if !strings.Contains(out, "removed") {
		t.Fatalf("uninstall should report removed:\n%s", out)
	}
}
