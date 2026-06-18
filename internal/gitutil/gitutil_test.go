package gitutil_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/internal/gitutil"
)

func TestGitHelpersReturnErrors(t *testing.T) {
	dir := t.TempDir()

	if _, err := gitutil.HeadBranch(dir); err == nil {
		t.Fatal("HeadBranch outside git repo error = nil, want error")
	}
	if _, err := gitutil.WorktreeDirty(dir); err == nil {
		t.Fatal("WorktreeDirty outside git repo error = nil, want error")
	}
}

func TestRepoRootReturnsPath(t *testing.T) {
	root, err := gitutil.RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot error = %v", err)
	}
	if root == "" {
		t.Fatal("RepoRoot = empty, want non-empty path")
	}
}

func TestHeadBranchReturnsCurrentBranch(t *testing.T) {
	repo := initRepo(t)

	branch, err := gitutil.HeadBranch(repo)
	if err != nil {
		t.Fatalf("HeadBranch error = %v", err)
	}
	if branch != "main" {
		t.Fatalf("HeadBranch = %q, want %q", branch, "main")
	}
}

func TestDeleteBranch(t *testing.T) {
	t.Run("deletes existing branch", func(t *testing.T) {
		repo := initRepo(t)
		gitCmd(t, repo, "branch", "to-delete")

		if err := gitutil.DeleteBranch(repo, "to-delete"); err != nil {
			t.Fatalf("DeleteBranch error = %v", err)
		}
	})

	t.Run("returns error for non-existent branch", func(t *testing.T) {
		repo := initRepo(t)

		if err := gitutil.DeleteBranch(repo, "no-such-branch"); err == nil {
			t.Fatal("DeleteBranch non-existent branch error = nil, want error")
		}
	})
}

func TestBranchExists(t *testing.T) {
	t.Run("true for existing branch", func(t *testing.T) {
		repo := initRepo(t)
		gitCmd(t, repo, "branch", "feature")

		exists, err := gitutil.BranchExists(repo, "feature")
		if err != nil {
			t.Fatalf("BranchExists error = %v", err)
		}
		if !exists {
			t.Fatal("BranchExists existing branch = false, want true")
		}
	})

	t.Run("true for the current branch", func(t *testing.T) {
		repo := initRepo(t)

		exists, err := gitutil.BranchExists(repo, "main")
		if err != nil {
			t.Fatalf("BranchExists error = %v", err)
		}
		if !exists {
			t.Fatal("BranchExists current branch = false, want true")
		}
	})

	t.Run("false for absent branch", func(t *testing.T) {
		repo := initRepo(t)

		exists, err := gitutil.BranchExists(repo, "no-such-branch")
		if err != nil {
			t.Fatalf("BranchExists error = %v", err)
		}
		if exists {
			t.Fatal("BranchExists absent branch = true, want false")
		}
	})

	t.Run("error outside a git repository", func(t *testing.T) {
		dir := t.TempDir()

		if _, err := gitutil.BranchExists(dir, "main"); err == nil {
			t.Fatal("BranchExists outside git repo error = nil, want error")
		}
	})
}

func TestPruneWorktrees(t *testing.T) {
	t.Run("removes registration for a deleted worktree directory", func(t *testing.T) {
		repo := initRepo(t)
		linked := filepath.Join(t.TempDir(), "linked")
		gitCmd(t, repo, "worktree", "add", "-b", "linked", linked)

		// Simulate a manual deletion of the worktree directory. Git still holds
		// the administrative entry (and considers "linked" checked out) until
		// the entry is pruned.
		if err := os.RemoveAll(linked); err != nil {
			t.Fatal(err)
		}

		if err := gitutil.PruneWorktrees(repo); err != nil {
			t.Fatalf("PruneWorktrees error = %v", err)
		}

		// After pruning, the branch is no longer considered checked out, so it
		// can be deleted.
		if err := gitutil.DeleteBranch(repo, "linked"); err != nil {
			t.Fatalf("DeleteBranch after prune error = %v", err)
		}
	})

	t.Run("is a no-op when there is nothing to prune", func(t *testing.T) {
		repo := initRepo(t)

		if err := gitutil.PruneWorktrees(repo); err != nil {
			t.Fatalf("PruneWorktrees error = %v", err)
		}
	})

	t.Run("error outside a git repository", func(t *testing.T) {
		dir := t.TempDir()

		if err := gitutil.PruneWorktrees(dir); err == nil {
			t.Fatal("PruneWorktrees outside git repo error = nil, want error")
		}
	})
}

func TestWorktreeDirty(t *testing.T) {
	t.Run("clean worktree returns false", func(t *testing.T) {
		repo := initRepo(t)

		dirty, err := gitutil.WorktreeDirty(repo)
		if err != nil {
			t.Fatalf("WorktreeDirty error = %v", err)
		}
		if dirty {
			t.Fatal("WorktreeDirty clean repo = true, want false")
		}
	})

	t.Run("untracked file returns true", func(t *testing.T) {
		repo := initRepo(t)
		if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}

		dirty, err := gitutil.WorktreeDirty(repo)
		if err != nil {
			t.Fatalf("WorktreeDirty error = %v", err)
		}
		if !dirty {
			t.Fatal("WorktreeDirty untracked file = false, want true")
		}
	})
}

func TestGitCommonDirSupportsLinkedWorktree(t *testing.T) {
	repo := initRepo(t)
	linked := filepath.Join(t.TempDir(), "linked")

	gitCmd(t, repo, "worktree", "add", "-b", "linked", linked)

	commonDir, err := gitutil.CommonDir(linked)
	if err != nil {
		t.Fatalf("CommonDir linked worktree error = %v", err)
	}
	want := filepath.Join(repo, ".git")
	if commonDir != want {
		t.Fatalf("CommonDir linked worktree = %q, want %q", commonDir, want)
	}
}

func TestConfigValue(t *testing.T) {
	repo := initRepo(t)

	t.Run("unset key returns configured=false without error", func(t *testing.T) {
		value, configured, err := gitutil.ConfigValue(repo, "kura.doesNotExist")
		if err != nil {
			t.Fatalf("ConfigValue error = %v, want nil", err)
		}
		if configured {
			t.Fatalf("ConfigValue configured = true, want false")
		}
		if value != "" {
			t.Fatalf("ConfigValue value = %q, want empty", value)
		}
	})

	t.Run("set key returns the value with a trailing newline", func(t *testing.T) {
		gitCmd(t, repo, "config", "kura.sealLockTimeoutMs", "5000")
		value, configured, err := gitutil.ConfigValue(repo, "kura.sealLockTimeoutMs")
		if err != nil {
			t.Fatalf("ConfigValue error = %v, want nil", err)
		}
		if !configured {
			t.Fatalf("ConfigValue configured = false, want true")
		}
		if value != "5000\n" {
			t.Fatalf("ConfigValue value = %q, want %q", value, "5000\n")
		}
	})

	t.Run("execution failure is returned as error", func(t *testing.T) {
		// Pointing git at a non-existent working directory makes the process
		// fail to start (a non-exit-status-1 failure), which must surface as a
		// real error rather than being treated as an unset key.
		_, configured, err := gitutil.ConfigValue(filepath.Join(repo, "does-not-exist"), "kura.sealLockTimeoutMs")
		if err == nil {
			t.Fatalf("ConfigValue error = nil, want error when git cannot run")
		}
		if configured {
			t.Fatalf("ConfigValue configured = true, want false on error")
		}
	})
}

func TestConfigLocalSetGetUnset(t *testing.T) {
	repo := initRepo(t)

	if _, ok, err := gitutil.ConfigGetLocal(repo, "core.hooksPath"); err != nil || ok {
		t.Fatalf("unset local key: ok=%v err=%v, want false/nil", ok, err)
	}
	if err := gitutil.SetConfigLocal(repo, "core.hooksPath", "/abs/hooks"); err != nil {
		t.Fatalf("SetConfigLocal: %v", err)
	}
	v, ok, err := gitutil.ConfigGetLocal(repo, "core.hooksPath")
	if err != nil || !ok || strings.TrimRight(v, "\n") != "/abs/hooks" {
		t.Fatalf("ConfigGetLocal = %q/%v/%v, want /abs/hooks", v, ok, err)
	}
	// Unsetting twice is a no-op the second time.
	if err := gitutil.UnsetConfigLocal(repo, "core.hooksPath"); err != nil {
		t.Fatalf("UnsetConfigLocal: %v", err)
	}
	if err := gitutil.UnsetConfigLocal(repo, "core.hooksPath"); err != nil {
		t.Fatalf("UnsetConfigLocal idempotent: %v", err)
	}
	if _, ok, _ := gitutil.ConfigGetLocal(repo, "core.hooksPath"); ok {
		t.Fatal("core.hooksPath should be unset")
	}
}

func TestConfigGetLocalExecutionError(t *testing.T) {
	// Pointing at a non-existent working directory makes git fail to start,
	// which must surface as a real error rather than an unset key.
	repo := initRepo(t)
	if _, configured, err := gitutil.ConfigGetLocal(filepath.Join(repo, "missing"), "core.hooksPath"); err == nil || configured {
		t.Fatalf("ConfigGetLocal should error when git cannot run; configured=%v err=%v", configured, err)
	}
}

func TestConfigGetAllWithScopeExecutionError(t *testing.T) {
	repo := initRepo(t)
	if _, err := gitutil.ConfigGetAllWithScope(filepath.Join(repo, "missing"), "core.hooksPath"); err == nil {
		t.Fatal("ConfigGetAllWithScope should error when git cannot run")
	}
}

func TestConfigGetAllWithScope(t *testing.T) {
	repo := initRepo(t)

	if vals, err := gitutil.ConfigGetAllWithScope(repo, "core.hooksPath"); err != nil || len(vals) != 0 {
		t.Fatalf("unset: vals=%v err=%v, want empty/nil", vals, err)
	}

	gitCmd(t, repo, "config", "--local", "core.hooksPath", "/local/hooks")
	vals, err := gitutil.ConfigGetAllWithScope(repo, "core.hooksPath")
	if err != nil {
		t.Fatalf("ConfigGetAllWithScope: %v", err)
	}
	if len(vals) != 1 || vals[0].Scope != "local" || vals[0].Value != "/local/hooks" {
		t.Fatalf("got %+v, want one local /local/hooks", vals)
	}

	// A worktree-scoped value is reported with higher precedence (later).
	gitCmd(t, repo, "config", "--local", "extensions.worktreeConfig", "true")
	gitCmd(t, repo, "config", "--worktree", "core.hooksPath", "/wt/hooks")
	vals, err = gitutil.ConfigGetAllWithScope(repo, "core.hooksPath")
	if err != nil {
		t.Fatalf("ConfigGetAllWithScope worktree: %v", err)
	}
	var sawWorktree bool
	for _, v := range vals {
		if v.Scope == "worktree" && v.Value == "/wt/hooks" {
			sawWorktree = true
		}
	}
	if !sawWorktree {
		t.Fatalf("expected a worktree-scoped value, got %+v", vals)
	}
}

func TestStagedFiles(t *testing.T) {
	repo := initRepo(t)

	if files, err := gitutil.StagedFiles(repo); err != nil || len(files) != 0 {
		t.Fatalf("clean index: files=%v err=%v, want empty/nil", files, err)
	}

	// Stage paths including one with a space to exercise NUL-safe parsing.
	for _, name := range []string{"a.txt", "with space.txt"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitCmd(t, repo, "add", name)
	}
	files, err := gitutil.StagedFiles(repo)
	if err != nil {
		t.Fatalf("StagedFiles: %v", err)
	}
	got := strings.Join(files, "|")
	if !strings.Contains(got, "a.txt") || !strings.Contains(got, "with space.txt") {
		t.Fatalf("StagedFiles = %v, want both staged paths", files)
	}

	if _, err := gitutil.StagedFiles(filepath.Join(repo, "nope")); err == nil {
		t.Fatal("StagedFiles in non-repo should error")
	}
}

func TestStagedFilesRename(t *testing.T) {
	repo := initRepo(t) // has tracked.txt committed

	// Stage a rename: tracked.txt → renamed.txt
	gitCmd(t, repo, "mv", "tracked.txt", "renamed.txt")

	files, err := gitutil.StagedFiles(repo)
	if err != nil {
		t.Fatalf("StagedFiles: %v", err)
	}
	got := strings.Join(files, "|")
	if !strings.Contains(got, "tracked.txt") {
		t.Errorf("StagedFiles = %v, want old rename path tracked.txt", files)
	}
	if !strings.Contains(got, "renamed.txt") {
		t.Errorf("StagedFiles = %v, want new rename path renamed.txt", files)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "init", "-b", "main")
	gitCmd(t, repo, "config", "user.email", "kura-test@example.com")
	gitCmd(t, repo, "config", "user.name", "Kura Test")

	tracked := filepath.Join(repo, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "add", "tracked.txt")
	gitCmd(t, repo, "commit", "-m", "initial")
	return repo
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
