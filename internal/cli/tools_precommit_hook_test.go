package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tooppoo/git-kura/internal/seal"
	"github.com/tooppoo/git-kura/internal/tools"
)

func runHook(t *testing.T, dir string, args ...string) error {
	t.Helper()
	var err error
	withWorkingDir(t, dir, func() {
		_, err = captureOutput(t, func(r *runner) error { return r.runPreCommitHook(args) })
	})
	return err
}

// writePreCommitMetaForHook installs metadata whose previous effective
// core.hooksPath is prevValue (a directory; may be absolute or relative). The
// previous hook chained at runtime is "<resolved prevValue>/pre-commit".
func writePreCommitMetaForHook(t *testing.T, repo, prevValue string) {
	t.Helper()
	commonDir := filepath.Join(repo, ".git")
	meta := tools.PreCommitMeta{
		InstallState:           tools.PreCommitStateInstalled,
		PreviousHooksPathState: "set",
		PreviousHooksPathValue: prevValue,
		NewHooksPathValue:      tools.PreCommitHooksDir(commonDir),
		WrapperChecksum:        tools.SHA256Hex([]byte(tools.PreCommitWrapperScript)),
	}
	entry := tools.MetadataEntry{
		Component:         tools.PreCommitComponentID,
		ManagedMode:       tools.ManagedModeConfig,
		ComponentMetadata: meta.ToMap(),
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	persistToolsEntry(t, repo, entry)
}

// writePrevHook creates an executable (unless mode says otherwise) pre-commit
// hook at "<dir>/pre-commit" and returns the directory.
func writePrevHook(t *testing.T, dir, script string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pre-commit"), []byte(script), mode); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPreCommitHookAllowsUnclaimedWhenKeyNone(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	stage(t, repo, "free.txt", "free\n")
	// repo itself is not a managed worktree → current key is none; unclaimed
	// staged files are allowed.
	if err := runHook(t, repo); err != nil {
		t.Fatalf("hook should allow unclaimed file with key=none: %v", err)
	}
}

func TestPreCommitHookRejectsClaimedWhenKeyNone(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	wt := openManagedWorktree(t, repo, "owner")
	stage(t, wt, "shared.txt", "x\n")
	withWorkingDir(t, wt, func() {
		if err := newTestRunner().cmdSealClaim(sealClaimOptions{}, []string{"shared.txt"}); err != nil {
			t.Fatalf("claim: %v", err)
		}
	})

	stage(t, repo, "shared.txt", "y\n")
	if code := exitCodeOf(runHook(t, repo)); code != exitSealConflict {
		t.Fatalf("expected seal-conflict exit %d, got %d", exitSealConflict, code)
	}
}

func TestPreCommitHookRejectsForeignKeyClaim(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	wtA := openManagedWorktree(t, repo, "alpha")
	wtB := openManagedWorktree(t, repo, "beta")

	stage(t, wtB, "shared.txt", "b\n")
	withWorkingDir(t, wtB, func() {
		if err := newTestRunner().cmdSealClaim(sealClaimOptions{}, []string{"shared.txt"}); err != nil {
			t.Fatalf("claim: %v", err)
		}
	})

	stage(t, wtA, "shared.txt", "a\n")
	if code := exitCodeOf(runHook(t, wtA)); code != exitSealConflict {
		t.Fatalf("expected seal-conflict, got exit %d", code)
	}

	stage(t, wtA, "ownfile.txt", "a\n")
	withWorkingDir(t, wtA, func() {
		if err := newTestRunner().cmdSealClaim(sealClaimOptions{}, []string{"ownfile.txt"}); err != nil {
			t.Fatalf("claim own: %v", err)
		}
	})
	git(t, wtA, "reset", "shared.txt")
	if err := runHook(t, wtA); err != nil {
		t.Fatalf("hook should allow own-claimed file: %v", err)
	}
}

func TestPreCommitHookHandlesSpecialCharPaths(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	wt := openManagedWorktree(t, repo, "owner")
	weird := "a dir/with space.txt"
	stage(t, wt, weird, "x\n")
	withWorkingDir(t, wt, func() {
		if err := newTestRunner().cmdSealClaim(sealClaimOptions{}, []string{weird}); err != nil {
			t.Fatalf("claim: %v", err)
		}
	})

	stage(t, repo, weird, "y\n")
	if code := exitCodeOf(runHook(t, repo)); code != exitSealConflict {
		t.Fatalf("expected conflict on space-containing path, got exit %d", code)
	}
}

func TestPreCommitHookChainsPreviousHookFailure(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	dir := writePrevHook(t, filepath.Join(repo, "prevhooks"), "#!/bin/sh\nexit 7\n", 0o755)
	writePreCommitMetaForHook(t, repo, dir)

	stage(t, repo, "free.txt", "free\n")
	if code := exitCodeOf(runHook(t, repo)); code != 7 {
		t.Fatalf("expected previous hook exit 7, got %d", code)
	}
}

func TestPreCommitHookChainsPreviousHookSuccess(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	marker := filepath.Join(repo, "ran")
	dir := writePrevHook(t, filepath.Join(repo, "prevhooks"), "#!/bin/sh\ntouch "+marker+"\nexit 0\n", 0o755)
	writePreCommitMetaForHook(t, repo, dir)

	stage(t, repo, "free.txt", "free\n")
	if err := runHook(t, repo); err != nil {
		t.Fatalf("hook should pass: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("previous hook should have run: %v", err)
	}
}

// TestPreCommitHookChainsPreviousHookPerWorktree is the regression test for the
// linked-worktree resolution finding: a relative previous core.hooksPath must
// chain the hook of the worktree the commit happens in, not the install
// worktree.
func TestPreCommitHookChainsPreviousHookPerWorktree(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	// Previous core.hooksPath is the RELATIVE value "relhooks".
	writePreCommitMetaForHook(t, repo, "relhooks")

	// Linked worktree alpha has its own relhooks/pre-commit that exits 42; the
	// install worktree (repo) deliberately has none.
	wt := openManagedWorktree(t, repo, "alpha")
	writePrevHook(t, filepath.Join(wt, "relhooks"), "#!/bin/sh\nexit 42\n", 0o755)

	stage(t, wt, "free.txt", "free\n")
	if code := exitCodeOf(runHook(t, wt)); code != 42 {
		t.Fatalf("hook in the linked worktree must chain <linked>/relhooks/pre-commit (exit 42), got %d", code)
	}

	// From the install worktree, the same relative value resolves to repo/relhooks
	// which has no hook, so nothing is chained and the commit is allowed.
	stage(t, repo, "free.txt", "free\n")
	if err := runHook(t, repo); err != nil {
		t.Fatalf("install worktree has no relhooks/pre-commit; hook should pass, got %v", err)
	}
}

func TestPreCommitHookSkipsNonExecutablePrevious(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	dir := writePrevHook(t, filepath.Join(repo, "prevhooks"), "#!/bin/sh\nexit 9\n", 0o644)
	writePreCommitMetaForHook(t, repo, dir)

	stage(t, repo, "free.txt", "free\n")
	if err := runHook(t, repo); err != nil {
		t.Fatalf("non-executable previous hook should be skipped, got: %v", err)
	}
}

func TestPreCommitHookSkipsMissingPrevious(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	writePreCommitMetaForHook(t, repo, filepath.Join(repo, "does-not-exist"))
	stage(t, repo, "free.txt", "free\n")
	if err := runHook(t, repo); err != nil {
		t.Fatalf("missing previous hook should be skipped: %v", err)
	}
}

func TestEvaluateSealedPaths(t *testing.T) {
	store := seal.PathStore{Paths: map[string]seal.Entry{
		"a.txt":     {Key: "k1"},
		"dir/b.txt": {Key: "k2"},
	}}
	if c := seal.EvaluateStorePaths(store, seal.KeyNone, []string{"a.txt", "free.txt"}); len(c) != 1 {
		t.Fatalf("key none: want 1 conflict, got %d", len(c))
	}
	if c := seal.EvaluateStorePaths(store, "k1", []string{"a.txt", "dir/b.txt"}); len(c) != 1 || c[0].SealedBy != "k2" {
		t.Fatalf("key k1: want 1 conflict by k2, got %v", c)
	}
	if c := seal.EvaluateStorePaths(store, "k1", []string{"./dir/b.txt"}); len(c) != 1 {
		t.Fatalf("want canonicalized match, got %d", len(c))
	}
}

func TestPreCommitHookFailsClosedOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	if code := exitCodeOf(runHook(t, dir)); code != exitGeneralError {
		t.Fatalf("hook outside a repo should fail closed (exit 1), got %d", code)
	}
}

func TestPreCommitHookFailsClosedOnUnreadableMetadata(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	// Make the worktree metadata directory unreadable (a file in its place), so
	// current-key derivation fails and the hook fails closed.
	metaParent := filepath.Join(repo, ".git", "kura", "meta")
	if err := os.MkdirAll(metaParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metaParent, "worktrees"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stage(t, repo, "free.txt", "free\n")
	if code := exitCodeOf(runHook(t, repo)); code != exitGeneralError {
		t.Fatalf("unreadable metadata should fail closed, got exit %d", code)
	}
}

func TestPreCommitHookFailsClosedOnAmbiguousKey(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")

	wt := openManagedWorktree(t, repo, "alpha")
	// Inject a second metadata file mapping another key to the same worktree
	// root, making the current-key derivation ambiguous.
	dupe := filepath.Join(repo, ".git", "kura", "meta", "worktrees", "beta.json")
	if err := os.WriteFile(dupe, []byte(`{"repositoryRoot":"`+repo+`","baseBranch":"main","worktreePath":"`+wt+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stage(t, wt, "free.txt", "free\n")
	if code := exitCodeOf(runHook(t, wt)); code != exitGeneralError {
		t.Fatalf("ambiguous key should fail closed, got exit %d", code)
	}
}

func TestPreCommitSealCheckFailsOnCorruptStore(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")
	stage(t, repo, "free.txt", "free\n")

	storeFile, _, err := seal.StorePaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storeFile, []byte("{not valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = preCommitSealCheck(repo, seal.KeyNone, "pre-hook")
	if exitCodeOf(err) != exitGeneralError {
		t.Fatalf("corrupt store should fail closed, got %v", err)
	}
}

func TestRunPreviousPreCommitNoMetadata(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	// No installed.json at all → nothing to chain.
	if err := newTestRunner().runPreviousPreCommit(repo, commonDir, nil); err != nil {
		t.Fatalf("no metadata should be a no-op, got %v", err)
	}
}

func TestSamePathSafe(t *testing.T) {
	if tools.SamePathSafe("", "/x") || tools.SamePathSafe("/x", "") {
		t.Fatal("empty inputs must never match")
	}
	if !tools.SamePathSafe("/a/b", "/a/b") {
		t.Fatal("identical clean paths should match")
	}
	if tools.SamePathSafe("/a/b", "/a/c") {
		t.Fatal("different paths should not match")
	}
}

func TestRunPreviousPreCommitEmptyPath(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	// Installed metadata with no previous hook recorded → nothing to chain.
	entry := tools.MetadataEntry{
		Component:         tools.PreCommitComponentID,
		ManagedMode:       tools.ManagedModeConfig,
		ComponentMetadata: tools.PreCommitMeta{InstallState: tools.PreCommitStateInstalled}.ToMap(),
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	persistToolsEntry(t, repo, entry)
	if err := newTestRunner().runPreviousPreCommit(repo, commonDir, nil); err != nil {
		t.Fatalf("empty previous path should be a no-op, got %v", err)
	}
}

func TestRunPreviousPreCommitInvalidMeta(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	// Write a pre-commit entry whose componentMetadata fails the pre-commit schema
	// (installState "bad" is not in the allowed enum). runPreviousPreCommit must
	// silently skip the invalid entry rather than hard-fail.
	entry := tools.MetadataEntry{
		Component:         tools.PreCommitComponentID,
		ManagedMode:       tools.ManagedModeConfig,
		ComponentMetadata: map[string]any{"installState": "bad"},
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	persistToolsEntry(t, repo, entry)
	if err := newTestRunner().runPreviousPreCommit(repo, commonDir, nil); err != nil {
		t.Fatalf("invalid meta should be silently skipped, got %v", err)
	}
}

func TestRunPreviousPreCommitRecursionGuard(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	hooksDir := tools.PreCommitHooksDir(commonDir)
	writeWrapperForTest(t, filepath.Join(hooksDir, "pre-commit"))
	// Metadata whose previous core.hooksPath points back at git-kura's own
	// managed dir must be skipped to avoid infinite recursion.
	writePreCommitMetaForHook(t, repo, hooksDir)
	if err := newTestRunner().runPreviousPreCommit(repo, commonDir, nil); err != nil {
		t.Fatalf("recursion guard should skip, got %v", err)
	}
}
