package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tooppoo/git-kura/internal/seal"
)

func TestRunCloseErrorPathsInProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix git worktree behavior")
	}

	t.Run("dirty worktree", func(t *testing.T) {
		cli := newTestCLI(t)
		repo := cli.initRepo(t)
		withWorkingDir(t, repo, func() {
			if err := run([]string{"open", "52"}); err != nil {
				t.Fatalf("open error = %v", err)
			}
			appendFile(t, filepath.Join(expectedWorktreePath(repo, "52"), "tracked.txt"), "dirty\n")
			if err := run([]string{"close", "52"}); err == nil {
				t.Fatal("close dirty worktree error = nil, want error")
			}
		})
	})

	t.Run("unmerged branch", func(t *testing.T) {
		cli := newTestCLI(t)
		repo := cli.initRepo(t)
		withWorkingDir(t, repo, func() {
			if err := run([]string{"open", "53"}); err != nil {
				t.Fatalf("open error = %v", err)
			}
			wt53 := expectedWorktreePath(repo, "53")
			writeFile(t, filepath.Join(wt53, "newfile.txt"), "content\n")
			git(t, wt53, "add", "newfile.txt")
			git(t, wt53, "commit", "-m", "unmerged commit")
			if err := run([]string{"close", "53"}); err == nil {
				t.Fatal("close with unmerged branch error = nil, want error")
			}
		})
	})
}

func TestRunCloseReleasesSealsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		if err := run([]string{"open", "51"}); err != nil {
			t.Fatalf("open error = %v", err)
		}
		storeFile := seedSealStore(t, repo, map[string]seal.Entry{
			"tracked.txt": {Key: "51"},
			"other.txt":   {Key: "99"},
		})

		if err := run([]string{"close", "51"}); err != nil {
			t.Fatalf("close error = %v", err)
		}

		store, err := seal.ReadStore(storeFile)
		if err != nil {
			t.Fatalf("readSealStore: %v", err)
		}
		if _, ok := store.Paths["tracked.txt"]; ok {
			t.Fatal("close did not release key 51's seal on tracked.txt")
		}
		if entry, ok := store.Paths["other.txt"]; !ok || entry.Key != "99" {
			t.Fatalf("close removed another key's seal; store = %v", store.Paths)
		}
	})
}

func TestRunCloseLockTimeoutInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		if err := run([]string{"open", "51"}); err != nil {
			t.Fatalf("open error = %v", err)
		}

		_, lockFile, err := seal.StorePaths(repo)
		if err != nil {
			t.Fatalf("pathsSealStore: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(lockFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lockFile, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(lockFile) }) //nolint:errcheck

		// 0 makes close fail immediately while the lock is held.
		git(t, repo, "config", "kura.sealLockTimeoutMs", "0")
		err = run([]string{"close", "51"})
		if err == nil {
			t.Fatal("close with held lock error = nil, want seal-lock-timeout")
		}
		var xe *exitError
		if !errors.As(err, &xe) || xe.code != exitSealLockTimeout {
			t.Fatalf("close error = %v, want exit code %d", err, exitSealLockTimeout)
		}

		// Nothing was torn down while the lock was held.
		assertPathExists(t, expectedWorktreePath(repo, "51"))
		assertPathExists(t, expectedMetadataPath(repo, "51"))
	})
}

func TestRunCloseMalformedStoreInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		if err := run([]string{"open", "51"}); err != nil {
			t.Fatalf("open error = %v", err)
		}

		storeFile, _, err := seal.StorePaths(repo)
		if err != nil {
			t.Fatalf("pathsSealStore: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(storeFile, []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := run([]string{"close", "51"}); err == nil {
			t.Fatal("close with malformed store error = nil, want error")
		}

		// The destructive cleanup never started.
		assertPathExists(t, expectedWorktreePath(repo, "51"))
		assertPathExists(t, expectedMetadataPath(repo, "51"))
	})
}

func TestRunCloseWorktreeDirGoneInProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix git worktree behavior")
	}
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		if err := run([]string{"open", "51"}); err != nil {
			t.Fatalf("open error = %v", err)
		}
		storeFile := seedSealStore(t, repo, map[string]seal.Entry{
			"tracked.txt": {Key: "51"},
		})

		// Simulate a manual deletion of the worktree directory: close must
		// prune the stale registration, delete the branch, and release the seal.
		if err := os.RemoveAll(expectedWorktreePath(repo, "51")); err != nil {
			t.Fatal(err)
		}

		if err := run([]string{"close", "51"}); err != nil {
			t.Fatalf("close error = %v", err)
		}

		store, err := seal.ReadStore(storeFile)
		if err != nil {
			t.Fatalf("readSealStore: %v", err)
		}
		if _, ok := store.Paths["tracked.txt"]; ok {
			t.Fatal("close did not release the seal after the worktree dir was removed")
		}
		assertPathMissing(t, expectedMetadataPath(repo, "51"))
	})
}

func TestRunCloseMetadataRemovalFailureInProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix filesystem semantics")
	}
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		if err := run([]string{"open", "51"}); err != nil {
			t.Fatalf("open error = %v", err)
		}

		// Replace the metadata file with a non-empty directory so the final
		// os.Remove fails with a non-NotExist error, after the worktree and
		// branch have already been cleaned up.
		metaPath := expectedMetadataPath(repo, "51")
		if err := os.Remove(metaPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(metaPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(metaPath, "blocker"), nil, 0o644); err != nil {
			t.Fatal(err)
		}

		if err := run([]string{"close", "51"}); err == nil {
			t.Fatal("close with undeletable metadata error = nil, want error")
		}

		// Earlier cleanup is not rolled back: the worktree is already gone.
		assertPathMissing(t, expectedWorktreePath(repo, "51"))
	})
}
