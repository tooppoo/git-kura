package tag

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
)

// --- helpers -----------------------------------------------------------------

// repoEnv holds a temporary git repo wired to a local bare "origin".
type repoEnv struct {
	workDir   string
	originDir string
}

// newRepoEnv creates a bare origin repo and a working clone with one commit on
// main that has been pushed. The test is cd-ed into workDir; the original
// directory is restored on cleanup.
func newRepoEnv(t *testing.T) *repoEnv {
	t.Helper()
	tmp := t.TempDir()
	originDir := filepath.Join(tmp, "origin.git")
	workDir := filepath.Join(tmp, "work")

	// Init bare "remote".
	git(t, tmp, "init", "--bare", "--initial-branch=main", originDir)
	// Clone into work dir.
	git(t, tmp, "clone", originDir, workDir)

	// Create initial commit.
	git(t, workDir, "config", "user.email", "test@example.com")
	git(t, workDir, "config", "user.name", "Test")
	readmePath := filepath.Join(workDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("init\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	git(t, workDir, "add", "README.md")
	git(t, workDir, "commit", "-m", "init")
	git(t, workDir, "push", "origin", "main")

	// Switch test to work dir.
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	return &repoEnv{workDir: workDir, originDir: originDir}
}

// git runs a git command in dir, failing the test on error.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// gitInWork runs a git command in the current working directory (workDir).
func gitInWork(t *testing.T, args ...string) {
	t.Helper()
	git(t, ".", args...)
}

// --- BuildPayload ------------------------------------------------------------

func TestBuildPayload_ReturnsValidJSON(t *testing.T) {
	h := New()
	raw, err := h.BuildPayload("v1.2.3")
	if err != nil {
		t.Fatalf("BuildPayload: %v", err)
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("BuildPayload returned invalid JSON: %v", err)
	}
}

// --- runPreflight ------------------------------------------------------------

func TestRunPreflight_CleanRepo_Passes(t *testing.T) {
	newRepoEnv(t)

	errs, warnings, err := runPreflight("v0.0.1")
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestRunPreflight_DirtyWorkingTree_ReturnsError(t *testing.T) {
	env := newRepoEnv(t)

	// Create an untracked file.
	if err := os.WriteFile(filepath.Join(env.workDir, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	errs, _, err := runPreflight("v0.0.1")
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !containsSubstring(errs, "working tree is not clean") {
		t.Errorf("expected 'working tree is not clean' error, got: %v", errs)
	}
}

func TestRunPreflight_TrackedChanges_ReturnsError(t *testing.T) {
	env := newRepoEnv(t)

	// Modify a tracked file without staging it.
	readmePath := filepath.Join(env.workDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("modified\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	errs, _, err := runPreflight("v0.0.1")
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !containsSubstring(errs, "working tree is not clean") {
		t.Errorf("expected 'working tree is not clean' error, got: %v", errs)
	}
}

func TestRunPreflight_WrongBranch_ReturnsError(t *testing.T) {
	newRepoEnv(t)

	gitInWork(t, "checkout", "-b", "feature/x")

	errs, _, err := runPreflight("v0.0.1")
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !containsSubstring(errs, "current branch is") {
		t.Errorf("expected branch error, got: %v", errs)
	}
}

func TestRunPreflight_LocalAheadOfRemote_ReturnsError(t *testing.T) {
	env := newRepoEnv(t)

	// Add a local commit that hasn't been pushed.
	extraPath := filepath.Join(env.workDir, "extra.txt")
	if err := os.WriteFile(extraPath, []byte("extra\n"), 0o644); err != nil {
		t.Fatalf("write extra: %v", err)
	}
	gitInWork(t, "add", "extra.txt")
	gitInWork(t, "commit", "-m", "unpushed commit")

	errs, _, err := runPreflight("v0.0.1")
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !containsSubstring(errs, "commit ID mismatch") {
		t.Errorf("expected commit mismatch error, got: %v", errs)
	}
}

func TestRunPreflight_LocalTagExists_ReturnsError(t *testing.T) {
	newRepoEnv(t)

	gitInWork(t, "tag", "-a", "v0.0.1", "-m", "v0.0.1")

	errs, _, err := runPreflight("v0.0.1")
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !containsSubstring(errs, `local tag "v0.0.1" already exists`) {
		t.Errorf("expected local tag error, got: %v", errs)
	}
}

func TestRunPreflight_RemoteTagExists_ReturnsError(t *testing.T) {
	env := newRepoEnv(t)

	// Create and push an annotated tag directly to origin.
	gitInWork(t, "tag", "-a", "v0.0.1", "-m", "v0.0.1")
	gitInWork(t, "push", "origin", "v0.0.1")
	// Remove the local tag so only the remote tag check fires.
	gitInWork(t, "tag", "-d", "v0.0.1")
	_ = env // suppress unused warning

	errs, _, err := runPreflight("v0.0.1")
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	if !containsSubstring(errs, `remote tag "v0.0.1" already exists`) {
		t.Errorf("expected remote tag error, got: %v", errs)
	}
}

// TestRemoteTagExists_AnnotatedTagPeeledRefNotMistaken verifies that the
// presence of a peeled ref (refs/tags/<ver>^{}) for an annotated tag on the
// remote does NOT cause remoteTagExists to return true for a *different*
// version that happens to share the same prefix.
func TestRemoteTagExists_AnnotatedTagPeeledRefNotMistaken(t *testing.T) {
	newRepoEnv(t)

	// Push an annotated tag v0.0.10 whose ls-remote output will include both
	// refs/tags/v0.0.10 and refs/tags/v0.0.10^{}.
	// Then query for v0.0.1 — which is NOT a prefix of refs/tags/v0.0.10 but
	// is a prefix of refs/tags/v0.0.10 in string terms — confirming our exact
	// match guard works correctly.
	gitInWork(t, "tag", "-a", "v0.0.10", "-m", "v0.0.10")
	gitInWork(t, "push", "origin", "v0.0.10")
	gitInWork(t, "tag", "-d", "v0.0.10") // remove local copy

	exists, err := remoteTagExists("v0.0.1")
	if err != nil {
		t.Fatalf("remoteTagExists: %v", err)
	}
	if exists {
		t.Error("remoteTagExists returned true for v0.0.1 when only v0.0.10 exists on remote")
	}
}

// --- Validate / Preflight via Handler interface ------------------------------

func TestHandler_Validate_CleanRepo(t *testing.T) {
	newRepoEnv(t)
	h := New()
	plan := planFor("v0.0.1")
	errs, warnings, err := h.Validate(plan)
	if err != nil {
		t.Fatalf("Validate internal error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(errs) != 0 {
		t.Errorf("expected no validation errors, got: %v", errs)
	}
}

func TestHandler_Preflight_CleanRepo(t *testing.T) {
	newRepoEnv(t)
	h := New()
	if err := h.Preflight(planFor("v0.0.1")); err != nil {
		t.Errorf("Preflight failed on clean repo: %v", err)
	}
}

func TestHandler_Preflight_DirtyRepo_Fails(t *testing.T) {
	env := newRepoEnv(t)
	if err := os.WriteFile(filepath.Join(env.workDir, "dirt.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	h := New()
	if err := h.Preflight(planFor("v0.0.1")); err == nil {
		t.Error("expected Preflight to fail on dirty repo, got nil")
	}
}

// --- Exec --------------------------------------------------------------------

func TestExec_CreatesAndPushesAnnotatedTag(t *testing.T) {
	env := newRepoEnv(t)
	h := New()
	if err := h.Exec(planFor("v0.0.1")); err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Verify local tag exists and is annotated.
	out, err := gitOutput(t, env.workDir, "cat-file", "-t", "v0.0.1")
	if err != nil {
		t.Fatalf("cat-file: %v", err)
	}
	if strings.TrimSpace(out) != "tag" {
		t.Errorf("expected annotated tag object type 'tag', got %q", strings.TrimSpace(out))
	}

	// Verify remote tag exists.
	out2, err := gitOutput(t, env.workDir, "ls-remote", "origin", "refs/tags/v0.0.1")
	if err != nil {
		t.Fatalf("ls-remote: %v", err)
	}
	if !strings.Contains(out2, "refs/tags/v0.0.1") {
		t.Errorf("expected refs/tags/v0.0.1 on remote, got: %q", out2)
	}
}

func TestExec_PushFailure_ReturnsError(t *testing.T) {
	env := newRepoEnv(t)

	// Create a second commit so we can point the remote tag to a different object.
	extra := filepath.Join(env.workDir, "extra.txt")
	if err := os.WriteFile(extra, []byte("extra\n"), 0o644); err != nil {
		t.Fatalf("write extra: %v", err)
	}
	gitInWork(t, "add", "extra.txt")
	gitInWork(t, "commit", "-m", "second commit")
	gitInWork(t, "push", "origin", "main")

	// Create annotated tag on HEAD (second commit) and push so the remote has it.
	gitInWork(t, "tag", "-a", "v0.0.1", "-m", "v0.0.1")
	gitInWork(t, "push", "origin", "v0.0.1")

	// Reset local to the first commit so HEAD now differs from the remote tag object.
	gitInWork(t, "reset", "--hard", "HEAD~1")
	// Remove the local tag so Exec can re-create it at the old HEAD.
	gitInWork(t, "tag", "-d", "v0.0.1")

	// Exec will create a local tag pointing to the old HEAD and try to push — the
	// remote already has v0.0.1 pointing to a different commit, so push is rejected.
	h := New()
	err := h.Exec(planFor("v0.0.1"))
	if err == nil {
		t.Fatal("expected Exec to fail when push is rejected, got nil")
	}
	if !strings.Contains(err.Error(), "push tag") {
		t.Errorf("expected push error, got: %v", err)
	}
}

// TestRunPreflight_NotInGitRepo_InternalError verifies that runPreflight returns
// a non-nil internal error (third return value) when git commands fail because
// the working directory is not inside a git repository.
func TestRunPreflight_NotInGitRepo_InternalError(t *testing.T) {
	dir := t.TempDir() // plain directory, not a git repo
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	_, _, internalErr := runPreflight("v0.0.1")
	if internalErr == nil {
		t.Fatal("expected internal error when not in a git repo, got nil")
	}
	if !strings.Contains(internalErr.Error(), "check working tree") {
		t.Errorf("unexpected error message: %v", internalErr)
	}
}

// TestGitRevParse_BadRef verifies that gitRevParse returns an error for a
// ref that does not exist in the repository.
func TestGitRevParse_BadRef(t *testing.T) {
	newRepoEnv(t)
	_, err := gitRevParse("refs/heads/nonexistent-branch-xyz-abc")
	if err == nil {
		t.Fatal("expected error for nonexistent ref, got nil")
	}
}

// TestLsRemoteMain_RefNotFound verifies that lsRemoteMain returns an error when
// the remote does not advertise refs/heads/main.
func TestLsRemoteMain_RefNotFound(t *testing.T) {
	tmp := t.TempDir()
	originDir := filepath.Join(tmp, "origin.git")
	workDir := filepath.Join(tmp, "work")

	// Create origin with a different default branch name.
	git(t, tmp, "init", "--bare", "--initial-branch=trunk", originDir)
	git(t, tmp, "clone", originDir, workDir)
	git(t, workDir, "config", "user.email", "test@example.com")
	git(t, workDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(workDir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	git(t, workDir, "add", "f")
	git(t, workDir, "commit", "-m", "init")
	git(t, workDir, "push", "origin", "trunk")

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	_, err = lsRemoteMain()
	if err == nil {
		t.Fatal("expected lsRemoteMain to fail when remote has no main, got nil")
	}
	if !strings.Contains(err.Error(), "refs/heads/main not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// --- helpers -----------------------------------------------------------------

// gitOutput runs a git command in dir and returns stdout.
func gitOutput(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), exitErr.Stderr)
		}
		return "", err
	}
	return string(out), nil
}

func planFor(version string) *schema.ReleasePlanEnvelope {
	return &schema.ReleasePlanEnvelope{
		SchemaVersion: schema.PlanSchemaVersion,
		Kind:          schema.PlanKind,
		PlanID:        "test-plan-id",
		Payload: schema.ReleasePlanPayload{
			TargetVersion: version,
			StepName:      "tag",
		},
	}
}

func containsSubstring(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
