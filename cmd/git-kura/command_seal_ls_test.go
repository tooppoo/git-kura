package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tooppoo/git-kura/internal/seal"
)

// seedSealStore writes the given path→entry map to the repository's seal
// store and returns the store file path.
func seedSealStore(t *testing.T, repo string, paths map[string]seal.Entry) string {
	t.Helper()
	storeFile, _, err := seal.StorePaths(repo)
	if err != nil {
		t.Fatalf("seal.StorePaths: %v", err)
	}
	if err := seal.WriteStore(storeFile, seal.PathStore{Paths: paths}); err != nil {
		t.Fatalf("seal.WriteStore: %v", err)
	}
	return storeFile
}

func TestCmdSealLsEmpty(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		// No paths.json at all → empty store, exit 0, empty stdout.
		stdout, err := captureStdout(t, func() error {
			return run([]string{"seal", "ls"})
		})
		if err != nil {
			t.Fatalf("seal ls error = %v, want nil", err)
		}
		if stdout != "" {
			t.Fatalf("stdout = %q, want empty", stdout)
		}

		// Store exists but has no sealed paths → same result.
		seedSealStore(t, repo, map[string]seal.Entry{})
		stdout, err = captureStdout(t, func() error {
			return run([]string{"seal", "ls"})
		})
		if err != nil {
			t.Fatalf("seal ls error = %v, want nil", err)
		}
		if stdout != "" {
			t.Fatalf("stdout = %q, want empty", stdout)
		}
	})
}

func TestCmdSealLsListsAllKeysSorted(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		seedSealStore(t, repo, map[string]seal.Entry{
			"src/z.go":      {Key: "key1"},
			"src/a.go":      {Key: "key1"},
			"docs/guide.md": {Key: "key2"},
		})

		// ls is repository-wide: it must show every key regardless of the
		// caller's worktree or environment.
		stdout, err := captureStdout(t, func() error {
			return run([]string{"seal", "ls"})
		})
		if err != nil {
			t.Fatalf("seal ls error = %v, want nil", err)
		}
		want := "key1\tsrc/a.go\nkey1\tsrc/z.go\nkey2\tdocs/guide.md\n"
		if stdout != want {
			t.Fatalf("stdout = %q, want %q", stdout, want)
		}
	})
}

func TestCmdSealLsFiltersByKey(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		seedSealStore(t, repo, map[string]seal.Entry{
			"src/a.go":      {Key: "key1"},
			"docs/guide.md": {Key: "key2"},
		})

		stdout, err := captureStdout(t, func() error {
			return run([]string{"seal", "ls", "key2"})
		})
		if err != nil {
			t.Fatalf("seal ls key2 error = %v, want nil", err)
		}
		if want := "key2\tdocs/guide.md\n"; stdout != want {
			t.Fatalf("stdout = %q, want %q", stdout, want)
		}

		// A key with no sealed paths is not an error: empty stdout, exit 0.
		stdout, err = captureStdout(t, func() error {
			return run([]string{"seal", "ls", "key3"})
		})
		if err != nil {
			t.Fatalf("seal ls key3 error = %v, want nil", err)
		}
		if stdout != "" {
			t.Fatalf("stdout = %q, want empty", stdout)
		}
	})
}

func TestCmdSealLsInvalidStore(t *testing.T) {
	for name, content := range map[string]string{
		"not json":            `{`,
		"wrong schemaVersion": `{"schemaVersion":2,"paths":{}}`,
		"bad structure":       `{"schemaVersion":1,"paths":{"src/a.go":"key1"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			cli := newTestCLI(t)
			repo := cli.initRepo(t)

			withWorkingDir(t, repo, func() {
				storeFile, _, err := seal.StorePaths(repo)
				if err != nil {
					t.Fatalf("seal.StorePaths: %v", err)
				}
				if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(storeFile, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}

				if err := run([]string{"seal", "ls"}); err == nil {
					t.Fatal("seal ls with invalid store error = nil, want error")
				}
			})
		})
	}
}

func TestCmdSealLsDoesNotBlockOnLock(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		seedSealStore(t, repo, map[string]seal.Entry{
			"src/a.go": {Key: "key1"},
		})

		_, lockFile, err := seal.StorePaths(repo)
		if err != nil {
			t.Fatalf("seal.StorePaths: %v", err)
		}
		if err := os.WriteFile(lockFile, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(lockFile) }) //nolint:errcheck

		// If ls (incorrectly) tried to take the lock, a zero timeout would make
		// it fail immediately; with the lock held it must still list regardless.
		git(t, repo, "config", "kura.sealLockTimeoutMs", "0")
		stdout, err := captureStdout(t, func() error {
			return run([]string{"seal", "ls"})
		})
		if err != nil {
			t.Fatalf("seal ls with held lock error = %v, want nil", err)
		}
		if want := "key1\tsrc/a.go\n"; stdout != want {
			t.Fatalf("stdout = %q, want %q", stdout, want)
		}
	})
}

func TestCmdSealLsOutsideRepo(t *testing.T) {
	outside := t.TempDir()
	withWorkingDir(t, outside, func() {
		if err := run([]string{"seal", "ls"}); err == nil {
			t.Fatal("run(seal ls) outside repo error = nil, want error")
		}
	})
}
