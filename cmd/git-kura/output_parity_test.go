package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/internal/output"
)

// TestAllStructuredCommandsHaveHumanRenderable verifies that every structured
// output success data type implements HumanRenderable. A type that does not
// implement the interface causes the human renderer to produce no output,
// silently dropping user-actionable information.
func TestAllStructuredCommandsHaveHumanRenderable(t *testing.T) {
	for name, data := range map[string]any{
		"lsData": lsData{Keys: []string{"key1"}},
		"worktreeJSON": worktreeJSON{
			SchemaVersion: 1, Key: "k", Kind: "worktree", Branch: "k",
			WorktreePath: "/p", RepositoryRoot: "/r", BaseBranch: "main",
		},
		"openDataJSON": openDataJSON{
			SchemaVersion: 1, Key: "k", Kind: "worktree", Branch: "k",
			WorktreePath: "/p", RepositoryRoot: "/r", BaseBranch: "main",
		},
		"closeDataJSON":   closeDataJSON{Key: "k", WorktreePath: "/p", Branch: "k"},
		"sealClaimData":   sealClaimData{CurrentKey: "k", Paths: []sealClaimPathItem{{Path: "f", Status: "claimed"}}},
		"sealUnclaimData": sealUnclaimData{CurrentKey: "k", Paths: []sealUnclaimPathItem{{Path: "f", Status: "released"}}},
		"sealTestData":    sealTestData{CurrentKey: "k", Passed: true, Results: []sealTestResultItem{}},
		"sealLsData":      sealLsData{Claims: []sealLsClaim{{Key: "k", Path: "f"}}},
		"sealDoctorData":  sealDoctorData{Healthy: true, Summary: sealDoctorSummary{}, Findings: []sealDoctorFinding{}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := data.(output.HumanRenderable); !ok {
				t.Fatalf("%s does not implement HumanRenderable; user-actionable information would be silently dropped in human mode", name)
			}
		})
	}
}

// --- seal claim unit parity tests ---

func TestSealClaimHumanOutputContainsClaimedPaths(t *testing.T) {
	var buf bytes.Buffer
	data := sealClaimData{
		CurrentKey: "key1",
		Paths: []sealClaimPathItem{
			{Path: "src/foo.go", Status: "claimed"},
			{Path: "src/bar.go", Status: "claimed"},
		},
	}
	if err := data.RenderHuman(&buf); err != nil {
		t.Fatalf("RenderHuman: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"src/foo.go", "src/bar.go", "claimed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("human output = %q, want it to contain %q", got, want)
		}
	}
}

func TestSealClaimHumanOutputContainsAlreadyOwnedPaths(t *testing.T) {
	var buf bytes.Buffer
	data := sealClaimData{
		CurrentKey: "key1",
		Paths: []sealClaimPathItem{
			{Path: "src/foo.go", Status: "already-owned"},
		},
	}
	if err := data.RenderHuman(&buf); err != nil {
		t.Fatalf("RenderHuman: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"src/foo.go", "already owned"} {
		if !strings.Contains(got, want) {
			t.Fatalf("human output = %q, want it to contain %q", got, want)
		}
	}
}

// --- seal unclaim unit parity tests ---

func TestSealUnclaimHumanOutputContainsReleasedPaths(t *testing.T) {
	var buf bytes.Buffer
	data := sealUnclaimData{
		CurrentKey: "key1",
		Paths: []sealUnclaimPathItem{
			{Path: "src/foo.go", Status: "released"},
		},
	}
	if err := data.RenderHuman(&buf); err != nil {
		t.Fatalf("RenderHuman: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"src/foo.go", "released"} {
		if !strings.Contains(got, want) {
			t.Fatalf("human output = %q, want it to contain %q", got, want)
		}
	}
}

func TestSealUnclaimHumanOutputContainsNotClaimedPaths(t *testing.T) {
	var buf bytes.Buffer
	data := sealUnclaimData{
		CurrentKey: "key1",
		Paths: []sealUnclaimPathItem{
			{Path: "src/bar.go", Status: "not-claimed"},
		},
	}
	if err := data.RenderHuman(&buf); err != nil {
		t.Fatalf("RenderHuman: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"src/bar.go", "not claimed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("human output = %q, want it to contain %q", got, want)
		}
	}
}

// --- close unit parity tests ---

func TestCloseHumanOutputContainsAffectedPathsAndEffects(t *testing.T) {
	var buf bytes.Buffer
	data := closeDataJSON{
		Key:               "task1",
		WorktreePath:      "/repo/.git/kura/worktrees/task1",
		Branch:            "task1",
		RemovedWorktree:   true,
		RemovedBranch:     true,
		RemovedMetadata:   true,
		ReleasedSealCount: 2,
	}
	if err := data.RenderHuman(&buf); err != nil {
		t.Fatalf("RenderHuman: %v", err)
	}
	got := buf.String()
	for _, want := range []string{
		"/repo/.git/kura/worktrees/task1",
		"task1",
		"removed worktree",
		"removed branch",
		"removed metadata",
		"2",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("human output = %q, want it to contain %q", got, want)
		}
	}
}

// --- diagnostic command distinction tests ---

// TestSealTestHumanConflictDistinguishableFromExecutionFailure verifies that
// in human mode a path conflict (business result, ok:true passed:false) produces
// a different message token than an execution failure (ok:false), so operators
// can distinguish "conflict found" from "test could not run".
func TestSealTestHumanConflictDistinguishableFromExecutionFailure(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")

	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)

	// Conflict: business result — token is "seal-conflict:" on stdout.
	conflict := cli.gitKura(wt2, "seal", "test", "tracked.txt")
	requireExitCode(t, conflict, exitSealConflict)
	requireStdoutContains(t, conflict, "seal-conflict:")
	requireStdoutContains(t, conflict, "key1")
	requireEmptyStderr(t, conflict)

	// Execution failure: not a managed worktree — token is "current-key-unresolved:" on stderr.
	execFail := cli.gitKura(repo, "seal", "test", "tracked.txt")
	requireNonZeroExitCode(t, execFail)
	if strings.Contains(execFail.stderr, "seal-conflict:") || strings.Contains(execFail.stdout, "seal-conflict:") {
		t.Fatalf("execution failure must not resemble a seal conflict: stdout=%q stderr=%q", execFail.stdout, execFail.stderr)
	}
	requireStderrContains(t, execFail, "current-key-unresolved:")
}

// TestSealDoctorHumanUnhealthyDistinguishableFromExecutionFailure verifies
// that an unhealthy store (inspection completed, found problems) produces a
// different token than an execution failure (outside a git repo), so operators
// can distinguish "checked and found issues" from "could not check".
func TestSealDoctorHumanUnhealthyDistinguishableFromExecutionFailure(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	storeFile, _, err := pathsSealStore(repo)
	if err != nil {
		t.Fatalf("pathsSealStore: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	// Path with a backslash is invalid in the seal store and triggers a finding.
	badPath := `bad\sep.go`
	content := fmt.Sprintf(`{"schemaVersion":1,"paths":{%q:{"key":"key1"}}}`, badPath)
	if err := os.WriteFile(storeFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	unhealthy := cli.gitKura(repo, "seal", "doctor")
	requireExitCode(t, unhealthy, exitSealDoctorError)
	requireEmptyStderr(t, unhealthy)
	// Unhealthy is a business result (ok:true, healthy:false) → stdout.
	requireStdoutContains(t, unhealthy, "seal-doctor-error:")
	requireStdoutContains(t, unhealthy, "sep.go")

	// Execution failure: outside a git repository — error goes to stderr, not stdout.
	execFail := cli.gitKura(t.TempDir(), "seal", "doctor")
	requireNonZeroExitCode(t, execFail)
	if strings.Contains(execFail.stderr, "seal-doctor-error:") || strings.Contains(execFail.stdout, "seal-doctor-error:") {
		t.Fatalf("execution failure must not resemble a doctor violation: stdout=%q stderr=%q", execFail.stdout, execFail.stderr)
	}
	requireStderrContains(t, execFail, "git repository")
}

// --- snapshot tests ---

// TestSealClaimHumanSnapshot verifies the exact human output format for a
// representative claim success.
func TestSealClaimHumanSnapshot(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	result := cli.gitKura(wt, "seal", "claim", "tracked.txt")
	requireExitCode(t, result, 0)
	want := "claimed: tracked.txt\n"
	if result.stdout != want {
		t.Fatalf("stdout = %q, want %q", result.stdout, want)
	}
	requireEmptyStderr(t, result)
}

// TestSealClaimAlreadyOwnedHumanSnapshot verifies the exact output when the
// current key already owns the path (idempotent re-claim).
func TestSealClaimAlreadyOwnedHumanSnapshot(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "tracked.txt"), 0)

	result := cli.gitKura(wt, "seal", "claim", "tracked.txt")
	requireExitCode(t, result, 0)
	want := "already owned: tracked.txt\n"
	if result.stdout != want {
		t.Fatalf("stdout = %q, want %q", result.stdout, want)
	}
	requireEmptyStderr(t, result)
}

// TestSealUnclaimHumanSnapshot verifies the exact human output for a
// representative unclaim success.
func TestSealUnclaimHumanSnapshot(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "tracked.txt"), 0)

	result := cli.gitKura(wt, "seal", "unclaim", "tracked.txt")
	requireExitCode(t, result, 0)
	want := "released: tracked.txt\n"
	if result.stdout != want {
		t.Fatalf("stdout = %q, want %q", result.stdout, want)
	}
	requireEmptyStderr(t, result)
}

// TestSealUnclaimNotClaimedHumanSnapshot verifies the exact output when the
// path was not claimed (idempotent unclaim).
func TestSealUnclaimNotClaimedHumanSnapshot(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	result := cli.gitKura(wt, "seal", "unclaim", "tracked.txt")
	requireExitCode(t, result, 0)
	want := "not claimed: tracked.txt\n"
	if result.stdout != want {
		t.Fatalf("stdout = %q, want %q", result.stdout, want)
	}
	requireEmptyStderr(t, result)
}

// TestCloseHumanSnapshot verifies the exact human output for a representative
// close success: worktree path, branch, and all effect lines appear.
func TestCloseHumanSnapshot(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	result := cli.gitKura(repo, "close", "key1")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	for _, want := range []string{wt, "key1", "removed worktree", "removed branch", "removed metadata"} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("stdout = %q, want it to contain %q", result.stdout, want)
		}
	}
}

// TestSealDoctorHealthyHumanSnapshot verifies that a healthy store produces no
// output — "no news is good news" for diagnostic success.
func TestSealDoctorHealthyHumanSnapshot(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "doctor")
	requireExitCode(t, result, 0)
	requireEmptyStdout(t, result)
	requireEmptyStderr(t, result)
}

// TestSealDoctorUnhealthyHumanSnapshot verifies that an unhealthy store reports
// all findings on stdout as a business result (ok:true, healthy:false).
func TestSealDoctorUnhealthyHumanSnapshot(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	storeFile, _, err := pathsSealStore(repo)
	if err != nil {
		t.Fatalf("pathsSealStore: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	badPath := `bad\sep.go`
	content := fmt.Sprintf(`{"schemaVersion":1,"paths":{%q:{"key":"key1"}}}`, badPath)
	if err := os.WriteFile(storeFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result := cli.gitKura(repo, "seal", "doctor")
	requireExitCode(t, result, exitSealDoctorError)
	requireEmptyStderr(t, result)
	requireStdoutContains(t, result, "seal-doctor-error:")
	requireStdoutContains(t, result, "sep.go")
}

// TestSealTestConflictHumanSnapshot verifies that a path conflict is reported
// on stdout (business result: ok:true, passed:false) with the path and owner key.
func TestSealTestConflictHumanSnapshot(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")

	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)

	result := cli.gitKura(wt2, "seal", "test", "tracked.txt")
	requireExitCode(t, result, exitSealConflict)
	requireEmptyStderr(t, result)
	requireStdoutContains(t, result, "seal-conflict:")
	requireStdoutContains(t, result, "tracked.txt")
	requireStdoutContains(t, result, "key1")
}

// TestOpenHumanSnapshot verifies the exact human output for a representative
// open success: worktree path, branch, and created-effect lines all appear.
func TestOpenHumanSnapshot(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "open", "key1")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	for _, want := range []string{"opened:", "key1", "created worktree", "created branch", "created metadata"} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("stdout = %q, want it to contain %q", result.stdout, want)
		}
	}
	// The worktree path must appear so the caller can use it.
	wt := extractOpenedPath(result.stdout)
	if wt == "" {
		t.Fatalf("extractOpenedPath returned empty string from stdout = %q", result.stdout)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("extracted path %q does not exist: %v", wt, err)
	}
}

// TestSealTestPassedHumanSnapshot verifies that a fully safe test produces no
// output in human mode — there is nothing actionable to show.
func TestSealTestPassedHumanSnapshot(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	result := cli.gitKura(wt, "seal", "test", "tracked.txt")
	requireExitCode(t, result, 0)
	requireEmptyStdout(t, result)
	requireEmptyStderr(t, result)
}
