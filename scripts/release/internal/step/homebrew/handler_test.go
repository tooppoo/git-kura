package homebrew

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
	"github.com/tooppoo/git-kura/scripts/release/internal/step"
)

const (
	testARM64Hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testAMD64Hash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestBuildPayload_NormalizesTapAndFormulaPath(t *testing.T) {
	dir := t.TempDir()
	h := New()
	h.ownerRepo = func() (string, string, error) { return "tooppoo", "git-kura", nil }
	h.SetOptions(step.Options{Tap: filepath.Join(dir, "..", filepath.Base(dir))})

	raw, err := h.BuildPayload("v0.0.7")
	if err != nil {
		t.Fatalf("BuildPayload: %v", err)
	}
	var p planPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("parse payload: %v", err)
	}

	wantTap, _ := filepath.Abs(dir)
	if p.TapPath != wantTap {
		t.Errorf("tapPath = %q, want %q", p.TapPath, wantTap)
	}
	if p.FormulaPath != filepath.Join(wantTap, "Formula", "git-kura.rb") {
		t.Errorf("formulaPath = %q", p.FormulaPath)
	}
	if p.FormulaVersion != "0.0.7" {
		t.Errorf("formulaVersion = %q, want 0.0.7", p.FormulaVersion)
	}
	if p.GitHubReleaseURL != "https://github.com/tooppoo/git-kura/releases/tag/v0.0.7" {
		t.Errorf("gitHubReleaseURL = %q", p.GitHubReleaseURL)
	}
	if len(p.MacOSArchives) != 2 {
		t.Fatalf("expected two macOS archives, got %d", len(p.MacOSArchives))
	}
	if p.MacOSArchives[0].Filename != "git-kura_v0.0.7_Darwin_arm64.tar.gz" {
		t.Errorf("arm64 archive = %q", p.MacOSArchives[0].Filename)
	}
	if p.MacOSArchives[1].Filename != "git-kura_v0.0.7_Darwin_x86_64.tar.gz" {
		t.Errorf("amd64 archive = %q", p.MacOSArchives[1].Filename)
	}
	if len(p.CommandPreview) == 0 {
		t.Fatal("commandPreview must not be empty")
	}
}

func TestBuildPayload_RequiresTap(t *testing.T) {
	h := New()
	h.ownerRepo = func() (string, string, error) { return "tooppoo", "git-kura", nil }
	if _, err := h.BuildPayload("v0.0.7"); err == nil {
		t.Fatal("expected --tap requirement error")
	}
}

func TestValidateWithData_Success(t *testing.T) {
	tap := newTapRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Tap: tap})
	plan := planForHandler(t, h, "v0.0.7")

	errs, warnings, raw, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("ValidateWithData internal error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	var data validateStepData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("parse stepData: %v", err)
	}
	if data.TapPath != tap {
		t.Errorf("tapPath = %q, want %q", data.TapPath, tap)
	}
	if data.FormulaPath != filepath.Join(tap, "Formula", "git-kura.rb") {
		t.Errorf("formulaPath = %q", data.FormulaPath)
	}
	if !hasSuccessfulAsset(data.Assets, "arm64") || !hasSuccessfulAsset(data.Assets, "amd64") {
		t.Fatalf("expected successful assets for both architectures: %+v", data.Assets)
	}
}

func TestValidateWithData_DirtyTapFails(t *testing.T) {
	tap := newTapRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Tap: tap})
	plan := planForHandler(t, h, "v0.0.7")

	if err := os.WriteFile(filepath.Join(tap, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	errs, _, _, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !contains(errs, "tap repository is not clean") {
		t.Fatalf("expected dirty tap validation error, got %v", errs)
	}
}

func TestValidateWithData_TapMustBeRepositoryRoot(t *testing.T) {
	tap := newTapRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Tap: filepath.Join(tap, "Formula")})
	plan := planForHandler(t, h, "v0.0.7")

	errs, _, _, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !contains(errs, "--tap must point to the Homebrew tap repository root") {
		t.Fatalf("expected tap root validation error, got %v", errs)
	}
}

func TestValidateWithData_MissingFormulaFails(t *testing.T) {
	tap := newTapRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Tap: tap})
	plan := planForHandler(t, h, "v0.0.7")

	if err := os.Remove(filepath.Join(tap, "Formula", "git-kura.rb")); err != nil {
		t.Fatalf("remove formula: %v", err)
	}
	git(t, tap, "add", "-A")
	git(t, tap, "commit", "-m", "remove formula")

	errs, _, _, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !contains(errs, "target formula file") {
		t.Fatalf("expected missing formula error, got %v", errs)
	}
}

func TestValidateWithData_RejectsPlanFormulaPathOutsideTap(t *testing.T) {
	tap := newTapRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Tap: tap})
	plan := planForHandler(t, h, "v0.0.7")
	p := planPayloadFromPlan(t, plan)
	p.FormulaPath = filepath.Join(t.TempDir(), "git-kura.rb")
	replacePlanPayload(t, plan, p)

	errs, _, _, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !contains(errs, "plan payload formulaPath") {
		t.Fatalf("expected formulaPath validation error, got %v", errs)
	}
}

func TestValidateWithData_RejectsPlanArchiveFilenames(t *testing.T) {
	tap := newTapRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Tap: tap})
	plan := planForHandler(t, h, "v0.0.7")
	p := planPayloadFromPlan(t, plan)
	p.MacOSArchives[0].Filename = "git-kura_v0.0.7_Darwin_wrong.tar.gz"
	replacePlanPayload(t, plan, p)

	errs, _, _, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !contains(errs, "plan payload macOSArchives") {
		t.Fatalf("expected macOSArchives validation error, got %v", errs)
	}
}

func TestValidateWithData_MissingArm64AssetFails(t *testing.T) {
	tap := newTapRepo(t)
	h, srv := newTestHandlerWithRelease(t, func(baseURL string) []githubAsset {
		return []githubAsset{
			{Name: "git-kura_v0.0.7_Darwin_x86_64.tar.gz", State: "uploaded", BrowserDownloadURL: baseURL + "/downloads/git-kura_v0.0.7_Darwin_x86_64.tar.gz"},
			{Name: checksumFileName, State: "uploaded", BrowserDownloadURL: baseURL + "/downloads/checksums.txt"},
		}
	}, defaultChecksumContent())
	defer srv.Close()
	h.SetOptions(step.Options{Tap: tap})
	plan := planForHandler(t, h, "v0.0.7")

	errs, _, _, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !contains(errs, `macOS arm64 archive "git-kura_v0.0.7_Darwin_arm64.tar.gz" not found`) {
		t.Fatalf("expected missing arm64 asset error, got %v", errs)
	}
	if !contains(errs, "both macOS architectures (arm64 and amd64) must validate successfully") {
		t.Fatalf("expected both-architectures validation error, got %v", errs)
	}
}

func TestValidateWithData_MissingAmd64ChecksumFails(t *testing.T) {
	tap := newTapRepo(t)
	h, srv := newTestHandlerWithRelease(t, defaultAssets, testARM64Hash+"  git-kura_v0.0.7_Darwin_arm64.tar.gz\n")
	defer srv.Close()
	h.SetOptions(step.Options{Tap: tap})
	plan := planForHandler(t, h, "v0.0.7")

	errs, _, _, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !contains(errs, `checksums.txt: no sha256 entry for "git-kura_v0.0.7_Darwin_x86_64.tar.gz"`) {
		t.Fatalf("expected missing amd64 checksum error, got %v", errs)
	}
}

func TestPreflightWithResult_DetectsTapMismatch(t *testing.T) {
	tap := newTapRepo(t)
	other := t.TempDir()
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Tap: tap})
	plan := planForHandler(t, h, "v0.0.7")
	_, _, raw, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("ValidateWithData: %v", err)
	}
	result := validateResultFor(raw)

	h.SetOptions(step.Options{Tap: other})
	if err := h.PreflightWithResult(plan, result); err == nil {
		t.Fatal("expected tap mismatch error")
	}
}

func TestPreflightAndExec_UpdatesFormulaOnly(t *testing.T) {
	tap := newTapRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Tap: tap})
	plan := planForHandler(t, h, "v0.0.7")
	_, _, raw, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("ValidateWithData: %v", err)
	}
	result := validateResultFor(raw)
	if err := h.PreflightWithResult(plan, result); err != nil {
		t.Fatalf("PreflightWithResult: %v", err)
	}
	if err := h.Exec(plan); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tap, "Formula", "git-kura.rb"))
	if err != nil {
		t.Fatalf("read formula: %v", err)
	}
	formula := string(content)
	if !strings.Contains(formula, `version "0.0.7"`) {
		t.Errorf("formula version not updated:\n%s", formula)
	}
	if !strings.Contains(formula, "git-kura_v0.0.7_Darwin_arm64.tar.gz") {
		t.Errorf("arm64 url not updated:\n%s", formula)
	}
	if !strings.Contains(formula, "git-kura_v0.0.7_Darwin_x86_64.tar.gz") {
		t.Errorf("amd64 url not updated:\n%s", formula)
	}
	if !strings.Contains(formula, testARM64Hash) || !strings.Contains(formula, testAMD64Hash) {
		t.Errorf("sha256 not updated:\n%s", formula)
	}
	// Old placeholder hashes must be gone.
	if strings.Contains(formula, "old-arm64-hash") || strings.Contains(formula, "old-x86_64-hash") {
		t.Errorf("old hashes still present:\n%s", formula)
	}

	// Working tree still has the formula change and it is the only change.
	status := strings.TrimSpace(gitOutput(t, tap, "status", "--porcelain"))
	if status != "M Formula/git-kura.rb" && status != " M Formula/git-kura.rb" {
		t.Errorf("expected only formula to be modified, got %q", status)
	}

	// No branch, no commit: still on main with the original commit.
	branch := strings.TrimSpace(gitOutput(t, tap, "branch", "--show-current"))
	if branch != "main" {
		t.Errorf("expected to stay on main, got %q", branch)
	}
	commitMsg := strings.TrimSpace(gitOutput(t, tap, "log", "--format=%s", "-1"))
	if commitMsg != "init" {
		t.Errorf("expected no new commit, last commit = %q", commitMsg)
	}
}

func TestExec_FailsWhenFormulaUpdateProducesNoDiff(t *testing.T) {
	tap := newTapRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()

	// Pre-write the already-updated formula and commit it so exec produces no diff.
	if err := os.WriteFile(filepath.Join(tap, "Formula", "git-kura.rb"), updatedFormulaContent(srv.URL), 0o644); err != nil {
		t.Fatalf("write updated formula: %v", err)
	}
	git(t, tap, "add", "Formula/git-kura.rb")
	git(t, tap, "commit", "-m", "already updated formula")

	h.SetOptions(step.Options{Tap: tap})
	plan := planForHandler(t, h, "v0.0.7")
	_, _, raw, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("ValidateWithData: %v", err)
	}
	result := validateResultFor(raw)
	if err := h.PreflightWithResult(plan, result); err != nil {
		t.Fatalf("PreflightWithResult: %v", err)
	}

	err = h.Exec(plan)
	if err == nil {
		t.Fatal("expected Exec to fail when formula update produces no diff")
	}
	if !strings.Contains(err.Error(), "no formula diff") {
		t.Errorf("expected no formula diff error, got %v", err)
	}
}

func TestExec_FailsWhenUnexpectedDiffExistsAfterUpdate(t *testing.T) {
	tap := newTapRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Tap: tap})
	plan := planForHandler(t, h, "v0.0.7")
	_, _, raw, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("ValidateWithData: %v", err)
	}
	result := validateResultFor(raw)
	if err := h.PreflightWithResult(plan, result); err != nil {
		t.Fatalf("PreflightWithResult: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tap, "unexpected.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write unexpected file: %v", err)
	}
	if err := h.Exec(plan); err == nil {
		t.Fatal("expected unexpected diff error")
	}
}

func TestExec_RequiresValidateResultPreflight(t *testing.T) {
	tap := newTapRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Tap: tap})
	plan := planForHandler(t, h, "v0.0.7")

	if err := h.Preflight(plan); err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if err := h.Exec(plan); err == nil {
		t.Fatal("expected Exec to require PreflightWithResult")
	}
}

func TestFailedPreflightWithResultClearsPreviousValidation(t *testing.T) {
	tap := newTapRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Tap: tap})
	plan := planForHandler(t, h, "v0.0.7")
	_, _, raw, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("ValidateWithData: %v", err)
	}
	result := validateResultFor(raw)
	if err := h.PreflightWithResult(plan, result); err != nil {
		t.Fatalf("PreflightWithResult: %v", err)
	}

	badResult := *result
	badResult.StepData = nil
	if err := h.PreflightWithResult(plan, &badResult); err == nil {
		t.Fatal("expected PreflightWithResult to fail with missing stepData")
	}
	if err := h.Exec(plan); err == nil {
		t.Fatal("expected Exec to reject stale validation after failed PreflightWithResult")
	}
}

func TestUpdateFormula_ReplacesVersionURLAndSHA(t *testing.T) {
	content := []byte(baseFormula("0.0.6",
		"https://example.com/old_Darwin_arm64.tar.gz", "old-arm64-hash",
		"https://example.com/old_Darwin_x86_64.tar.gz", "old-x86_64-hash"))
	updates := map[string]assetResult{
		"arm64": {GoArch: "arm64", DarwinToken: "arm64", BrowserDownloadURL: "https://dl/git-kura_v0.0.7_Darwin_arm64.tar.gz", SHA256: testARM64Hash},
		"amd64": {GoArch: "amd64", DarwinToken: "x86_64", BrowserDownloadURL: "https://dl/git-kura_v0.0.7_Darwin_x86_64.tar.gz", SHA256: testAMD64Hash},
	}
	out, err := updateFormula(content, "0.0.7", updates)
	if err != nil {
		t.Fatalf("updateFormula: %v", err)
	}
	got := string(out)
	for _, want := range []string{
		`version "0.0.7"`,
		`url "https://dl/git-kura_v0.0.7_Darwin_arm64.tar.gz"`,
		`sha256 "` + testARM64Hash + `"`,
		`url "https://dl/git-kura_v0.0.7_Darwin_x86_64.tar.gz"`,
		`sha256 "` + testAMD64Hash + `"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("updated formula missing %q:\n%s", want, got)
		}
	}
	// version.to_s in the test block must be preserved (not treated as a version DSL line).
	if !strings.Contains(got, "version.to_s") {
		t.Errorf("test block version.to_s was corrupted:\n%s", got)
	}
}

func TestUpdateFormula_FailsWhenArchPairMissing(t *testing.T) {
	// Formula without an arm64 url/sha256 pair.
	content := []byte(`class GitKura < Formula
  version "0.0.6"
  on_macos do
    on_intel do
      url "https://example.com/old_Darwin_x86_64.tar.gz"
      sha256 "old-x86_64-hash"
    end
  end
end
`)
	updates := map[string]assetResult{
		"arm64": {DarwinToken: "arm64", BrowserDownloadURL: "https://dl/a", SHA256: testARM64Hash},
		"amd64": {DarwinToken: "x86_64", BrowserDownloadURL: "https://dl/b", SHA256: testAMD64Hash},
	}
	if _, err := updateFormula(content, "0.0.7", updates); err == nil {
		t.Fatal("expected error when arm64 pair is missing")
	}
}

func TestParseChecksums(t *testing.T) {
	content := testARM64Hash + "  git-kura_v0.0.7_Darwin_arm64.tar.gz\n" +
		strings.ToUpper(testAMD64Hash) + "  uppercase-normalized.tar.gz\n" +
		testAMD64Hash + "  *binary-marker.tar.gz\n" +
		"bad  bad.tar.gz\n"
	got := parseChecksums(content)
	if got["git-kura_v0.0.7_Darwin_arm64.tar.gz"] != testARM64Hash {
		t.Fatalf("expected arm64 checksum entry, got %#v", got)
	}
	if got["uppercase-normalized.tar.gz"] != testAMD64Hash {
		t.Fatalf("expected uppercase checksum to be normalized, got %#v", got)
	}
	if got["binary-marker.tar.gz"] != testAMD64Hash {
		t.Fatalf("expected binary marker filename to be normalized, got %#v", got)
	}
}

func TestOwnerRepoFromRemoteURL_DoesNotLeakCredentialRemote(t *testing.T) {
	owner, repo, err := ownerRepoFromRemoteURL("https://user:secret-token@github.com/tooppoo/homebrew-tap-catalog.git")
	if err != nil {
		t.Fatalf("ownerRepoFromRemoteURL: %v", err)
	}
	if owner != "tooppoo" || repo != "homebrew-tap-catalog" {
		t.Fatalf("owner/repo = %s/%s, want tooppoo/homebrew-tap-catalog", owner, repo)
	}

	_, _, err = ownerRepoFromRemoteURL("https://user:secret-token@example.com/tooppoo/git-kura.git")
	if err == nil {
		t.Fatal("expected unsupported remote URL error")
	}
	if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "user:") {
		t.Fatalf("error leaked remote credentials: %v", err)
	}
}

func TestPorcelainPaths_RenameMustTouchOnlyFormula(t *testing.T) {
	allowed := filepath.ToSlash(filepath.Join("Formula", "git-kura.rb"))
	if !allPathsAllowed(porcelainPaths(" M Formula/git-kura.rb"), allowed) {
		t.Fatal("expected direct formula modification to be allowed")
	}
	if allPathsAllowed(porcelainPaths("R  Formula/other.rb -> Formula/git-kura.rb"), allowed) {
		t.Fatal("rename from another path into the formula must not be allowed")
	}
}

// --- fixtures ----------------------------------------------------------------

func newTestHandler(t *testing.T) (*Handler, *httptest.Server) {
	t.Helper()
	return newTestHandlerWithRelease(t, defaultAssets, defaultChecksumContent())
}

func newTestHandlerWithRelease(t *testing.T, assets func(string) []githubAsset, checksumContent string) (*Handler, *httptest.Server) {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/tooppoo/git-kura/releases/tags/v0.0.7":
			writeJSON(t, w, githubRelease{
				TagName: "v0.0.7",
				Assets:  assets(srv.URL),
			})
		case "/downloads/checksums.txt":
			_, _ = w.Write([]byte(checksumContent))
		default:
			http.NotFound(w, r)
		}
	}))
	h := New()
	h.apiBaseURL = srv.URL
	h.ownerRepo = func() (string, string, error) { return "tooppoo", "git-kura", nil }
	return h, srv
}

func defaultAssets(baseURL string) []githubAsset {
	return []githubAsset{
		{Name: "git-kura_v0.0.7_Darwin_arm64.tar.gz", State: "uploaded", BrowserDownloadURL: baseURL + "/downloads/git-kura_v0.0.7_Darwin_arm64.tar.gz"},
		{Name: "git-kura_v0.0.7_Darwin_x86_64.tar.gz", State: "uploaded", BrowserDownloadURL: baseURL + "/downloads/git-kura_v0.0.7_Darwin_x86_64.tar.gz"},
		{Name: checksumFileName, State: "uploaded", BrowserDownloadURL: baseURL + "/downloads/checksums.txt"},
	}
}

func defaultChecksumContent() string {
	return testARM64Hash + "  git-kura_v0.0.7_Darwin_arm64.tar.gz\n" +
		testAMD64Hash + "  git-kura_v0.0.7_Darwin_x86_64.tar.gz\n"
}

// baseFormula renders a two-architecture Homebrew formula in the conventional
// url-then-sha256 shape used by the git-kura tap.
func baseFormula(version, armURL, armSHA, intelURL, intelSHA string) string {
	return fmt.Sprintf(`class GitKura < Formula
  desc "Parallel worktree workflow helper"
  homepage "https://github.com/tooppoo/git-kura"
  version "%s"

  on_macos do
    on_arm do
      url "%s"
      sha256 "%s"
    end
    on_intel do
      url "%s"
      sha256 "%s"
    end
  end

  def install
    bin.install "git-kura"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/git-kura --version")
  end
end
`, version, armURL, armSHA, intelURL, intelSHA)
}

// updatedFormulaContent renders the formula already pointing at v0.0.7 assets so
// exec produces no diff.
func updatedFormulaContent(baseURL string) []byte {
	return []byte(baseFormula("0.0.7",
		baseURL+"/downloads/git-kura_v0.0.7_Darwin_arm64.tar.gz", testARM64Hash,
		baseURL+"/downloads/git-kura_v0.0.7_Darwin_x86_64.tar.gz", testAMD64Hash))
}

func validateResultFor(raw json.RawMessage) *schema.ValidateResult {
	return &schema.ValidateResult{
		SchemaVersion: schema.ValidateSchemaVersion,
		Kind:          schema.ValidateKind,
		TargetVersion: "v0.0.7",
		StepName:      "homebrew",
		Status:        schema.ValidateStatusSuccess,
		StepData:      raw,
	}
}

func planForHandler(t *testing.T, h *Handler, version string) *schema.ReleasePlanEnvelope {
	t.Helper()
	raw, err := h.BuildPayload(version)
	if err != nil {
		t.Fatalf("BuildPayload: %v", err)
	}
	return &schema.ReleasePlanEnvelope{
		SchemaVersion: schema.PlanSchemaVersion,
		Kind:          schema.PlanKind,
		PlanID:        "test-plan",
		PayloadHash:   "sha256:test",
		Payload: schema.ReleasePlanPayload{
			TargetVersion: version,
			StepName:      "homebrew",
			StepData:      raw,
		},
	}
}

func planPayloadFromPlan(t *testing.T, plan *schema.ReleasePlanEnvelope) planPayload {
	t.Helper()
	var p planPayload
	if err := json.Unmarshal(plan.Payload.StepData, &p); err != nil {
		t.Fatalf("parse plan payload: %v", err)
	}
	return p
}

func replacePlanPayload(t *testing.T, plan *schema.ReleasePlanEnvelope, p planPayload) {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal plan payload: %v", err)
	}
	plan.Payload.StepData = raw
}

// newTapRepo creates a temporary git repository representing a Homebrew tap with
// a seed Formula/git-kura.rb pointing at old placeholder assets.
func newTapRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	if err := os.MkdirAll(filepath.Join(dir, "Formula"), 0o755); err != nil {
		t.Fatalf("mkdir Formula: %v", err)
	}
	formula := baseFormula("0.0.6",
		"https://example.com/git-kura_v0.0.6_Darwin_arm64.tar.gz", "old-arm64-hash",
		"https://example.com/git-kura_v0.0.6_Darwin_x86_64.tar.gz", "old-x86_64-hash")
	if err := os.WriteFile(filepath.Join(dir, "Formula", "git-kura.rb"), []byte(formula), 0o644); err != nil {
		t.Fatalf("write formula: %v", err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "init")
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func contains(items []string, substr string) bool {
	for _, item := range items {
		if strings.Contains(item, substr) {
			return true
		}
	}
	return false
}
