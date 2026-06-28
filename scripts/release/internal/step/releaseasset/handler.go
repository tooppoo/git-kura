// Package releaseasset implements the release-asset validate-only step.
// It verifies that all expected GitHub Release assets exist and are accessible
// before package-manager steps (scoop, winget) run.
//
// This step has no external side effects and does not exec. Exec is explicitly
// rejected to make the validate-only contract visible at runtime.
package releaseasset

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"

	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
)

// Handler implements the release-asset validate-only step.
type Handler struct{}

// New returns a Handler for the release-asset step.
func New() *Handler { return &Handler{} }

// --- plan payload types ------------------------------------------------------

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

// --- validate result types ---------------------------------------------------

type validateStepData struct {
	Assets []assetResult `json:"assets"`
}

type assetResult struct {
	AssetKind          string            `json:"assetKind"`
	Status             string            `json:"status"`
	Filename           string            `json:"filename,omitempty"`
	OS                 string            `json:"os,omitempty"`
	Arch               string            `json:"arch,omitempty"`
	BrowserDownloadURL string            `json:"browserDownloadUrl,omitempty"`
	ArchiveFilename    string            `json:"archiveFilename,omitempty"`
	Checks             map[string]string `json:"checks,omitempty"`
	Error              string            `json:"error,omitempty"`
}

const statusOK = "success"
const statusFail = "failure"

// --- GitHub API types --------------------------------------------------------

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	State              string `json:"state"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// --- Handler methods ---------------------------------------------------------

// BuildPayload generates the plan payload listing all expected release assets.
func (h *Handler) BuildPayload(version string) (json.RawMessage, error) {
	p := buildPlanPayload(version)
	return json.Marshal(p)
}

// Validate implements step.Handler; it delegates to ValidateWithData and
// discards the step-specific data so the signature matches the interface.
func (h *Handler) Validate(plan *schema.ReleasePlanEnvelope) ([]string, []string, error) {
	errs, warnings, _, err := h.ValidateWithData(plan)
	return errs, warnings, err
}

// ValidateWithData implements step.DetailedValidator.
// It calls the GitHub API to verify every expected release asset and records
// per-asset results in the returned stepData.
func (h *Handler) ValidateWithData(plan *schema.ReleasePlanEnvelope) ([]string, []string, json.RawMessage, error) {
	var p planPayload
	if err := json.Unmarshal(plan.Payload.StepData, &p); err != nil {
		return nil, nil, nil, fmt.Errorf("parse release-asset plan payload: %w", err)
	}

	owner, repo, err := githubOwnerRepo()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolve GitHub owner/repo: %w", err)
	}

	version := plan.Payload.TargetVersion
	release, releaseErr := fetchGitHubRelease(owner, repo, version)

	// Rate-limit or network error becomes a validation failure (not internal)
	// so the result file is still written with status=failure.
	if releaseErr != nil {
		errs := []string{releaseErr.Error()}
		sd, _ := json.Marshal(validateStepData{Assets: []assetResult{{
			AssetKind: "github-release",
			Filename:  version,
			Status:    statusFail,
			Error:     releaseErr.Error(),
		}}})
		return errs, nil, sd, nil
	}

	// Build a name→asset map from the GitHub Release asset list.
	assetMap := make(map[string]githubAsset, len(release.Assets))
	for _, a := range release.Assets {
		assetMap[a.Name] = a
	}

	var errs []string
	var results []assetResult

	// 1. Platform archives: existence + metadata confirmation from asset list.
	windowsURLs := map[string]string{}
	for _, pa := range p.PlatformArchives {
		r := checkAssetExists(assetMap, pa.Filename, "platform-archive")
		r.OS = pa.OS
		r.Arch = pa.Arch
		if r.Status == statusFail {
			errs = append(errs, fmt.Sprintf("platform archive %q not found in GitHub Release assets", pa.Filename))
		}
		if pa.OS == "windows" {
			windowsURLs[pa.Arch] = r.BrowserDownloadURL
		}
		results = append(results, r)
	}

	// 2. Package-manager Windows archives: same files as Windows platform
	//    archives but recorded separately per arch for scoop/winget steps.
	for _, wa := range p.PackageManagerWindowsArchives {
		r := assetResult{
			AssetKind: "package-manager-windows-archive",
			OS:        "windows",
			Arch:      wa.Arch,
			Filename:  wa.Filename,
			Status:    statusOK,
		}
		if url, ok := windowsURLs[wa.Arch]; ok {
			r.BrowserDownloadURL = url
		} else {
			r.Status = statusFail
			r.Error = "asset not found in GitHub Release"
			errs = append(errs, fmt.Sprintf("package-manager Windows %s archive %q not found in GitHub Release assets", wa.Arch, wa.Filename))
		}
		results = append(results, r)
	}

	// 3. checksums.txt: existence + download + parse + entry verification.
	checksumContent := ""
	if a, ok := assetMap[p.ChecksumFile]; ok {
		content, dlErr := downloadText(a.BrowserDownloadURL)
		if dlErr != nil {
			errs = append(errs, fmt.Sprintf("download %q: %v", p.ChecksumFile, dlErr))
			results = append(results, assetResult{
				AssetKind: "checksum-file",
				Filename:  p.ChecksumFile,
				Status:    statusFail,
				Error:     dlErr.Error(),
			})
		} else {
			checksumContent = content
			results = append(results, assetResult{
				AssetKind:          "checksum-file",
				Filename:           p.ChecksumFile,
				Status:             statusOK,
				BrowserDownloadURL: a.BrowserDownloadURL,
			})
		}
	} else {
		errs = append(errs, fmt.Sprintf("%q not found in GitHub Release assets", p.ChecksumFile))
		results = append(results, assetResult{
			AssetKind: "checksum-file",
			Filename:  p.ChecksumFile,
			Status:    statusFail,
			Error:     "not found in GitHub Release assets",
		})
	}

	if checksumContent != "" {
		checked := map[string]bool{}
		for _, pa := range p.PlatformArchives {
			if !checked[pa.Filename] {
				checked[pa.Filename] = true
				if !hasChecksumEntry(checksumContent, pa.Filename) {
					errs = append(errs, fmt.Sprintf("checksums.txt: no sha256 entry for %q", pa.Filename))
				}
			}
		}
		for _, wa := range p.PackageManagerWindowsArchives {
			if !checked[wa.Filename] {
				checked[wa.Filename] = true
				if !hasChecksumEntry(checksumContent, wa.Filename) {
					errs = append(errs, fmt.Sprintf("checksums.txt: no sha256 entry for package-manager Windows %s archive %q", wa.Arch, wa.Filename))
				}
			}
		}
	}

	// 4. Signature file: existence in asset list.
	{
		r := checkAssetExists(assetMap, p.SignatureFile, "signature-file")
		if r.Status == statusFail {
			errs = append(errs, fmt.Sprintf("%q not found in GitHub Release assets", p.SignatureFile))
		}
		results = append(results, r)
	}

	// 5. SBOM assets: existence per archive in asset list.
	for _, sa := range p.SBOMAssets {
		r := checkAssetExists(assetMap, sa.SBOMFilename, "sbom")
		r.ArchiveFilename = sa.ArchiveFilename
		if r.Status == statusFail {
			errs = append(errs, fmt.Sprintf("SBOM asset %q not found in GitHub Release assets", sa.SBOMFilename))
		}
		results = append(results, r)
	}

	// 6. Tools archive: existence in asset list.
	{
		r := checkAssetExists(assetMap, p.ToolsArchive, "tools-archive")
		if r.Status == statusFail {
			errs = append(errs, fmt.Sprintf("tools archive %q not found in GitHub Release assets", p.ToolsArchive))
		}
		results = append(results, r)
	}

	// 7. Tools sidecar manifest: download + field validation.
	if a, ok := assetMap[p.ToolsSidecar]; ok {
		r, sidecarErrs := validateToolsSidecar(
			a.BrowserDownloadURL, p.ToolsSidecar,
			p.ToolsArchive, goreleaerVersion(version),
		)
		errs = append(errs, sidecarErrs...)
		results = append(results, r)
	} else {
		errs = append(errs, fmt.Sprintf("tools sidecar manifest %q not found in GitHub Release assets", p.ToolsSidecar))
		results = append(results, assetResult{
			AssetKind: "tools-sidecar",
			Filename:  p.ToolsSidecar,
			Status:    statusFail,
			Error:     "not found in GitHub Release assets",
		})
	}

	sd, marshalErr := json.Marshal(validateStepData{Assets: results})
	if marshalErr != nil {
		return nil, nil, nil, fmt.Errorf("marshal validate step data: %w", marshalErr)
	}
	return errs, nil, sd, nil
}

// Preflight rejects exec for this validate-only step.
func (h *Handler) Preflight(_ *schema.ReleasePlanEnvelope) error {
	return fmt.Errorf("release-asset is a validate-only step: exec is not supported")
}

// Exec rejects execution for this validate-only step.
func (h *Handler) Exec(_ *schema.ReleasePlanEnvelope) error {
	return fmt.Errorf("release-asset is a validate-only step: exec is not supported")
}

// --- asset name generation ---------------------------------------------------

// goreleaerVersion strips the leading "v" from a semver string to produce the
// version token GoReleaser uses in template expansions (e.g. v0.0.7 → 0.0.7).
func goreleaerVersion(version string) string {
	return strings.TrimPrefix(version, "v")
}

// buildPlanPayload computes the expected asset names for the given CLI version.
func buildPlanPayload(version string) planPayload {
	ver := goreleaerVersion(version)

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

// archiveFilename returns the GoReleaser archive filename for a given platform.
// Template: git-kura_<version>_<TitleOs>_<arch>.<ext>
// where amd64 is mapped to x86_64 to match the GoReleaser name_template.
func archiveFilename(version, goos, goarch, ext string) string {
	osTitle := strings.ToUpper(goos[:1]) + goos[1:]
	archName := goarch
	if goarch == "amd64" {
		archName = "x86_64"
	}
	return fmt.Sprintf("git-kura_%s_%s_%s.%s", version, osTitle, archName, ext)
}

// --- GitHub helpers ----------------------------------------------------------

// githubOwnerRepo extracts the GitHub owner and repo from the git remote
// origin URL, supporting both HTTPS and SSH remote formats.
func githubOwnerRepo() (string, string, error) {
	c := exec.Command("git", "remote", "get-url", "origin")
	out, err := c.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", "", fmt.Errorf("git remote get-url origin: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", "", fmt.Errorf("git remote get-url origin: %w", err)
	}
	rawURL := strings.TrimSpace(string(out))
	rawURL = strings.TrimSuffix(rawURL, ".git")

	switch {
	case strings.HasPrefix(rawURL, "git@github.com:"):
		return splitOwnerRepo(strings.TrimPrefix(rawURL, "git@github.com:"))
	case strings.HasPrefix(rawURL, "https://github.com/"):
		return splitOwnerRepo(strings.TrimPrefix(rawURL, "https://github.com/"))
	default:
		return "", "", fmt.Errorf("cannot parse GitHub owner/repo from remote URL %q", rawURL)
	}
}

func splitOwnerRepo(ownerSlashRepo string) (string, string, error) {
	idx := strings.IndexByte(ownerSlashRepo, '/')
	if idx < 1 || idx == len(ownerSlashRepo)-1 {
		return "", "", fmt.Errorf("unexpected owner/repo format %q", ownerSlashRepo)
	}
	return ownerSlashRepo[:idx], ownerSlashRepo[idx+1:], nil
}

// fetchGitHubRelease fetches the GitHub Release for the given tag using the
// unauthenticated public API. Rate-limit and release-not-found conditions are
// returned as plain errors (they become validation failures).
func fetchGitHubRelease(owner, repo, tag string) (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, tag)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build GitHub API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "git-kura-release-script")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("GitHub Release for tag %q not found (HTTP 404)", tag)
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		return nil, fmt.Errorf("GitHub API rate limit exceeded; retry after authentication or wait for the rate limit to reset")
	case resp.StatusCode != http.StatusOK:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GitHub API returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode GitHub Release response: %w", err)
	}
	return &release, nil
}

// checkAssetExists returns an assetResult reflecting whether name is present
// in the GitHub Release asset list.
func checkAssetExists(assetMap map[string]githubAsset, name, kind string) assetResult {
	a, ok := assetMap[name]
	if !ok {
		return assetResult{
			AssetKind: kind,
			Filename:  name,
			Status:    statusFail,
			Error:     "not found in GitHub Release assets",
		}
	}
	return assetResult{
		AssetKind:          kind,
		Filename:           name,
		Status:             statusOK,
		BrowserDownloadURL: a.BrowserDownloadURL,
	}
}

// downloadText fetches a URL and returns the response body as a string.
func downloadText(url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "git-kura-release-script")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}
	return string(b), nil
}

// hasChecksumEntry reports whether checksums.txt content includes a sha256
// entry for filename. GoReleaser writes lines as: <sha256hex>  <filename>
func hasChecksumEntry(content, filename string) bool {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == filename {
			return true
		}
	}
	return false
}

// toolsSidecarManifest is the JSON structure of the tools sidecar manifest.
type toolsSidecarManifest struct {
	ArchiveName       string `json:"archiveName"`
	ArchiveChecksum   string `json:"archiveChecksum"`
	ChecksumAlgorithm string `json:"checksumAlgorithm"`
	Version           string `json:"version"`
}

// validateToolsSidecar downloads and validates the tools sidecar manifest.
// expectedArchive is the expected archiveName value; expectedVersion is the
// GoReleaser version string (without "v" prefix).
func validateToolsSidecar(url, filename, expectedArchive, expectedVersion string) (assetResult, []string) {
	content, err := downloadText(url)
	if err != nil {
		return assetResult{
			AssetKind: "tools-sidecar",
			Filename:  filename,
			Status:    statusFail,
			Error:     err.Error(),
		}, []string{fmt.Sprintf("download tools sidecar manifest %q: %v", filename, err)}
	}

	var m toolsSidecarManifest
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		return assetResult{
			AssetKind: "tools-sidecar",
			Filename:  filename,
			Status:    statusFail,
			Error:     fmt.Sprintf("parse JSON: %v", err),
		}, []string{fmt.Sprintf("tools sidecar manifest %q: JSON parse error: %v", filename, err)}
	}

	checks := map[string]string{
		"versionCheck":           statusOK,
		"archiveNameCheck":       statusOK,
		"archiveChecksumCheck":   statusOK,
		"checksumAlgorithmCheck": statusOK,
	}
	var errs []string

	if m.Version != expectedVersion {
		checks["versionCheck"] = statusFail
		errs = append(errs, fmt.Sprintf("tools sidecar manifest version %q does not match expected %q", m.Version, expectedVersion))
	}
	if m.ArchiveName != expectedArchive {
		checks["archiveNameCheck"] = statusFail
		errs = append(errs, fmt.Sprintf("tools sidecar manifest archiveName %q does not match expected %q", m.ArchiveName, expectedArchive))
	}
	if m.ArchiveChecksum == "" {
		checks["archiveChecksumCheck"] = statusFail
		errs = append(errs, "tools sidecar manifest archiveChecksum is empty")
	}
	if m.ChecksumAlgorithm != "sha256" {
		checks["checksumAlgorithmCheck"] = statusFail
		errs = append(errs, fmt.Sprintf("tools sidecar manifest checksumAlgorithm %q is not sha256", m.ChecksumAlgorithm))
	}

	overall := statusOK
	for _, v := range checks {
		if v == statusFail {
			overall = statusFail
			break
		}
	}

	return assetResult{
		AssetKind:          "tools-sidecar",
		Filename:           filename,
		Status:             overall,
		BrowserDownloadURL: url,
		Checks:             checks,
	}, errs
}
