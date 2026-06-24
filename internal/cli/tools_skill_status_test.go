package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/internal/tools"
)

func TestSkillStatusNotInstalled(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := skillDeps(t, []byte("c"), []byte("co"))

	out, err := runToolsCLI(t, repo, deps, "status", tools.ClaudeSkillComponentID)
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

	out, err := runToolsCLI(t, repo, deps, "status", tools.ClaudeSkillComponentID)
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

	if _, err := runToolsCLI(t, repo, deps, "install", tools.ClaudeSkillComponentID); err != nil {
		t.Fatalf("install: %v", err)
	}

	out, err := runToolsCLI(t, repo, deps, "status", tools.ClaudeSkillComponentID)
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

	if _, err := runToolsCLI(t, repo, deps, "install", tools.ClaudeSkillComponentID); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Modify the installed file
	appendFile(t, claudeSkillDest(repo), "\nuser addition")

	out, err := runToolsCLI(t, repo, deps, "status", tools.ClaudeSkillComponentID)
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

	if _, err := runToolsCLI(t, repo, deps, "install", tools.ClaudeSkillComponentID); err != nil {
		t.Fatalf("install: %v", err)
	}

	if err := os.Remove(claudeSkillDest(repo)); err != nil {
		t.Fatal(err)
	}

	out, err := runToolsCLI(t, repo, deps, "status", tools.ClaudeSkillComponentID)
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

func TestPendingComponentStatus(t *testing.T) {
	c := tools.PendingComponent{ComponentID: "future-tool", TrackingURL: "https://github.com/tooppoo/git-kura/issues/999"}
	out := c.Status(tools.Context{})
	if out.Result.Action != tools.ActionNotInstalled {
		t.Fatalf("pending status action = %q, want %q", out.Result.Action, tools.ActionNotInstalled)
	}
	if !strings.Contains(out.Result.Reason, "future-tool") && !strings.Contains(out.Result.Reason, "not yet") {
		t.Fatalf("pending status reason = %q, want it to mention the component or pending state", out.Result.Reason)
	}
}
