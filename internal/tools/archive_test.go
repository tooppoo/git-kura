package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTarGzExtractsFilesAndDirsAndSkipsSymlinks(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "sub/", Typeflag: tar.TypeDir, Mode: 0o755})
	fdata := []byte("hi")
	_ = tw.WriteHeader(&tar.Header{Name: "sub/file.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(fdata))})
	_, _ = tw.Write(fdata)
	_ = tw.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "sub/file.txt", Mode: 0o777})
	_ = tw.Close()
	_ = gz.Close()

	dest := filepath.Join(t.TempDir(), "root")
	if err := extractTarGz(buf.Bytes(), dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "sub", "file.txt"))
	if err != nil || string(got) != "hi" {
		t.Fatalf("file = %q, err = %v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(dest, "link")); !os.IsNotExist(err) {
		t.Fatalf("symlink entry should be skipped")
	}
}

func TestExtractTarGzFailsWhenFileTargetIsADir(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "x", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1})
	_, _ = tw.Write([]byte("y"))
	_ = tw.Close()
	_ = gz.Close()

	dest := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(filepath.Join(dest, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractTarGz(buf.Bytes(), dest); err == nil {
		t.Fatal("writing a file over an existing directory should fail")
	}
}

func TestExtractTarGzRejectsBackslashTraversal(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: `..\escape.txt`, Mode: 0o644, Size: 3, Typeflag: tar.TypeReg}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte("bad"))
	_ = tw.Close()
	_ = gz.Close()

	if err := extractTarGz(buf.Bytes(), filepath.Join(t.TempDir(), "root")); err == nil {
		t.Fatal("backslash entry should be rejected")
	}
}

func TestExtractTarGzRejectsInvalidGzip(t *testing.T) {
	if err := extractTarGz([]byte("not a gzip stream"), t.TempDir()); err == nil {
		t.Fatal("invalid gzip should be rejected")
	}
}

func TestExtractTarGzRejectsTraversal(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "../escape.txt", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte("bad"))
	_ = tw.Close()
	_ = gz.Close()

	dest := t.TempDir()
	if err := extractTarGz(buf.Bytes(), filepath.Join(dest, "root")); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
}
