// Package scoop implements the Scoop bucket update release step.
package scoop

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
	"github.com/tooppoo/git-kura/scripts/release/internal/step"
)

const (
	checksumFileName = "checksums.txt"
	statusOK         = "success"
	statusFail       = "failure"
)

// Handler updates bucket/git-kura.json in an external Scoop bucket repository.
type Handler struct {
	bucketPath string

	httpClient *http.Client
	apiBaseURL string
	ownerRepo  func() (string, string, error)

	validated           *validateStepData
	validatedFromResult bool
}

// New returns a Handler for the scoop step.
func New() *Handler {
	return &Handler{
		httpClient: http.DefaultClient,
		apiBaseURL: "https://api.github.com",
		ownerRepo:  githubOwnerRepo,
	}
}

// SetOptions receives the --bucket path from the release command.
func (h *Handler) SetOptions(options step.Options) {
	h.bucketPath = options.Bucket
	h.validated = nil
	h.validatedFromResult = false
}

type planPayload struct {
	BucketPath      string           `json:"bucketPath"`
	ManifestPath    string           `json:"manifestPath"`
	ManifestVersion string           `json:"manifestVersion"`
	ChecksumFile    string           `json:"checksumFile"`
	WindowsArchives []windowsArchive `json:"windowsArchives"`
	CommandPreview  []commandPreview `json:"commandPreview"`
}

type windowsArchive struct {
	ScoopArchitecture string `json:"scoopArchitecture"`
	GoArch            string `json:"goArch"`
	Filename          string `json:"filename"`
}

type commandPreview struct {
	Name             string   `json:"name"`
	WorkingDirectory string   `json:"workingDirectory,omitempty"`
	Command          []string `json:"command"`
}

type validateStepData struct {
	BucketPath     string           `json:"bucketPath"`
	ManifestPath   string           `json:"manifestPath"`
	Assets         []assetResult    `json:"assets"`
	CommandPreview []commandPreview `json:"commandPreview"`
}

type assetResult struct {
	ScoopArchitecture  string            `json:"scoopArchitecture"`
	GoArch             string            `json:"goArch"`
	Filename           string            `json:"filename"`
	Status             string            `json:"status"`
	BrowserDownloadURL string            `json:"browserDownloadUrl,omitempty"`
	SHA256             string            `json:"sha256,omitempty"`
	Checks             map[string]string `json:"checks,omitempty"`
	Error              string            `json:"error,omitempty"`
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	State              string `json:"state"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// BuildPayload records the external bucket repository and target manifest path.
func (h *Handler) BuildPayload(version string) (json.RawMessage, error) {
	p, err := h.buildPlanPayload(version)
	if err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

// Validate implements step.Handler.
func (h *Handler) Validate(plan *schema.ReleasePlanEnvelope) ([]string, []string, error) {
	errs, warnings, _, err := h.ValidateWithData(plan)
	return errs, warnings, err
}

// ValidateWithData validates the bucket repo, manifest, release URLs, and hashes.
func (h *Handler) ValidateWithData(plan *schema.ReleasePlanEnvelope) ([]string, []string, json.RawMessage, error) {
	errs, warnings, data, err := h.validate(plan)
	if err != nil {
		return nil, nil, nil, err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal scoop validate step data: %w", err)
	}
	return errs, warnings, raw, nil
}

// Preflight reruns validation immediately before exec.
func (h *Handler) Preflight(plan *schema.ReleasePlanEnvelope) error {
	h.validated = nil
	h.validatedFromResult = false
	errs, _, data, err := h.validate(plan)
	if err != nil {
		return err
	}
	if len(errs) > 0 {
		return fmt.Errorf("preflight failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	h.validated = data
	return nil
}

// PreflightWithResult verifies CLI/plan/result bucket consistency and reruns validation.
func (h *Handler) PreflightWithResult(plan *schema.ReleasePlanEnvelope, result *schema.ValidateResult) error {
	h.validated = nil
	h.validatedFromResult = false

	var planData planPayload
	if err := json.Unmarshal(plan.Payload.StepData, &planData); err != nil {
		return fmt.Errorf("parse scoop plan payload: %w", err)
	}
	var resultData validateStepData
	if len(result.StepData) == 0 {
		return fmt.Errorf("validate result is missing scoop stepData")
	}
	if err := json.Unmarshal(result.StepData, &resultData); err != nil {
		return fmt.Errorf("parse scoop validate stepData: %w", err)
	}

	bucket, err := normalizeBucketPath(h.bucketPath)
	if err != nil {
		return err
	}
	if bucket != planData.BucketPath {
		return fmt.Errorf("CLI --bucket %q does not match plan bucketPath %q", bucket, planData.BucketPath)
	}
	if bucket != resultData.BucketPath {
		return fmt.Errorf("CLI --bucket %q does not match validate result bucketPath %q", bucket, resultData.BucketPath)
	}
	if planData.ManifestPath != resultData.ManifestPath {
		return fmt.Errorf("validate result manifestPath %q does not match plan manifestPath %q", resultData.ManifestPath, planData.ManifestPath)
	}
	if err := h.Preflight(plan); err != nil {
		return err
	}
	h.validatedFromResult = true
	return nil
}

// Exec updates the Scoop manifest and prints the resulting diff and commands.
func (h *Handler) Exec(plan *schema.ReleasePlanEnvelope) error {
	data := h.validated
	if data == nil || !h.validatedFromResult {
		return fmt.Errorf("scoop exec requires successful validate result preflight")
	}

	var p planPayload
	if err := json.Unmarshal(plan.Payload.StepData, &p); err != nil {
		return fmt.Errorf("parse scoop plan payload: %w", err)
	}
	updates := map[string]assetResult{}
	for _, a := range data.Assets {
		if a.Status == statusOK {
			updates[a.ScoopArchitecture] = a
		}
	}
	if len(updates) != 2 {
		return fmt.Errorf("validated asset data is incomplete: need 64bit and arm64")
	}

	content, err := os.ReadFile(p.ManifestPath)
	if err != nil {
		return fmt.Errorf("read manifest %s: %w", p.ManifestPath, err)
	}
	updated, err := updateManifest(content, p.ManifestVersion, updates)
	if err != nil {
		return fmt.Errorf("update manifest %s: %w", p.ManifestPath, err)
	}
	if err := os.WriteFile(p.ManifestPath, updated, 0o644); err != nil {
		return fmt.Errorf("write manifest %s: %w", p.ManifestPath, err)
	}

	if err := ensureOnlyManifestDiff(p.BucketPath); err != nil {
		return err
	}

	fmt.Printf("scoop manifest updated: %s\n", p.ManifestPath)
	if err := printGitDiff(p.BucketPath); err != nil {
		return err
	}
	printCommandPreview(data.CommandPreview)
	return nil
}

func (h *Handler) buildPlanPayload(version string) (planPayload, error) {
	bucket, err := normalizeBucketPath(h.bucketPath)
	if err != nil {
		return planPayload{}, err
	}
	return expectedPlanPayload(bucket, version), nil
}

func expectedPlanPayload(bucket, version string) planPayload {
	manifest := filepath.Join(bucket, "bucket", "git-kura.json")
	return planPayload{
		BucketPath:      bucket,
		ManifestPath:    manifest,
		ManifestVersion: strings.TrimPrefix(version, "v"),
		ChecksumFile:    checksumFileName,
		WindowsArchives: []windowsArchive{
			{ScoopArchitecture: "64bit", GoArch: "amd64", Filename: archiveFilename(version, "amd64")},
			{ScoopArchitecture: "arm64", GoArch: "arm64", Filename: archiveFilename(version, "arm64")},
		},
		CommandPreview: buildCommandPreview(bucket),
	}
}

func (h *Handler) validate(plan *schema.ReleasePlanEnvelope) ([]string, []string, *validateStepData, error) {
	var p planPayload
	if err := json.Unmarshal(plan.Payload.StepData, &p); err != nil {
		return nil, nil, nil, fmt.Errorf("parse scoop plan payload: %w", err)
	}

	var errs []string
	if h.bucketPath == "" {
		errs = append(errs, "--bucket is required for --step scoop")
	} else if bucket, err := normalizeBucketPath(h.bucketPath); err != nil {
		errs = append(errs, err.Error())
	} else if bucket != p.BucketPath {
		errs = append(errs, fmt.Sprintf("CLI --bucket %q does not match plan bucketPath %q", bucket, p.BucketPath))
	}
	errs = append(errs, validatePlanPayload(plan.Payload.TargetVersion, p)...)

	if err := validateBucketRepo(p.BucketPath); err != nil {
		errs = append(errs, err.Error())
	}
	if err := validateManifestFile(p.ManifestPath); err != nil {
		errs = append(errs, err.Error())
	}

	manifestArchErrs := validateManifestArchitectures(p.ManifestPath)
	errs = append(errs, manifestArchErrs...)

	expected := expectedPlanPayload(p.BucketPath, plan.Payload.TargetVersion)
	assets, assetErrs, err := h.validateReleaseAssets(plan.Payload.TargetVersion, expected)
	if err != nil {
		return nil, nil, nil, err
	}
	errs = append(errs, assetErrs...)
	if !hasSuccessfulAsset(assets, "64bit") || !hasSuccessfulAsset(assets, "arm64") {
		errs = append(errs, "both Scoop architectures (64bit and arm64) must validate successfully")
	}

	data := &validateStepData{
		BucketPath:     p.BucketPath,
		ManifestPath:   p.ManifestPath,
		Assets:         assets,
		CommandPreview: expected.CommandPreview,
	}
	return errs, nil, data, nil
}

func validatePlanPayload(version string, p planPayload) []string {
	expected := expectedPlanPayload(p.BucketPath, version)
	var errs []string
	if p.ManifestPath != expected.ManifestPath {
		errs = append(errs, fmt.Sprintf("plan payload manifestPath %q must be %q", p.ManifestPath, expected.ManifestPath))
	}
	if p.ManifestVersion != expected.ManifestVersion {
		errs = append(errs, fmt.Sprintf("plan payload manifestVersion %q must be %q", p.ManifestVersion, expected.ManifestVersion))
	}
	if p.ChecksumFile != expected.ChecksumFile {
		errs = append(errs, fmt.Sprintf("plan payload checksumFile %q must be %q", p.ChecksumFile, expected.ChecksumFile))
	}
	if !reflect.DeepEqual(p.WindowsArchives, expected.WindowsArchives) {
		errs = append(errs, "plan payload windowsArchives must match the target Windows amd64 and arm64 release archives")
	}
	if !reflect.DeepEqual(p.CommandPreview, expected.CommandPreview) {
		errs = append(errs, "plan payload commandPreview must match the derived Scoop validation command preview")
	}
	return errs
}

func (h *Handler) validateReleaseAssets(version string, p planPayload) ([]assetResult, []string, error) {
	owner, repo, err := h.ownerRepo()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve GitHub owner/repo: %w", err)
	}
	release, err := h.fetchGitHubRelease(owner, repo, version)
	if err != nil {
		return []assetResult{{
			ScoopArchitecture: "release",
			Filename:          version,
			Status:            statusFail,
			Error:             err.Error(),
		}}, []string{err.Error()}, nil
	}

	assetMap := map[string]githubAsset{}
	for _, a := range release.Assets {
		assetMap[a.Name] = a
	}

	checksums, checksumErrs := h.fetchChecksums(assetMap, p.ChecksumFile)
	errs := append([]string{}, checksumErrs...)

	results := make([]assetResult, 0, len(p.WindowsArchives))
	for _, wa := range p.WindowsArchives {
		r := assetResult{
			ScoopArchitecture: wa.ScoopArchitecture,
			GoArch:            wa.GoArch,
			Filename:          wa.Filename,
			Checks:            map[string]string{},
		}
		a, ok := assetMap[wa.Filename]
		switch {
		case !ok:
			r.Status = statusFail
			r.Error = "not found in GitHub Release assets"
			r.Checks["assetListCheck"] = statusFail
			errs = append(errs, fmt.Sprintf("Windows %s archive %q not found in GitHub Release assets", wa.GoArch, wa.Filename))
		case a.State != "uploaded":
			r.Status = statusFail
			r.Error = fmt.Sprintf("asset state is %q, expected \"uploaded\"", a.State)
			r.Checks["assetListCheck"] = statusFail
			errs = append(errs, fmt.Sprintf("Windows %s archive %q is not uploaded", wa.GoArch, wa.Filename))
		case a.BrowserDownloadURL == "":
			r.Status = statusFail
			r.Error = "browser_download_url is empty"
			r.Checks["assetListCheck"] = statusFail
			errs = append(errs, fmt.Sprintf("Windows %s archive %q has empty browser_download_url", wa.GoArch, wa.Filename))
		default:
			r.BrowserDownloadURL = a.BrowserDownloadURL
			r.Checks["assetListCheck"] = statusOK
		}

		if hash, ok := checksums[wa.Filename]; ok {
			r.SHA256 = hash
			r.Checks["checksumEntryCheck"] = statusOK
		} else {
			if r.Status == "" {
				r.Status = statusFail
			}
			if r.Error == "" {
				r.Error = "sha256 entry not found in checksums.txt"
			}
			r.Checks["checksumEntryCheck"] = statusFail
			errs = append(errs, fmt.Sprintf("checksums.txt: no sha256 entry for %q", wa.Filename))
		}

		if r.Status == "" {
			r.Status = statusOK
		}
		results = append(results, r)
	}
	return results, errs, nil
}

func (h *Handler) fetchChecksums(assetMap map[string]githubAsset, name string) (map[string]string, []string) {
	a, ok := assetMap[name]
	if !ok {
		return map[string]string{}, []string{fmt.Sprintf("%q not found in GitHub Release assets", name)}
	}
	if a.State != "uploaded" || a.BrowserDownloadURL == "" {
		return map[string]string{}, []string{fmt.Sprintf("%q has invalid GitHub asset metadata", name)}
	}
	content, err := h.downloadText(a.BrowserDownloadURL)
	if err != nil {
		return map[string]string{}, []string{fmt.Sprintf("download %q: %v", name, err)}
	}
	return parseChecksums(content), nil
}

func normalizeBucketPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("--bucket is required for --step scoop")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("normalize bucket path %q: %w", path, err)
	}
	return filepath.Clean(abs), nil
}

func validateBucketRepo(bucket string) error {
	out, err := gitCapture(bucket, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(out) != "true" {
		if err != nil {
			return fmt.Errorf("bucket repository %q is not a git worktree: %v", bucket, err)
		}
		return fmt.Errorf("bucket repository %q is not a git worktree", bucket)
	}
	top, err := gitCapture(bucket, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("resolve bucket repository root: %w", err)
	}
	if !samePath(bucket, strings.TrimSpace(top)) {
		return fmt.Errorf("--bucket must point to the Scoop bucket repository root %q, got %q", strings.TrimSpace(top), bucket)
	}
	status, err := gitCapture(bucket, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("check bucket repository clean status: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("bucket repository is not clean:\n%s", strings.TrimSpace(status))
	}
	return nil
}

func samePath(a, b string) bool {
	clean := func(p string) string {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		return filepath.Clean(p)
	}
	return clean(a) == clean(b)
}

func validateManifestFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("target manifest file %q does not exist", path)
	}
	if info.IsDir() {
		return fmt.Errorf("target manifest path %q is a directory", path)
	}
	return nil
}

func validateManifestArchitectures(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("read target manifest %q: %v", path, err)}
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return []string{fmt.Sprintf("parse target manifest %q as JSON: %v", path, err)}
	}
	arch, ok := root["architecture"].(map[string]any)
	if !ok {
		return []string{`target manifest must contain object field "architecture"`}
	}
	var errs []string
	for _, key := range []string{"64bit", "arm64"} {
		if _, ok := arch[key].(map[string]any); !ok {
			errs = append(errs, fmt.Sprintf("target manifest must contain architecture.%s object", key))
		}
	}
	return errs
}

func hasSuccessfulAsset(assets []assetResult, arch string) bool {
	for _, a := range assets {
		if a.ScoopArchitecture == arch && a.Status == statusOK && a.BrowserDownloadURL != "" && a.SHA256 != "" {
			return true
		}
	}
	return false
}

func updateManifest(content []byte, version string, updates map[string]assetResult) ([]byte, error) {
	var root map[string]any
	if err := json.Unmarshal(content, &root); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	arch, ok := root["architecture"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf(`manifest must contain object field "architecture"`)
	}
	for _, key := range []string{"64bit", "arm64"} {
		update, ok := updates[key]
		if !ok {
			return nil, fmt.Errorf("missing validated update for architecture.%s", key)
		}
		entry, ok := arch[key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("manifest must contain architecture.%s object", key)
		}
		entry["url"] = update.BrowserDownloadURL
		entry["hash"] = update.SHA256
	}
	root["version"] = version
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal JSON: %w", err)
	}
	return append(out, '\n'), nil
}

func ensureOnlyManifestDiff(bucket string) error {
	status, err := gitCapture(bucket, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("check bucket repository diff after exec: %w", err)
	}
	allowed := filepath.ToSlash(filepath.Join("bucket", "git-kura.json"))
	var unexpected []string
	for _, line := range strings.Split(strings.TrimRight(status, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !allPathsAllowed(porcelainPaths(line), allowed) {
			unexpected = append(unexpected, line)
		}
	}
	if len(unexpected) > 0 {
		return fmt.Errorf("bucket repository has unexpected diff outside %s:\n%s", allowed, strings.Join(unexpected, "\n"))
	}
	return nil
}

func allPathsAllowed(paths []string, allowed string) bool {
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if path != allowed {
			return false
		}
	}
	return true
}

func porcelainPaths(line string) []string {
	if len(line) <= 3 {
		return []string{filepath.ToSlash(strings.TrimSpace(line))}
	}
	path := strings.TrimSpace(line[3:])
	if strings.Contains(path, " -> ") {
		parts := strings.Split(path, " -> ")
		paths := make([]string, 0, len(parts))
		for _, part := range parts {
			paths = append(paths, filepath.ToSlash(strings.TrimSpace(part)))
		}
		return paths
	}
	return []string{filepath.ToSlash(path)}
}

func printGitDiff(bucket string) error {
	fmt.Println("scoop manifest diff:")
	c := exec.Command("git", "-C", bucket, "diff", "--", filepath.ToSlash(filepath.Join("bucket", "git-kura.json")))
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("git diff: %w", err)
	}
	return nil
}

func printCommandPreview(commands []commandPreview) {
	fmt.Println("scoop command preview:")
	for _, c := range commands {
		if c.WorkingDirectory != "" {
			fmt.Printf("  (%s) %s\n", c.WorkingDirectory, joinCommand(c.Command))
		} else {
			fmt.Printf("  %s\n", joinCommand(c.Command))
		}
	}
}

func buildCommandPreview(bucket string) []commandPreview {
	return []commandPreview{
		{
			Name:             "manifest-validation",
			WorkingDirectory: bucket,
			Command:          []string{"pwsh", "-NoProfile", "-File", filepath.Join(bucket, "bin", "checkver.ps1"), "git-kura"},
		},
		{
			Name:             "bucket-test",
			WorkingDirectory: bucket,
			Command:          []string{"pwsh", "-NoProfile", "-File", filepath.Join(bucket, "bin", "test.ps1"), "git-kura"},
		},
		{
			Name:    "install-verification",
			Command: []string{"scoop", "install", filepath.Join(bucket, "bucket", "git-kura.json")},
		},
		{
			Name:    "uninstall-after-verification",
			Command: []string{"scoop", "uninstall", "git-kura"},
		},
	}
}

func joinCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\'' || r == '"' || r == '\\'
	}) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func archiveFilename(version, goarch string) string {
	archName := goarch
	if goarch == "amd64" {
		archName = "x86_64"
	}
	return fmt.Sprintf("git-kura_%s_Windows_%s.zip", version, archName)
}

func parseChecksums(content string) map[string]string {
	entries := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			hash := strings.ToLower(fields[0])
			if isSHA256Hex(hash) {
				entries[strings.TrimPrefix(fields[1], "*")] = hash
			}
		}
	}
	return entries
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

func gitCapture(dir string, args ...string) (string, error) {
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := c.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git -C %s %s: %s", dir, strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git -C %s %s: %w", dir, strings.Join(args, " "), err)
	}
	return string(out), nil
}

func githubOwnerRepo() (string, string, error) {
	c := exec.Command("git", "remote", "get-url", "origin")
	out, err := c.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", "", fmt.Errorf("git remote get-url origin: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", "", fmt.Errorf("git remote get-url origin: %w", err)
	}
	return ownerRepoFromRemoteURL(string(out))
}

func ownerRepoFromRemoteURL(rawURL string) (string, string, error) {
	rawURL = strings.TrimSuffix(strings.TrimSpace(rawURL), ".git")
	switch {
	case strings.HasPrefix(rawURL, "git@github.com:"):
		return splitOwnerRepo(strings.TrimPrefix(rawURL, "git@github.com:"))
	case strings.HasPrefix(rawURL, "https://github.com/"):
		return splitOwnerRepo(strings.TrimPrefix(rawURL, "https://github.com/"))
	case strings.HasPrefix(rawURL, "https://"):
		u, err := url.Parse(rawURL)
		if err == nil && strings.EqualFold(u.Hostname(), "github.com") {
			return splitOwnerRepo(strings.Trim(u.Path, "/"))
		}
	}
	return "", "", fmt.Errorf("cannot parse GitHub owner/repo from origin remote URL")
}

func splitOwnerRepo(ownerSlashRepo string) (string, string, error) {
	idx := strings.IndexByte(ownerSlashRepo, '/')
	if idx < 1 || idx == len(ownerSlashRepo)-1 {
		return "", "", fmt.Errorf("unexpected owner/repo format")
	}
	owner, repo := ownerSlashRepo[:idx], ownerSlashRepo[idx+1:]
	if strings.Contains(owner, "@") || strings.Contains(repo, "@") {
		return "", "", fmt.Errorf("unexpected owner/repo format")
	}
	return owner, repo, nil
}

func (h *Handler) fetchGitHubRelease(owner, repo, tag string) (*githubRelease, error) {
	url := strings.TrimRight(h.apiBaseURL, "/") + fmt.Sprintf("/repos/%s/%s/releases/tags/%s", owner, repo, tag)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build GitHub API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "git-kura-release-script")

	resp, err := h.httpClient.Do(req)
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

func (h *Handler) downloadText(url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "git-kura-release-script")

	resp, err := h.httpClient.Do(req)
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
