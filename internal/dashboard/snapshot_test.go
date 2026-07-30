package dashboard

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tooppoo/git-kura/internal/seal"
)

func strPtr(s string) *string { return &s }

func TestBuildSnapshotUnionOfOpenAndClaimedKeys(t *testing.T) {
	snap := BuildSnapshot(
		[]string{"beta", "alpha"},
		[]seal.LsClaim{
			{Key: "beta", Path: "b/two.txt"},
			{Key: "beta", Path: "a/one.txt"},
			{Key: "gone", Path: "c/three.txt"},
		},
		nil,
	)

	wantKeys := []string{"alpha", "beta", "gone"}
	gotKeys := make([]string, len(snap.Groups))
	for i, g := range snap.Groups {
		gotKeys[i] = g.Key
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("group keys = %v, want %v", gotKeys, wantKeys)
	}

	if snap.OpenKeys != 2 {
		t.Fatalf("OpenKeys = %d, want 2", snap.OpenKeys)
	}
	if snap.ClaimedPaths != 3 {
		t.Fatalf("ClaimedPaths = %d, want 3", snap.ClaimedPaths)
	}
}

func TestBuildSnapshotZeroClaimOpenKeyIsListed(t *testing.T) {
	snap := BuildSnapshot([]string{"idle"}, nil, nil)

	if len(snap.Groups) != 1 {
		t.Fatalf("len(Groups) = %d, want 1", len(snap.Groups))
	}
	g := snap.Groups[0]
	if g.Key != "idle" || len(g.Paths) != 0 || g.Orphaned {
		t.Fatalf("group = %+v, want idle with no paths and not orphaned", g)
	}
}

func TestBuildSnapshotOrphanedClaims(t *testing.T) {
	snap := BuildSnapshot(
		[]string{"open-key"},
		[]seal.LsClaim{{Key: "removed-task", Path: "x.txt"}},
		nil,
	)

	var orphan *Group
	for i := range snap.Groups {
		if snap.Groups[i].Key == "removed-task" {
			orphan = &snap.Groups[i]
		}
	}
	if orphan == nil {
		t.Fatalf("removed-task group missing: %+v", snap.Groups)
	}
	if !orphan.Orphaned {
		t.Fatalf("removed-task Orphaned = false, want true")
	}
	for _, g := range snap.Groups {
		if g.Key == "open-key" && g.Orphaned {
			t.Fatalf("open-key marked orphaned")
		}
	}
}

func TestBuildSnapshotPathsAreSorted(t *testing.T) {
	snap := BuildSnapshot(
		[]string{"k"},
		[]seal.LsClaim{
			{Key: "k", Path: "z.txt"},
			{Key: "k", Path: "a.txt"},
			{Key: "k", Path: "m/x.txt"},
		},
		nil,
	)

	want := []string{"a.txt", "m/x.txt", "z.txt"}
	if !reflect.DeepEqual(snap.Groups[0].Paths, want) {
		t.Fatalf("paths = %v, want %v", snap.Groups[0].Paths, want)
	}
}

func TestBuildSnapshotViolationsExcludedFromClaims(t *testing.T) {
	badPath := "a/../b.txt"
	snap := BuildSnapshot(
		[]string{"k"},
		[]seal.LsClaim{
			{Key: "k", Path: "good.txt"},
			{Key: "k", Path: badPath},
		},
		[]seal.DoctorFinding{
			{Severity: "error", Code: "non-normalized-path", Path: strPtr(badPath), Message: "not normalized"},
		},
	)

	if len(snap.Violations) != 1 {
		t.Fatalf("len(Violations) = %d, want 1", len(snap.Violations))
	}
	if snap.Violations[0].Code != "non-normalized-path" || snap.Violations[0].Path != badPath {
		t.Fatalf("violation = %+v, want non-normalized-path for %q", snap.Violations[0], badPath)
	}
	if !reflect.DeepEqual(snap.Groups[0].Paths, []string{"good.txt"}) {
		t.Fatalf("paths = %v, want only good.txt", snap.Groups[0].Paths)
	}
	if snap.ClaimedPaths != 1 {
		t.Fatalf("ClaimedPaths = %d, want 1", snap.ClaimedPaths)
	}
}

func TestBuildSnapshotDuplicateCanonicalExcludesBothEntries(t *testing.T) {
	// InspectPathStore attaches the duplicate-canonical-path finding only to
	// the second raw entry in sorted order; the clean first entry ("b.txt")
	// carries no finding but its ownership is contested, so it must not be
	// listed as a normal claim either.
	dup := "x/../b.txt"
	snap := BuildSnapshot(
		[]string{"a-key"},
		[]seal.LsClaim{
			{Key: "a-key", Path: "b.txt"},
			{Key: "b-key", Path: dup},
			{Key: "a-key", Path: "ok.txt"},
		},
		[]seal.DoctorFinding{
			{Severity: "error", Code: "duplicate-canonical-path", Path: strPtr(dup), Message: "duplicate"},
		},
	)

	if len(snap.Groups) != 1 || snap.Groups[0].Key != "a-key" {
		t.Fatalf("groups = %+v, want only open key a-key", snap.Groups)
	}
	if !reflect.DeepEqual(snap.Groups[0].Paths, []string{"ok.txt"}) {
		t.Fatalf("paths = %v, want contested b.txt excluded", snap.Groups[0].Paths)
	}
	if snap.ClaimedPaths != 1 {
		t.Fatalf("ClaimedPaths = %d, want 1", snap.ClaimedPaths)
	}
}

func TestBuildSnapshotViolationsAreSorted(t *testing.T) {
	snap := BuildSnapshot(nil, nil, []seal.DoctorFinding{
		{Severity: "error", Code: "invalid-stored-path", Path: strPtr("z.txt"), Message: "bad"},
		{Severity: "error", Code: "invalid-stored-path", Path: strPtr("a.txt"), Message: "bad"},
	})

	if snap.Violations[0].Path != "a.txt" || snap.Violations[1].Path != "z.txt" {
		t.Fatalf("violations not sorted by path: %+v", snap.Violations)
	}
}

// initGitRepo creates a real git repository for Collect tests.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

func writeState(t *testing.T, repo string, openKeys []string, storeJSON string) {
	t.Helper()
	metaDir := filepath.Join(repo, ".git", "kura", "meta", "worktrees")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, k := range openKeys {
		if err := os.WriteFile(filepath.Join(metaDir, k+".json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if storeJSON != "" {
		sealDir := filepath.Join(repo, ".git", "kura", "seals")
		if err := os.MkdirAll(sealDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sealDir, "paths.json"), []byte(storeJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCollectEmptyRepo(t *testing.T) {
	repo := initGitRepo(t)

	snap, err := Collect(repo)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(snap.Groups) != 0 || len(snap.Violations) != 0 {
		t.Fatalf("snapshot = %+v, want empty", snap)
	}
}

func TestCollectAggregatesOpenKeysAndClaims(t *testing.T) {
	repo := initGitRepo(t)
	writeState(t, repo, []string{"alpha", "idle"},
		`{"schemaVersion":1,"paths":{"src/a.go":{"key":"alpha"},"src/b.go":{"key":"gone"}}}`)

	snap, err := Collect(repo)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	gotKeys := make([]string, len(snap.Groups))
	for i, g := range snap.Groups {
		gotKeys[i] = g.Key
	}
	if !reflect.DeepEqual(gotKeys, []string{"alpha", "gone", "idle"}) {
		t.Fatalf("group keys = %v, want [alpha gone idle]", gotKeys)
	}
	for _, g := range snap.Groups {
		if g.Key == "gone" && !g.Orphaned {
			t.Fatalf("gone should be orphaned: %+v", g)
		}
	}
	if snap.OpenKeys != 2 || snap.ClaimedPaths != 2 {
		t.Fatalf("counts = %d open / %d claimed, want 2/2", snap.OpenKeys, snap.ClaimedPaths)
	}
}

func TestCollectReportsIntegrityViolations(t *testing.T) {
	repo := initGitRepo(t)
	writeState(t, repo, nil,
		`{"schemaVersion":1,"paths":{"a/../b.txt":{"key":"k"},"b.txt":{"key":"k"},"ok.txt":{"key":"k"}}}`)

	snap, err := Collect(repo)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	codes := make(map[string]bool)
	for _, v := range snap.Violations {
		codes[v.Code] = true
	}
	if !codes["non-normalized-path"] || !codes["duplicate-canonical-path"] {
		t.Fatalf("violations = %+v, want non-normalized-path and duplicate-canonical-path", snap.Violations)
	}
	if !reflect.DeepEqual(snap.Groups[0].Paths, []string{"ok.txt"}) {
		t.Fatalf("paths = %v, want only ok.txt", snap.Groups[0].Paths)
	}
}

func TestCollectExcludesContestedDuplicateFirstEntry(t *testing.T) {
	repo := initGitRepo(t)
	// "b.txt" sorts before "x/../b.txt", so the clean entry gets no finding
	// from InspectPathStore; the dashboard must still not show it as a
	// normal claim because its canonical path is contested.
	writeState(t, repo, nil,
		`{"schemaVersion":1,"paths":{"b.txt":{"key":"a-key"},"x/../b.txt":{"key":"b-key"},"ok.txt":{"key":"a-key"}}}`)

	snap, err := Collect(repo)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if len(snap.Groups) != 1 || snap.Groups[0].Key != "a-key" {
		t.Fatalf("groups = %+v, want only a-key", snap.Groups)
	}
	if !reflect.DeepEqual(snap.Groups[0].Paths, []string{"ok.txt"}) {
		t.Fatalf("paths = %v, want contested b.txt excluded", snap.Groups[0].Paths)
	}
	if len(snap.Violations) != 1 || snap.Violations[0].Code != "duplicate-canonical-path" {
		t.Fatalf("violations = %+v, want one duplicate-canonical-path", snap.Violations)
	}
}

func TestCollectStoreValidationError(t *testing.T) {
	repo := initGitRepo(t)
	writeState(t, repo, nil, `{"unexpected":true}`)

	_, err := Collect(repo)
	if err == nil {
		t.Fatalf("Collect succeeded, want validation error")
	}
	var verr seal.StoreValidationErr
	if !errors.As(err, &verr) {
		t.Fatalf("Collect error = %v, want StoreValidationErr", err)
	}
}

func TestCollectOutsideGitRepository(t *testing.T) {
	dir := t.TempDir()
	if _, err := Collect(dir); err == nil {
		t.Fatalf("Collect succeeded outside a git repository, want error")
	}
}
