package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

		// get --json is a structured request: an execution-time failure before the
		// worktree is open is reported as an ok:false envelope on stdout, with the
		// renderer having already written it (run returns a non-nil sentinel).
		stdout, err = captureStdout(t, func() error {
			return run([]string{"get", "51", "--json"})
		})
		if err == nil {
			t.Fatal("get --json before open error = nil, want error")
		}
		requireErrorEnvelope(t, stdout, "get")

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
		data := requireSuccessEnvelopeData(t, stdout, "get", getDataSchema)
		if data["dirty"] != true {
			t.Fatalf("dirty = %v, want true", data["dirty"])
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

func TestRunGetAllModesInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		if err := run([]string{"open", "51"}); err != nil {
			t.Fatalf("open error = %v", err)
		}

		branch, err := captureStdout(t, func() error { return run([]string{"get", "51", "--branch"}) })
		if err != nil {
			t.Fatalf("get --branch error = %v", err)
		}
		if strings.TrimSpace(branch) != "51" {
			t.Fatalf("get --branch = %q, want 51", branch)
		}

		root, err := captureStdout(t, func() error { return run([]string{"get", "51", "--root"}) })
		if err != nil {
			t.Fatalf("get --root error = %v", err)
		}
		if strings.TrimSpace(root) != repo {
			t.Fatalf("get --root = %q, want %s", root, repo)
		}

		toon, err := captureStdout(t, func() error { return run([]string{"get", "51", "--toon"}) })
		if err != nil {
			t.Fatalf("get --toon error = %v", err)
		}
		if !strings.Contains(toon, "worktreePath") {
			t.Fatalf("get --toon = %q, want it to contain worktreePath", toon)
		}

		// --format json is an alias of --json and produces the same envelope.
		shortJSON, err := captureStdout(t, func() error { return run([]string{"get", "51", "--json"}) })
		if err != nil {
			t.Fatalf("get --json error = %v", err)
		}
		formatJSON, err := captureStdout(t, func() error { return run([]string{"get", "51", "--format", "json"}) })
		if err != nil {
			t.Fatalf("get --format json error = %v", err)
		}
		requireSuccessEnvelopeData(t, formatJSON, "get", getDataSchema)
		if shortJSON != formatJSON {
			t.Fatalf("--json and --format json differ:\n%s\n%s", shortJSON, formatJSON)
		}
	})
}
