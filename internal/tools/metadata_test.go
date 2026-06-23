package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireLockFailsUnderFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireLock(filepath.Join(f, "sub", "l.lock"), 0); err == nil {
		t.Fatal("acquireLock under a file should fail")
	}
}

func TestReadMetadataVariants(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	store, err := ReadMetadata(missing)
	if err != nil {
		t.Fatalf("ReadMetadata missing: %v", err)
	}
	if store.SchemaVersion != metadataSchemaVersion || len(store.Components) != 0 {
		t.Fatalf("missing store = %#v", store)
	}

	badJSON := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(badJSON, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMetadata(badJSON); err == nil || !strings.Contains(err.Error(), "parse tools metadata") {
		t.Fatalf("bad json err = %v", err)
	}

	badSchema := filepath.Join(t.TempDir(), "bad-schema.json")
	if err := os.WriteFile(badSchema, []byte(`{"schemaVersion":1,"components":{"x":{"component":"x","managedMode":"bad"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMetadata(badSchema); err == nil || !strings.Contains(err.Error(), "does not conform") {
		t.Fatalf("bad schema err = %v", err)
	}
}

func TestWriteMetadataInitializesNilComponents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "installed.json")
	if err := WriteMetadata(path, MetadataStore{}); err != nil {
		t.Fatalf("WriteMetadata: %v", err)
	}
	store, err := ReadMetadata(path)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if store.SchemaVersion != metadataSchemaVersion || store.Components == nil || len(store.Components) != 0 {
		t.Fatalf("store = %#v", store)
	}
}

func TestWriteMetadataRejectsInvalidStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed.json")
	err := WriteMetadata(path, MetadataStore{Components: map[string]MetadataEntry{
		"bad": {Component: "bad", ManagedMode: "bogus"},
	}})
	if err == nil || !strings.Contains(err.Error(), "refusing to write") {
		t.Fatalf("WriteMetadata invalid err = %v", err)
	}
}

func TestMetadataFilesystemErrors(t *testing.T) {
	dirPath := filepath.Join(t.TempDir(), "metadata-dir")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMetadata(dirPath); err == nil || !strings.Contains(err.Error(), "read tools metadata") {
		t.Fatalf("ReadMetadata directory err = %v", err)
	}

	fileAsParent := filepath.Join(t.TempDir(), "file-parent")
	if err := os.WriteFile(fileAsParent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteMetadata(filepath.Join(fileAsParent, "installed.json"), MetadataStore{}); err == nil || !strings.Contains(err.Error(), "create tools metadata dir") {
		t.Fatalf("WriteMetadata under file err = %v", err)
	}
}
