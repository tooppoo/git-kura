package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestSkillStatusInstalledButFileMissing(t *testing.T) {
	repo := toolsTestRepo(t)
	content := []byte("skill content")
	deps := skillDeps(t, content, []byte("codex"))

	if _, err := runToolsCLI(t, repo, deps, "install", claudeSkillComponentID); err != nil {
		t.Fatalf("install: %v", err)
	}

	if err := os.Remove(claudeSkillDest(repo)); err != nil {
		t.Fatal(err)
	}

	out, err := runToolsCLI(t, repo, deps, "status", claudeSkillComponentID)
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "not-installed") {
		t.Fatalf("status with missing file should report not-installed:\n%s", out)
	}
	if !strings.Contains(out, "missing") {
		t.Fatalf("status with missing file should mention missing:\n%s", out)
	}
}

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

func TestPendingComponentStatus(t *testing.T) {
	c := newPendingComponent("future-tool", "https://github.com/tooppoo/git-kura/issues/999")
	out := c.status(toolsContext{})
	if out.result.Action != actionNotInstalled {
		t.Fatalf("pending status action = %q, want %q", out.result.Action, actionNotInstalled)
	}
	if !strings.Contains(out.result.Reason, "future-tool") && !strings.Contains(out.result.Reason, "not yet") {
		t.Fatalf("pending status reason = %q, want it to mention the component or pending state", out.result.Reason)
	}
}
