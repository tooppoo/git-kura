package worktree_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/internal/worktree"
)

func TestBranchName(t *testing.T) {
	for _, tc := range []struct {
		key  string
		want string
	}{
		{"51", "51"},
		{"ABC-123", "ABC-123"},
		{"release-2026-06", "release-2026-06"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			if got := worktree.BranchName(tc.key); got != tc.want {
				t.Fatalf("BranchName(%q) = %q, want %q", tc.key, got, tc.want)
			}
		})
	}
}

func TestWorktreePath(t *testing.T) {
	for _, tc := range []struct {
		stateDir string
		key      string
		want     string
	}{
		{
			stateDir: filepath.Join("/home", "user", "repo", ".git", "kura"),
			key:      "51",
			want:     filepath.Join("/home", "user", "repo", ".git", "kura", "worktrees", "51"),
		},
		{
			stateDir: filepath.Join("/home", "user", "myproject", ".git", "kura"),
			key:      "feature",
			want:     filepath.Join("/home", "user", "myproject", ".git", "kura", "worktrees", "feature"),
		},
	} {
		t.Run(tc.key, func(t *testing.T) {
			if got := worktree.PathInStateDir(tc.stateDir, tc.key); got != tc.want {
				t.Fatalf("PathInStateDir(%q, %q) = %q, want %q", tc.stateDir, tc.key, got, tc.want)
			}
		})
	}
}

func TestMetadataPath(t *testing.T) {
	stateDir := filepath.Join("/home", "user", "repo", ".git", "kura")
	want := filepath.Join("/home", "user", "repo", ".git", "kura", "meta", "worktrees", "51.json")
	if got := worktree.MetadataPathInStateDir(stateDir, "51"); got != want {
		t.Fatalf("MetadataPathInStateDir(%q, 51) = %q, want %q", stateDir, got, want)
	}
}

func TestReadMetadata(t *testing.T) {
	repo := initRepo(t)
	path, err := worktree.MetadataPath(repo, "51")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, `{"repositoryRoot":"/repo","baseBranch":"main","worktreePath":"/tmp/worktree"}`)

	meta, err := worktree.ReadMetadata(repo, "51")
	if err != nil {
		t.Fatalf("ReadMetadata error = %v", err)
	}
	if meta.RepositoryRoot != "/repo" || meta.BaseBranch != "main" || meta.WorktreePath != "/tmp/worktree" {
		t.Fatalf("metadata = %+v, want repositoryRoot /repo, baseBranch main, worktreePath /tmp/worktree", meta)
	}

	writeFile(t, path, `{`)
	if _, err := worktree.ReadMetadata(repo, "51"); err == nil {
		t.Fatal("ReadMetadata invalid JSON error = nil, want error")
	}

	if _, err := worktree.ReadMetadata(repo, "missing"); err == nil {
		t.Fatal("ReadMetadata missing file error = nil, want error")
	}
}

func TestReadStructuredMetadata(t *testing.T) {
	repo := initRepo(t)

	wtPath, err := worktree.Path(repo, "51")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := worktree.MetadataPath(repo, "51")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := worktree.ReadStructuredMetadata(repo, "51", wtPath, false); err == nil {
		t.Fatal("ReadStructuredMetadata unopened key error = nil, want error")
	} else if !strings.Contains(err.Error(), "not open") {
		t.Fatalf("error = %q, want it to mention not open", err.Error())
	}

	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.ReadStructuredMetadata(repo, "51", wtPath, true); err == nil {
		t.Fatal("ReadStructuredMetadata missing metadata error = nil, want error")
	} else if !strings.Contains(err.Error(), "metadata") || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %q, want it to mention missing metadata", err.Error())
	}

	if err := os.MkdirAll(filepath.Dir(metadata), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, metadata, `{`)
	if _, err := worktree.ReadStructuredMetadata(repo, "51", wtPath, true); err == nil {
		t.Fatal("ReadStructuredMetadata invalid JSON error = nil, want error")
	} else if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %q, want it to mention invalid", err.Error())
	}

	writeFile(t, metadata, `{"repositoryRoot":"`+repo+`","baseBranch":"main","worktreePath":"`+wtPath+`"}`)
	meta, err := worktree.ReadStructuredMetadata(repo, "51", wtPath, true)
	if err != nil {
		t.Fatalf("ReadStructuredMetadata error = %v", err)
	}
	if meta.RepositoryRoot != repo || meta.BaseBranch != "main" || meta.WorktreePath != wtPath {
		t.Fatalf("metadata = %+v, want repositoryRoot %s, baseBranch main, worktreePath %s", meta, repo, wtPath)
	}

	if _, err := worktree.ReadStructuredMetadata(repo, "51", wtPath, false); err == nil {
		t.Fatal("ReadStructuredMetadata missing worktree error = nil, want error")
	} else if !strings.Contains(err.Error(), "worktree") || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %q, want it to mention missing worktree", err.Error())
	}
}

func TestPathHelpersReturnErrorsOutsideRepository(t *testing.T) {
	dir := t.TempDir()

	if _, err := worktree.StateDir(dir); err == nil {
		t.Fatal("StateDir outside git repo error = nil, want error")
	}
	if _, err := worktree.Path(dir, "51"); err == nil {
		t.Fatal("Path outside git repo error = nil, want error")
	}
	if _, err := worktree.MetadataPath(dir, "51"); err == nil {
		t.Fatal("MetadataPath outside git repo error = nil, want error")
	}
}

func TestCurrentKey(t *testing.T) {
	repo := initRepoWithCommit(t)
	key := "51"
	wtTop := addManagedWorktree(t, repo, key)

	t.Run("inside managed worktree", func(t *testing.T) {
		got, err := worktree.CurrentKey(wtTop)
		if err != nil {
			t.Fatalf("CurrentKey error = %v", err)
		}
		if got != key {
			t.Fatalf("CurrentKey = %q, want %q", got, key)
		}
	})

	t.Run("from a subdirectory of the worktree", func(t *testing.T) {
		sub := filepath.Join(wtTop, "sub")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		// CommonDir resolves from any directory inside the worktree, but the key
		// is derived from the worktree top-level, so callers must pass the
		// top-level. Passing the top-level resolved from the subdirectory still
		// yields the same key.
		top := revParseShowToplevel(t, sub)
		got, err := worktree.CurrentKey(top)
		if err != nil {
			t.Fatalf("CurrentKey error = %v", err)
		}
		if got != key {
			t.Fatalf("CurrentKey = %q, want %q", got, key)
		}
	})

	t.Run("outside any managed worktree", func(t *testing.T) {
		if _, err := worktree.CurrentKey(repo); err == nil {
			t.Fatal("CurrentKey in main checkout error = nil, want error")
		}
	})
}

func TestResolveKeyForWorktreeRoot(t *testing.T) {
	repo := initRepoWithCommit(t)
	stateDir := filepath.Join(repo, ".git", "kura")

	// No metadata directory yet → zero matches, no error.
	if keys, err := worktree.ResolveKeyForWorktreeRoot(stateDir, repo); err != nil || len(keys) != 0 {
		t.Fatalf("empty: keys=%v err=%v, want none", keys, err)
	}

	wtTop := addManagedWorktree(t, repo, "51")

	t.Run("exactly one match", func(t *testing.T) {
		keys, err := worktree.ResolveKeyForWorktreeRoot(stateDir, wtTop)
		if err != nil {
			t.Fatalf("ResolveKeyForWorktreeRoot: %v", err)
		}
		if len(keys) != 1 || keys[0] != "51" {
			t.Fatalf("keys = %v, want [51]", keys)
		}
	})

	t.Run("no match for an unmanaged worktree root", func(t *testing.T) {
		keys, err := worktree.ResolveKeyForWorktreeRoot(stateDir, repo)
		if err != nil || len(keys) != 0 {
			t.Fatalf("keys=%v err=%v, want none", keys, err)
		}
	})

	t.Run("ambiguous metadata yields multiple matches", func(t *testing.T) {
		dupe := filepath.Join(stateDir, "meta", "worktrees", "dupe.json")
		writeFile(t, dupe, `{"repositoryRoot":"`+repo+`","baseBranch":"main","worktreePath":"`+wtTop+`"}`)
		keys, err := worktree.ResolveKeyForWorktreeRoot(stateDir, wtTop)
		if err != nil {
			t.Fatalf("ResolveKeyForWorktreeRoot: %v", err)
		}
		if len(keys) != 2 {
			t.Fatalf("keys = %v, want two matches", keys)
		}
	})

	t.Run("corrupt metadata file is skipped", func(t *testing.T) {
		bad := filepath.Join(stateDir, "meta", "worktrees", "bad.json")
		writeFile(t, bad, `{not json`)
		keys, err := worktree.ResolveKeyForWorktreeRoot(stateDir, wtTop)
		if err != nil {
			t.Fatalf("corrupt entry must not fail the scan: %v", err)
		}
		if len(keys) < 1 {
			t.Fatalf("valid matches must still be found, got %v", keys)
		}
	})
}

func TestResolveKeyForWorktreeRootReadDirError(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "kura")
	// Create the metadata path as a regular file so ReadDir fails with a
	// non-"not exist" error.
	metaParent := filepath.Join(stateDir, "meta")
	if err := os.MkdirAll(metaParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metaParent, "worktrees"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.ResolveKeyForWorktreeRoot(stateDir, dir); err == nil {
		t.Fatal("ResolveKeyForWorktreeRoot should error when the metadata dir is unreadable")
	}
}

func TestCurrentKeyMissingMetadata(t *testing.T) {
	repo := initRepoWithCommit(t)
	wtTop := addManagedWorktree(t, repo, "51")

	metaPath := filepath.Join(repo, ".git", "kura", "meta", "worktrees", "51.json")
	if err := os.Remove(metaPath); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.CurrentKey(wtTop); err == nil {
		t.Fatal("CurrentKey with missing metadata error = nil, want error")
	}
}

func TestCurrentKeyInconsistentMetadata(t *testing.T) {
	repo := initRepoWithCommit(t)
	wtTop := addManagedWorktree(t, repo, "51")

	metaPath := filepath.Join(repo, ".git", "kura", "meta", "worktrees", "51.json")
	writeFile(t, metaPath, `{"repositoryRoot":"`+repo+`","baseBranch":"main","worktreePath":"/somewhere/else"}`)
	if _, err := worktree.CurrentKey(wtTop); err == nil {
		t.Fatal("CurrentKey with inconsistent metadata error = nil, want error")
	}
}

func TestCurrentKeyInvalidMetadata(t *testing.T) {
	repo := initRepoWithCommit(t)
	wtTop := addManagedWorktree(t, repo, "51")

	metaPath := filepath.Join(repo, ".git", "kura", "meta", "worktrees", "51.json")
	writeFile(t, metaPath, `{`)
	if _, err := worktree.CurrentKey(wtTop); err == nil {
		t.Fatal("CurrentKey with invalid metadata error = nil, want error")
	}
}

func TestCurrentKeyMetadataViolatesSchema(t *testing.T) {
	repo := initRepoWithCommit(t)
	wtTop := addManagedWorktree(t, repo, "51")

	// Parseable JSON, but missing the required baseBranch/worktreePath fields.
	metaPath := filepath.Join(repo, ".git", "kura", "meta", "worktrees", "51.json")
	writeFile(t, metaPath, `{"repositoryRoot":"`+repo+`"}`)
	if _, err := worktree.CurrentKey(wtTop); err == nil {
		t.Fatal("CurrentKey with schema-violating metadata error = nil, want error")
	}
}

func initRepo(t *testing.T) string {
	t.Helper()

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "init", "-b", "main")
	return repo
}

func initRepoWithCommit(t *testing.T) string {
	t.Helper()
	repo := initRepo(t)
	gitCmd(t, repo, "config", "user.email", "kura-test@example.com")
	gitCmd(t, repo, "config", "user.name", "Kura Test")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "initial\n")
	gitCmd(t, repo, "add", "tracked.txt")
	gitCmd(t, repo, "commit", "-m", "initial")
	return repo
}

// addManagedWorktree creates a managed worktree for key and writes its
// metadata the way "git kura open" does, returning the worktree's top-level.
func addManagedWorktree(t *testing.T, repo, key string) string {
	t.Helper()
	wtPath, err := worktree.Path(repo, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "worktree", "add", wtPath, "-b", key, "HEAD")

	top := revParseShowToplevel(t, wtPath)
	metaPath, err := worktree.MetadataPath(repo, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, metaPath, `{"repositoryRoot":"`+repo+`","baseBranch":"main","worktreePath":"`+top+`"}`)
	return top
}

func revParseShowToplevel(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse --show-toplevel in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
