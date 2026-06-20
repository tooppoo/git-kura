package main

import (
	"os"
	"strings"
	"testing"
)

func TestToolsInstallCreatesThenIsIdempotent(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("hello tool\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}

	out, err := runToolsCLI(t, repo, deps, "install", "alpha")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(out, "created") {
		t.Fatalf("first install should be created:\n%s", out)
	}
	assertPathExists(t, comp.destAbs(repo))
	assertPathExists(t, installedJSONPath(repo))

	// Second install with unchanged asset is idempotent (skipped), and the
	// archive is served from cache (no second download).
	out, err = runToolsCLI(t, repo, deps, "install", "alpha")
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if !strings.Contains(out, "skipped") {
		t.Fatalf("second install should be skipped:\n%s", out)
	}
	if fetcher.archiveCalls != 1 {
		t.Fatalf("archive should be downloaded once and then cached; archiveCalls=%d", fetcher.archiveCalls)
	}
}

func TestToolsInstallUpdatesAcrossVersions(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssetsForVersion(t, "1.2.3", []byte("v1\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: "1.2.3", fetcher: fetcher}

	if _, err := runToolsCLI(t, repo, deps, "install", "alpha"); err != nil {
		t.Fatalf("install 1.2.3: %v", err)
	}

	// A newer binary version ships a different asset. This uses a distinct cache
	// directory, so it is a legitimate update rather than a same-version release
	// inconsistency.
	fetcher2, comp2 := fixtureAssetsForVersion(t, "1.2.4", []byte("v2\n"))
	deps2 := toolsDeps{registry: newToolsRegistry(comp2), version: "1.2.4", fetcher: fetcher2}
	out, err := runToolsCLI(t, repo, deps2, "install", "alpha")
	if err != nil {
		t.Fatalf("install 1.2.4: %v", err)
	}
	if !strings.Contains(out, "updated") {
		t.Fatalf("install of a newer version should be updated:\n%s", out)
	}
	data, _ := os.ReadFile(comp2.destAbs(repo))
	if string(data) != "v2\n" {
		t.Fatalf("destination = %q, want updated content", data)
	}
}

func TestToolsInstallFailsOnSameVersionCacheInconsistency(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("v1\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}
	if _, err := runToolsCLI(t, repo, deps, "install", "alpha"); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// The same version now advertises a different archive checksum: a release
	// asset must never change, so install must fail rather than refresh or use
	// the cache.
	tampered := makeSidecar(t, fixtureVersion, fixtureArchiveName, checksumAlgorithmSHA256, []byte("different release bytes"))
	badFetcher := &fakeFetcher{sidecar: tampered, archive: fetcher.archive}
	deps2 := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: badFetcher}

	out, err := runToolsCLI(t, repo, deps2, "install", "alpha")
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(strings.ToLower(out), "inconsistent") {
		t.Fatalf("same-version checksum change should fail as a release inconsistency:\n%s", out)
	}
	// The cache must not be used or replaced: no re-download happened.
	if badFetcher.archiveCalls != 0 {
		t.Fatalf("inconsistent cache must not be replaced; archiveCalls=%d", badFetcher.archiveCalls)
	}
	// The original install is untouched.
	data, _ := os.ReadFile(comp.destAbs(repo))
	if string(data) != "v1\n" {
		t.Fatalf("destination = %q, want unchanged v1 content", data)
	}
}

func TestToolsUninstallRemovesManagedFile(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("x\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}

	if _, err := runToolsCLI(t, repo, deps, "install", "alpha"); err != nil {
		t.Fatalf("install: %v", err)
	}
	out, err := runToolsCLI(t, repo, deps, "uninstall", "alpha")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !strings.Contains(out, "removed") {
		t.Fatalf("uninstall should remove:\n%s", out)
	}
	assertPathMissing(t, comp.destAbs(repo))

	// Uninstalling again is a not-installed no-op.
	out, err = runToolsCLI(t, repo, deps, "uninstall", "alpha")
	if err != nil {
		t.Fatalf("second uninstall: %v", err)
	}
	if !strings.Contains(out, "not-installed") {
		t.Fatalf("second uninstall should be not-installed:\n%s", out)
	}
}

func TestToolsUninstallSkipsUserModifiedFile(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("original\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}

	if _, err := runToolsCLI(t, repo, deps, "install", "alpha"); err != nil {
		t.Fatalf("install: %v", err)
	}
	// User edits the managed file out from under git-kura.
	if err := os.WriteFile(comp.destAbs(repo), []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runToolsCLI(t, repo, deps, "uninstall", "alpha")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !strings.Contains(out, "skipped") {
		t.Fatalf("uninstall of modified file should skip:\n%s", out)
	}
	assertPathExists(t, comp.destAbs(repo))
	// Skipped is not a failure: metadata is retained so the file stays managed.
	assertPathExists(t, installedJSONPath(repo))
}

func TestToolsConfigManagedUninstall(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, _ := fixtureAssets(t, []byte("ignored\n"))
	cfg := &configFixture{componentID: "cfg", configKey: "kura.fixtureValue", value: "managed"}
	deps := toolsDeps{registry: newToolsRegistry(cfg), version: fixtureVersion, fetcher: fetcher}

	if _, err := runToolsCLI(t, repo, deps, "install", "cfg"); err != nil {
		t.Fatalf("install cfg: %v", err)
	}
	// Metadata records config managed mode.
	store := requireJSONFile(t, installedJSONPath(repo))
	comps := store["components"].(map[string]any)
	entry := comps["cfg"].(map[string]any)
	if entry["managedMode"] != managedModeConfig {
		t.Fatalf("managedMode = %v, want %q", entry["managedMode"], managedModeConfig)
	}

	// A user changes the value: uninstall must not revert it.
	git(t, repo, "config", "kura.fixtureValue", "user-set")
	out, err := runToolsCLI(t, repo, deps, "uninstall", "cfg")
	if err != nil {
		t.Fatalf("uninstall cfg: %v", err)
	}
	if !strings.Contains(out, "skipped") {
		t.Fatalf("uninstall of user-changed config should skip:\n%s", out)
	}
	if v, _ := cfg.currentValue(repo); v != "user-set" {
		t.Fatalf("config value = %q, want it left untouched", v)
	}
}

func TestToolsMultipleComponentsContinueAfterFailure(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("ok\n"))
	fail := &failingFixture{componentID: "boom"}
	deps := toolsDeps{registry: newToolsRegistry(fail, comp), version: fixtureVersion, fetcher: fetcher}

	out, err := runToolsCLI(t, repo, deps, "install", "boom", "alpha")
	// At least one failure means non-zero exit.
	requireToolsExit(t, err, exitGeneralError)
	// But the non-failing component is still processed.
	if !strings.Contains(out, "created") {
		t.Fatalf("alpha should still be installed despite boom failing:\n%s", out)
	}
	assertPathExists(t, comp.destAbs(repo))
}

func TestToolsInstallFailsOnNonReleaseBinary(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("x\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: "dev", fetcher: fetcher}

	out, err := runToolsCLI(t, repo, deps, "install", "alpha")
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(out, "failed") {
		t.Fatalf("install on dev build should fail:\n%s", out)
	}
	if fetcher.sidecarCalls != 0 {
		t.Fatalf("non-release build must not fetch any asset; sidecarCalls=%d", fetcher.sidecarCalls)
	}
	assertPathMissing(t, installedJSONPath(repo))
}
