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

	t.Run("unset key returns ok=false without error", func(t *testing.T) {
		value, ok, err := gitutil.ConfigValue(repo, "kura.doesNotExist")
		if err != nil {
			t.Fatalf("ConfigValue error = %v, want nil", err)
		}
		if ok {
			t.Fatalf("ConfigValue ok = true, want false")
		}
		if value != "" {
			t.Fatalf("ConfigValue value = %q, want empty", value)
		}
	})

	t.Run("set key returns the value with a trailing newline", func(t *testing.T) {
		gitCmd(t, repo, "config", "kura.sealLockTimeoutMs", "5000")
		value, ok, err := gitutil.ConfigValue(repo, "kura.sealLockTimeoutMs")
		if err != nil {
			t.Fatalf("ConfigValue error = %v, want nil", err)
		}
		if !ok {
			t.Fatalf("ConfigValue ok = false, want true")
		}
		if value != "5000\n" {
			t.Fatalf("ConfigValue value = %q, want %q", value, "5000\n")
		}
	})

	t.Run("execution failure is returned as error", func(t *testing.T) {
		// Pointing git at a non-existent working directory makes the process
		// fail to start (a non-exit-status-1 failure), which must surface as a
		// real error rather than being treated as an unset key.
		_, ok, err := gitutil.ConfigValue(filepath.Join(repo, "does-not-exist"), "kura.sealLockTimeoutMs")
		if err == nil {
			t.Fatalf("ConfigValue error = nil, want error when git cannot run")
		}
		if ok {
			t.Fatalf("ConfigValue ok = true, want false on error")
		}
	})
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
