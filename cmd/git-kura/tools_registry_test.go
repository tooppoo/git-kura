package main

import (
	"strings"
	"testing"
)

func TestToolsProductionRegistryRecognizesComponents(t *testing.T) {
	reg := productionToolsRegistry()
	got := reg.ids()
	want := []string{"pre-commit", "claude-skill", "codex-skill"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("production registry IDs = %v, want %v", got, want)
	}
	for _, id := range want {
		if _, ok := reg.get(id); !ok {
			t.Fatalf("component %q not recognized", id)
		}
	}
	// No dummy/test components must leak into the production registry.
	for _, id := range got {
		if strings.Contains(id, "dummy") || strings.Contains(id, "test") || id == "alpha" {
			t.Fatalf("unexpected component %q in production registry", id)
		}
	}
}

func TestNewToolsRegistryRejectsDuplicateID(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate component ID should panic")
		}
	}()
	newToolsRegistry(&failingFixture{componentID: "dup"}, &failingFixture{componentID: "dup"})
}

func TestToolsPendingComponentInstallAndUninstall(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, _ := fixtureAssets(t, []byte("x\n"))
	// Use a dedicated pending component directly rather than the production
	// registry, since all production components are now implemented.
	pending := newPendingComponent("future-tool", "https://github.com/tooppoo/git-kura/issues/999")
	deps := toolsDeps{registry: newToolsRegistry(pending), version: fixtureVersion, fetcher: fetcher}

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
