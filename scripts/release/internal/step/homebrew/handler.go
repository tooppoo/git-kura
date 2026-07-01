// Package homebrew implements the Homebrew tap Formula update release step.
//
// It updates Formula/git-kura.rb in an external Homebrew tap repository checkout
// (tooppoo/homebrew-tap-catalog) to point at the macOS arm64/amd64 archives of a
// GitHub Release. This step never commits, pushes, or opens a pull request: exec
// updates the formula, confirms no other file changed, prints the diff, and
// prints the Homebrew verification commands a maintainer must run manually
// before creating the tap repository PR.
package homebrew

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
	"regexp"
	"strings"
	"time"

	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
	"github.com/tooppoo/git-kura/scripts/release/internal/step"
)

const (
	checksumFileName = "checksums.txt"
	statusOK         = "success"
	statusFail       = "failure"
)

// formulaRelPath is the tap-relative path of the git-kura formula. The path is
// fixed by issue #123 and must not be configurable.
var formulaRelPath = filepath.Join("Formula", "git-kura.rb")

// Handler updates Formula/git-kura.rb in an external Homebrew tap repository.
type Handler struct {
	tapPath string

	httpClient *http.Client
	apiBaseURL string
	ownerRepo  func() (string, string, error)

	validated           *validateStepData
	validatedFromResult bool
}

// New returns a Handler for the homebrew step.
func New() *Handler {
	return &Handler{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		apiBaseURL: "https://api.github.com",
		ownerRepo:  githubOwnerRepo,
	}
}

// SetOptions receives the --tap path from the release command.
func (h *Handler) SetOptions(options step.Options) {
	h.tapPath = options.Tap
	h.validated = nil
	h.validatedFromResult = false
}

type planPayload struct {
	TapPath          string           `json:"tapPath"`
	FormulaPath      string           `json:"formulaPath"`
	FormulaVersion   string           `json:"formulaVersion"`
	ChecksumFile     string           `json:"checksumFile"`
	MacOSArchives    []macOSArchive   `json:"macOSArchives"`
	CommandPreview   []commandPreview `json:"commandPreview"`
	GitHubReleaseURL string           `json:"gitHubReleaseURL"`
}

type macOSArchive struct {
	GoArch      string `json:"goArch"`
	DarwinToken string `json:"darwinToken"`
	Filename    string `json:"filename"`
}

type commandPreview struct {
	Name             string   `json:"name"`
	WorkingDirectory string   `json:"workingDirectory,omitempty"`
	Command          []string `json:"command"`
}

type validateStepData struct {
	TapPath        string           `json:"tapPath"`
	FormulaPath    string           `json:"formulaPath"`
	Assets         []assetResult    `json:"assets"`
	CommandPreview []commandPreview `json:"commandPreview"`
}

type assetResult struct {
	GoArch             string            `json:"goArch"`
	DarwinToken        string            `json:"darwinToken"`
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

// BuildPayload records the external tap repository and target formula path.
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

// ValidateWithData validates the tap repo, formula, release URLs, and hashes.
func (h *Handler) ValidateWithData(plan *schema.ReleasePlanEnvelope) ([]string, []string, json.RawMessage, error) {
	errs, warnings, data, err := h.validate(plan)
	if err != nil {
		return nil, nil, nil, err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal homebrew validate step data: %w", err)
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

// PreflightWithResult verifies CLI/plan/result tap consistency and reruns validation.
func (h *Handler) PreflightWithResult(plan *schema.ReleasePlanEnvelope, result *schema.ValidateResult) error {
	h.validated = nil
	h.validatedFromResult = false

	var planData planPayload
	if err := json.Unmarshal(plan.Payload.StepData, &planData); err != nil {
		return fmt.Errorf("parse homebrew plan payload: %w", err)
	}
	var resultData validateStepData
	if len(result.StepData) == 0 {
		return fmt.Errorf("validate result is missing homebrew stepData")
	}
	if err := json.Unmarshal(result.StepData, &resultData); err != nil {
		return fmt.Errorf("parse homebrew validate stepData: %w", err)
	}

	tap, err := normalizeTapPath(h.tapPath)
	if err != nil {
		return err
	}
	if tap != planData.TapPath {
		return fmt.Errorf("CLI --tap %q does not match plan tapPath %q", tap, planData.TapPath)
	}
	if tap != resultData.TapPath {
		return fmt.Errorf("CLI --tap %q does not match validate result tapPath %q", tap, resultData.TapPath)
	}
	if planData.FormulaPath != resultData.FormulaPath {
		return fmt.Errorf("validate result formulaPath %q does not match plan formulaPath %q", resultData.FormulaPath, planData.FormulaPath)
	}
	if err := h.Preflight(plan); err != nil {
		return err
	}
	h.validatedFromResult = true
	return nil
}

// Exec updates the formula, confirms only the formula changed, prints the diff,
// and prints the manual Homebrew verification commands. It does not commit,
// push, or open a pull request.
func (h *Handler) Exec(plan *schema.ReleasePlanEnvelope) error {
	data := h.validated
	if data == nil || !h.validatedFromResult {
		return fmt.Errorf("homebrew exec requires successful validate result preflight")
	}

	var p planPayload
	if err := json.Unmarshal(plan.Payload.StepData, &p); err != nil {
		return fmt.Errorf("parse homebrew plan payload: %w", err)
	}
	updates := map[string]assetResult{}
	for _, a := range data.Assets {
		if a.Status == statusOK {
			updates[a.GoArch] = a
		}
	}
	if len(updates) != 2 {
		return fmt.Errorf("validated asset data is incomplete: need macOS arm64 and amd64")
	}

	content, err := os.ReadFile(p.FormulaPath)
	if err != nil {
		return fmt.Errorf("read formula %s: %w", p.FormulaPath, err)
	}
	updated, err := updateFormula(content, p.FormulaVersion, updates)
	if err != nil {
		return fmt.Errorf("update formula %s: %w", p.FormulaPath, err)
	}
	if err := os.WriteFile(p.FormulaPath, updated, 0o644); err != nil {
		return fmt.Errorf("write formula %s: %w", p.FormulaPath, err)
	}

	if err := ensureOnlyFormulaDiff(p.TapPath); err != nil {
		return err
	}

	fmt.Printf("homebrew formula updated: %s\n", p.FormulaPath)
	if err := printGitDiff(p.TapPath); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("NOTE: This step does not commit, push, or open a pull request in the tap repository.")
	fmt.Println("Review the diff above, run the printed verification commands, then commit and open a PR in the tap repository manually.")
	printCommandPreview(data.CommandPreview)
	return nil
}

func (h *Handler) buildPlanPayload(version string) (planPayload, error) {
	tap, err := normalizeTapPath(h.tapPath)
	if err != nil {
		return planPayload{}, err
	}
	// ownerRepo failure leaves gitHubReleaseURL empty; validate catches this.
	var releaseURL string
	if owner, repo, err := h.ownerRepo(); err == nil {
		releaseURL = gitHubReleaseURL(owner, repo, version)
	}
	return expectedPlanPayload(tap, version, releaseURL), nil
}

func expectedPlanPayload(tap, version, releaseURL string) planPayload {
	formula := filepath.Join(tap, formulaRelPath)
	return planPayload{
		TapPath:          tap,
		FormulaPath:      formula,
		FormulaVersion:   strings.TrimPrefix(version, "v"),
		ChecksumFile:     checksumFileName,
		GitHubReleaseURL: releaseURL,
		MacOSArchives: []macOSArchive{
			{GoArch: "arm64", DarwinToken: "arm64", Filename: archiveFilename(version, "arm64")},
			{GoArch: "amd64", DarwinToken: "x86_64", Filename: archiveFilename(version, "amd64")},
		},
		CommandPreview: buildCommandPreview(tap),
	}
}

func (h *Handler) validate(plan *schema.ReleasePlanEnvelope) ([]string, []string, *validateStepData, error) {
	var p planPayload
	if err := json.Unmarshal(plan.Payload.StepData, &p); err != nil {
		return nil, nil, nil, fmt.Errorf("parse homebrew plan payload: %w", err)
	}

	var errs []string
	if h.tapPath == "" {
		errs = append(errs, "--tap is required for --step homebrew")
	} else if tap, err := normalizeTapPath(h.tapPath); err != nil {
		errs = append(errs, err.Error())
	} else if tap != p.TapPath {
		errs = append(errs, fmt.Sprintf("CLI --tap %q does not match plan tapPath %q", tap, p.TapPath))
	}

	owner, repo, ownerRepoErr := h.ownerRepo()
	var releaseURL string
	if ownerRepoErr != nil {
		errs = append(errs, fmt.Sprintf("resolve GitHub owner/repo: %v", ownerRepoErr))
	} else {
		releaseURL = gitHubReleaseURL(owner, repo, plan.Payload.TargetVersion)
	}

	expected := expectedPlanPayload(p.TapPath, plan.Payload.TargetVersion, releaseURL)
	errs = append(errs, validatePlanPayload(p, expected, releaseURL)...)

	if err := validateTapRepo(p.TapPath); err != nil {
		errs = append(errs, err.Error())
	}
	if err := validateFormulaFile(p.FormulaPath); err != nil {
		errs = append(errs, err.Error())
	}

	assets, assetErrs, err := h.validateReleaseAssets(plan.Payload.TargetVersion, expected)
	if err != nil {
		return nil, nil, nil, err
	}
	errs = append(errs, assetErrs...)
	if !hasSuccessfulAsset(assets, "arm64") || !hasSuccessfulAsset(assets, "amd64") {
		errs = append(errs, "both macOS architectures (arm64 and amd64) must validate successfully")
	}

	data := &validateStepData{
		TapPath:        p.TapPath,
		FormulaPath:    p.FormulaPath,
		Assets:         assets,
		CommandPreview: expected.CommandPreview,
	}
	return errs, nil, data, nil
}

func validatePlanPayload(p, expected planPayload, releaseURL string) []string {
	var errs []string
	if p.FormulaPath != expected.FormulaPath {
		errs = append(errs, fmt.Sprintf("plan payload formulaPath %q must be %q", p.FormulaPath, expected.FormulaPath))
	}
	if p.FormulaVersion != expected.FormulaVersion {
		errs = append(errs, fmt.Sprintf("plan payload formulaVersion %q must be %q", p.FormulaVersion, expected.FormulaVersion))
	}
	if p.ChecksumFile != expected.ChecksumFile {
		errs = append(errs, fmt.Sprintf("plan payload checksumFile %q must be %q", p.ChecksumFile, expected.ChecksumFile))
	}
	if !reflect.DeepEqual(p.MacOSArchives, expected.MacOSArchives) {
		errs = append(errs, "plan payload macOSArchives must match the target macOS arm64 and amd64 release archives")
	}
	if !reflect.DeepEqual(p.CommandPreview, expected.CommandPreview) {
		errs = append(errs, "plan payload commandPreview must match the derived Homebrew validation command preview")
	}
	if releaseURL != "" && p.GitHubReleaseURL != releaseURL {
		errs = append(errs, fmt.Sprintf("plan payload gitHubReleaseURL %q must be %q", p.GitHubReleaseURL, releaseURL))
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
			GoArch:   "release",
			Filename: version,
			Status:   statusFail,
			Error:    err.Error(),
		}}, []string{err.Error()}, nil
	}

	assetMap := map[string]githubAsset{}
	for _, a := range release.Assets {
		assetMap[a.Name] = a
	}

	checksums, checksumErrs := h.fetchChecksums(assetMap, p.ChecksumFile)
	errs := append([]string{}, checksumErrs...)

	results := make([]assetResult, 0, len(p.MacOSArchives))
	for _, ma := range p.MacOSArchives {
		r := assetResult{
			GoArch:      ma.GoArch,
			DarwinToken: ma.DarwinToken,
			Filename:    ma.Filename,
			Checks:      map[string]string{},
		}
		a, ok := assetMap[ma.Filename]
		switch {
		case !ok:
			r.Status = statusFail
			r.Error = "not found in GitHub Release assets"
			r.Checks["assetListCheck"] = statusFail
			errs = append(errs, fmt.Sprintf("macOS %s archive %q not found in GitHub Release assets", ma.GoArch, ma.Filename))
		case a.State != "uploaded":
			r.Status = statusFail
			r.Error = fmt.Sprintf("asset state is %q, expected \"uploaded\"", a.State)
			r.Checks["assetListCheck"] = statusFail
			errs = append(errs, fmt.Sprintf("macOS %s archive %q is not uploaded", ma.GoArch, ma.Filename))
		case a.BrowserDownloadURL == "":
			r.Status = statusFail
			r.Error = "browser_download_url is empty"
			r.Checks["assetListCheck"] = statusFail
			errs = append(errs, fmt.Sprintf("macOS %s archive %q has empty browser_download_url", ma.GoArch, ma.Filename))
		default:
			r.BrowserDownloadURL = a.BrowserDownloadURL
			r.Checks["assetListCheck"] = statusOK
		}

		if hash, ok := checksums[ma.Filename]; ok {
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
			errs = append(errs, fmt.Sprintf("checksums.txt: no sha256 entry for %q", ma.Filename))
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

// updateFormula sets version and, per macOS architecture, the url and sha256 in
// the formula. It relies on the Homebrew convention that each architecture block
// has a `url "..."` line immediately followed by its `sha256 "..."` line, and
// that a single `version "..."` line exists. Each target must match exactly once
// so an unexpected formula shape fails loudly instead of updating the wrong line.
func updateFormula(content []byte, version string, updates map[string]assetResult) ([]byte, error) {
	text := string(content)

	verRe := regexp.MustCompile(`(version\s+")[^"]*(")`)
	if n := len(verRe.FindAllStringIndex(text, -1)); n != 1 {
		return nil, fmt.Errorf(`expected exactly one version "..." line in formula, found %d`, n)
	}
	text = verRe.ReplaceAllString(text, `${1}`+escapeReplacement(version)+`${2}`)

	for _, goArch := range []string{"arm64", "amd64"} {
		u, ok := updates[goArch]
		if !ok {
			return nil, fmt.Errorf("missing validated update for macOS %s", goArch)
		}
		pat := fmt.Sprintf(`(url\s+")[^"]*_Darwin_%s\.tar\.gz("\s+sha256\s+")[^"]*(")`, regexp.QuoteMeta(u.DarwinToken))
		re := regexp.MustCompile(pat)
		if n := len(re.FindAllStringIndex(text, -1)); n != 1 {
			return nil, fmt.Errorf("expected exactly one macOS %s url/sha256 pair in formula, found %d", goArch, n)
		}
		text = re.ReplaceAllString(text, `${1}`+escapeReplacement(u.BrowserDownloadURL)+`${2}`+escapeReplacement(u.SHA256)+`${3}`)
	}
	return []byte(text), nil
}

// escapeReplacement escapes '$' so dynamic values are inserted literally by
// Regexp.ReplaceAllString (which otherwise interprets $name / ${name}).
func escapeReplacement(s string) string {
	return strings.ReplaceAll(s, "$", "$$")
}

func ensureOnlyFormulaDiff(tap string) error {
	status, err := gitCapture(tap, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("check tap repository diff after exec: %w", err)
	}
	allowed := filepath.ToSlash(formulaRelPath)
	var unexpected []string
	hasFormulaDiff := false
	for _, line := range strings.Split(strings.TrimRight(status, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if allPathsAllowed(porcelainPaths(line), allowed) {
			hasFormulaDiff = true
		} else {
			unexpected = append(unexpected, line)
		}
	}
	if len(unexpected) > 0 {
		return fmt.Errorf("tap repository has unexpected diff outside %s:\n%s", allowed, strings.Join(unexpected, "\n"))
	}
	if !hasFormulaDiff {
		return fmt.Errorf("tap repository has no formula diff for %s after update", allowed)
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

func printGitDiff(tap string) error {
	fmt.Println("homebrew formula diff:")
	c := exec.Command("git", "-C", tap, "diff", "--", filepath.ToSlash(formulaRelPath))
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("git diff: %w", err)
	}
	return nil
}

func printCommandPreview(commands []commandPreview) {
	fmt.Println("homebrew command preview:")
	for _, c := range commands {
		if c.WorkingDirectory != "" {
			fmt.Printf("  (%s) %s\n", c.WorkingDirectory, joinCommand(c.Command))
		} else {
			fmt.Printf("  %s\n", joinCommand(c.Command))
		}
	}
}

func buildCommandPreview(tap string) []commandPreview {
	formula := "./" + filepath.ToSlash(formulaRelPath)
	return []commandPreview{
		{Name: "formula-install", WorkingDirectory: tap, Command: []string{"brew", "install", formula}},
		{Name: "formula-test", Command: []string{"brew", "test", "git-kura"}},
		{Name: "formula-audit", WorkingDirectory: tap, Command: []string{"brew", "audit", "--strict", "--online", "--formula", formula}},
		{Name: "formula-uninstall", Command: []string{"brew", "uninstall", "git-kura"}},
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
	token := goarch
	if goarch == "amd64" {
		token = "x86_64"
	}
	return fmt.Sprintf("git-kura_%s_Darwin_%s.tar.gz", version, token)
}

func hasSuccessfulAsset(assets []assetResult, goArch string) bool {
	for _, a := range assets {
		if a.GoArch == goArch && a.Status == statusOK && a.BrowserDownloadURL != "" && a.SHA256 != "" {
			return true
		}
	}
	return false
}

func normalizeTapPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("--tap is required for --step homebrew")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("normalize tap path %q: %w", path, err)
	}
	return filepath.Clean(abs), nil
}

func validateTapRepo(tap string) error {
	out, err := gitCapture(tap, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(out) != "true" {
		if err != nil {
			return fmt.Errorf("tap repository %q is not a git worktree: %v", tap, err)
		}
		return fmt.Errorf("tap repository %q is not a git worktree", tap)
	}
	top, err := gitCapture(tap, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("resolve tap repository root: %w", err)
	}
	if !samePath(tap, strings.TrimSpace(top)) {
		return fmt.Errorf("--tap must point to the Homebrew tap repository root %q, got %q", strings.TrimSpace(top), tap)
	}
	status, err := gitCapture(tap, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("check tap repository clean status: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("tap repository is not clean:\n%s", strings.TrimSpace(status))
	}
	return nil
}

func validateFormulaFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("target formula file %q does not exist", path)
	}
	if info.IsDir() {
		return fmt.Errorf("target formula path %q is a directory", path)
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

// gitHubReleaseURL returns the GitHub Release URL for the given owner, repo, and version.
func gitHubReleaseURL(owner, repo, version string) string {
	return fmt.Sprintf("https://github.com/%s/%s/releases/tag/%s", owner, repo, version)
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
	apiURL := strings.TrimRight(h.apiBaseURL, "/") + fmt.Sprintf("/repos/%s/%s/releases/tags/%s", owner, repo, tag)
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
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

func (h *Handler) downloadText(rawURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "git-kura-release-script")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d", rawURL, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}
	return string(b), nil
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
