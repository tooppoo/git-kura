package main

import (
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/internal/tools"
)

func TestToolsPendingComponentInstallAndUninstall(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, _ := fixtureAssets(t, []byte("x\n"))
	pending := tools.PendingComponent{ComponentID: "future-tool", TrackingURL: "https://github.com/tooppoo/git-kura/issues/999"}
	deps := toolsDeps{registry: mustToolsRegistry(t, pending), version: fixtureVersion, fetcher: fetcher}

	// install of a pending component resolves and verifies the asset, then fails
	// with a tracking-issue reason; the command exits non-zero and writes no
	// metadata.
	out, err := runToolsCLI(t, repo, deps, "install", "future-tool")
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(out, "failed") || !strings.Contains(out, "not yet implemented") {
		t.Fatalf("pending install should fail with a tracking reason:\n%s", out)
	}
	assertPathMissing(t, installedJSONPath(repo))

	// uninstall of a pending component is a not-installed no-op.
	out, err = runToolsCLI(t, repo, deps, "uninstall", "future-tool")
	if err != nil {
		t.Fatalf("pending uninstall: %v", err)
	}
	if !strings.Contains(out, "not-installed") {
		t.Fatalf("pending uninstall should be not-installed:\n%s", out)
	}
}
