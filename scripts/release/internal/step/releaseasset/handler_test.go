package releaseasset

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- isSHA256Hex -------------------------------------------------------------

func TestIsSHA256Hex(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{strings.Repeat("a", 64), true},
		{strings.Repeat("f", 64), true},
		{"0123456789abcdef" + strings.Repeat("a", 48), true},
		{strings.Repeat("A", 64), false}, // uppercase rejected
		{strings.Repeat("a", 63), false}, // too short
		{strings.Repeat("a", 65), false}, // too long
		{"", false},
		{strings.Repeat("g", 64), false}, // 'g' is not hex
	}
	for _, tc := range cases {
		got := isSHA256Hex(tc.s)
		if got != tc.want {
			t.Errorf("isSHA256Hex(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

// --- hasChecksumEntry --------------------------------------------------------

func TestHasChecksumEntry(t *testing.T) {
	validHash := strings.Repeat("a", 64)
	cases := []struct {
		name     string
		content  string
		filename string
		want     bool
	}{
		{
			name:     "exact match with valid sha256",
			content:  validHash + "  somefile.tar.gz",
			filename: "somefile.tar.gz",
			want:     true,
		},
		{
			name:     "filename mismatch",
			content:  validHash + "  other.tar.gz",
			filename: "somefile.tar.gz",
			want:     false,
		},
		{
			name:     "hash too short — rejected even with correct filename",
			content:  "deadbeef  somefile.tar.gz",
			filename: "somefile.tar.gz",
			want:     false,
		},
		{
			name:     "uppercase hash — rejected (GoReleaser outputs lowercase)",
			content:  strings.Repeat("A", 64) + "  somefile.tar.gz",
			filename: "somefile.tar.gz",
			want:     false,
		},
		{
			name:     "non-hex character in hash",
			content:  strings.Repeat("g", 64) + "  somefile.tar.gz",
			filename: "somefile.tar.gz",
			want:     false,
		},
		{
			name:     "match in multiline content",
			content:  validHash + "  other.tar.gz\n" + validHash + "  target.zip",
			filename: "target.zip",
			want:     true,
		},
		{
			name:     "empty content",
			content:  "",
			filename: "somefile.tar.gz",
			want:     false,
		},
		{
			name:     "three fields — not a valid entry",
			content:  validHash + "  extra  somefile.tar.gz",
			filename: "somefile.tar.gz",
			want:     false,
		},
		{
			name:     "only one field — no filename",
			content:  validHash,
			filename: "somefile.tar.gz",
			want:     false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hasChecksumEntry(tc.content, tc.filename)
			if got != tc.want {
				t.Errorf("hasChecksumEntry: got %v, want %v", got, tc.want)
			}
		})
	}
}

// --- checkAssetExists --------------------------------------------------------

func TestCheckAssetExists(t *testing.T) {
	assetMap := map[string]githubAsset{
		"found.tar.gz": {
			Name:               "found.tar.gz",
			State:              "uploaded",
			BrowserDownloadURL: "https://example.com/found.tar.gz",
		},
		"not-uploaded.tar.gz": {
			Name:               "not-uploaded.tar.gz",
			State:              "open",
			BrowserDownloadURL: "https://example.com/not-uploaded.tar.gz",
		},
		"no-url.tar.gz": {
			Name:  "no-url.tar.gz",
			State: "uploaded",
		},
	}

	t.Run("not found", func(t *testing.T) {
		r := checkAssetExists(assetMap, "missing.tar.gz", "platform-archive")
		if r.Status != statusFail {
			t.Errorf("want failure, got %s", r.Status)
		}
	})
	t.Run("state not uploaded", func(t *testing.T) {
		r := checkAssetExists(assetMap, "not-uploaded.tar.gz", "platform-archive")
		if r.Status != statusFail {
			t.Errorf("want failure for state!=uploaded, got %s (error: %s)", r.Status, r.Error)
		}
		if r.BrowserDownloadURL != "" {
			t.Errorf("want no URL on metadata failure, got %s", r.BrowserDownloadURL)
		}
	})
	t.Run("empty browser_download_url", func(t *testing.T) {
		r := checkAssetExists(assetMap, "no-url.tar.gz", "platform-archive")
		if r.Status != statusFail {
			t.Errorf("want failure for empty URL, got %s (error: %s)", r.Status, r.Error)
		}
	})
	t.Run("success", func(t *testing.T) {
		r := checkAssetExists(assetMap, "found.tar.gz", "platform-archive")
		if r.Status != statusOK {
			t.Errorf("want success, got %s (error: %s)", r.Status, r.Error)
		}
		if r.BrowserDownloadURL == "" {
			t.Error("want non-empty BrowserDownloadURL on success")
		}
		if r.AssetKind != "platform-archive" {
			t.Errorf("want kind=platform-archive, got %s", r.AssetKind)
		}
	})
}

// --- goreleaerVersion --------------------------------------------------------

func TestGoreleaerVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v0.0.7", "0.0.7"},
		{"v1.2.3", "1.2.3"},
		{"0.0.7", "0.0.7"}, // no-op when already stripped
	}
	for _, tc := range cases {
		got := goreleaerVersion(tc.in)
		if got != tc.want {
			t.Errorf("goreleaerVersion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- archiveFilename ---------------------------------------------------------

func TestArchiveFilename(t *testing.T) {
	cases := []struct {
		version, goos, goarch, ext, want string
	}{
		{"v0.0.7", "linux", "amd64", "tar.gz", "git-kura_v0.0.7_Linux_x86_64.tar.gz"},
		{"v0.0.7", "linux", "arm64", "tar.gz", "git-kura_v0.0.7_Linux_arm64.tar.gz"},
		{"v0.0.7", "darwin", "amd64", "tar.gz", "git-kura_v0.0.7_Darwin_x86_64.tar.gz"},
		{"v0.0.7", "darwin", "arm64", "tar.gz", "git-kura_v0.0.7_Darwin_arm64.tar.gz"},
		{"v0.0.7", "windows", "amd64", "zip", "git-kura_v0.0.7_Windows_x86_64.zip"},
		{"v0.0.7", "windows", "arm64", "zip", "git-kura_v0.0.7_Windows_arm64.zip"},
	}
	for _, tc := range cases {
		got := archiveFilename(tc.version, tc.goos, tc.goarch, tc.ext)
		if got != tc.want {
			t.Errorf("archiveFilename(%q,%q,%q,%q) = %q, want %q",
				tc.version, tc.goos, tc.goarch, tc.ext, got, tc.want)
		}
	}
}

// --- buildPlanPayload --------------------------------------------------------

func TestBuildPlanPayload(t *testing.T) {
	version := "v0.0.7"
	p := buildPlanPayload(version)

	if len(p.PlatformArchives) != 6 {
		t.Errorf("want 6 platform archives, got %d", len(p.PlatformArchives))
	}
	if len(p.SBOMAssets) != 6 {
		t.Errorf("want 6 SBOM assets, got %d", len(p.SBOMAssets))
	}
	if len(p.PackageManagerWindowsArchives) != 2 {
		t.Errorf("want 2 package-manager Windows archives, got %d", len(p.PackageManagerWindowsArchives))
	}
	if p.ChecksumFile != "checksums.txt" {
		t.Errorf("checksumFile = %q, want checksums.txt", p.ChecksumFile)
	}
	if p.SignatureFile != "checksums.txt.sigstore.json" {
		t.Errorf("signatureFile = %q, want checksums.txt.sigstore.json", p.SignatureFile)
	}
	// Tools archive and sidecar use the goreleaser version (no v prefix).
	if p.ToolsArchive != "git-kura-tools_0.0.7.tar.gz" {
		t.Errorf("toolsArchive = %q, want git-kura-tools_0.0.7.tar.gz", p.ToolsArchive)
	}
	if p.ToolsSidecar != "git-kura-tools_0.0.7.json" {
		t.Errorf("toolsSidecar = %q, want git-kura-tools_0.0.7.json", p.ToolsSidecar)
	}
	// SBOM names must follow the convention <archive>.sbom.json.
	for _, sa := range p.SBOMAssets {
		want := sa.ArchiveFilename + ".sbom.json"
		if sa.SBOMFilename != want {
			t.Errorf("SBOM for %q: got %q, want %q", sa.ArchiveFilename, sa.SBOMFilename, want)
		}
	}
	// Windows package-manager archives must be a subset of platform archives.
	winFiles := map[string]bool{}
	for _, pa := range p.PlatformArchives {
		if pa.OS == "windows" {
			winFiles[pa.Filename] = true
		}
	}
	for _, wa := range p.PackageManagerWindowsArchives {
		if !winFiles[wa.Filename] {
			t.Errorf("package-manager archive %q not in platform archives", wa.Filename)
		}
	}
}

// --- splitOwnerRepo ----------------------------------------------------------

func TestSplitOwnerRepo(t *testing.T) {
	cases := []struct {
		in        string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"tooppoo/git-kura", "tooppoo", "git-kura", false},
		{"owner/repo-name", "owner", "repo-name", false},
		{"/git-kura", "", "", true}, // empty owner
		{"tooppoo/", "", "", true},  // empty repo
		{"tooppoo", "", "", true},   // missing slash
	}
	for _, tc := range cases {
		owner, repo, err := splitOwnerRepo(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("splitOwnerRepo(%q): want error, got nil (owner=%q repo=%q)", tc.in, owner, repo)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitOwnerRepo(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if owner != tc.wantOwner || repo != tc.wantRepo {
			t.Errorf("splitOwnerRepo(%q) = (%q, %q), want (%q, %q)",
				tc.in, owner, repo, tc.wantOwner, tc.wantRepo)
		}
	}
}

// --- downloadText ------------------------------------------------------------

func TestDownloadText(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("hello world"))
		}))
		defer srv.Close()

		got, err := downloadText(srv.URL)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "hello world" {
			t.Errorf("got %q, want %q", got, "hello world")
		}
	})
	t.Run("non-200 status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		_, err := downloadText(srv.URL)
		if err == nil {
			t.Fatal("want error for non-200 status, got nil")
		}
	})
}

// --- validateToolsSidecar ----------------------------------------------------

func TestValidateToolsSidecar(t *testing.T) {
	manifest := func(version, archiveName, checksum, algo string) []byte {
		b, _ := json.Marshal(toolsSidecarManifest{
			Version:           version,
			ArchiveName:       archiveName,
			ArchiveChecksum:   checksum,
			ChecksumAlgorithm: algo,
		})
		return b
	}

	const (
		wantVersion = "0.0.7"
		wantArchive = "git-kura-tools_0.0.7.tar.gz"
		sidecarName = "git-kura-tools_0.0.7.json"
	)

	t.Run("all fields valid", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(manifest(wantVersion, wantArchive, "abc123", "sha256"))
		}))
		defer srv.Close()

		r, errs := validateToolsSidecar(srv.URL, sidecarName, wantArchive, wantVersion)
		if r.Status != statusOK {
			t.Errorf("want success, got %s (checks: %v, errs: %v)", r.Status, r.Checks, errs)
		}
		if len(errs) != 0 {
			t.Errorf("want no errors, got %v", errs)
		}
		for k, v := range r.Checks {
			if v != statusOK {
				t.Errorf("check %q = %q, want %q", k, v, statusOK)
			}
		}
	})

	t.Run("version mismatch", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(manifest("0.0.6", wantArchive, "abc123", "sha256"))
		}))
		defer srv.Close()

		r, errs := validateToolsSidecar(srv.URL, sidecarName, wantArchive, wantVersion)
		if r.Status != statusFail {
			t.Errorf("want failure for version mismatch, got %s", r.Status)
		}
		if r.Checks["versionCheck"] != statusFail {
			t.Errorf("versionCheck = %q, want failure", r.Checks["versionCheck"])
		}
		if len(errs) == 0 {
			t.Error("want at least one error, got none")
		}
	})

	t.Run("archiveName mismatch", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(manifest(wantVersion, "wrong-archive.tar.gz", "abc123", "sha256"))
		}))
		defer srv.Close()

		r, errs := validateToolsSidecar(srv.URL, sidecarName, wantArchive, wantVersion)
		if r.Status != statusFail {
			t.Errorf("want failure for archiveName mismatch, got %s", r.Status)
		}
		if r.Checks["archiveNameCheck"] != statusFail {
			t.Errorf("archiveNameCheck = %q, want failure", r.Checks["archiveNameCheck"])
		}
		if len(errs) == 0 {
			t.Error("want at least one error, got none")
		}
	})

	t.Run("empty archiveChecksum", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(manifest(wantVersion, wantArchive, "", "sha256"))
		}))
		defer srv.Close()

		r, errs := validateToolsSidecar(srv.URL, sidecarName, wantArchive, wantVersion)
		if r.Status != statusFail {
			t.Errorf("want failure for empty checksum, got %s", r.Status)
		}
		if r.Checks["archiveChecksumCheck"] != statusFail {
			t.Errorf("archiveChecksumCheck = %q, want failure", r.Checks["archiveChecksumCheck"])
		}
		if len(errs) == 0 {
			t.Error("want at least one error, got none")
		}
	})

	t.Run("unsupported checksumAlgorithm", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(manifest(wantVersion, wantArchive, "abc123", "md5"))
		}))
		defer srv.Close()

		r, errs := validateToolsSidecar(srv.URL, sidecarName, wantArchive, wantVersion)
		if r.Status != statusFail {
			t.Errorf("want failure for wrong algorithm, got %s", r.Status)
		}
		if r.Checks["checksumAlgorithmCheck"] != statusFail {
			t.Errorf("checksumAlgorithmCheck = %q, want failure", r.Checks["checksumAlgorithmCheck"])
		}
		if len(errs) == 0 {
			t.Error("want at least one error, got none")
		}
	})

	t.Run("download failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		r, errs := validateToolsSidecar(srv.URL, sidecarName, wantArchive, wantVersion)
		if r.Status != statusFail {
			t.Errorf("want failure for download error, got %s", r.Status)
		}
		if len(errs) == 0 {
			t.Error("want at least one error, got none")
		}
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not-json"))
		}))
		defer srv.Close()

		r, errs := validateToolsSidecar(srv.URL, sidecarName, wantArchive, wantVersion)
		if r.Status != statusFail {
			t.Errorf("want failure for invalid JSON, got %s", r.Status)
		}
		if len(errs) == 0 {
			t.Error("want at least one error, got none")
		}
	})
}
