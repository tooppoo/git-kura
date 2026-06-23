package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubTransport is an http.RoundTripper that returns a fixed status and body.
type stubTransport struct {
	status int
	body   []byte
}

func (s stubTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.status,
		Status:     http.StatusText(s.status),
		Body:       io.NopCloser(bytes.NewReader(s.body)),
		Header:     make(http.Header),
	}, nil
}

// errTransport always returns a transport-level error.
type errTransport struct{}

func (errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport boom")
}

func TestGithubReleaseFetcher(t *testing.T) {
	body := []byte("payload")
	g := GithubReleaseFetcher{Client: &http.Client{Transport: stubTransport{status: http.StatusOK, body: body}}}
	if got, err := g.FetchSidecar("v1.2.3", "1.2.3"); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("FetchSidecar = %q, %v", got, err)
	}
	if got, err := g.FetchArchive("v1.2.3", "git-kura-tools_1.2.3.tar.gz"); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("FetchArchive = %q, %v", got, err)
	}
	if url := g.DownloadURL("v1.2.3", "asset.tar.gz"); url != "https://github.com/tooppoo/git-kura/releases/download/v1.2.3/asset.tar.gz" {
		t.Fatalf("DownloadURL = %q", url)
	}

	bad := GithubReleaseFetcher{Client: &http.Client{Transport: stubTransport{status: http.StatusNotFound, body: nil}}}
	if _, err := bad.FetchSidecar("v9.9.9", "9.9.9"); err == nil {
		t.Fatal("expected error on 404")
	}
	if NewGithubReleaseFetcher().Client == nil {
		t.Fatal("NewGithubReleaseFetcher should set a client")
	}
}

func TestGithubFetcherTransportError(t *testing.T) {
	g := GithubReleaseFetcher{Client: &http.Client{Transport: errTransport{}}}
	if _, err := g.get("https://example.invalid/x"); err == nil {
		t.Fatal("transport error should propagate")
	}
	origDefault := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: errTransport{}}
	t.Cleanup(func() { http.DefaultClient = origDefault })
	if _, err := (GithubReleaseFetcher{}).get("https://example.invalid/default-client"); err == nil {
		t.Fatal("zero-value fetcher should use http.DefaultClient and propagate transport errors")
	}
}

// --- assetResolver tests -----------------------------------------------

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func makeTestArchive(t *testing.T, manifest ArchiveManifest, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	write := func(name string, data []byte) {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	mj, _ := json.Marshal(manifest)
	write(ArchiveManifestName, mj)
	for name, data := range files {
		write(name, data)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeTestSidecar(t *testing.T, version, archiveName, algorithm string, archive []byte) []byte {
	t.Helper()
	s := SidecarManifest{
		ArchiveName:       archiveName,
		ArchiveChecksum:   SHA256Hex(archive),
		ChecksumAlgorithm: algorithm,
		Version:           version,
	}
	b, _ := json.Marshal(s)
	return b
}

func TestToolsResolveIgnoresCorruptCache(t *testing.T) {
	dir := t.TempDir()
	content := []byte("y\n")
	manifest := ArchiveManifest{SchemaVersion: 1, Components: map[string]ArchiveManifestComponent{
		"alpha": {Files: map[string]string{"alpha/tool.txt": SHA256Hex(content)}},
	}}
	archive := makeTestArchive(t, manifest, map[string][]byte{"alpha/tool.txt": content})
	sidecar := makeTestSidecar(t, "1.2.3", "git-kura-tools_1.2.3.tar.gz", ChecksumAlgorithmSHA256, archive)
	fetcher := &fakeFetcher{sidecar: sidecar, archive: archive}

	r := &assetResolver{version: "1.2.3", commonDir: dir, fetcher: fetcher}
	cacheDir := r.cacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheMetadataPath(cacheDir), []byte("{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}

	asset, err := r.resolve()
	if err != nil {
		t.Fatalf("resolve with corrupt cache: %v", err)
	}
	if asset == nil {
		t.Fatal("expected asset")
	}
	if fetcher.archiveCalls != 1 {
		t.Fatalf("archive should be downloaded after corrupt cache; archiveCalls=%d", fetcher.archiveCalls)
	}
}

func TestToolsResolveUsesValidCache(t *testing.T) {
	dir := t.TempDir()
	content := []byte("cached\n")
	manifest := ArchiveManifest{SchemaVersion: 1, Components: map[string]ArchiveManifestComponent{
		"alpha": {Files: map[string]string{"alpha/tool.txt": SHA256Hex(content)}},
	}}
	archive := makeTestArchive(t, manifest, map[string][]byte{"alpha/tool.txt": content})
	sidecar := makeTestSidecar(t, "1.2.3", "git-kura-tools_1.2.3.tar.gz", ChecksumAlgorithmSHA256, archive)
	fetcher := &fakeFetcher{sidecar: sidecar, archive: archive}

	first := &assetResolver{version: "1.2.3", commonDir: dir, fetcher: fetcher}
	if _, err := first.resolve(); err != nil {
		t.Fatalf("initial resolve: %v", err)
	}
	if fetcher.archiveCalls != 1 {
		t.Fatalf("initial archiveCalls = %d, want 1", fetcher.archiveCalls)
	}

	cachedFetcher := &fakeFetcher{sidecar: sidecar, archiveErr: errors.New("archive should not be fetched")}
	second := &assetResolver{version: "1.2.3", commonDir: dir, fetcher: cachedFetcher}
	asset, err := second.resolve()
	if err != nil {
		t.Fatalf("cached resolve: %v", err)
	}
	if cachedFetcher.archiveCalls != 0 {
		t.Fatalf("cached archiveCalls = %d, want 0", cachedFetcher.archiveCalls)
	}
	if got := string(mustReadFile(t, asset.Path("alpha/tool.txt"))); got != "cached\n" {
		t.Fatalf("cached asset content = %q", got)
	}
}

type fakeFetcher struct {
	sidecar      []byte
	archive      []byte
	sidecarErr   error
	archiveErr   error
	sidecarCalls int
	archiveCalls int
}

func (f *fakeFetcher) FetchSidecar(tag, version string) ([]byte, error) {
	f.sidecarCalls++
	if f.sidecarErr != nil {
		return nil, f.sidecarErr
	}
	return f.sidecar, nil
}

func (f *fakeFetcher) FetchArchive(tag, name string) ([]byte, error) {
	f.archiveCalls++
	if f.archiveErr != nil {
		return nil, f.archiveErr
	}
	return f.archive, nil
}

func TestToolsResolveErrorPaths(t *testing.T) {
	dir := t.TempDir()
	good := makeTestSidecar(t, "1.2.3", "a.tar.gz", ChecksumAlgorithmSHA256, []byte("ignored"))
	badChecksumSidecar := makeTestSidecar(t, "1.2.3", "a.tar.gz", ChecksumAlgorithmSHA256, []byte("different"))

	cases := []struct {
		name    string
		fetcher *fakeFetcher
	}{
		{"sidecar fetch error", &fakeFetcher{sidecarErr: errors.New("net down")}},
		{"sidecar parse error", &fakeFetcher{sidecar: []byte("not json")}},
		{"sidecar version mismatch", &fakeFetcher{sidecar: mustJSON(SidecarManifest{ChecksumAlgorithm: ChecksumAlgorithmSHA256, Version: "9.9.9", ArchiveName: "a.tar.gz", ArchiveChecksum: "abc"})}},
		{"sidecar algorithm mismatch", &fakeFetcher{sidecar: mustJSON(SidecarManifest{ChecksumAlgorithm: "sha512", Version: "1.2.3", ArchiveName: "a.tar.gz", ArchiveChecksum: "abc"})}},
		{"missing archive fields", &fakeFetcher{sidecar: mustJSON(SidecarManifest{ChecksumAlgorithm: ChecksumAlgorithmSHA256, Version: "1.2.3"})}},
		{"archive fetch error", &fakeFetcher{sidecar: good, archiveErr: errors.New("net down")}},
		{"archive checksum mismatch", &fakeFetcher{sidecar: badChecksumSidecar, archive: []byte("actual")}},
	}
	for _, tc := range cases {
		r := &assetResolver{version: "1.2.3", commonDir: filepath.Join(dir, tc.name), fetcher: tc.fetcher}
		if _, err := r.resolve(); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}

func TestToolsResolveRejectsNonReleaseVersion(t *testing.T) {
	r := &assetResolver{version: "dev", commonDir: t.TempDir(), fetcher: &fakeFetcher{}}
	if _, err := r.resolve(); err == nil || !strings.Contains(err.Error(), "official release") {
		t.Fatalf("resolve dev version err = %v, want official release error", err)
	}
}

func TestToolsResolveRejectsInconsistentCache(t *testing.T) {
	dir := t.TempDir()
	sidecar := SidecarManifest{
		Version:           "1.2.3",
		ArchiveName:       "git-kura-tools_1.2.3.tar.gz",
		ArchiveChecksum:   "release-sum",
		ChecksumAlgorithm: ChecksumAlgorithmSHA256,
	}
	r := &assetResolver{version: "1.2.3", commonDir: dir, fetcher: &fakeFetcher{}}
	cacheDir := r.cacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := cacheMetadata{
		ReleaseVersion:    "1.2.3",
		ArchiveName:       sidecar.ArchiveName,
		ArchiveChecksum:   "cached-sum",
		ChecksumAlgorithm: ChecksumAlgorithmSHA256,
	}
	if err := os.WriteFile(cacheMetadataPath(cacheDir), mustJSON(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := r.tryCache(cacheDir, sidecar); err == nil || ok {
		t.Fatalf("tryCache inconsistent = ok %v err %v, want error", ok, err)
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

	sidecar := makeTestSidecar(t, "1.2.3", "a.tar.gz", ChecksumAlgorithmSHA256, buf.Bytes())
	r := &assetResolver{version: "1.2.3", commonDir: dir, fetcher: &fakeFetcher{sidecar: sidecar, archive: buf.Bytes()}}
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
	_ = tw.WriteHeader(&tar.Header{Name: ArchiveManifestName, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(bad))})
	_, _ = tw.Write(bad)
	_ = tw.Close()
	_ = gz.Close()

	sidecar := makeTestSidecar(t, "1.2.3", "a.tar.gz", ChecksumAlgorithmSHA256, buf.Bytes())
	r := &assetResolver{version: "1.2.3", commonDir: dir, fetcher: &fakeFetcher{sidecar: sidecar, archive: buf.Bytes()}}
	if _, err := r.resolve(); err == nil {
		t.Fatal("invalid archive manifest should fail to resolve")
	}
}

func TestExtractTarGzPublicWrapper(t *testing.T) {
	archive := makeTestArchive(t, ArchiveManifest{SchemaVersion: 1}, map[string][]byte{"dir/file.txt": []byte("ok\n")})
	dest := filepath.Join(t.TempDir(), "asset")
	if err := ExtractTarGz(archive, dest); err != nil {
		t.Fatalf("ExtractTarGz: %v", err)
	}
	if got := string(mustReadFile(t, filepath.Join(dest, "dir", "file.txt"))); got != "ok\n" {
		t.Fatalf("extracted content = %q", got)
	}
}
