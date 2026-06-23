package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/internal/tools"
)

func TestToolsInstallFailsOnUnsupportedChecksumAlgorithm(t *testing.T) {
	repo := toolsTestRepo(t)
	comp := &fileFixture{componentID: "alpha", archiveRel: "alpha/tool.txt", destRel: ".kura-tools/alpha.txt"}
	content := []byte("x\n")
	manifest := tools.ArchiveManifest{SchemaVersion: 1, Components: map[string]tools.ArchiveManifestComponent{"alpha": {Files: map[string]string{comp.archiveRel: tools.SHA256Hex(content)}}}}
	archive := makeToolsArchive(t, manifest, map[string][]byte{comp.archiveRel: content})
	sidecar := makeSidecar(t, fixtureVersion, fixtureArchiveName, "sha512", archive)
	deps := toolsDeps{registry: mustToolsRegistry(t, comp), version: fixtureVersion, fetcher: &fakeFetcher{sidecar: sidecar, archive: archive}}

	out, err := runToolsCLI(t, repo, deps, "install", "alpha")
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(out, "failed") || !strings.Contains(strings.ToLower(out), "checksum algorithm") {
		t.Fatalf("install should fail with unsupported checksum algorithm:\n%s", out)
	}
	assertPathMissing(t, installedJSONPath(repo))
}

func TestToolsInstallFailsOnSidecarVersionMismatch(t *testing.T) {
	repo := toolsTestRepo(t)
	comp := &fileFixture{componentID: "alpha", archiveRel: "alpha/tool.txt", destRel: ".kura-tools/alpha.txt"}
	content := []byte("x\n")
	manifest := tools.ArchiveManifest{SchemaVersion: 1, Components: map[string]tools.ArchiveManifestComponent{"alpha": {Files: map[string]string{comp.archiveRel: tools.SHA256Hex(content)}}}}
	archive := makeToolsArchive(t, manifest, map[string][]byte{comp.archiveRel: content})
	// The sidecar is fetched for 1.2.3 but its internal version says 9.9.9.
	sidecar := makeSidecar(t, "9.9.9", fixtureArchiveName, tools.ChecksumAlgorithmSHA256, archive)
	fetcher := &fakeFetcher{sidecar: sidecar, archive: archive}
	deps := toolsDeps{registry: mustToolsRegistry(t, comp), version: fixtureVersion, fetcher: fetcher}

	out, err := runToolsCLI(t, repo, deps, "install", "alpha")
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(strings.ToLower(out), "does not match the binary release version") {
		t.Fatalf("sidecar version mismatch should fail the install:\n%s", out)
	}
	// The mismatch is caught before downloading/extracting the archive.
	if fetcher.archiveCalls != 0 {
		t.Fatalf("version mismatch must fail before extraction; archiveCalls=%d", fetcher.archiveCalls)
	}
	assertPathMissing(t, installedJSONPath(repo))
}

func TestToolsInstallFailsOnChecksumMismatch(t *testing.T) {
	repo := toolsTestRepo(t)
	comp := &fileFixture{componentID: "alpha", archiveRel: "alpha/tool.txt", destRel: ".kura-tools/alpha.txt"}
	content := []byte("x\n")
	manifest := tools.ArchiveManifest{SchemaVersion: 1, Components: map[string]tools.ArchiveManifestComponent{"alpha": {Files: map[string]string{comp.archiveRel: tools.SHA256Hex(content)}}}}
	archive := makeToolsArchive(t, manifest, map[string][]byte{comp.archiveRel: content})
	// Sidecar advertises a checksum for a different archive.
	sidecar := makeSidecar(t, fixtureVersion, fixtureArchiveName, tools.ChecksumAlgorithmSHA256, []byte("different bytes"))
	deps := toolsDeps{registry: mustToolsRegistry(t, comp), version: fixtureVersion, fetcher: &fakeFetcher{sidecar: sidecar, archive: archive}}

	out, err := runToolsCLI(t, repo, deps, "install", "alpha")
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(strings.ToLower(out), "checksum mismatch") {
		t.Fatalf("install should fail with checksum mismatch:\n%s", out)
	}
	assertPathMissing(t, installedJSONPath(repo))
}

func TestToolsArchiveManifestPerFileChecksumReadable(t *testing.T) {
	// The framework must expose the archive manifest's component-to-asset
	// mapping and per-file checksums to component implementations.
	repo := toolsTestRepo(t)
	content := []byte("inspect me\n")
	fetcher, _ := fixtureAssets(t, content)

	var seen tools.ArchiveManifestComponent
	var sawAsset bool
	inspector := componentFunc{
		idVal: "inspect",
		installFn: func(ctx tools.InstallContext) tools.Outcome {
			ac, ok := ctx.Asset.ComponentAssets("alpha")
			seen, sawAsset = ac, ok
			return tools.Outcome{Result: tools.Result{Component: "inspect", Action: tools.ActionSkipped, Reason: "inspection only"}}
		},
	}
	deps := toolsDeps{registry: mustToolsRegistry(t, inspector), version: fixtureVersion, fetcher: fetcher}
	if _, err := runToolsCLI(t, repo, deps, "install", "inspect"); err != nil {
		t.Fatalf("install inspect: %v", err)
	}
	if !sawAsset {
		t.Fatal("component could not read the archive manifest mapping for alpha")
	}
	if seen.Files["alpha/tool.txt"] != tools.SHA256Hex(content) {
		t.Fatalf("per-file checksum = %q, want %q", seen.Files["alpha/tool.txt"], tools.SHA256Hex(content))
	}
}

func TestToolsResolveIgnoresCorruptCache(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("y\n"))
	deps := toolsDeps{registry: mustToolsRegistry(t, comp), version: fixtureVersion, fetcher: fetcher}

	// Seed a corrupt cache metadata file at the known path.
	cacheDir := filepath.Join(repo, ".git", "kura", "tools", "cache", fixtureVersion)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "cache.json"), []byte("{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runToolsCLI(t, repo, deps, "install", "alpha")
	if err != nil {
		t.Fatalf("install with corrupt cache: %v", err)
	}
	if !strings.Contains(out, "created") {
		t.Fatalf("corrupt cache should be ignored and asset re-downloaded:\n%s", out)
	}
	if fetcher.archiveCalls != 1 {
		t.Fatalf("archive should be downloaded after corrupt cache; archiveCalls=%d", fetcher.archiveCalls)
	}
}
