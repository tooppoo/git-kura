package main

import (
	"strings"
	"testing"
)

func TestRunOpenDryRunJSONInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		// Create the worktree first so the dry run finds all three conflicts:
		// worktree path, branch, and metadata.
		if err := run([]string{"open", "51"}); err != nil {
			t.Fatalf("open error = %v", err)
		}

		stdout, err := captureStdout(t, func() error {
			return run([]string{"open", "51", "--dry-run", "--json"})
		})
		if err != nil {
			t.Fatalf("open --dry-run --json error = %v", err)
		}

		data := requireSuccessEnvelopeData(t, stdout, "open", openDryRunDataSchema)
		if data["worktreePath"] != expectedWorktreePath(repo, "51") {
			t.Fatalf("worktreePath = %v, want %s", data["worktreePath"], expectedWorktreePath(repo, "51"))
		}

		env := requireJSONMetadata(t, stdout)
		warnings := env["warnings"].([]any)
		if len(warnings) != 1 {
			t.Fatalf("warnings = %v, want one", warnings)
		}
		details := warnings[0].(map[string]any)["details"].(map[string]any)
		if len(details["conflicts"].([]any)) != 3 {
			t.Fatalf("conflicts = %v, want three", details["conflicts"])
		}

		// The dry run must not have removed or altered the existing worktree.
		assertPathExists(t, expectedWorktreePath(repo, "51"))
	})
}

func TestRunOpenDryRunJSONReportsExecutionFailure(t *testing.T) {
	outside := t.TempDir()

	withWorkingDir(t, outside, func() {
		stdout, err := captureStdout(t, func() error {
			return run([]string{"open", "51", "--json", "--dry-run"})
		})
		if err == nil {
			t.Fatal("open --json --dry-run outside repo error = nil, want error")
		}
		// A valid dry-run JSON request that fails at execution time emits an
		// ok:false envelope on stdout.
		requireErrorEnvelope(t, stdout, "open")
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

func TestRunOpenDryRunHumanReadable(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		stdout, err := captureStdout(t, func() error {
			return run([]string{"open", "51", "--dry-run"})
		})
		if err != nil {
			t.Fatalf("open --dry-run error = %v", err)
		}
		if strings.Contains(stdout, "\"ok\"") {
			t.Fatalf("open --dry-run stdout must not be an envelope: %q", stdout)
		}
		if !strings.Contains(stdout, "51") || !strings.Contains(stdout, expectedWorktreePath(repo, "51")) {
			t.Fatalf("dry-run output = %q, want branch 51 and path %s", stdout, expectedWorktreePath(repo, "51"))
		}
	})
}
