package releaseasset

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
)

func TestIsSHA256Hex(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{strings.Repeat("a", 64), true},
		{strings.Repeat("f", 64), true},
		{"0123456789abcdef" + strings.Repeat("a", 48), true},
		{strings.Repeat("A", 64), false},
		{strings.Repeat("a", 63), false},
		{strings.Repeat("a", 65), false},
		{"", false},
		{strings.Repeat("g", 64), false},
	}
	for _, tc := range cases {
		got := isSHA256Hex(tc.s)
		if got != tc.want {
			t.Errorf("isSHA256Hex(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestHasChecksumEntry(t *testing.T) {
	validHash := strings.Repeat("a", 64)
	cases := []struct {
		name     string
		content  string
		filename string
		want     bool
	}{
		{"exact match with valid sha256", validHash + "  somefile.tar.gz", "somefile.tar.gz", true},
		{"filename mismatch", validHash + "  other.tar.gz", "somefile.tar.gz", false},
		{"hash too short", "deadbeef  somefile.tar.gz", "somefile.tar.gz", false},
		{"uppercase hash", strings.Repeat("A", 64) + "  somefile.tar.gz", "somefile.tar.gz", false},
		{"non-hex character", strings.Repeat("g", 64) + "  somefile.tar.gz", "somefile.tar.gz", false},
		{"match in multiline content", validHash + "  other.tar.gz\n" + validHash + "  target.zip", "target.zip", true},
		{"empty content", "", "somefile.tar.gz", false},
		{"three fields", validHash + "  extra  somefile.tar.gz", "somefile.tar.gz", false},
		{"only one field", validHash, "somefile.tar.gz", false},
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

func TestValidateArchiveEntryPathRejectsTraversalAndWindowsPaths(t *testing.T) {
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"git-kura/README.md", false},
		{"git-kura\\README.md", false},
		{"../README.md", true},
		{"git-kura/../README.md", true},
		{"..\\README.md", true},
		{"git-kura\\..\\README.md", true},
		{"/tmp/git-kura", true},
		{"C:/tmp/git-kura", true},
		{"C:\\tmp\\git-kura", true},
		{"//server/share/git-kura", true},
		{"\\\\server\\share\\git-kura", true},
		{"", true},
		{".", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateArchiveEntryPath(tc.name)
			if tc.wantErr && err == nil {
				t.Fatal("want error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestResultLogLineIncludesSortedChecks(t *testing.T) {
	got := resultLogLine(assetResult{
		AssetKind: "platform-archive",
		Filename:  "archive.tar.gz",
		Status:    statusFail,
		Error:     "archive content validation failed",
		Checks: map[string]string{
			"readmeCheck":  statusFail,
			"binaryCheck":  statusOK,
			"licenseCheck": statusOK,
		},
	})
	want := "release-asset kind=platform-archive name=archive.tar.gz path= status=failure reason=archive content validation failed checks=binaryCheck:success,licenseCheck:success,readmeCheck:failure"
	if got != want {
		t.Fatalf("resultLogLine() = %q, want %q", got, want)
	}
}

func TestGoreleaserVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v0.0.7", "0.0.7"},
		{"v1.2.3", "1.2.3"},
		{"0.0.7", "0.0.7"},
	}
	for _, tc := range cases {
		got := goreleaserVersion(tc.in)
		if got != tc.want {
			t.Errorf("goreleaserVersion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

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
			t.Errorf("archiveFilename(%q,%q,%q,%q) = %q, want %q", tc.version, tc.goos, tc.goarch, tc.ext, got, tc.want)
		}
	}
}

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
	if p.ToolsArchive != "git-kura-tools_0.0.7.tar.gz" {
		t.Errorf("toolsArchive = %q, want git-kura-tools_0.0.7.tar.gz", p.ToolsArchive)
	}
	if p.ToolsSidecar != "git-kura-tools_0.0.7.json" {
		t.Errorf("toolsSidecar = %q, want git-kura-tools_0.0.7.json", p.ToolsSidecar)
	}
	if p.VersionFile != "VERSION" {
		t.Errorf("versionFile = %q, want VERSION", p.VersionFile)
	}
	for _, sa := range p.SBOMAssets {
		want := sa.ArchiveFilename + ".sbom.json"
		if sa.SBOMFilename != want {
			t.Errorf("SBOM for %q: got %q, want %q", sa.ArchiveFilename, sa.SBOMFilename, want)
		}
	}
}

func TestSelectUploadArtifactsUsesAllowlist(t *testing.T) {
	artifacts := []goreleaserArtifact{
		{Name: "binary", Path: "dist/binary", Type: "Binary"},
		{Name: "metadata.json", Path: "dist/metadata.json", Type: "Metadata"},
		{Name: "git-kura_v0.0.7_Linux_x86_64.tar.gz", Path: "dist/git-kura_v0.0.7_Linux_x86_64.tar.gz", Type: "Archive", Extra: extraID("release-archives")},
		{Name: "checksums.txt", Path: "dist/checksums.txt", Type: "Checksum"},
		{Name: "checksums.txt.sigstore.json", Path: "dist/checksums.txt.sigstore.json", Type: "Signature"},
		{Name: "git-kura_v0.0.7_Linux_x86_64.tar.gz.sbom.json", Path: "dist/git-kura_v0.0.7_Linux_x86_64.tar.gz.sbom.json", Type: "SBOM"},
		{Name: "git-kura-tools_0.0.7.tar.gz", Path: "dist/git-kura-tools_0.0.7.tar.gz", Type: "File"},
		{Name: "git-kura-tools_0.0.7.json", Path: "dist/git-kura-tools_0.0.7.json", Type: "File"},
		{Name: "VERSION", Path: "VERSION", Type: "File"},
		{Name: "unrelated.txt", Path: "dist/unrelated.txt", Type: "File"},
	}
	got := selectUploadArtifacts(artifacts)
	names := make(map[string]bool)
	for _, a := range got {
		names[a.Name] = true
	}
	for _, name := range []string{"binary", "metadata.json", "unrelated.txt"} {
		if names[name] {
			t.Errorf("%s should not be upload-selected", name)
		}
	}
	for _, name := range []string{
		"git-kura_v0.0.7_Linux_x86_64.tar.gz",
		"checksums.txt",
		"checksums.txt.sigstore.json",
		"git-kura_v0.0.7_Linux_x86_64.tar.gz.sbom.json",
		"git-kura-tools_0.0.7.tar.gz",
		"git-kura-tools_0.0.7.json",
		"VERSION",
	} {
		if !names[name] {
			t.Errorf("%s should be upload-selected", name)
		}
	}
}

func TestValidateWithDataLocalArtifactsSuccess(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	dist := filepath.Join(root, "dist")
	output := filepath.Join(t.TempDir(), "github-output")
	t.Setenv(envDistDir, dist)
	t.Setenv("GITHUB_OUTPUT", output)

	plan := buildTestPlan(t, "v0.0.7")
	writeReleaseFixture(t, dist, "v0.0.7", false)

	errs, warnings, stepData, err := New().ValidateWithData(plan)
	if err != nil {
		t.Fatalf("ValidateWithData returned internal error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
	var data validateStepData
	if err := json.Unmarshal(stepData, &data); err != nil {
		t.Fatalf("parse stepData: %v", err)
	}
	if len(data.UploadFiles) != 17 {
		t.Fatalf("uploadFiles length = %d, want 17", len(data.UploadFiles))
	}
	if !containsString(data.UploadFiles, "VERSION") {
		t.Fatalf("uploadFiles missing VERSION: %v", data.UploadFiles)
	}
	b, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read GITHUB_OUTPUT: %v", err)
	}
	if !strings.Contains(string(b), "files<<EOF\n") {
		t.Fatalf("GITHUB_OUTPUT missing files heredoc: %s", string(b))
	}
	if strings.Contains(string(b), "metadata.json") {
		t.Fatalf("GITHUB_OUTPUT includes local-only metadata: %s", string(b))
	}
}

func TestValidateWithDataAddsToolsFromToolsDistWhenMissingFromArtifactsOutput(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	dist := filepath.Join(root, "dist")
	toolsDist := filepath.Join(t.TempDir(), "tools-dist")
	t.Setenv(envDistDir, dist)
	t.Setenv(envToolsDistDir, toolsDist)

	plan := buildTestPlan(t, "v0.0.7")
	writeReleaseFixture(t, dist, "v0.0.7", false)
	moveToolsOutOfArtifactsJSON(t, dist, toolsDist, "v0.0.7")

	errs, _, stepData, err := New().ValidateWithData(plan)
	if err != nil {
		t.Fatalf("ValidateWithData returned internal error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
	var data validateStepData
	if err := json.Unmarshal(stepData, &data); err != nil {
		t.Fatalf("parse stepData: %v", err)
	}
	for _, want := range []string{
		filepath.Join(toolsDist, "git-kura-tools_0.0.7.tar.gz"),
		filepath.Join(toolsDist, "git-kura-tools_0.0.7.json"),
	} {
		if !containsString(data.UploadFiles, want) {
			t.Fatalf("uploadFiles missing %s: %v", want, data.UploadFiles)
		}
	}
}

func TestValidateWithDataAddsVersionFileFromRepositoryRootWhenMissingFromArtifactsOutput(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	dist := filepath.Join(root, "dist")
	t.Setenv(envDistDir, dist)

	plan := buildTestPlan(t, "v0.0.7")
	writeReleaseFixture(t, dist, "v0.0.7", false)

	errs, _, stepData, err := New().ValidateWithData(plan)
	if err != nil {
		t.Fatalf("ValidateWithData returned internal error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
	var data validateStepData
	if err := json.Unmarshal(stepData, &data); err != nil {
		t.Fatalf("parse stepData: %v", err)
	}
	if !containsString(data.UploadFiles, "VERSION") {
		t.Fatalf("uploadFiles missing repository root VERSION: %v", data.UploadFiles)
	}
}

func TestValidateWithDataVersionMismatchFails(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	dist := filepath.Join(root, "dist")
	t.Setenv(envDistDir, dist)

	plan := buildTestPlan(t, "v0.0.7")
	writeReleaseFixture(t, dist, "v0.0.7", false)
	if err := os.WriteFile("VERSION", []byte("v0.0.8\n"), 0o644); err != nil {
		t.Fatalf("write VERSION: %v", err)
	}

	errs, _, _, err := New().ValidateWithData(plan)
	if err != nil {
		t.Fatalf("ValidateWithData returned internal error: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("expected validation errors")
	}
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, `VERSION "v0.0.8" does not match expected "v0.0.7"`) {
		t.Fatalf("expected VERSION mismatch, got %s", joined)
	}
}

func TestValidateWithDataChecksumMismatchFails(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	dist := filepath.Join(root, "dist")
	t.Setenv(envDistDir, dist)

	plan := buildTestPlan(t, "v0.0.7")
	writeReleaseFixture(t, dist, "v0.0.7", true)

	errs, _, _, err := New().ValidateWithData(plan)
	if err != nil {
		t.Fatalf("ValidateWithData returned internal error: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("expected validation errors")
	}
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %s", joined)
	}
}

func buildTestPlan(t *testing.T, version string) *schema.ReleasePlanEnvelope {
	t.Helper()
	payload, err := New().BuildPayload(version)
	if err != nil {
		t.Fatalf("BuildPayload: %v", err)
	}
	return &schema.ReleasePlanEnvelope{
		Payload: schema.ReleasePlanPayload{
			TargetVersion: version,
			StepName:      "release-asset",
			StepData:      payload,
		},
	}
}

func writeReleaseFixture(t *testing.T, dist, version string, corruptChecksum bool) {
	t.Helper()
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	p := buildPlanPayload(version)
	if err := os.WriteFile(p.VersionFile, []byte(version+"\n"), 0o644); err != nil {
		t.Fatalf("write VERSION: %v", err)
	}
	var artifacts []goreleaserArtifact
	checksumLines := []string{}

	for _, pa := range p.PlatformArchives {
		path := filepath.Join(dist, pa.Filename)
		if pa.OS == "windows" {
			writeZip(t, path)
		} else {
			writeTarGz(t, path)
		}
		sum := mustSHA256File(t, path)
		if corruptChecksum && pa.OS == "linux" && pa.Arch == "amd64" {
			sum = strings.Repeat("0", 64)
		}
		checksumLines = append(checksumLines, sum+"  "+pa.Filename)
		artifacts = append(artifacts, goreleaserArtifact{
			Name:   pa.Filename,
			Path:   path,
			Goos:   pa.OS,
			Goarch: pa.Arch,
			Type:   "Archive",
			Extra:  extraID("release-archives"),
		})
		sbomPath := filepath.Join(dist, pa.Filename+".sbom.json")
		if err := os.WriteFile(sbomPath, []byte(`{"bomFormat":"CycloneDX"}`), 0o644); err != nil {
			t.Fatalf("write sbom: %v", err)
		}
		artifacts = append(artifacts, goreleaserArtifact{
			Name: pa.Filename + ".sbom.json",
			Path: sbomPath,
			Type: "SBOM",
		})
	}

	checksumPath := filepath.Join(dist, p.ChecksumFile)
	if err := os.WriteFile(checksumPath, []byte(strings.Join(checksumLines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write checksums: %v", err)
	}
	artifacts = append(artifacts, goreleaserArtifact{Name: p.ChecksumFile, Path: checksumPath, Type: "Checksum"})

	signaturePath := filepath.Join(dist, p.SignatureFile)
	if err := os.WriteFile(signaturePath, []byte(`{"mediaType":"application/vnd.dev.sigstore.bundle"}`), 0o644); err != nil {
		t.Fatalf("write signature: %v", err)
	}
	artifacts = append(artifacts, goreleaserArtifact{Name: p.SignatureFile, Path: signaturePath, Type: "Signature"})

	toolsPath := filepath.Join(dist, p.ToolsArchive)
	if err := os.WriteFile(toolsPath, []byte("tools archive"), 0o644); err != nil {
		t.Fatalf("write tools archive: %v", err)
	}
	toolsSum := mustSHA256File(t, toolsPath)
	sidecarPath := filepath.Join(dist, p.ToolsSidecar)
	sidecar := toolsSidecarManifest{
		ArchiveName:       p.ToolsArchive,
		ArchiveChecksum:   toolsSum,
		ChecksumAlgorithm: "sha256",
		Version:           goreleaserVersion(version),
	}
	sidecarJSON, err := json.Marshal(sidecar)
	if err != nil {
		t.Fatalf("marshal sidecar: %v", err)
	}
	if err := os.WriteFile(sidecarPath, sidecarJSON, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	artifacts = append(artifacts,
		goreleaserArtifact{Name: p.ToolsArchive, Path: toolsPath, Type: "File"},
		goreleaserArtifact{Name: p.ToolsSidecar, Path: sidecarPath, Type: "File"},
		goreleaserArtifact{Name: "metadata.json", Path: filepath.Join(dist, "metadata.json"), Type: "Metadata"},
		goreleaserArtifact{Name: "git-kura", Path: filepath.Join(dist, "git-kura"), Type: "Binary"},
	)

	metadataPath := filepath.Join(dist, "metadata.json")
	metadata, err := json.Marshal(goreleaserMetadata{Tag: version, Version: goreleaserVersion(version), Commit: "abc123"})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if err := os.WriteFile(metadataPath, metadata, 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	artifactsJSON, err := json.Marshal(artifacts)
	if err != nil {
		t.Fatalf("marshal artifacts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dist, "artifacts.json"), artifactsJSON, 0o644); err != nil {
		t.Fatalf("write artifacts: %v", err)
	}
}

func moveToolsOutOfArtifactsJSON(t *testing.T, dist, toolsDist, version string) {
	t.Helper()
	if err := os.MkdirAll(toolsDist, 0o755); err != nil {
		t.Fatalf("mkdir tools dist: %v", err)
	}
	p := buildPlanPayload(version)
	for _, name := range []string{p.ToolsArchive, p.ToolsSidecar} {
		if err := os.Rename(filepath.Join(dist, name), filepath.Join(toolsDist, name)); err != nil {
			t.Fatalf("move tool artifact %s: %v", name, err)
		}
	}
	var artifacts []goreleaserArtifact
	b, err := os.ReadFile(filepath.Join(dist, "artifacts.json"))
	if err != nil {
		t.Fatalf("read artifacts: %v", err)
	}
	if err := json.Unmarshal(b, &artifacts); err != nil {
		t.Fatalf("parse artifacts: %v", err)
	}
	filtered := artifacts[:0]
	for _, a := range artifacts {
		if a.Name != p.ToolsArchive && a.Name != p.ToolsSidecar {
			filtered = append(filtered, a)
		}
	}
	out, err := json.Marshal(filtered)
	if err != nil {
		t.Fatalf("marshal filtered artifacts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dist, "artifacts.json"), out, 0o644); err != nil {
		t.Fatalf("write filtered artifacts: %v", err)
	}
}

func writeTarGz(t *testing.T, path string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range map[string]string{
		"git-kura/git-kura":                          "binary",
		"git-kura/README.md":                         "readme",
		"git-kura/LICENSE":                           "license",
		"git-kura/third_party_licenses/licenses.txt": "third party",
	} {
		b := []byte(content)
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(b))}); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write(b); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write tar.gz: %v", err)
	}
}

func writeZip(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(f)
	for name, content := range map[string]string{
		"git-kura/git-kura.exe":                      "binary",
		"git-kura/README.md":                         "readme",
		"git-kura/LICENSE":                           "license",
		"git-kura/third_party_licenses/licenses.txt": "third party",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create entry: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("zip file close: %v", err)
	}
}

func mustSHA256File(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read for sha256: %v", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func extraID(id string) map[string]json.RawMessage {
	b, _ := json.Marshal(id)
	return map[string]json.RawMessage{"ID": b}
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
