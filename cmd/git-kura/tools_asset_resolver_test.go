package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolsInstallFailsOnUnsupportedChecksumAlgorithm(t *testing.T) {
	repo := toolsTestRepo(t)
	comp := &fileFixture{componentID: "alpha", archiveRel: "alpha/tool.txt", destRel: ".kura-tools/alpha.txt"}
	content := []byte("x\n")
	manifest := archiveManifest{SchemaVersion: 1, Components: map[string]archiveManifestComponent{"alpha": {Files: map[string]string{comp.archiveRel: sha256hex(content)}}}}
	archive := makeToolsArchive(t, manifest, map[string][]byte{comp.archiveRel: content})
	sidecar := makeSidecar(t, fixtureVersion, fixtureArchiveName, "sha512", archive)
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: &fakeFetcher{sidecar: sidecar, archive: archive}}

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
	manifest := archiveManifest{SchemaVersion: 1, Components: map[string]archiveManifestComponent{"alpha": {Files: map[string]string{comp.archiveRel: sha256hex(content)}}}}
	archive := makeToolsArchive(t, manifest, map[string][]byte{comp.archiveRel: content})
	// The sidecar is fetched for 1.2.3 but its internal version says 9.9.9.
	sidecar := makeSidecar(t, "9.9.9", fixtureArchiveName, checksumAlgorithmSHA256, archive)
	fetcher := &fakeFetcher{sidecar: sidecar, archive: archive}
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}

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
	manifest := archiveManifest{SchemaVersion: 1, Components: map[string]archiveManifestComponent{"alpha": {Files: map[string]string{comp.archiveRel: sha256hex(content)}}}}
	archive := makeToolsArchive(t, manifest, map[string][]byte{comp.archiveRel: content})
	// Sidecar advertises a checksum for a different archive.
	sidecar := makeSidecar(t, fixtureVersion, fixtureArchiveName, checksumAlgorithmSHA256, []byte("different bytes"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: &fakeFetcher{sidecar: sidecar, archive: archive}}

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

	var seen archiveManifestComponent
	var sawAsset bool
	inspector := componentFunc{
		idVal: "inspect",
		installFn: func(ctx toolsInstallContext) toolsOutcome {
			ac, ok := ctx.asset.componentAssets("alpha")
			seen, sawAsset = ac, ok
			return toolsOutcome{result: toolsResult{Component: "inspect", Action: actionSkipped, Reason: "inspection only"}}
		},
	}
	deps := toolsDeps{registry: newToolsRegistry(inspector), version: fixtureVersion, fetcher: fetcher}
	if _, err := runToolsCLI(t, repo, deps, "install", "inspect"); err != nil {
		t.Fatalf("install inspect: %v", err)
	}
	if !sawAsset {
		t.Fatal("component could not read the archive manifest mapping for alpha")
	}
	if seen.Files["alpha/tool.txt"] != sha256hex(content) {
		t.Fatalf("per-file checksum = %q, want %q", seen.Files["alpha/tool.txt"], sha256hex(content))
	}
}

func TestToolsResolveIgnoresCorruptCache(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("y\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}

	// Seed a corrupt cache metadata file for this version.
	resolver := &toolsAssetResolver{version: fixtureVersion, commonDir: filepath.Join(repo, ".git"), fetcher: fetcher}
	cacheDir := resolver.cacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheMetadataPath(cacheDir), []byte("{ broken"), 0o644); err != nil {
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

func TestToolsResolveErrorPaths(t *testing.T) {
	dir := t.TempDir()
	good := makeSidecar(t, "1.2.3", "a.tar.gz", checksumAlgorithmSHA256, []byte("ignored"))

	cases := []struct {
		name    string
		fetcher *fakeFetcher
	}{
		{"sidecar fetch error", &fakeFetcher{sidecarErr: errors.New("net down")}},
		{"sidecar parse error", &fakeFetcher{sidecar: []byte("not json")}},
		{"missing archive fields", &fakeFetcher{sidecar: mustJSON(sidecarManifest{ChecksumAlgorithm: checksumAlgorithmSHA256, Version: "1.2.3"})}},
		{"archive fetch error", &fakeFetcher{sidecar: good, archiveErr: errors.New("net down")}},
	}
	for _, tc := range cases {
		r := &toolsAssetResolver{version: "1.2.3", commonDir: filepath.Join(dir, tc.name), fetcher: tc.fetcher}
		if _, err := r.resolve(); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}

func TestToolsResolveFailsWhenArchiveLacksManifest(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "only.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	_ = gz.Close()

	sidecar := makeSidecar(t, "1.2.3", "a.tar.gz", checksumAlgorithmSHA256, buf.Bytes())
	r := &toolsAssetResolver{version: "1.2.3", commonDir: dir, fetcher: &fakeFetcher{sidecar: sidecar, archive: buf.Bytes()}}
	if _, err := r.resolve(); err == nil {
		t.Fatal("archive without manifest.json should fail to resolve")
	}
}

func TestToolsResolveFailsOnInvalidArchiveManifest(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	bad := []byte("{ not json")
	_ = tw.WriteHeader(&tar.Header{Name: toolsArchiveManifestName, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(bad))})
	_, _ = tw.Write(bad)
	_ = tw.Close()
	_ = gz.Close()

	sidecar := makeSidecar(t, "1.2.3", "a.tar.gz", checksumAlgorithmSHA256, buf.Bytes())
	r := &toolsAssetResolver{version: "1.2.3", commonDir: dir, fetcher: &fakeFetcher{sidecar: sidecar, archive: buf.Bytes()}}
	if _, err := r.resolve(); err == nil {
		t.Fatal("invalid archive manifest should fail to resolve")
	}
}
