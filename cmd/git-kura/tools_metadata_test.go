package main

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestToolsCorruptMetadataRefusesDestructiveOps(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("x\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}

	// Write a corrupt metadata store.
	storeFile := installedJSONPath(repo)
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storeFile, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := runToolsCLI(t, repo, deps, "uninstall", "alpha")
	requireToolsExit(t, err, exitGeneralError)
	// The corrupt store is left as-is; no destructive change happened.
	data, _ := os.ReadFile(storeFile)
	if string(data) != "{ not json" {
		t.Fatalf("corrupt store should be untouched, got %q", data)
	}
}

func TestToolsStatusFailsOnCorruptMetadata(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := toolsDeps{registry: productionToolsRegistry(), version: fixtureVersion, fetcher: &fakeFetcher{}}
	storeFile := installedJSONPath(repo)
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storeFile, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runToolsCLI(t, repo, deps, "status"); err == nil {
		t.Fatal("status should fail when metadata cannot be read")
	}
}

func TestToolsInstallFailsWhenLockHeld(t *testing.T) {
	repo := toolsTestRepo(t)
	// A zero timeout makes a single lock attempt with no retry.
	git(t, repo, "config", sealLockTimeoutConfigKey, "0")
	fetcher, comp := fixtureAssets(t, []byte("x\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}

	_, lockFile, err := toolsMetadataPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(lockFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = runToolsCLI(t, repo, deps, "install", "alpha")
	requireToolsExit(t, err, exitSealLockTimeout)
	assertPathMissing(t, installedJSONPath(repo))
	assertPathMissing(t, comp.destAbs(repo))
}

func TestToolsMetadataRoundTripAndValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "installed.json")

	// Absent store reads as empty.
	s, err := readToolsMetadata(path)
	if err != nil || len(s.Components) != 0 {
		t.Fatalf("absent store: %v, %+v", err, s)
	}

	// Round-trip a valid entry.
	s.Components["x"] = toolsMetadataEntry{Component: "x", ManagedMode: managedModeFile, CreatedAt: "t0", UpdatedAt: "t1"}
	if err := writeToolsMetadata(path, s); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readToolsMetadata(path)
	if err != nil || got.Components["x"].Component != "x" {
		t.Fatalf("round-trip: %v, %+v", err, got)
	}

	// An unsupported schemaVersion is rejected.
	if err := os.WriteFile(path, []byte(`{"schemaVersion":2,"components":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readToolsMetadata(path); err == nil {
		t.Fatal("unsupported schemaVersion should be rejected")
	}

	// Non-JSON is rejected.
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readToolsMetadata(path); err == nil {
		t.Fatal("non-JSON store should be rejected")
	}

	// writeToolsMetadata refuses a store that violates the schema (managedMode
	// is required and constrained to file/config).
	bad := toolsMetadataStore{Components: map[string]toolsMetadataEntry{
		"y": {Component: "y", CreatedAt: "t0", UpdatedAt: "t1"},
	}}
	if err := writeToolsMetadata(filepath.Join(dir, "bad.json"), bad); err == nil {
		t.Fatal("writing an invalid entry should be refused")
	}
}

func TestToolsHelpersFailWhenParentIsAFile(t *testing.T) {
	// A regular file cannot have children, so directory creation and reads under
	// it fail deterministically — exercising the I/O error branches.
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	under := filepath.Join(f, "sub", "x.json")

	if _, err := readToolsMetadata(under); err == nil {
		t.Fatal("readToolsMetadata under a file should fail")
	}
	if err := writeToolsMetadata(under, toolsMetadataStore{}); err == nil {
		t.Fatal("writeToolsMetadata under a file should fail")
	}
	if _, err := acquireToolsLock(filepath.Join(f, "sub", "l.lock"), 0); err == nil {
		t.Fatal("acquireToolsLock under a file should fail")
	}

	var empty bytes.Buffer
	gz := gzip.NewWriter(&empty)
	_ = gz.Close()
	if err := extractTarGz(empty.Bytes(), filepath.Join(f, "root")); err == nil {
		t.Fatal("extractTarGz under a file should fail")
	}
}
