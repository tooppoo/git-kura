package scoop

import (
	"encoding/json"
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
	testAMD64Hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testARM64Hash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestBuildPayload_NormalizesBucketAndManifestPath(t *testing.T) {
	dir := t.TempDir()
	h := New()
	h.SetOptions(step.Options{Bucket: filepath.Join(dir, "..", filepath.Base(dir))})

	raw, err := h.BuildPayload("v0.0.7")
	if err != nil {
		t.Fatalf("BuildPayload: %v", err)
	}
	var p planPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("parse payload: %v", err)
	}

	wantBucket, _ := filepath.Abs(dir)
	if p.BucketPath != wantBucket {
		t.Errorf("bucketPath = %q, want %q", p.BucketPath, wantBucket)
	}
	if p.ManifestPath != filepath.Join(wantBucket, "bucket", "git-kura.json") {
		t.Errorf("manifestPath = %q", p.ManifestPath)
	}
	if p.ManifestVersion != "0.0.7" {
		t.Errorf("manifestVersion = %q, want 0.0.7", p.ManifestVersion)
	}
	if len(p.CommandPreview) == 0 {
		t.Fatal("commandPreview must not be empty")
	}
}

func TestBuildPayload_RequiresBucket(t *testing.T) {
	h := New()
	if _, err := h.BuildPayload("v0.0.7"); err == nil {
		t.Fatal("expected --bucket requirement error")
	}
}

func TestValidateWithData_Success(t *testing.T) {
	bucket := newBucketRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Bucket: bucket})
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
	if data.BucketPath != bucket {
		t.Errorf("bucketPath = %q, want %q", data.BucketPath, bucket)
	}
	if data.ManifestPath != filepath.Join(bucket, "bucket", "git-kura.json") {
		t.Errorf("manifestPath = %q", data.ManifestPath)
	}
	if !hasSuccessfulAsset(data.Assets, "64bit") || !hasSuccessfulAsset(data.Assets, "arm64") {
		t.Fatalf("expected successful assets for both architectures: %+v", data.Assets)
	}
}

func TestValidateWithData_DirtyBucketFails(t *testing.T) {
	bucket := newBucketRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Bucket: bucket})
	plan := planForHandler(t, h, "v0.0.7")

	if err := os.WriteFile(filepath.Join(bucket, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	errs, _, _, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !contains(errs, "bucket repository is not clean") {
		t.Fatalf("expected dirty bucket validation error, got %v", errs)
	}
}

func TestValidateWithData_BucketMustBeRepositoryRoot(t *testing.T) {
	bucket := newBucketRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Bucket: filepath.Join(bucket, "bucket")})
	plan := planForHandler(t, h, "v0.0.7")

	errs, _, _, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !contains(errs, "--bucket must point to the Scoop bucket repository root") {
		t.Fatalf("expected bucket root validation error, got %v", errs)
	}
}

func TestValidateWithData_MissingManifestFails(t *testing.T) {
	bucket := newBucketRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Bucket: bucket})
	plan := planForHandler(t, h, "v0.0.7")

	if err := os.Remove(filepath.Join(bucket, "bucket", "git-kura.json")); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}
	git(t, bucket, "add", "-A")
	git(t, bucket, "commit", "-m", "remove manifest")

	errs, _, _, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !contains(errs, "target manifest file") {
		t.Fatalf("expected missing manifest error, got %v", errs)
	}
}

func TestValidateWithData_RejectsPlanManifestPathOutsideBucket(t *testing.T) {
	bucket := newBucketRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Bucket: bucket})
	plan := planForHandler(t, h, "v0.0.7")
	p := planPayloadFromPlan(t, plan)
	outside := filepath.Join(t.TempDir(), "git-kura.json")
	copyFile(t, filepath.Join(bucket, "bucket", "git-kura.json"), outside)
	p.ManifestPath = outside
	replacePlanPayload(t, plan, p)

	errs, _, _, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !contains(errs, "plan payload manifestPath") {
		t.Fatalf("expected manifestPath validation error, got %v", errs)
	}
}

func TestValidateWithData_RejectsPlanArchiveFilenames(t *testing.T) {
	bucket := newBucketRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Bucket: bucket})
	plan := planForHandler(t, h, "v0.0.7")
	p := planPayloadFromPlan(t, plan)
	p.WindowsArchives[0].Filename = "git-kura_v0.0.7_Windows_wrong.zip"
	replacePlanPayload(t, plan, p)

	errs, _, _, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !contains(errs, "plan payload windowsArchives") {
		t.Fatalf("expected windowsArchives validation error, got %v", errs)
	}
}

func TestPreflightWithResult_DetectsBucketMismatch(t *testing.T) {
	bucket := newBucketRepo(t)
	other := t.TempDir()
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Bucket: bucket})
	plan := planForHandler(t, h, "v0.0.7")
	_, _, raw, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("ValidateWithData: %v", err)
	}
	result := &schema.ValidateResult{
		SchemaVersion: schema.ValidateSchemaVersion,
		Kind:          schema.ValidateKind,
		TargetVersion: "v0.0.7",
		StepName:      "scoop",
		Status:        schema.ValidateStatusSuccess,
		StepData:      raw,
	}

	h.SetOptions(step.Options{Bucket: other})
	if err := h.PreflightWithResult(plan, result); err == nil {
		t.Fatal("expected bucket mismatch error")
	}
}

func TestPreflightAndExec_UpdateManifestAndLeaveOnlyManifestDiff(t *testing.T) {
	bucket := newBucketRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Bucket: bucket})
	plan := planForHandler(t, h, "v0.0.7")
	_, _, raw, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("ValidateWithData: %v", err)
	}
	result := &schema.ValidateResult{
		SchemaVersion: schema.ValidateSchemaVersion,
		Kind:          schema.ValidateKind,
		TargetVersion: "v0.0.7",
		StepName:      "scoop",
		Status:        schema.ValidateStatusSuccess,
		StepData:      raw,
	}

	if err := h.PreflightWithResult(plan, result); err != nil {
		t.Fatalf("PreflightWithResult: %v", err)
	}
	if err := h.Exec(plan); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	var manifest map[string]any
	b, err := os.ReadFile(filepath.Join(bucket, "bucket", "git-kura.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if manifest["version"] != "0.0.7" {
		t.Errorf("version = %v, want 0.0.7", manifest["version"])
	}
	arch := manifest["architecture"].(map[string]any)
	amd64 := arch["64bit"].(map[string]any)
	arm64 := arch["arm64"].(map[string]any)
	if amd64["hash"] != testAMD64Hash || arm64["hash"] != testARM64Hash {
		t.Errorf("hashes not updated: amd64=%v arm64=%v", amd64["hash"], arm64["hash"])
	}
	if !strings.Contains(amd64["url"].(string), "git-kura_v0.0.7_Windows_x86_64.zip") {
		t.Errorf("64bit URL not updated: %v", amd64["url"])
	}
	if !strings.Contains(arm64["url"].(string), "git-kura_v0.0.7_Windows_arm64.zip") {
		t.Errorf("arm64 URL not updated: %v", arm64["url"])
	}

	status := gitOutput(t, bucket, "status", "--porcelain")
	lines := strings.Split(strings.TrimSpace(status), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "bucket/git-kura.json") {
		t.Fatalf("expected only manifest diff, got %q", status)
	}
}

func TestExec_RequiresValidateResultPreflight(t *testing.T) {
	bucket := newBucketRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Bucket: bucket})
	plan := planForHandler(t, h, "v0.0.7")

	if err := h.Preflight(plan); err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if err := h.Exec(plan); err == nil {
		t.Fatal("expected Exec to require PreflightWithResult")
	}
}

func TestFailedPreflightWithResultClearsPreviousValidation(t *testing.T) {
	bucket := newBucketRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Bucket: bucket})
	plan := planForHandler(t, h, "v0.0.7")
	_, _, raw, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("ValidateWithData: %v", err)
	}
	result := &schema.ValidateResult{
		SchemaVersion: schema.ValidateSchemaVersion,
		Kind:          schema.ValidateKind,
		TargetVersion: "v0.0.7",
		StepName:      "scoop",
		Status:        schema.ValidateStatusSuccess,
		StepData:      raw,
	}
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

func TestExec_FailsWhenUnexpectedDiffExistsAfterUpdate(t *testing.T) {
	bucket := newBucketRepo(t)
	h, srv := newTestHandler(t)
	defer srv.Close()
	h.SetOptions(step.Options{Bucket: bucket})
	plan := planForHandler(t, h, "v0.0.7")
	_, _, raw, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("ValidateWithData: %v", err)
	}
	result := &schema.ValidateResult{
		SchemaVersion: schema.ValidateSchemaVersion,
		Kind:          schema.ValidateKind,
		TargetVersion: "v0.0.7",
		StepName:      "scoop",
		Status:        schema.ValidateStatusSuccess,
		StepData:      raw,
	}
	if err := h.PreflightWithResult(plan, result); err != nil {
		t.Fatalf("PreflightWithResult: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "unexpected.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write unexpected file: %v", err)
	}
	if err := h.Exec(plan); err == nil {
		t.Fatal("expected unexpected diff error")
	}
}

func TestParseChecksums(t *testing.T) {
	content := testAMD64Hash + "  git-kura_v0.0.7_Windows_x86_64.zip\n" +
		strings.ToUpper(testARM64Hash) + "  uppercase-normalized.zip\n" +
		testARM64Hash + "  *binary-marker.zip\n" +
		"bad  bad.zip\n"
	got := parseChecksums(content)
	if got["git-kura_v0.0.7_Windows_x86_64.zip"] != testAMD64Hash {
		t.Fatalf("expected amd64 checksum entry, got %#v", got)
	}
	if got["uppercase-normalized.zip"] != testARM64Hash {
		t.Fatalf("expected uppercase checksum to be normalized, got %#v", got)
	}
	if got["binary-marker.zip"] != testARM64Hash {
		t.Fatalf("expected binary marker filename to be normalized, got %#v", got)
	}
}

func TestValidateWithData_MissingArm64AssetFails(t *testing.T) {
	bucket := newBucketRepo(t)
	h, srv := newTestHandlerWithRelease(t, func(baseURL string) []githubAsset {
		return []githubAsset{
			{Name: "git-kura_v0.0.7_Windows_x86_64.zip", State: "uploaded", BrowserDownloadURL: baseURL + "/downloads/git-kura_v0.0.7_Windows_x86_64.zip"},
			{Name: checksumFileName, State: "uploaded", BrowserDownloadURL: baseURL + "/downloads/checksums.txt"},
		}
	}, defaultChecksumContent())
	defer srv.Close()
	h.SetOptions(step.Options{Bucket: bucket})
	plan := planForHandler(t, h, "v0.0.7")

	errs, _, _, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !contains(errs, `Windows arm64 archive "git-kura_v0.0.7_Windows_arm64.zip" not found`) {
		t.Fatalf("expected missing arm64 asset error, got %v", errs)
	}
	if !contains(errs, "both Scoop architectures (64bit and arm64) must validate successfully") {
		t.Fatalf("expected both-architectures validation error, got %v", errs)
	}
}

func TestValidateWithData_MissingArm64ChecksumFails(t *testing.T) {
	bucket := newBucketRepo(t)
	h, srv := newTestHandlerWithRelease(t, defaultAssets, testAMD64Hash+"  git-kura_v0.0.7_Windows_x86_64.zip\n")
	defer srv.Close()
	h.SetOptions(step.Options{Bucket: bucket})
	plan := planForHandler(t, h, "v0.0.7")

	errs, _, _, err := h.ValidateWithData(plan)
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !contains(errs, `checksums.txt: no sha256 entry for "git-kura_v0.0.7_Windows_arm64.zip"`) {
		t.Fatalf("expected missing arm64 checksum error, got %v", errs)
	}
	if !contains(errs, "both Scoop architectures (64bit and arm64) must validate successfully") {
		t.Fatalf("expected both-architectures validation error, got %v", errs)
	}
}

func TestPorcelainPaths_RenameMustTouchOnlyManifest(t *testing.T) {
	allowed := filepath.ToSlash(filepath.Join("bucket", "git-kura.json"))
	if !allPathsAllowed(porcelainPaths(" M bucket/git-kura.json"), allowed) {
		t.Fatal("expected direct manifest modification to be allowed")
	}
	if allPathsAllowed(porcelainPaths("R  bucket/other.json -> bucket/git-kura.json"), allowed) {
		t.Fatal("rename from another path into the manifest must not be allowed")
	}
}

func TestOwnerRepoFromRemoteURL_DoesNotLeakCredentialRemote(t *testing.T) {
	owner, repo, err := ownerRepoFromRemoteURL("https://user:secret-token@github.com/tooppoo/git-kura.git")
	if err != nil {
		t.Fatalf("ownerRepoFromRemoteURL: %v", err)
	}
	if owner != "tooppoo" || repo != "git-kura" {
		t.Fatalf("owner/repo = %s/%s, want tooppoo/git-kura", owner, repo)
	}

	_, _, err = ownerRepoFromRemoteURL("https://user:secret-token@example.com/tooppoo/git-kura.git")
	if err == nil {
		t.Fatal("expected unsupported remote URL error")
	}
	if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "user:") {
		t.Fatalf("error leaked remote credentials: %v", err)
	}
}

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
		{Name: "git-kura_v0.0.7_Windows_x86_64.zip", State: "uploaded", BrowserDownloadURL: baseURL + "/downloads/git-kura_v0.0.7_Windows_x86_64.zip"},
		{Name: "git-kura_v0.0.7_Windows_arm64.zip", State: "uploaded", BrowserDownloadURL: baseURL + "/downloads/git-kura_v0.0.7_Windows_arm64.zip"},
		{Name: checksumFileName, State: "uploaded", BrowserDownloadURL: baseURL + "/downloads/checksums.txt"},
	}
}

func defaultChecksumContent() string {
	return testAMD64Hash + "  git-kura_v0.0.7_Windows_x86_64.zip\n" +
		testARM64Hash + "  git-kura_v0.0.7_Windows_arm64.zip\n"
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
			StepName:      "scoop",
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

func newBucketRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	if err := os.MkdirAll(filepath.Join(dir, "bucket"), 0o755); err != nil {
		t.Fatalf("mkdir bucket: %v", err)
	}
	manifest := []byte(`{
  "version": "0.0.6",
  "architecture": {
    "64bit": {
      "url": "https://example.com/old-x64.zip",
      "hash": "old-x64"
    },
    "arm64": {
      "url": "https://example.com/old-arm64.zip",
      "hash": "old-arm64"
    }
  },
  "bin": "git-kura.exe"
}
`)
	if err := os.WriteFile(filepath.Join(dir, "bucket", "git-kura.json"), manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
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

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
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
