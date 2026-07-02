// Package releaseasset implements the release-asset validate-only step.
// It verifies GoReleaser-generated local artifacts before GitHub Release
// creation so the uploaded file list exactly matches the validated file list.
package releaseasset

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
)

const (
	envArtifactsJSON = "GIT_KURA_GORELEASER_ARTIFACTS"
	envMetadataJSON  = "GIT_KURA_GORELEASER_METADATA"
	envDistDir       = "GIT_KURA_DIST_DIR"
	envToolsDistDir  = "GIT_KURA_TOOLS_DIST_DIR"
)

// Handler implements the release-asset validate-only step.
type Handler struct{}

// New returns a Handler for the release-asset step.
func New() *Handler { return &Handler{} }

type planPayload struct {
	PlatformArchives              []platformArchive `json:"platformArchives"`
	ChecksumFile                  string            `json:"checksumFile"`
	SignatureFile                 string            `json:"signatureFile"`
	SBOMAssets                    []sbomAsset       `json:"sbomAssets"`
	ToolsArchive                  string            `json:"toolsArchive"`
	ToolsSidecar                  string            `json:"toolsSidecar"`
	PackageManagerWindowsArchives []windowsArchive  `json:"packageManagerWindowsArchives"`
}

type platformArchive struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Filename string `json:"filename"`
}

type sbomAsset struct {
	ArchiveFilename string `json:"archiveFilename"`
	SBOMFilename    string `json:"sbomFilename"`
}

type windowsArchive struct {
	Arch     string `json:"arch"`
	Filename string `json:"filename"`
}

type validateStepData struct {
	Assets      []assetResult `json:"assets"`
	UploadFiles []string      `json:"uploadFiles"`
}

type assetResult struct {
	AssetKind       string            `json:"assetKind"`
	Status          string            `json:"status"`
	Filename        string            `json:"filename,omitempty"`
	Path            string            `json:"path,omitempty"`
	OS              string            `json:"os,omitempty"`
	Arch            string            `json:"arch,omitempty"`
	ArchiveFilename string            `json:"archiveFilename,omitempty"`
	Checks          map[string]string `json:"checks,omitempty"`
	Error           string            `json:"error,omitempty"`
	Expected        string            `json:"expected,omitempty"`
	Actual          string            `json:"actual,omitempty"`
}

const statusOK = "success"
const statusFail = "failure"

type goreleaserMetadata struct {
	Tag     string `json:"tag"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

type goreleaserArtifact struct {
	Name   string                     `json:"name"`
	Path   string                     `json:"path"`
	Goos   string                     `json:"goos"`
	Goarch string                     `json:"goarch"`
	Type   string                     `json:"type"`
	Extra  map[string]json.RawMessage `json:"extra"`
}

type toolsSidecarManifest struct {
	ArchiveName       string `json:"archiveName"`
	ArchiveChecksum   string `json:"archiveChecksum"`
	ChecksumAlgorithm string `json:"checksumAlgorithm"`
	Version           string `json:"version"`
}

// BuildPayload generates the plan payload listing all expected release assets.
func (h *Handler) BuildPayload(version string) (json.RawMessage, error) {
	p := buildPlanPayload(version)
	return json.Marshal(p)
}

func (h *Handler) Validate(plan *schema.ReleasePlanEnvelope) ([]string, []string, error) {
	errs, warnings, _, err := h.ValidateWithData(plan)
	return errs, warnings, err
}

func (h *Handler) ValidateWithData(plan *schema.ReleasePlanEnvelope) ([]string, []string, json.RawMessage, error) {
	var p planPayload
	if err := json.Unmarshal(plan.Payload.StepData, &p); err != nil {
		return nil, nil, nil, fmt.Errorf("parse release-asset plan payload: %w", err)
	}

	distDir := os.Getenv(envDistDir)
	if distDir == "" {
		distDir = "dist"
	}

	metadata, err := loadMetadata(distDir)
	if err != nil {
		return nil, nil, nil, err
	}

	var errs []string
	refName := os.Getenv("GITHUB_REF_NAME")
	if refName == "" {
		refName = plan.Payload.TargetVersion
	}
	if metadata.Tag == "" {
		errs = append(errs, "metadata.tag is empty")
	} else if metadata.Tag != refName {
		errs = append(errs, fmt.Sprintf("metadata.tag %q does not match expected tag %q", metadata.Tag, refName))
	}

	artifacts, err := loadArtifacts(distDir)
	if err != nil {
		return nil, nil, nil, err
	}
	artifacts = augmentToolsArtifacts(artifacts, p.ToolsArchive, p.ToolsSidecar)
	byName := map[string]goreleaserArtifact{}
	for _, a := range artifacts {
		if a.Name != "" {
			byName[a.Name] = a
		}
	}

	uploadArtifacts := selectUploadArtifacts(artifacts)
	uploadFiles := make([]string, 0, len(uploadArtifacts))
	for _, a := range uploadArtifacts {
		uploadFiles = append(uploadFiles, a.Path)
	}

	var results []assetResult
	for _, r := range validatePlatformArchives(p.PlatformArchives, byName, distDir) {
		if r.Status == statusFail {
			errs = append(errs, formatResultError(r))
		}
		results = append(results, r)
	}
	for _, r := range validatePackageManagerWindowsArchives(p.PackageManagerWindowsArchives, p.PlatformArchives, byName) {
		if r.Status == statusFail {
			errs = append(errs, formatResultError(r))
		}
		results = append(results, r)
	}

	checksumResult, checksumErrs := validateChecksums(p.ChecksumFile, p.PlatformArchives, byName, distDir)
	errs = append(errs, checksumErrs...)
	results = append(results, checksumResult)

	signatureResult := validateFileArtifact(p.SignatureFile, "signature-file", byName, distDir)
	if signatureResult.Status == statusFail {
		errs = append(errs, formatResultError(signatureResult))
	}
	results = append(results, signatureResult)

	for _, r := range validateSBOMs(p.SBOMAssets, byName, distDir) {
		if r.Status == statusFail {
			errs = append(errs, formatResultError(r))
		}
		results = append(results, r)
	}

	toolsArchive, toolsSidecar, toolsErrs := validateTools(p.ToolsArchive, p.ToolsSidecar, goreleaserVersion(plan.Payload.TargetVersion), byName, distDir)
	errs = append(errs, toolsErrs...)
	results = append(results, toolsArchive, toolsSidecar)

	uploadErrs := validateUploadSet(uploadArtifacts, results)
	errs = append(errs, uploadErrs...)

	for _, r := range results {
		printResult(r)
	}
	if len(errs) == 0 {
		if err := writeGitHubOutput("files", strings.Join(uploadFiles, "\n")); err != nil {
			return nil, nil, nil, err
		}
	}

	sd, marshalErr := json.Marshal(validateStepData{Assets: results, UploadFiles: uploadFiles})
	if marshalErr != nil {
		return nil, nil, nil, fmt.Errorf("marshal validate step data: %w", marshalErr)
	}
	return errs, nil, sd, nil
}

func (h *Handler) Preflight(_ *schema.ReleasePlanEnvelope) error {
	return fmt.Errorf("release-asset is a validate-only step: exec is not supported")
}

func (h *Handler) Exec(_ *schema.ReleasePlanEnvelope) error {
	return fmt.Errorf("release-asset is a validate-only step: exec is not supported")
}

func goreleaserVersion(version string) string {
	return strings.TrimPrefix(version, "v")
}

func buildPlanPayload(version string) planPayload {
	ver := goreleaserVersion(version)
	type entry struct{ goos, goarch, ext string }
	platforms := []entry{
		{"linux", "amd64", "tar.gz"},
		{"linux", "arm64", "tar.gz"},
		{"darwin", "amd64", "tar.gz"},
		{"darwin", "arm64", "tar.gz"},
		{"windows", "amd64", "zip"},
		{"windows", "arm64", "zip"},
	}

	archives := make([]platformArchive, 0, len(platforms))
	sboms := make([]sbomAsset, 0, len(platforms))
	for _, e := range platforms {
		fn := archiveFilename(version, e.goos, e.goarch, e.ext)
		archives = append(archives, platformArchive{OS: e.goos, Arch: e.goarch, Filename: fn})
		sboms = append(sboms, sbomAsset{ArchiveFilename: fn, SBOMFilename: fn + ".sbom.json"})
	}

	return planPayload{
		PlatformArchives: archives,
		ChecksumFile:     "checksums.txt",
		SignatureFile:    "checksums.txt.sigstore.json",
		SBOMAssets:       sboms,
		ToolsArchive:     fmt.Sprintf("git-kura-tools_%s.tar.gz", ver),
		ToolsSidecar:     fmt.Sprintf("git-kura-tools_%s.json", ver),
		PackageManagerWindowsArchives: []windowsArchive{
			{Arch: "amd64", Filename: archiveFilename(version, "windows", "amd64", "zip")},
			{Arch: "arm64", Filename: archiveFilename(version, "windows", "arm64", "zip")},
		},
	}
}

func archiveFilename(version, goos, goarch, ext string) string {
	osTitle := strings.ToUpper(goos[:1]) + goos[1:]
	archName := goarch
	if goarch == "amd64" {
		archName = "x86_64"
	}
	return fmt.Sprintf("git-kura_%s_%s_%s.%s", version, osTitle, archName, ext)
}

func loadMetadata(distDir string) (goreleaserMetadata, error) {
	var metadata goreleaserMetadata
	if err := unmarshalJSONInput(envMetadataJSON, filepath.Join(distDir, "metadata.json"), &metadata); err != nil {
		return metadata, fmt.Errorf("load GoReleaser metadata: %w", err)
	}
	return metadata, nil
}

func loadArtifacts(distDir string) ([]goreleaserArtifact, error) {
	var artifacts []goreleaserArtifact
	if err := unmarshalJSONInput(envArtifactsJSON, filepath.Join(distDir, "artifacts.json"), &artifacts); err != nil {
		return nil, fmt.Errorf("load GoReleaser artifacts: %w", err)
	}
	return artifacts, nil
}

func augmentToolsArtifacts(artifacts []goreleaserArtifact, archiveName, sidecarName string) []goreleaserArtifact {
	byName := map[string]bool{}
	for _, a := range artifacts {
		byName[a.Name] = true
	}
	toolsDir := os.Getenv(envToolsDistDir)
	if toolsDir == "" {
		toolsDir = ".tools-dist"
	}
	for _, name := range []string{archiveName, sidecarName} {
		if byName[name] {
			continue
		}
		path := filepath.Join(toolsDir, name)
		if _, err := os.Stat(path); err == nil {
			artifacts = append(artifacts, goreleaserArtifact{
				Name: name,
				Path: path,
				Type: "File",
			})
		}
	}
	return artifacts
}

func unmarshalJSONInput(envName, fallbackPath string, v any) error {
	raw := os.Getenv(envName)
	var b []byte
	var err error
	if raw == "" {
		b, err = os.ReadFile(fallbackPath)
		if err != nil {
			return fmt.Errorf("read %s or set %s: %w", fallbackPath, envName, err)
		}
	} else {
		b = []byte(raw)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return err
	}
	return nil
}

func selectUploadArtifacts(artifacts []goreleaserArtifact) []goreleaserArtifact {
	var out []goreleaserArtifact
	for _, a := range artifacts {
		if a.Type == "Metadata" || a.Type == "Binary" {
			continue
		}
		switch {
		case a.Type == "Archive" && artifactExtraString(a, "ID") == "release-archives":
			out = append(out, a)
		case a.Name == "checksums.txt":
			out = append(out, a)
		case a.Name == "checksums.txt.sigstore.json":
			out = append(out, a)
		case a.Type == "SBOM":
			out = append(out, a)
		case matchToolsArchive(a.Name):
			out = append(out, a)
		case matchToolsSidecar(a.Name):
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func artifactExtraString(a goreleaserArtifact, key string) string {
	if a.Extra == nil {
		return ""
	}
	raw, ok := a.Extra[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func matchToolsArchive(name string) bool {
	ok, _ := filepath.Match("git-kura-tools_*.tar.gz", name)
	return ok
}

func matchToolsSidecar(name string) bool {
	ok, _ := filepath.Match("git-kura-tools_*.json", name)
	return ok
}

func validatePlatformArchives(expected []platformArchive, byName map[string]goreleaserArtifact, distDir string) []assetResult {
	results := make([]assetResult, 0, len(expected))
	for _, pa := range expected {
		a, ok := byName[pa.Filename]
		r := assetResult{AssetKind: "platform-archive", Filename: pa.Filename, OS: pa.OS, Arch: pa.Arch}
		if !ok {
			r.Status = statusFail
			r.Error = "not found in GoReleaser artifacts output"
			results = append(results, r)
			continue
		}
		r.Path = a.Path
		checks := map[string]string{
			"allowlistCheck":          statusOK,
			"formatCheck":             statusOK,
			"binaryCheck":             statusOK,
			"readmeCheck":             statusOK,
			"licenseCheck":            statusOK,
			"thirdPartyLicensesCheck": statusOK,
		}
		if a.Type != "Archive" || artifactExtraString(a, "ID") != "release-archives" {
			checks["allowlistCheck"] = statusFail
		}
		wantExt := ".tar.gz"
		if pa.OS == "windows" {
			wantExt = ".zip"
		}
		if !strings.HasSuffix(pa.Filename, wantExt) {
			checks["formatCheck"] = statusFail
		}
		entries, err := archiveEntries(resolveArtifactPath(a.Path, distDir), pa.OS)
		if err != nil {
			r.Status = statusFail
			r.Checks = checks
			r.Error = err.Error()
			results = append(results, r)
			continue
		}
		wantBinary := "git-kura"
		if pa.OS == "windows" {
			wantBinary = "git-kura.exe"
		}
		if !entryBasenameExists(entries, wantBinary) {
			checks["binaryCheck"] = statusFail
		}
		if !entryBasenameExists(entries, "README.md") {
			checks["readmeCheck"] = statusFail
		}
		if !entryBasenameExists(entries, "LICENSE") {
			checks["licenseCheck"] = statusFail
		}
		if !entryUnderDirExists(entries, "third_party_licenses") {
			checks["thirdPartyLicensesCheck"] = statusFail
		}
		r.Status = statusFromChecks(checks)
		r.Checks = checks
		if r.Status == statusFail {
			r.Error = "archive content validation failed"
		}
		results = append(results, r)
	}
	return results
}

func validatePackageManagerWindowsArchives(expected []windowsArchive, platform []platformArchive, byName map[string]goreleaserArtifact) []assetResult {
	platformWindows := map[string]bool{}
	for _, pa := range platform {
		if pa.OS == "windows" {
			platformWindows[pa.Filename] = true
		}
	}
	results := make([]assetResult, 0, len(expected))
	for _, wa := range expected {
		r := assetResult{AssetKind: "package-manager-windows-archive", Filename: wa.Filename, OS: "windows", Arch: wa.Arch}
		a, ok := byName[wa.Filename]
		switch {
		case !ok:
			r.Status = statusFail
			r.Error = "not found in GoReleaser artifacts output"
		case !platformWindows[wa.Filename]:
			r.Status = statusFail
			r.Path = a.Path
			r.Error = "not included in Windows platform archive set"
		default:
			r.Status = statusOK
			r.Path = a.Path
		}
		results = append(results, r)
	}
	return results
}

func validateChecksums(name string, platform []platformArchive, byName map[string]goreleaserArtifact, distDir string) (assetResult, []string) {
	r := validateFileArtifact(name, "checksum-file", byName, distDir)
	if r.Status == statusFail {
		return r, []string{formatResultError(r)}
	}
	content, err := os.ReadFile(resolveArtifactPath(r.Path, distDir))
	if err != nil {
		r.Status = statusFail
		r.Error = err.Error()
		return r, []string{formatResultError(r)}
	}
	entries := parseChecksumEntries(string(content))
	checks := map[string]string{}
	var errs []string
	for _, pa := range platform {
		checkName := pa.Filename
		want, ok := entries[pa.Filename]
		if !ok {
			checks[checkName] = statusFail
			errs = append(errs, fmt.Sprintf("checksums.txt: no sha256 entry for %q", pa.Filename))
			continue
		}
		if !isSHA256Hex(want) {
			checks[checkName] = statusFail
			errs = append(errs, fmt.Sprintf("checksums.txt: digest for %q is not lowercase sha256 hex", pa.Filename))
			continue
		}
		archive, ok := byName[pa.Filename]
		if !ok {
			checks[checkName] = statusFail
			errs = append(errs, fmt.Sprintf("checksums.txt: archive %q not found in artifacts output", pa.Filename))
			continue
		}
		actual, err := sha256File(resolveArtifactPath(archive.Path, distDir))
		if err != nil {
			checks[checkName] = statusFail
			errs = append(errs, fmt.Sprintf("checksums.txt: compute sha256 for %q: %v", pa.Filename, err))
			continue
		}
		if actual != want {
			checks[checkName] = statusFail
			errs = append(errs, fmt.Sprintf("checksums.txt: checksum mismatch for %q: expected %s actual %s", pa.Filename, want, actual))
			r.Expected = want
			r.Actual = actual
			continue
		}
		checks[checkName] = statusOK
	}
	r.Checks = checks
	r.Status = statusFromChecks(checks)
	if r.Status == statusFail {
		r.Error = "checksum validation failed"
	}
	return r, errs
}

func validateFileArtifact(name, kind string, byName map[string]goreleaserArtifact, distDir string) assetResult {
	a, ok := byName[name]
	if !ok {
		return assetResult{AssetKind: kind, Filename: name, Status: statusFail, Error: "not found in GoReleaser artifacts output"}
	}
	path := resolveArtifactPath(a.Path, distDir)
	if _, err := os.Stat(path); err != nil {
		return assetResult{AssetKind: kind, Filename: name, Path: a.Path, Status: statusFail, Error: err.Error()}
	}
	return assetResult{AssetKind: kind, Filename: name, Path: a.Path, Status: statusOK}
}

func validateSBOMs(expected []sbomAsset, byName map[string]goreleaserArtifact, distDir string) []assetResult {
	results := make([]assetResult, 0, len(expected))
	for _, sa := range expected {
		r := validateFileArtifact(sa.SBOMFilename, "sbom", byName, distDir)
		r.ArchiveFilename = sa.ArchiveFilename
		if r.Status == statusOK {
			if byName[sa.SBOMFilename].Type != "SBOM" {
				r.Status = statusFail
				r.Error = "artifact type is not SBOM"
			} else if err := parseJSONFile(resolveArtifactPath(r.Path, distDir)); err != nil {
				r.Status = statusFail
				r.Error = fmt.Sprintf("parse JSON: %v", err)
			}
		}
		results = append(results, r)
	}
	return results
}

func validateTools(archiveName, sidecarName, expectedVersion string, byName map[string]goreleaserArtifact, distDir string) (assetResult, assetResult, []string) {
	archiveResult := validateFileArtifact(archiveName, "tools-archive", byName, distDir)
	sidecarResult := validateFileArtifact(sidecarName, "tools-sidecar", byName, distDir)
	var errs []string
	if archiveResult.Status == statusFail {
		errs = append(errs, formatResultError(archiveResult))
	}
	if sidecarResult.Status == statusFail {
		errs = append(errs, formatResultError(sidecarResult))
		return archiveResult, sidecarResult, errs
	}

	var m toolsSidecarManifest
	if err := readJSONFile(resolveArtifactPath(sidecarResult.Path, distDir), &m); err != nil {
		sidecarResult.Status = statusFail
		sidecarResult.Error = fmt.Sprintf("parse JSON: %v", err)
		errs = append(errs, formatResultError(sidecarResult))
		return archiveResult, sidecarResult, errs
	}

	actualChecksum := ""
	if archiveResult.Status == statusOK {
		sum, err := sha256File(resolveArtifactPath(archiveResult.Path, distDir))
		if err != nil {
			archiveResult.Status = statusFail
			archiveResult.Error = err.Error()
			errs = append(errs, formatResultError(archiveResult))
		} else {
			actualChecksum = sum
		}
	}

	checks := map[string]string{
		"archiveNameCheck":       statusOK,
		"archiveChecksumCheck":   statusOK,
		"checksumAlgorithmCheck": statusOK,
		"versionCheck":           statusOK,
	}
	if m.ArchiveName != archiveName {
		checks["archiveNameCheck"] = statusFail
		errs = append(errs, fmt.Sprintf("tools sidecar manifest archiveName %q does not match expected %q", m.ArchiveName, archiveName))
	}
	if !isSHA256Hex(m.ArchiveChecksum) {
		checks["archiveChecksumCheck"] = statusFail
		errs = append(errs, fmt.Sprintf("tools sidecar manifest archiveChecksum %q is not a valid sha256 hex string", m.ArchiveChecksum))
	} else if actualChecksum != "" && m.ArchiveChecksum != actualChecksum {
		checks["archiveChecksumCheck"] = statusFail
		sidecarResult.Expected = m.ArchiveChecksum
		sidecarResult.Actual = actualChecksum
		errs = append(errs, fmt.Sprintf("tools archive checksum mismatch: expected %s actual %s", m.ArchiveChecksum, actualChecksum))
	}
	if m.ChecksumAlgorithm != "sha256" {
		checks["checksumAlgorithmCheck"] = statusFail
		errs = append(errs, fmt.Sprintf("tools sidecar manifest checksumAlgorithm %q is not sha256", m.ChecksumAlgorithm))
	}
	if m.Version != expectedVersion {
		checks["versionCheck"] = statusFail
		errs = append(errs, fmt.Sprintf("tools sidecar manifest version %q does not match expected %q", m.Version, expectedVersion))
	}
	sidecarResult.Checks = checks
	sidecarResult.Status = statusFromChecks(checks)
	if sidecarResult.Status == statusFail && sidecarResult.Error == "" {
		sidecarResult.Error = "tools sidecar validation failed"
	}
	return archiveResult, sidecarResult, errs
}

func validateUploadSet(uploadArtifacts []goreleaserArtifact, results []assetResult) []string {
	resultByName := map[string]assetResult{}
	for _, r := range results {
		if r.Filename != "" && r.AssetKind != "package-manager-windows-archive" {
			resultByName[r.Filename] = r
		}
	}
	var errs []string
	for _, a := range uploadArtifacts {
		r, ok := resultByName[a.Name]
		if !ok {
			errs = append(errs, fmt.Sprintf("upload artifact %q is allowlisted but was not validated", a.Name))
			continue
		}
		if r.Status != statusOK {
			errs = append(errs, fmt.Sprintf("upload artifact %q failed validation", a.Name))
		}
	}
	return errs
}

func archiveEntries(path, goos string) ([]string, error) {
	if goos == "windows" {
		return zipEntries(path)
	}
	return tarGzEntries(path)
}

func tarGzEntries(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	var entries []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := validateArchiveEntryPath(h.Name); err != nil {
			return nil, err
		}
		if h.FileInfo().Mode().IsRegular() {
			entries = append(entries, filepath.ToSlash(h.Name))
		}
	}
	return entries, nil
}

func zipEntries(path string) ([]string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	var entries []string
	for _, f := range zr.File {
		if err := validateArchiveEntryPath(f.Name); err != nil {
			return nil, err
		}
		if !f.FileInfo().IsDir() {
			entries = append(entries, filepath.ToSlash(f.Name))
		}
	}
	return entries, nil
}

func validateArchiveEntryPath(name string) error {
	normalized := strings.ReplaceAll(name, "\\", "/")
	if normalized == "" || strings.HasPrefix(normalized, "/") || strings.HasPrefix(normalized, "//") || hasWindowsVolumePrefix(normalized) {
		return fmt.Errorf("unsafe archive entry path %q", name)
	}
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return fmt.Errorf("unsafe archive entry path %q", name)
		}
	}
	clean := path.Clean(normalized)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("unsafe archive entry path %q", name)
	}
	return nil
}

func hasWindowsVolumePrefix(name string) bool {
	return len(name) >= 2 && name[1] == ':' && ((name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z'))
}

func entryBasenameExists(entries []string, base string) bool {
	for _, e := range entries {
		if filepath.Base(e) == base {
			return true
		}
	}
	return false
}

func entryUnderDirExists(entries []string, dir string) bool {
	prefix := strings.TrimSuffix(dir, "/") + "/"
	for _, e := range entries {
		if strings.Contains(e, "/"+prefix) || strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

func parseChecksumEntries(content string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			out[fields[1]] = fields[0]
		}
	}
	return out
}

func hasChecksumEntry(content, filename string) bool {
	entries := parseChecksumEntries(content)
	return isSHA256Hex(entries[filename])
}

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func parseJSONFile(path string) error {
	var v any
	return readJSONFile(path, &v)
}

func readJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func resolveArtifactPath(path, distDir string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if strings.HasPrefix(filepath.ToSlash(path), filepath.ToSlash(distDir)+"/") || filepath.Base(path) != path {
		return path
	}
	return filepath.Join(distDir, path)
}

func statusFromChecks(checks map[string]string) string {
	for _, v := range checks {
		if v == statusFail {
			return statusFail
		}
	}
	return statusOK
}

func formatResultError(r assetResult) string {
	if r.Error != "" {
		return fmt.Sprintf("%s %q: %s", r.AssetKind, r.Filename, r.Error)
	}
	return fmt.Sprintf("%s %q failed validation", r.AssetKind, r.Filename)
}

func printResult(r assetResult) {
	fmt.Println(resultLogLine(r))
}

func resultLogLine(r assetResult) string {
	parts := []string{
		"release-asset",
		"kind=" + r.AssetKind,
		"name=" + r.Filename,
		"path=" + r.Path,
		"status=" + r.Status,
	}
	if r.Error != "" {
		parts = append(parts, "reason="+r.Error)
	}
	if len(r.Checks) > 0 {
		parts = append(parts, "checks="+formatChecks(r.Checks))
	}
	if r.Expected != "" || r.Actual != "" {
		parts = append(parts, "expected="+r.Expected, "actual="+r.Actual)
	}
	return strings.Join(parts, " ")
}

func formatChecks(checks map[string]string) string {
	keys := make([]string, 0, len(checks))
	for k := range checks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+":"+checks[k])
	}
	return strings.Join(parts, ",")
}

func writeGitHubOutput(name, value string) error {
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open GITHUB_OUTPUT: %w", err)
	}
	defer func() { _ = f.Close() }()
	_, err = fmt.Fprintf(f, "%s<<EOF\n%s\nEOF\n", name, value)
	return err
}
