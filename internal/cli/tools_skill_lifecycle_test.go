package cli

import (
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/internal/tools"
)

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
	out, err = runToolsCLI(t, repo, deps, "status", tools.ClaudeSkillComponentID, tools.CodexSkillComponentID)
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
