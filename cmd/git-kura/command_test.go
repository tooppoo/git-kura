package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// In-process command tests cover dispatch and command branches without spawning
// the compiled binary. End-to-end CLI behavior is covered by integration tests.

func TestRunHelpAndUsage(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "top-level short help", args: []string{"-h"}, want: "Usage: git kura"},
		{name: "top-level long help", args: []string{"--help"}, want: "Usage: git kura"},
		{name: "short version", args: []string{"-v"}, want: version},
		{name: "long version", args: []string{"--version"}, want: version},
		{name: "get help", args: []string{"get", "--help"}, want: "Usage: git kura get"},
		{name: "open help", args: []string{"open", "--help"}, want: "Usage: git kura open"},
		{name: "close help", args: []string{"close", "--help"}, want: "Usage: git kura close"},
		{name: "ls help", args: []string{"ls", "--help"}, want: "Usage: git kura ls"},
		{name: "seal help (short)", args: []string{"seal", "--help"}, want: "Usage: git kura seal"},
		{name: "seal ls help", args: []string{"seal", "ls", "--help"}, want: "Usage: git kura seal ls"},
		{name: "seal doctor help", args: []string{"seal", "doctor", "--help"}, want: "Usage: git kura seal doctor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, err := captureStdout(t, func() error {
				return run(tc.args)
			})
			if err != nil {
				t.Fatalf("run(%v) error = %v, want nil", tc.args, err)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Fatalf("stdout = %q, want it to contain %q", stdout, tc.want)
			}
		})
	}

	for _, args := range [][]string{
		{},
		{"unknown"},
	} {
		t.Run(strings.Join(append([]string{"error"}, args...), " "), func(t *testing.T) {
			if err := run(args); err == nil {
				t.Fatalf("run(%v) error = nil, want error", args)
			}
		})
	}
}

func TestRunArgumentErrors(t *testing.T) {
	for _, args := range [][]string{
		{"get"},
		{"get", "51", "--format"},
		{"open", "51", "--extra"},
		{"close", "51", "--extra"},
		{"ls", "unexpected"},
		{"seal"},
		{"seal", "unknown"},
		{"seal", "ls", "key1", "key2"},
		{"seal", "ls", "--all"},
		{"seal", "ls", "--key", "key1"},
		{"seal", "ls", "..invalid"},
		{"seal", "doctor", "key1"},
		{"seal", "doctor", "--fix"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if err := run(args); err == nil {
				t.Fatalf("run(%v) error = nil, want error", args)
			}
		})
	}
}

func TestRunSealDoctorUsageErrorsUseExitCode2(t *testing.T) {
	for _, args := range [][]string{
		{"seal", "doctor", "key1"},
		{"seal", "doctor", "--fix"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			err := run(args)
			if err == nil {
				t.Fatalf("run(%v) error = nil, want error", args)
			}
			var xe *exitError
			if !errors.As(err, &xe) || xe.code != exitUsageError {
				t.Fatalf("run(%v) exit code = %v, want %d (err: %v)", args, xe, exitUsageError, err)
			}
		})
	}
}

func TestRunCommandsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		stdout, err := captureStdout(t, func() error {
			return run([]string{"get", "51", "--path"})
		})
		if err == nil {
			t.Fatal("get --path before open error = nil, want error")
		}
		if stdout != "" {
			t.Fatalf("get --path before open stdout = %q, want empty", stdout)
		}

		stdout, err = captureStdout(t, func() error {
			return run([]string{"get", "51", "--branch"})
		})
		if err == nil {
			t.Fatal("get --branch before open error = nil, want error")
		}
		if stdout != "" {
			t.Fatalf("get --branch before open stdout = %q, want empty", stdout)
		}

		stdout, err = captureStdout(t, func() error {
			return run([]string{"get", "51", "--json"})
		})
		if err == nil {
			t.Fatal("get --json before open error = nil, want error")
		}
		if stdout != "" {
			t.Fatalf("get --json before open stdout = %q, want empty", stdout)
		}

		stdout, err = captureStdout(t, func() error {
			return run([]string{"get", "51", "--root"})
		})
		if err == nil {
			t.Fatal("get --root before open error = nil, want error")
		}
		if stdout != "" {
			t.Fatalf("get --root before open stdout = %q, want empty", stdout)
		}

		stdout, err = captureStdout(t, func() error {
			return run([]string{"open", "51", "--dry-run"})
		})
		if err != nil {
			t.Fatalf("open --dry-run error = %v", err)
		}
		dryRun := requireJSONMetadata(t, stdout)
		if dryRun["branch"] != "51" || dryRun["worktreePath"] != expectedWorktreePath(repo, "51") {
			t.Fatalf("dry-run metadata = %+v, want branch 51 and path %s", dryRun, expectedWorktreePath(repo, "51"))
		}

		if err := run([]string{"open", "51"}); err != nil {
			t.Fatalf("open error = %v", err)
		}
		assertPathExists(t, expectedWorktreePath(repo, "51"))
		assertPathExists(t, expectedMetadataPath(repo, "51"))

		stdout, err = captureStdout(t, func() error {
			return run([]string{"get", "51", "--path"})
		})
		if err != nil {
			t.Fatalf("get --path error = %v", err)
		}
		if strings.TrimSpace(stdout) != expectedWorktreePath(repo, "51") {
			t.Fatalf("get --path stdout = %q, want %q", stdout, expectedWorktreePath(repo, "51"))
		}

		stdout, err = captureStdout(t, func() error {
			return run([]string{"get", "51", "--branch"})
		})
		if err != nil {
			t.Fatalf("get --branch error = %v", err)
		}
		if strings.TrimSpace(stdout) != "51" {
			t.Fatalf("get --branch stdout = %q, want 51", stdout)
		}

		stdout, err = captureStdout(t, func() error {
			return run([]string{"get", "51", "--root"})
		})
		if err != nil {
			t.Fatalf("get --root error = %v", err)
		}
		if strings.TrimSpace(stdout) != repo {
			t.Fatalf("get --root stdout = %q, want %q", stdout, repo)
		}

		stdout, err = captureStdout(t, func() error {
			return run([]string{"get", "51", "--toon"})
		})
		if err != nil {
			t.Fatalf("get --toon error = %v", err)
		}
		for _, want := range []string{"schemaVersion", "worktreePath", "baseBranch", "exists"} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("toon stdout = %q, want it to contain %q", stdout, want)
			}
		}

		if err := run([]string{"close", "51"}); err != nil {
			t.Fatalf("close error = %v", err)
		}
		assertPathMissing(t, expectedWorktreePath(repo, "51"))
		assertPathMissing(t, expectedMetadataPath(repo, "51"))
	})
}

func TestRunCommandErrorPathsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		if err := run([]string{"close", "missing"}); err != nil {
			t.Fatalf("close missing worktree error = %v, want nil", err)
		}

		if err := run([]string{"open", "51"}); err != nil {
			t.Fatalf("open error = %v", err)
		}
		if err := run([]string{"open", "51"}); err == nil {
			t.Fatal("duplicate open error = nil, want error")
		}

		appendFile(t, filepath.Join(expectedWorktreePath(repo, "51"), "tracked.txt"), "dirty\n")
		stdout, err := captureStdout(t, func() error {
			return run([]string{"get", "51", "--json"})
		})
		if err != nil {
			t.Fatalf("get --json dirty error = %v", err)
		}
		metadata := requireJSONMetadata(t, stdout)
		if metadata["dirty"] != true {
			t.Fatalf("dirty = %v, want true", metadata["dirty"])
		}
	})
}

func TestRunCommandsOutsideRepositoryInProcess(t *testing.T) {
	outside := t.TempDir()

	withWorkingDir(t, outside, func() {
		for _, args := range [][]string{
			{"get", "51", "--path"},
			{"get", "51", "--root"},
			{"get", "51", "--json"},
			{"open", "51"},
			{"close", "51"},
			{"ls"},
		} {
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				if err := run(args); err == nil {
					t.Fatalf("run(%v) error = nil, want error", args)
				}
			})
		}
	})
}

func TestRunStructuredOutputRequiresMetadataForExistingWorktree(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		if err := run([]string{"open", "51"}); err != nil {
			t.Fatalf("open error = %v", err)
		}
		if err := os.Remove(expectedMetadataPath(repo, "51")); err != nil {
			t.Fatal(err)
		}
		if err := run([]string{"get", "51", "--json"}); err == nil {
			t.Fatal("get --json with missing metadata error = nil, want error")
		}
		if err := run([]string{"get", "51", "--toon"}); err == nil {
			t.Fatal("get --toon with missing metadata error = nil, want error")
		}
		if err := run([]string{"close", "51"}); err != nil {
			t.Fatalf("close with missing metadata error = %v, want nil", err)
		}
	})
}

func TestRunLsIgnoresNonMetadataEntries(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		if err := run([]string{"open", "51"}); err != nil {
			t.Fatalf("open error = %v", err)
		}

		metaDir := filepath.Join(expectedStateDir(repo), "meta", "worktrees")
		writeFile(t, filepath.Join(metaDir, "notjson"), "noise")
		if err := os.Mkdir(filepath.Join(metaDir, "subdir"), 0o755); err != nil {
			t.Fatal(err)
		}

		stdout, err := captureStdout(t, func() error {
			return run([]string{"ls"})
		})
		if err != nil {
			t.Fatalf("ls error = %v", err)
		}
		lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
		if len(lines) != 1 || lines[0] != "51" {
			t.Fatalf("ls stdout = %q, want only line \"51\"", stdout)
		}
	})
}

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

func TestOpenDryRunEmptyRepo(t *testing.T) {
	emptyRepo := t.TempDir()
	git(t, emptyRepo, "init", "-b", "main")
	git(t, emptyRepo, "config", "user.email", "test@example.com")
	git(t, emptyRepo, "config", "user.name", "Test")

	withWorkingDir(t, emptyRepo, func() {
		err := run([]string{"open", "51", "--dry-run"})
		if err == nil {
			t.Fatal("open --dry-run in empty repo error = nil, want error")
		}
		if !strings.Contains(err.Error(), "base branch") {
			t.Fatalf("error %q does not mention 'base branch'", err.Error())
		}
	})
}

func TestRunLsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		stdout, err := captureStdout(t, func() error {
			return run([]string{"ls"})
		})
		if err != nil {
			t.Fatalf("ls with no worktrees error = %v, want nil", err)
		}
		if strings.TrimSpace(stdout) != "" {
			t.Fatalf("ls with no worktrees stdout = %q, want empty", stdout)
		}

		if err := run([]string{"open", "51"}); err != nil {
			t.Fatalf("open 51 error = %v", err)
		}
		if err := run([]string{"open", "52"}); err != nil {
			t.Fatalf("open 52 error = %v", err)
		}

		stdout, err = captureStdout(t, func() error {
			return run([]string{"ls"})
		})
		if err != nil {
			t.Fatalf("ls error = %v", err)
		}
		for _, key := range []string{"51", "52"} {
			found := false
			for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
				if line == key {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("ls stdout = %q, want line %q", stdout, key)
			}
		}

		if err := run([]string{"close", "51"}); err != nil {
			t.Fatalf("close 51 error = %v", err)
		}

		stdout, err = captureStdout(t, func() error {
			return run([]string{"ls"})
		})
		if err != nil {
			t.Fatalf("ls after close error = %v", err)
		}
		for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
			if line == "51" {
				t.Fatalf("ls after close stdout = %q, want no line 51", stdout)
			}
		}
		found := false
		for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
			if line == "52" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("ls after close stdout = %q, want line 52", stdout)
		}
	})
}

// seedSealStore writes the given path→entry map to the repository's seal
// store and returns the store file path.
func seedSealStore(t *testing.T, repo string, paths map[string]sealEntry) string {
	t.Helper()
	storeFile, _, err := pathsSealStore(repo)
	if err != nil {
		t.Fatalf("pathsSealStore: %v", err)
	}
	if err := writeSealStore(storeFile, sealPathStore{Paths: paths}); err != nil {
		t.Fatalf("writeSealStore: %v", err)
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
		seedSealStore(t, repo, map[string]sealEntry{})
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
		seedSealStore(t, repo, map[string]sealEntry{
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
		seedSealStore(t, repo, map[string]sealEntry{
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
				storeFile, _, err := pathsSealStore(repo)
				if err != nil {
					t.Fatalf("pathsSealStore: %v", err)
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
		seedSealStore(t, repo, map[string]sealEntry{
			"src/a.go": {Key: "key1"},
		})

		_, lockFile, err := pathsSealStore(repo)
		if err != nil {
			t.Fatalf("pathsSealStore: %v", err)
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

func TestRunCloseReleasesSealsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		if err := run([]string{"open", "51"}); err != nil {
			t.Fatalf("open error = %v", err)
		}
		storeFile := seedSealStore(t, repo, map[string]sealEntry{
			"tracked.txt": {Key: "51"},
			"other.txt":   {Key: "99"},
		})

		if err := run([]string{"close", "51"}); err != nil {
			t.Fatalf("close error = %v", err)
		}

		store, err := readSealStore(storeFile)
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

		_, lockFile, err := pathsSealStore(repo)
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

		storeFile, _, err := pathsSealStore(repo)
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
		storeFile := seedSealStore(t, repo, map[string]sealEntry{
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

		store, err := readSealStore(storeFile)
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
