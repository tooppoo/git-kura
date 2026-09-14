package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenCopiesWorktreeIncludeFromCurrentWorktree(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), ".env*\nconfig/\n")
	writeFile(t, filepath.Join(repo, ".worktreeinclude"), ".env*\nconfig/**\n!config/debug.log\ntracked.txt\nuntracked.txt\n")
	git(t, repo, "add", ".gitignore", ".worktreeinclude")
	git(t, repo, "commit", "-m", "configure worktree includes")
	writeFile(t, filepath.Join(repo, ".env"), "main environment")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "dirty tracked contents")
	writeFile(t, filepath.Join(repo, "untracked.txt"), "unignored contents")
	if err := os.Mkdir(filepath.Join(repo, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "config/secret"), "secret")
	writeFile(t, filepath.Join(repo, "config/debug.log"), "log")

	result := cli.gitKura(filepath.Join(repo, "config"), "open", "source", "--json")
	requireExitCode(t, result, 0)
	data := requireSuccessEnvelopeData(t, result.stdout, "open", openDataSchema)
	if data["dirty"] != false {
		t.Fatalf("dirty = %v, want false for ignored copies", data["dirty"])
	}
	source := expectedWorktreePath(repo, "source")
	assertOpenIncludeContent(t, source, ".env", "main environment")
	assertOpenIncludeContent(t, source, "config/secret", "secret")
	assertOpenIncludeContent(t, source, "tracked.txt", "initial\n")
	assertPathMissing(t, filepath.Join(source, "config/debug.log"))
	assertPathMissing(t, filepath.Join(source, "untracked.txt"))

	// A linked worktree uses its own local manifest and files, even when the
	// command runs from a subdirectory. It must not use the main checkout's env.
	writeFile(t, filepath.Join(source, ".env"), "linked environment")
	writeFile(t, filepath.Join(source, ".env.local"), "linked only")
	writeFile(t, filepath.Join(source, ".worktreeinclude"), ".env.local\n")
	result = cli.gitKura(filepath.Join(source, "config"), "open", "child", "--json")
	requireExitCode(t, result, 0)
	requireSuccessEnvelopeData(t, result.stdout, "open", openDataSchema)
	child := expectedWorktreePath(repo, "child")
	assertOpenIncludeContent(t, child, ".env.local", "linked only")
	assertPathMissing(t, filepath.Join(child, ".env"))
	assertPathMissing(t, filepath.Join(child, "config"))
}

func TestOpenIncludeDryRunHasNoSideEffects(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	// An invalid manifest would fail real application; a dry run never reads it.
	if err := os.Mkdir(filepath.Join(repo, ".worktreeinclude"), 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repo, func() {
		stdout, err := captureOutput(t, func(r *runner) error {
			return r.run([]string{"open", "preview", "--dry-run", "--json"})
		})
		if err != nil {
			t.Fatal(err)
		}
		requireSuccessEnvelopeData(t, stdout, "open", openDryRunDataSchema)
	})
	assertPathMissing(t, expectedWorktreePath(repo, "preview"))
	assertPathMissing(t, expectedMetadataPath(repo, "preview"))
	if strings.Contains(git(t, repo, "branch", "--list"), "preview") {
		t.Fatal("dry run created branch")
	}
}

func TestOpenIncludeFailureRetainsManagedWorktree(t *testing.T) {
	cli := newTestCLI(t)
	for _, format := range []string{"human", "json", "toon"} {
		t.Run(format, func(t *testing.T) {
			repo := cli.initRepo(t)
			if err := os.Mkdir(filepath.Join(repo, ".worktreeinclude"), 0o755); err != nil {
				t.Fatal(err)
			}
			withWorkingDir(t, repo, func() {
				var stdout, stderr bytes.Buffer
				args := []string{"open", "retained"}
				if format != "human" {
					args = append(args, "--"+format)
				}
				code := Run(args, &stdout, &stderr, testVersion)
				if code != exitGeneralError {
					t.Fatalf("exit = %v, want 1", code)
				}
				message := stdout.String() + stderr.String()
				for _, want := range []string{".worktreeinclude", "regular file", "retained", "manually"} {
					if !strings.Contains(message, want) {
						t.Fatalf("output = %q, want %q", message, want)
					}
				}
				if format == "json" {
					requireErrorEnvelope(t, stdout.String(), "open")
				}
				if format != "human" && stderr.Len() != 0 {
					t.Fatalf("structured stderr = %q", stderr.String())
				}
				if format == "human" && stdout.Len() != 0 {
					t.Fatalf("failure stdout = %q", stdout.String())
				}
				// Metadata remains usable by get after the copy error.
				if err := newTestRunner().run([]string{"get", "retained", "--json"}); err != nil {
					t.Fatal(err)
				}
			})
			path := expectedWorktreePath(repo, "retained")
			assertPathExists(t, path)
			assertPathExists(t, expectedMetadataPath(repo, "retained"))
			if got := strings.TrimSpace(git(t, path, "branch", "--show-current")); got != "retained" {
				t.Fatalf("branch = %q", got)
			}
		})
	}
}

func TestOpenIncludePartialFailureRetainsCopiedFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix permissions")
	}
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), "*.local\n")
	writeFile(t, filepath.Join(repo, ".worktreeinclude"), "*.local\n")
	writeFile(t, filepath.Join(repo, "a.local"), "copied before failure")
	writeFile(t, filepath.Join(repo, "z.local"), "unreadable")
	if err := os.Chmod(filepath.Join(repo, "z.local"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(repo, "z.local"), 0o600) })
	if _, err := os.ReadFile(filepath.Join(repo, "z.local")); err == nil {
		t.Skip("current user bypasses permissions")
	}
	withWorkingDir(t, repo, func() {
		stdout, err := captureOutput(t, func(r *runner) error { return r.run([]string{"open", "partial", "--json"}) })
		if err == nil {
			t.Fatal("open succeeded, want copy failure")
		}
		requireErrorEnvelope(t, stdout, "open")
		if !strings.Contains(stdout, "z.local") {
			t.Fatalf("output = %q, want failed path", stdout)
		}
	})
	assertOpenIncludeContent(t, expectedWorktreePath(repo, "partial"), "a.local", "copied before failure")
	assertPathExists(t, expectedMetadataPath(repo, "partial"))
}

func TestOpenIncludeReportsDirtyWhenIgnoreRulesAreUncommitted(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), ".env\n")
	writeFile(t, filepath.Join(repo, ".worktreeinclude"), ".env\n")
	writeFile(t, filepath.Join(repo, ".env"), "local")
	withWorkingDir(t, repo, func() {
		stdout, err := captureOutput(t, func(r *runner) error { return r.run([]string{"open", "dirty", "--json"}) })
		if err != nil {
			t.Fatal(err)
		}
		data := requireSuccessEnvelopeData(t, stdout, "open", openDataSchema)
		if data["dirty"] != true {
			t.Fatalf("dirty = %v, want true", data["dirty"])
		}
	})
	assertOpenIncludeContent(t, expectedWorktreePath(repo, "dirty"), ".env", "local")
}

func assertOpenIncludeContent(t *testing.T, root, name, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, name))
	if err != nil || string(got) != want {
		t.Fatalf("%q = %q, error = %v, want %q", name, got, err, want)
	}
}
