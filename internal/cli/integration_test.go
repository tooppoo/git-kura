package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/internal/seal"
)

// Integration tests exercise the git-kura binary through Git's subcommand
// dispatch against real temporary repositories.

func TestRepositoryContext(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "ls succeeds in repository", args: []string{"ls"}},
		{name: "open succeeds in repository", args: []string{"open", "51"}},
		{name: "get succeeds in repository", args: []string{"get", "51", "--path"}},
		{name: "close succeeds in repository", args: []string{"close", "51"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := cli.gitKura(repo, tc.args...)
			requireExitCode(t, result, 0)
		})
	}

	outside := t.TempDir()
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "get path fails outside repository", args: []string{"get", "51", "--path"}},
		{name: "get branch fails outside repository", args: []string{"get", "51", "--branch"}},
		{name: "open fails outside repository", args: []string{"open", "51"}},
		{name: "close fails outside repository", args: []string{"close", "51"}},
		{name: "ls fails outside repository", args: []string{"ls"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := cli.gitKura(outside, tc.args...)
			requireNonZeroExitCode(t, result)
			requireEmptyStdout(t, result)
			requireStderrContains(t, result, "repository")
			assertPathMissing(t, filepath.Join(outside, ".git"))
			assertPathMissing(t, expectedStateDir(outside))
			assertPathMissing(t, filepath.Join(outside, ".git-kura.toml"))
		})
	}

	// get --json is a structured request, so an execution-time failure (here,
	// running outside a repository) is reported as an ok:false envelope on stdout
	// rather than a bare stderr message.
	t.Run("get json fails outside repository", func(t *testing.T) {
		result := cli.gitKura(outside, "get", "51", "--json")
		requireNonZeroExitCode(t, result)
		requireEmptyStderr(t, result)
		env := requireErrorEnvelope(t, result.stdout, "get")
		errObj := env["error"].(map[string]any)
		if !strings.Contains(errObj["message"].(string), "repository") {
			t.Fatalf("error.message = %v, want it to mention the repository", errObj["message"])
		}
		assertPathMissing(t, filepath.Join(outside, ".git"))
		assertPathMissing(t, expectedStateDir(outside))
	})

	// open --json and close --json outside a repository produce ok:false envelopes.
	t.Run("open json fails outside repository", func(t *testing.T) {
		result := cli.gitKura(outside, "open", "51", "--json")
		requireNonZeroExitCode(t, result)
		requireEmptyStderr(t, result)
		requireErrorEnvelope(t, result.stdout, "open")
	})

	t.Run("close json fails outside repository", func(t *testing.T) {
		result := cli.gitKura(outside, "close", "51", "--json")
		requireNonZeroExitCode(t, result)
		requireEmptyStderr(t, result)
		requireErrorEnvelope(t, result.stdout, "close")
	})
}

func TestKeyValidationRejectsUnsafeKeysWithoutFilesystemChanges(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	for _, key := range []string{
		"../x",
		"@{upstream}",
	} {
		t.Run(printableName(key), func(t *testing.T) {
			before := gitRefs(t, repo)
			result := cli.gitKura(repo, "open", key)
			requireNonZeroExitCode(t, result)
			requireEmptyStdout(t, result)
			requireStderrContains(t, result, "key")
			if after := gitRefs(t, repo); after != before {
				t.Fatalf("git refs changed for invalid key %q\nbefore:\n%s\nafter:\n%s", key, before, after)
			}
			assertPathMissing(t, expectedWorktreePath(repo, key))
		})
	}
}

func TestGetPathIsStateIndependentAndScriptFriendly(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	want := expectedWorktreePath(repo, "51")
	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)

	for _, mutate := range []struct {
		name string
		fn   func(t *testing.T)
	}{
		{name: "clean", fn: func(t *testing.T) {}},
		{name: "unstaged changes", fn: func(t *testing.T) { appendFile(t, filepath.Join(repo, "tracked.txt"), "unstaged\n") }},
		{name: "staged changes", fn: func(t *testing.T) {
			writeFile(t, filepath.Join(repo, "staged.txt"), "staged\n")
			git(t, repo, "add", "staged.txt")
		}},
		{name: "untracked file", fn: func(t *testing.T) { writeFile(t, filepath.Join(repo, "untracked.txt"), "untracked\n") }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			mutate.fn(t)
			result := cli.gitKura(repo, "get", "51", "--path")
			requireExitCode(t, result, 0)
			requireStdoutLine(t, result, want)
			requireCleanValueStdout(t, result)
		})
	}
}

func TestGetPath(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	missing := cli.gitKura(repo, "get", "51", "--path")
	requireNonZeroExitCode(t, missing)
	requireEmptyStdout(t, missing)
	requireStderrContains(t, missing, "not open")

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	result := cli.gitKura(repo, "get", "51", "--path")
	requireExitCode(t, result, 0)
	requireStdoutLine(t, result, expectedWorktreePath(repo, "51"))
	requireCleanValueStdout(t, result)

	invalid := cli.gitKura(repo, "get", "../x", "--path")
	requireNonZeroExitCode(t, invalid)
	requireEmptyStdout(t, invalid)

	outside := cli.gitKura(t.TempDir(), "get", "51", "--path")
	requireNonZeroExitCode(t, outside)
	requireEmptyStdout(t, outside)
}

func TestGetDefaultRequiresOpenWorktree(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	beforeOpen := cli.gitKura(repo, "get", "51")
	requireNonZeroExitCode(t, beforeOpen)
	requireEmptyStdout(t, beforeOpen)
	requireStderrContains(t, beforeOpen, "not open")

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	requireStdoutLine(t, cli.gitKura(repo, "get", "51"), expectedWorktreePath(repo, "51"))

	requireExitCode(t, cli.gitKura(repo, "close", "51"), 0)
	afterClose := cli.gitKura(repo, "get", "51")
	requireNonZeroExitCode(t, afterClose)
	requireEmptyStdout(t, afterClose)
	requireStderrContains(t, afterClose, "not open")

	pathAfterClose := cli.gitKura(repo, "get", "51", "--path")
	requireNonZeroExitCode(t, pathAfterClose)
	requireEmptyStdout(t, pathAfterClose)
	requireStderrContains(t, pathAfterClose, "not open")
}

func TestGetPathCommandSubstitutionPOSIX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell command substitution is covered by the Windows-specific test")
	}

	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	result := cli.posixShell(repo, `cd "$(git kura get 51 --path)"`)
	requireExitCode(t, result, 0)
}

func TestGetPathCommandSubstitutionWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows command substitution is covered on Windows")
	}

	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	result := cli.windowsCommand(repo, `for /f "delims=" %p in ('git kura get 51 --path') do cd /d "%p"`)
	requireExitCode(t, result, 0)
}

func TestGetBranch(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	missing := cli.gitKura(repo, "get", "51", "--branch")
	requireNonZeroExitCode(t, missing)
	requireEmptyStdout(t, missing)
	requireStderrContains(t, missing, "not open")

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	result := cli.gitKura(repo, "get", "51", "--branch")
	requireExitCode(t, result, 0)
	requireStdoutLine(t, result, "51")
	requireCleanValueStdout(t, result)

	invalid := cli.gitKura(repo, "get", "../x", "--branch")
	requireNonZeroExitCode(t, invalid)
	requireEmptyStdout(t, invalid)

	outside := cli.gitKura(t.TempDir(), "get", "51", "--branch")
	requireNonZeroExitCode(t, outside)
	requireEmptyStdout(t, outside)
}

func TestGetRoot(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	missing := cli.gitKura(repo, "get", "51", "--root")
	requireNonZeroExitCode(t, missing)
	requireEmptyStdout(t, missing)
	requireStderrContains(t, missing, "not open")

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	result := cli.gitKura(repo, "get", "51", "--root")
	requireExitCode(t, result, 0)
	requireStdoutLine(t, result, repo)
	requireCleanValueStdout(t, result)

	invalid := cli.gitKura(repo, "get", "../x", "--root")
	requireNonZeroExitCode(t, invalid)
	requireEmptyStdout(t, invalid)

	outside := cli.gitKura(t.TempDir(), "get", "51", "--root")
	requireNonZeroExitCode(t, outside)
	requireEmptyStdout(t, outside)
}

func TestCloseRemovesWorktreeAndMetadata(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	metadataPath := expectedMetadataPath(repo, "51")
	assertPathExists(t, expectedWorktreePath(repo, "51"))
	assertPathExists(t, metadataPath)

	requireExitCode(t, cli.gitKura(repo, "close", "51"), 0)
	assertPathMissing(t, expectedWorktreePath(repo, "51"))
	assertPathMissing(t, metadataPath)
}

func TestCloseAllowsReopenWithSameKey(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	requireExitCode(t, cli.gitKura(repo, "close", "51"), 0)
	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)

	assertPathExists(t, expectedWorktreePath(repo, "51"))
	assertPathExists(t, expectedMetadataPath(repo, "51"))
}

func TestOpenCreatesMetadata(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)

	metadataPath := expectedMetadataPath(repo, "51")
	assertPathExists(t, metadataPath)

	metadata := requireJSONFile(t, metadataPath)
	if metadata["repositoryRoot"] != repo {
		t.Fatalf("metadata repositoryRoot = %v, want %s", metadata["repositoryRoot"], repo)
	}
	if metadata["baseBranch"] != "main" {
		t.Fatalf("metadata baseBranch = %v, want main", metadata["baseBranch"])
	}
	if metadata["worktreePath"] != expectedWorktreePath(repo, "51") {
		t.Fatalf("metadata worktreePath = %v, want %s", metadata["worktreePath"], expectedWorktreePath(repo, "51"))
	}
}

func TestOpenDryRunPrintsPlannedWorktree(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	// open --dry-run on its own prints human-readable output (not a JSON object)
	// and shows at least the planned worktree path, branch, repository root, and
	// base branch.
	result := cli.gitKura(repo, "open", "51", "--dry-run")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)
	if strings.Contains(result.stdout, "\"ok\"") {
		t.Fatalf("open --dry-run stdout must not be a JSON envelope: %q", result.stdout)
	}
	for _, want := range []string{
		expectedWorktreePath(repo, "51"),
		"51",
		repo,
		"main",
	} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("dry-run output = %q, want it to contain %q", result.stdout, want)
		}
	}
	assertPathMissing(t, expectedWorktreePath(repo, "51"))
	assertPathMissing(t, expectedMetadataPath(repo, "51"))
}

func TestOpenDryRunJSONPrintsEnvelope(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	for _, args := range [][]string{
		{"open", "51", "--dry-run", "--json"},
		{"open", "51", "--json", "--dry-run"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			result := cli.gitKura(repo, args...)
			requireExitCode(t, result, 0)
			requireEmptyStderr(t, result)

			data := requireSuccessEnvelopeData(t, result.stdout, "open", openDryRunDataSchema)
			if data["branch"] != "51" {
				t.Fatalf("dry-run branch = %v, want 51", data["branch"])
			}
			if data["worktreePath"] != expectedWorktreePath(repo, "51") {
				t.Fatalf("dry-run worktreePath = %v, want %s", data["worktreePath"], expectedWorktreePath(repo, "51"))
			}
			if data["baseBranch"] != "main" {
				t.Fatalf("dry-run baseBranch = %v, want main", data["baseBranch"])
			}
			if data["exists"] != false || data["dirty"] != false {
				t.Fatalf("dry-run exists/dirty = %v/%v, want false/false", data["exists"], data["dirty"])
			}

			env := requireJSONMetadata(t, result.stdout)
			if warnings, ok := env["warnings"].([]any); !ok || len(warnings) != 0 {
				t.Fatalf("warnings = %v, want empty array for a non-conflicting dry run", env["warnings"])
			}

			assertPathMissing(t, expectedWorktreePath(repo, "51"))
			assertPathMissing(t, expectedMetadataPath(repo, "51"))
		})
	}
}

func TestOpenDryRunReportsConflictsAsWarnings(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	// Actually create the worktree so the worktree path, branch, and metadata all
	// already exist; a subsequent dry run must report all three as conflicts.
	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)

	result := cli.gitKura(repo, "open", "51", "--dry-run", "--json")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)
	requireConformsToEnvelopeSchema(t, result.stdout)

	env := requireJSONMetadata(t, result.stdout)
	if env["ok"] != true {
		t.Fatalf("conflicting dry-run ok = %v, want true", env["ok"])
	}
	warnings, ok := env["warnings"].([]any)
	if !ok || len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one entry", env["warnings"])
	}
	warning := warnings[0].(map[string]any)
	if warning["code"] != "open-dry-run-conflict" {
		t.Fatalf("warning code = %v, want open-dry-run-conflict", warning["code"])
	}
	details := warning["details"].(map[string]any)
	conflicts, ok := details["conflicts"].([]any)
	if !ok || len(conflicts) != 3 {
		t.Fatalf("conflicts = %v, want three entries", details["conflicts"])
	}
	gotTypes := map[string]bool{}
	for _, c := range conflicts {
		gotTypes[c.(map[string]any)["type"].(string)] = true
	}
	for _, want := range []string{"worktree-path", "branch", "metadata"} {
		if !gotTypes[want] {
			t.Fatalf("conflict types = %v, want it to contain %q", gotTypes, want)
		}
	}
}

func TestOpenDryRunReportsSingleConflict(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	// Create only the branch (no worktree, no metadata) so the dry run reports
	// exactly one conflict, of type branch.
	git(t, repo, "branch", "51")

	result := cli.gitKura(repo, "open", "51", "--dry-run", "--json")
	requireExitCode(t, result, 0)

	env := requireJSONMetadata(t, result.stdout)
	warnings := env["warnings"].([]any)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one entry", warnings)
	}
	details := warnings[0].(map[string]any)["details"].(map[string]any)
	conflicts := details["conflicts"].([]any)
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %v, want exactly one", conflicts)
	}
	if conflicts[0].(map[string]any)["type"] != "branch" {
		t.Fatalf("conflict type = %v, want branch", conflicts[0].(map[string]any)["type"])
	}
}

func TestOpenDryRunHumanShowsConflictWarning(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)

	result := cli.gitKura(repo, "open", "51", "--dry-run")
	requireExitCode(t, result, 0)
	if !strings.Contains(result.stderr, "warning") {
		t.Fatalf("stderr = %q, want it to contain a warning", result.stderr)
	}
	if !strings.Contains(result.stderr, "conflict") {
		t.Fatalf("stderr = %q, want it to mention the conflict", result.stderr)
	}
}

func TestOpenStoresWorktreeAndMetadataInGitCommonDir(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)

	wantStateDir := filepath.Join(repo, ".git", "kura")
	wantWorktreePath := filepath.Join(wantStateDir, "worktrees", "51")
	wantMetadataPath := filepath.Join(wantStateDir, "meta", "worktrees", "51.json")

	assertPathExists(t, wantWorktreePath)
	assertPathExists(t, wantMetadataPath)
	requireStdoutLine(t, cli.gitKura(repo, "get", "51", "--path"), wantWorktreePath)

	metadata := requireJSONFile(t, wantMetadataPath)
	if metadata["worktreePath"] != wantWorktreePath {
		t.Fatalf("metadata worktreePath = %v, want %s", metadata["worktreePath"], wantWorktreePath)
	}
}

func TestGetStructuredOutputUsesOpenTimeBaseBranch(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	git(t, repo, "checkout", "-b", "later")

	result := cli.gitKura(repo, "get", "51", "--json")
	requireExitCode(t, result, 0)

	data := requireSuccessEnvelopeData(t, result.stdout, "get", getDataSchema)
	if data["baseBranch"] != "main" {
		t.Fatalf("json baseBranch = %v, want open-time base branch main", data["baseBranch"])
	}
}

func TestGetStructuredOutputFailsWhenMetadataIsMissing(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	if err := os.Remove(expectedMetadataPath(repo, "51")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	jsonResult := cli.gitKura(repo, "get", "51", "--json")
	requireNonZeroExitCode(t, jsonResult)
	requireEmptyStderr(t, jsonResult)
	requireErrorEnvelopeMessageContains(t, jsonResult.stdout, "get", "metadata", "missing")

	toonResult := cli.gitKura(repo, "get", "51", "--toon")
	requireNonZeroExitCode(t, toonResult)
	requireEmptyStderr(t, toonResult)
	requireTOONErrorMessageContains(t, toonResult.stdout, "get", "metadata", "missing")

	pathResult := cli.gitKura(repo, "get", "51", "--path")
	requireNonZeroExitCode(t, pathResult)
	requireEmptyStdout(t, pathResult)
	requireStderrContains(t, pathResult, "metadata")

	branchResult := cli.gitKura(repo, "get", "51", "--branch")
	requireNonZeroExitCode(t, branchResult)
	requireEmptyStdout(t, branchResult)
	requireStderrContains(t, branchResult, "metadata")
}

func TestGetStructuredOutputFailsWhenWorktreeIsMissing(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	if err := os.RemoveAll(expectedWorktreePath(repo, "51")); err != nil {
		t.Fatal(err)
	}

	jsonResult := cli.gitKura(repo, "get", "51", "--json")
	requireNonZeroExitCode(t, jsonResult)
	requireEmptyStderr(t, jsonResult)
	requireErrorEnvelopeMessageContains(t, jsonResult.stdout, "get", "worktree", "missing", "metadata exists")
}

func TestGetStructuredOutputFailsForUnopenedKey(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "1"), 0)

	jsonResult := cli.gitKura(repo, "get", "2", "--json")
	requireNonZeroExitCode(t, jsonResult)
	requireEmptyStderr(t, jsonResult)
	requireErrorEnvelopeMessageContains(t, jsonResult.stdout, "get", "not open", "git kura open 2")

	toonResult := cli.gitKura(repo, "get", "2", "--toon")
	requireNonZeroExitCode(t, toonResult)
	requireEmptyStderr(t, toonResult)
	requireTOONErrorMessageContains(t, toonResult.stdout, "get", "not open", "git kura open 2")
}

func TestGetTOONOutputContainsMetadataFields(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	result := cli.gitKura(repo, "get", "51", "--toon")
	requireExitCode(t, result, 0)

	for _, want := range []string{
		"schemaVersion",
		"key",
		"kind",
		"branch",
		"worktreePath",
		"repositoryRoot",
		"baseBranch",
		"exists",
		"dirty",
	} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("toon output = %q, want it to contain field %q", result.stdout, want)
		}
	}
}

func TestGetJSONOutputConformsToSchema(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	result := cli.gitKura(repo, "get", "51", "--json")
	requireExitCode(t, result, 0)

	requireSuccessEnvelopeData(t, result.stdout, "get", getDataSchema)
}

func TestGetFormatJSONIsAliasForJSON(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)

	jsonResult := cli.gitKura(repo, "get", "51", "--json")
	requireExitCode(t, jsonResult, 0)
	formatResult := cli.gitKura(repo, "get", "51", "--format", "json")
	requireExitCode(t, formatResult, 0)

	// --format json is an alias of --json and produces the same envelope.
	requireSuccessEnvelopeData(t, formatResult.stdout, "get", getDataSchema)
	if jsonResult.stdout != formatResult.stdout {
		t.Fatalf("--json and --format json differ:\n--json: %s\n--format json: %s", jsonResult.stdout, formatResult.stdout)
	}
}

func TestGetScalarAndJSONCombinationIsUsageError(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)

	// Scalar output and JSON output are mutually exclusive. Combining them is a
	// normal usage error (exit code 2, stderr message, no JSON envelope), not a
	// valid JSON request, regardless of flag order.
	for _, args := range [][]string{
		{"get", "51", "--path", "--json"},
		{"get", "51", "--json", "--path"},
		{"get", "51", "--path", "--format", "json"},
		{"get", "51", "--format", "json", "--path"},
		{"get", "51", "--branch", "--json"},
		{"get", "51", "--root", "--json"},
	} {
		t.Run(strings.Join(args[1:], " "), func(t *testing.T) {
			result := cli.gitKura(repo, args...)
			requireExitCode(t, result, 2)
			requireEmptyStdout(t, result)
			if result.stderr == "" {
				t.Fatal("stderr = empty, want a usage error message")
			}
			if strings.Contains(result.stderr, "\"ok\"") {
				t.Fatalf("usage error must not be a JSON envelope: %q", result.stderr)
			}
		})
	}
}

func TestOpenJSONWithoutDryRun(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	// open --json (without --dry-run) performs the real creation and returns a
	// JSON envelope with ok:true and the effect fields set.
	result := cli.gitKura(repo, "open", "51", "--json")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "open", openDataSchema)
	if data["branch"] != "51" {
		t.Fatalf("branch = %v, want 51", data["branch"])
	}
	createdWorktree, ok := data["createdWorktree"].(bool)
	if !ok || !createdWorktree {
		t.Fatalf("createdWorktree = %v, want true", data["createdWorktree"])
	}
	createdBranch, ok := data["createdBranch"].(bool)
	if !ok || !createdBranch {
		t.Fatalf("createdBranch = %v, want true", data["createdBranch"])
	}
	createdMetadata, ok := data["createdMetadata"].(bool)
	if !ok || !createdMetadata {
		t.Fatalf("createdMetadata = %v, want true", data["createdMetadata"])
	}
	assertPathExists(t, expectedWorktreePath(repo, "51"))
}

func TestOpenJSONConformsToEnvelopeSchema(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "open", "51", "--json")
	requireExitCode(t, result, 0)
	requireConformsToEnvelopeSchema(t, result.stdout)
}

func TestCloseJSON(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)

	result := cli.gitKura(repo, "close", "51", "--json")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "close", closeDataSchema)
	if data["key"] != "51" {
		t.Fatalf("key = %v, want 51", data["key"])
	}
	if data["removedWorktree"] != true {
		t.Fatalf("removedWorktree = %v, want true", data["removedWorktree"])
	}
	if data["removedBranch"] != true {
		t.Fatalf("removedBranch = %v, want true", data["removedBranch"])
	}
	if data["removedMetadata"] != true {
		t.Fatalf("removedMetadata = %v, want true", data["removedMetadata"])
	}
	if data["releasedSealCount"] != float64(0) {
		t.Fatalf("releasedSealCount = %v, want 0", data["releasedSealCount"])
	}
	assertPathMissing(t, expectedWorktreePath(repo, "51"))
}

func TestCloseJSONConformsToEnvelopeSchema(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)

	result := cli.gitKura(repo, "close", "51", "--json")
	requireExitCode(t, result, 0)
	requireConformsToEnvelopeSchema(t, result.stdout)
}

func TestSealClaimJSON(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "task1")

	// Create the file in the managed worktree: seal paths are relative to the
	// worktree root (git rev-parse --show-toplevel from the worktree directory).
	if err := os.WriteFile(filepath.Join(wt, "target.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := cli.gitKura(wt, "seal", "claim", "--json", "target.txt")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "seal.claim", sealClaimDataSchema)
	if data["currentKey"] != "task1" {
		t.Fatalf("currentKey = %v, want task1", data["currentKey"])
	}
	paths, _ := data["paths"].([]any)
	if len(paths) != 1 {
		t.Fatalf("paths len = %d, want 1", len(paths))
	}
	item := paths[0].(map[string]any)
	if item["status"] != "claimed" {
		t.Fatalf("paths[0].status = %v, want claimed", item["status"])
	}
}

func TestSealClaimJSONIdempotent(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "task1")

	if err := os.WriteFile(filepath.Join(wt, "target.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Claim once to set up state.
	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "target.txt"), 0)

	// Claim again with --json: already-owned is non-blocking.
	result := cli.gitKura(wt, "seal", "claim", "--json", "target.txt")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "seal.claim", sealClaimDataSchema)
	paths, _ := data["paths"].([]any)
	if len(paths) != 1 {
		t.Fatalf("paths len = %d, want 1", len(paths))
	}
	if paths[0].(map[string]any)["status"] != "already-owned" {
		t.Fatalf("paths[0].status = %v, want already-owned", paths[0].(map[string]any)["status"])
	}
}

func TestSealClaimJSONConflictErrorEnvelope(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := cli.openWorktree(t, repo, "task1")
	wt2 := cli.openWorktree(t, repo, "task2")

	// Create in both worktrees: claim checks file existence in each worktree root.
	for _, wt := range []string{wt1, wt2} {
		if err := os.WriteFile(filepath.Join(wt, "shared.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// task1 claims the file.
	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "shared.txt"), 0)

	// task2 tries to claim it with --json → error envelope with details.
	result := cli.gitKura(wt2, "seal", "claim", "--json", "shared.txt")
	requireNonZeroExitCode(t, result)
	requireEmptyStderr(t, result)

	env := requireErrorEnvelope(t, result.stdout, "seal.claim")
	errObj := env["error"].(map[string]any)
	if errObj["code"] != "seal-conflict" {
		t.Fatalf("error.code = %v, want seal-conflict", errObj["code"])
	}
	details, ok := errObj["details"].(map[string]any)
	if !ok {
		t.Fatalf("error.details missing or not an object: %v", errObj["details"])
	}
	if details["phase"] != "preflight" {
		t.Fatalf("details.phase = %v, want preflight", details["phase"])
	}
	if details["currentKey"] != "task2" {
		t.Fatalf("details.currentKey = %v, want task2", details["currentKey"])
	}
	conflicts, ok := details["conflicts"].([]any)
	if !ok || len(conflicts) == 0 {
		t.Fatalf("details.conflicts missing or empty: %v", details["conflicts"])
	}
	c := conflicts[0].(map[string]any)
	if c["path"] != "shared.txt" {
		t.Fatalf("conflicts[0].path = %v, want shared.txt", c["path"])
	}
	if c["ownerKey"] != "task1" {
		t.Fatalf("conflicts[0].ownerKey = %v, want task1", c["ownerKey"])
	}
	if c["requestedKey"] != "task2" {
		t.Fatalf("conflicts[0].requestedKey = %v, want task2", c["requestedKey"])
	}
}

func TestSealClaimJSONConformsToEnvelopeSchema(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "task1")

	if err := os.WriteFile(filepath.Join(wt, "target.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := cli.gitKura(wt, "seal", "claim", "--json", "target.txt")
	requireExitCode(t, result, 0)
	requireConformsToEnvelopeSchema(t, result.stdout)
}

func TestSealUnclaimJSON(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "task1")

	if err := os.WriteFile(filepath.Join(wt, "target.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "target.txt"), 0)

	result := cli.gitKura(wt, "seal", "unclaim", "--json", "target.txt")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "seal.unclaim", sealUnclaimDataSchema)
	if data["currentKey"] != "task1" {
		t.Fatalf("currentKey = %v, want task1", data["currentKey"])
	}
	paths, _ := data["paths"].([]any)
	if len(paths) != 1 {
		t.Fatalf("paths len = %d, want 1", len(paths))
	}
	if paths[0].(map[string]any)["status"] != "released" {
		t.Fatalf("paths[0].status = %v, want released", paths[0].(map[string]any)["status"])
	}
}

func TestSealUnclaimJSONIdempotent(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "task1")

	// Unclaim doesn't check file existence, so the file location doesn't matter;
	// use the worktree dir for consistency.
	if err := os.WriteFile(filepath.Join(wt, "target.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Unclaim a path that was never claimed: not-claimed is non-blocking.
	result := cli.gitKura(wt, "seal", "unclaim", "--json", "target.txt")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "seal.unclaim", sealUnclaimDataSchema)
	paths, _ := data["paths"].([]any)
	if len(paths) != 1 {
		t.Fatalf("paths len = %d, want 1", len(paths))
	}
	if paths[0].(map[string]any)["status"] != "not-claimed" {
		t.Fatalf("paths[0].status = %v, want not-claimed", paths[0].(map[string]any)["status"])
	}
}

func TestSealUnclaimJSONConflictErrorEnvelope(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := cli.openWorktree(t, repo, "task1")
	wt2 := cli.openWorktree(t, repo, "task2")

	// File only needs to exist in wt1 for the claim; unclaim doesn't stat-check.
	if err := os.WriteFile(filepath.Join(wt1, "shared.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "shared.txt"), 0)

	// task2 tries to unclaim task1's path → error envelope.
	result := cli.gitKura(wt2, "seal", "unclaim", "--json", "shared.txt")
	requireNonZeroExitCode(t, result)
	requireEmptyStderr(t, result)

	env := requireErrorEnvelope(t, result.stdout, "seal.unclaim")
	errObj := env["error"].(map[string]any)
	if errObj["code"] != "seal-conflict" {
		t.Fatalf("error.code = %v, want seal-conflict", errObj["code"])
	}
	details, ok := errObj["details"].(map[string]any)
	if !ok {
		t.Fatalf("error.details missing or not an object: %v", errObj["details"])
	}
	if details["currentKey"] != "task2" {
		t.Fatalf("details.currentKey = %v, want task2", details["currentKey"])
	}
	conflicts, ok := details["conflicts"].([]any)
	if !ok || len(conflicts) == 0 {
		t.Fatalf("details.conflicts missing or empty: %v", details["conflicts"])
	}
}

func TestSealUnclaimJSONConformsToEnvelopeSchema(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "task1")

	if err := os.WriteFile(filepath.Join(wt, "target.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "target.txt"), 0)

	result := cli.gitKura(wt, "seal", "unclaim", "--json", "target.txt")
	requireExitCode(t, result, 0)
	requireConformsToEnvelopeSchema(t, result.stdout)
}

func TestLsNoOpenWorktrees(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "ls")
	requireExitCode(t, result, 0)
	requireEmptyStdout(t, result)
	requireEmptyStderr(t, result)
}

func TestLsListsOpenWorktrees(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	requireExitCode(t, cli.gitKura(repo, "open", "FEAT-1"), 0)

	result := cli.gitKura(repo, "ls")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)
	requireStdoutContainsLine(t, result, "51")
	requireStdoutContainsLine(t, result, "FEAT-1")
}

func TestLsShowsOnlyOpenWorktrees(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	requireExitCode(t, cli.gitKura(repo, "open", "52"), 0)
	requireExitCode(t, cli.gitKura(repo, "close", "51"), 0)

	result := cli.gitKura(repo, "ls")
	requireExitCode(t, result, 0)
	requireStdoutContainsLine(t, result, "52")
	requireStdoutNotContainsLine(t, result, "51")
}

// --- seal claim / unclaim integration tests ---

func TestSealClaimOutsideWorktreeFails(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	// The main checkout is a git repository but not a git-kura managed worktree.
	result := cli.gitKura(repo, "seal", "claim", "tracked.txt")
	requireNonZeroExitCode(t, result)
	requireStderrContains(t, result, "managed worktree")
}

func TestSealUnclaimOutsideWorktreeFails(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "unclaim", "tracked.txt")
	requireNonZeroExitCode(t, result)
	requireStderrContains(t, result, "managed worktree")
}

func TestSealClaimSucceeds(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	result := cli.gitKura(wt, "seal", "claim", "tracked.txt")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)
	// Human output shows per-path claim status.
	if !strings.Contains(result.stdout, "tracked.txt") {
		t.Fatalf("stdout = %q, want it to contain claimed path", result.stdout)
	}
	if !strings.Contains(result.stdout, "claimed") {
		t.Fatalf("stdout = %q, want it to mention 'claimed'", result.stdout)
	}
}

func TestSealClaimIsIdempotent(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "tracked.txt"), 0)
	result := cli.gitKura(wt, "seal", "claim", "tracked.txt")
	requireExitCode(t, result, 0)
}

func TestSealClaimRejectsDifferentKey(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")

	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)

	// The lock is NOT held here: the rejection below is purely a cross-key
	// seal conflict, not lock contention.
	requireNoSealLock(t, repo)

	result := cli.gitKura(wt2, "seal", "claim", "tracked.txt")
	requireExitCode(t, result, int(exitSealConflict))
	requireStderrContains(t, result, "seal-conflict:")
	requireStderrContains(t, result, "key1")
}

func TestSealUnclaimRejectsDifferentKey(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")

	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)

	// The lock is NOT held here: the rejection below is purely a cross-key
	// seal conflict, not lock contention.
	requireNoSealLock(t, repo)

	result := cli.gitKura(wt2, "seal", "unclaim", "tracked.txt")
	requireExitCode(t, result, int(exitSealConflict))
	requireStderrContains(t, result, "seal-conflict:")
	requireStderrContains(t, result, "key1")
}

func TestSealClaimConflictListsAllSealedPaths(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	commitFile(t, repo, "second.txt", "content\n")
	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")
	wt3 := cli.openWorktree(t, repo, "key3")

	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)
	requireExitCode(t, cli.gitKura(wt2, "seal", "claim", "second.txt"), 0)
	requireNoSealLock(t, repo)

	result := cli.gitKura(wt3, "seal", "claim", "tracked.txt", "second.txt")
	requireExitCode(t, result, int(exitSealConflict))
	requireStderrContains(t, result, "tracked.txt")
	requireStderrContains(t, result, "key1")
	requireStderrContains(t, result, "second.txt")
	requireStderrContains(t, result, "key2")
}

func TestSealClaimRejectsNonExistentFile(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	result := cli.gitKura(wt, "seal", "claim", "nosuchfile.txt")
	requireNonZeroExitCode(t, result)
	requireStderrContains(t, result, "nosuchfile.txt")
}

func TestSealClaimResolvesPathsFromRepoRootNotCwd(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")
	sub := filepath.Join(wt1, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// Run from a subdirectory of the worktree: "tracked.txt" must resolve to the
	// file at the worktree root, not sub/tracked.txt (which does not exist). The
	// current key is still derived from the worktree, not the subdirectory.
	requireExitCode(t, cli.gitKura(sub, "seal", "claim", "tracked.txt"), 0)

	// The path sealed from the subdirectory is the root file: a different key
	// is rejected when targeting it from another worktree.
	result := cli.gitKura(wt2, "seal", "claim", "tracked.txt")
	requireExitCode(t, result, int(exitSealConflict))
	requireStderrContains(t, result, "key1")
}

func TestSealClaimRejectsAbsolutePath(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	absPath := filepath.Join(wt, "tracked.txt")
	result := cli.gitKura(wt, "seal", "claim", absPath)
	requireNonZeroExitCode(t, result)
}

func TestSealClaimRejectsPathOutsideRepo(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	result := cli.gitKura(wt, "seal", "claim", "../outside.txt")
	requireNonZeroExitCode(t, result)
}

// sealLockFilePath resolves the seal store lock file path for a repository.
func sealLockFilePath(t *testing.T, repo string) string {
	t.Helper()
	commonDir := strings.TrimSpace(git(t, repo, "rev-parse", "--git-common-dir"))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repo, commonDir)
	}
	return filepath.Join(commonDir, "kura", "seals", "paths.lock")
}

// requireNoSealLock asserts the seal store lock is not held, so a subsequent
// failure cannot be caused by lock contention.
func requireNoSealLock(t *testing.T, repo string) {
	t.Helper()
	lockPath := sealLockFilePath(t, repo)
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("seal store lock %s should not exist (stat err: %v)", lockPath, err)
	}
}

func TestSealClaimLockTimeout(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	// Hold the lock manually.
	lockPath := sealLockFilePath(t, repo)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	defer func() { _ = os.Remove(lockPath) }()

	// Resolve the lock timeout from git config; 0 makes a single attempt that
	// fails immediately while the lock is held, keeping the test fast.
	git(t, repo, "config", "kura.sealLockTimeoutMs", "0")
	result := cli.gitKura(wt, "seal", "claim", "tracked.txt")

	requireExitCode(t, result, int(exitSealLockTimeout))
	requireStderrContains(t, result, "seal-lock-timeout:")
}

func TestSealUnclaimIsIdempotentWhenNotSealed(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	result := cli.gitKura(wt, "seal", "unclaim", "tracked.txt")
	requireExitCode(t, result, 0)
}

func TestSealUnclaimRemovesPath(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")

	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)

	result := cli.gitKura(wt1, "seal", "unclaim", "tracked.txt")
	requireExitCode(t, result, 0)

	// After removal, a different key can seal the same path
	result2 := cli.gitKura(wt2, "seal", "claim", "tracked.txt")
	requireExitCode(t, result2, 0)
}

func TestSealClaimMultiplePaths(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	commitFile(t, repo, "second.txt", "content\n")
	wt := cli.openWorktree(t, repo, "key1")

	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "tracked.txt"), 0)
	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "second.txt"), 0)
}

func TestSealClaimWorksAcrossWorktrees(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt51 := cli.openWorktree(t, repo, "51")
	wt52 := cli.openWorktree(t, repo, "52")

	// tracked.txt is committed and present in every worktree.
	result := cli.gitKura(wt51, "seal", "claim", "tracked.txt")
	requireExitCode(t, result, 0)

	// The shared store prevents a different worktree's key from sealing the
	// same path.
	result2 := cli.gitKura(wt52, "seal", "claim", "tracked.txt")
	requireNonZeroExitCode(t, result2)
	requireStderrContains(t, result2, "51")
}

func TestSealClaimMissingPathArg(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "claim")
	requireNonZeroExitCode(t, result)
}

func TestSealUnclaimMissingPathArg(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "unclaim")
	requireNonZeroExitCode(t, result)
}

// --- seal test integration tests ---

func TestSealTestOutsideWorktreeFails(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	// The main checkout is a git repository but not a managed worktree. The
	// failure must be a context error, distinguishable from a seal-conflict.
	// seal-conflict (business result) goes to stdout; context errors go to stderr.
	result := cli.gitKura(repo, "seal", "test", "tracked.txt")
	requireNonZeroExitCode(t, result)
	requireStderrContains(t, result, "managed worktree")
	if strings.Contains(result.stderr, "seal-conflict:") || strings.Contains(result.stdout, "seal-conflict:") {
		t.Fatalf("context error must not look like a seal-conflict: stdout=%q stderr=%q", result.stdout, result.stderr)
	}
}

func TestSealTestUnsealedSucceeds(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	result := cli.gitKura(wt, "seal", "test", "tracked.txt")
	requireExitCode(t, result, 0)
	requireEmptyStdout(t, result)
	requireEmptyStderr(t, result)
}

func TestSealTestNonExistentPathSucceeds(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	// A path inside the repository that does not exist yet is unclaimed, so the
	// check passes — supporting pre-create checks.
	result := cli.gitKura(wt, "seal", "test", "new-file.txt")
	requireExitCode(t, result, 0)
	requireEmptyStdout(t, result)
}

func TestSealTestCurrentKeySucceeds(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "tracked.txt"), 0)

	result := cli.gitKura(wt, "seal", "test", "tracked.txt")
	requireExitCode(t, result, 0)
	requireEmptyStdout(t, result)
}

func TestSealTestRejectsDifferentKey(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")

	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)

	// The lock is NOT held here: the rejection below is purely a cross-key
	// seal conflict, not lock contention.
	requireNoSealLock(t, repo)

	result := cli.gitKura(wt2, "seal", "test", "tracked.txt")
	requireExitCode(t, result, int(exitSealConflict))
	// seal test conflict is a business result (ok:true, passed:false) → stdout.
	requireStdoutContains(t, result, "seal-conflict:")
	requireStdoutContains(t, result, "key1")
}

func TestSealTestConflictListsAllSealedPaths(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	commitFile(t, repo, "second.txt", "content\n")
	commitFile(t, repo, "third.txt", "content\n")
	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")
	wt3 := cli.openWorktree(t, repo, "key3")

	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)
	requireExitCode(t, cli.gitKura(wt2, "seal", "claim", "second.txt"), 0)
	requireNoSealLock(t, repo)

	// third.txt is safe; the two foreign claims must both be reported.
	result := cli.gitKura(wt3, "seal", "test", "tracked.txt", "second.txt", "third.txt")
	requireExitCode(t, result, int(exitSealConflict))
	// All conflicts go to stdout as a business result.
	requireStdoutContains(t, result, "tracked.txt")
	requireStdoutContains(t, result, "key1")
	requireStdoutContains(t, result, "second.txt")
	requireStdoutContains(t, result, "key2")
}

func TestSealTestRejectsPathOutsideRepo(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	result := cli.gitKura(wt, "seal", "test", "../outside.txt")
	requireNonZeroExitCode(t, result)
}

func TestSealTestRejectsUndefinedOptions(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	for _, opt := range []string{"--all", "--unsealed", "--staged"} {
		result := cli.gitKura(wt, "seal", "test", opt, "tracked.txt")
		requireNonZeroExitCode(t, result)
	}
}

func TestSealTestMissingPathArg(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "test")
	requireNonZeroExitCode(t, result)
}

func TestSealTestHelpFlag(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "test", "--help")
	requireExitCode(t, result, 0)
	if !strings.Contains(result.stdout, "Usage: git kura seal test") {
		t.Fatalf("help output = %s, want usage line", result.stdout)
	}
}

// --- seal ls integration tests ---

func TestSealLsListsAllKeysIgnoringCurrentWorktree(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	commitFile(t, repo, "second.txt", "content\n")
	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")

	requireExitCode(t, cli.gitKura(wt2, "seal", "claim", "second.txt"), 0)
	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)

	// ls is repository-wide: the current worktree's key must not narrow the
	// output, even when ls is run from inside a managed worktree.
	result := cli.gitKura(wt1, "seal", "ls")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)
	if want := "key1\ttracked.txt\nkey2\tsecond.txt\n"; result.stdout != want {
		t.Fatalf("stdout = %q, want %q", result.stdout, want)
	}
}

func TestSealLsFiltersByKeyArgument(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	commitFile(t, repo, "second.txt", "content\n")
	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")

	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)
	requireExitCode(t, cli.gitKura(wt2, "seal", "claim", "second.txt"), 0)

	result := cli.gitKura(repo, "seal", "ls", "key2")
	requireExitCode(t, result, 0)
	if want := "key2\tsecond.txt\n"; result.stdout != want {
		t.Fatalf("stdout = %q, want %q", result.stdout, want)
	}
}

func TestSealLsSeesStoreFromWorktree(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "51")

	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "tracked.txt"), 0)

	// The store is shared via the git common dir, so ls shows the same
	// repository-wide result from the main checkout and from the worktree.
	for _, dir := range []string{repo, wt} {
		result := cli.gitKura(dir, "seal", "ls")
		requireExitCode(t, result, 0)
		if want := "51\ttracked.txt\n"; result.stdout != want {
			t.Fatalf("stdout in %s = %q, want %q", dir, result.stdout, want)
		}
	}
}

func TestSealLsEmptyStoreSucceeds(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "ls")
	requireExitCode(t, result, 0)
	requireEmptyStdout(t, result)
	requireEmptyStderr(t, result)
}

func TestSealLsHelpFlag(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "ls", "--help")
	requireExitCode(t, result, 0)
	if !strings.Contains(result.stdout, "Usage: git kura seal ls [--json] [--toon] [key]") {
		t.Fatalf("help output = %s, want usage line", result.stdout)
	}
}

// --- seal doctor integration tests ---

func TestSealDoctorMissingStoreSucceedsSilently(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "doctor")
	requireExitCode(t, result, 0)
	requireEmptyStdout(t, result)
	requireEmptyStderr(t, result)
}

func TestSealDoctorDetectsInvalidStoreWithExitCode7(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	storeFile, _, err := seal.StorePaths(repo)
	if err != nil {
		t.Fatalf("pathsSealStore: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storeFile, []byte(`{"schemaVersion":1,"paths":{"src\\a.go":{"key":"key1"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result := cli.gitKura(repo, "seal", "doctor")
	requireExitCode(t, result, int(exitSealDoctorError))
	requireEmptyStderr(t, result)
	// Unhealthy is a business result (ok:true, healthy:false) → stdout.
	requireStdoutContains(t, result, "seal-doctor-error:")
	requireStdoutContains(t, result, `src\\a.go`)
}

func TestSealDoctorDoesNotCreateOrAcquireLock(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	storeFile := seedSealStore(t, repo, map[string]seal.Entry{
		"tracked.txt": {Key: "key1"},
	})
	before := readFileString(t, storeFile)
	_, lockFile, err := seal.StorePaths(repo)
	if err != nil {
		t.Fatalf("pathsSealStore: %v", err)
	}
	if err := os.WriteFile(lockFile, []byte("held"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A zero lock timeout would make doctor fail fast if it ever regressed into
	// acquiring the (held) lock; doctor must ignore it and succeed regardless.
	git(t, repo, "config", "kura.sealLockTimeoutMs", "0")
	result := cli.gitKura(repo, "seal", "doctor")
	requireExitCode(t, result, 0)
	requireEmptyStdout(t, result)
	requireEmptyStderr(t, result)
	if got := readFileString(t, storeFile); got != before {
		t.Fatalf("doctor mutated store: before %q after %q", before, got)
	}
	if got := readFileString(t, lockFile); got != "held" {
		t.Fatalf("doctor mutated lock file: got %q", got)
	}
}

func TestSealDoctorUsageErrorsExitCode2(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	for _, args := range [][]string{
		{"seal", "doctor", "key1"},
		{"seal", "doctor", "--fix"},
	} {
		result := cli.gitKura(repo, args...)
		requireExitCode(t, result, int(exitUsageError))
		requireEmptyStdout(t, result)
	}
}

func TestSealDoctorHelpFlag(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "doctor", "--help")
	requireExitCode(t, result, 0)
	if !strings.Contains(result.stdout, "Usage: git kura seal doctor") {
		t.Fatalf("help output = %s, want usage line", result.stdout)
	}
}

func TestSealClaimHelpFlag(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "claim", "--help")
	requireExitCode(t, result, 0)
	if !strings.Contains(result.stdout, "managed worktree") {
		t.Fatalf("help output should describe worktree-derived key resolution: %s", result.stdout)
	}
}

func TestSealUnclaimHelpFlag(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "unclaim", "--help")
	requireExitCode(t, result, 0)
	if !strings.Contains(result.stdout, "managed worktree") {
		t.Fatalf("help output should describe worktree-derived key resolution: %s", result.stdout)
	}
}

// --- close releases seals (issue #43) ---

// requireSealClaimed asserts that "<key>\t<path>" appears in `seal ls`.
func requireSealClaimed(t *testing.T, cli *testCLI, repo, key, path string) {
	t.Helper()
	res := cli.gitKura(repo, "seal", "ls", key)
	requireExitCode(t, res, 0)
	requireStdoutContainsLine(t, res, key+"\t"+path)
}

// requireNoSeals asserts that `seal ls <key>` lists nothing.
func requireNoSeals(t *testing.T, cli *testCLI, repo, key string) {
	t.Helper()
	res := cli.gitKura(repo, "seal", "ls", key)
	requireExitCode(t, res, 0)
	if strings.TrimSpace(res.stdout) != "" {
		t.Fatalf("seal ls %s = %q, want no claims", key, res.stdout)
	}
}

func TestCloseReleasesOnlyTargetKeySeals(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	commitFile(t, repo, "file2.txt", "two\n")
	commitFile(t, repo, "file3.txt", "three\n")

	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")

	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt", "file2.txt"), 0)
	requireExitCode(t, cli.gitKura(wt2, "seal", "claim", "file3.txt"), 0)

	// key2 cannot claim key1's paths yet.
	conflict := cli.gitKura(wt2, "seal", "claim", "tracked.txt")
	requireExitCode(t, conflict, int(exitSealConflict))

	requireExitCode(t, cli.gitKura(repo, "close", "key1"), 0)

	// key1's claims are gone; key2's claim survives.
	requireNoSeals(t, cli, repo, "key1")
	requireSealClaimed(t, cli, repo, "key2", "file3.txt")

	// The paths key1 released can now be claimed by key2.
	requireExitCode(t, cli.gitKura(wt2, "seal", "claim", "tracked.txt", "file2.txt"), 0)
}

func TestCloseWithoutSealsSucceeds(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	cli.openWorktree(t, repo, "key1")

	requireExitCode(t, cli.gitKura(repo, "close", "key1"), 0)
	assertPathMissing(t, expectedWorktreePath(repo, "key1"))
	assertPathMissing(t, expectedMetadataPath(repo, "key1"))
}

func TestCloseWorktreeCleanupFailureKeepsSeals(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix git worktree behavior")
	}
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := cli.openWorktree(t, repo, "key1")
	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)

	// A dirty worktree makes `git worktree remove` fail, so close must abort
	// before releasing the key's seals.
	appendFile(t, filepath.Join(wt1, "tracked.txt"), "dirty\n")

	requireNonZeroExitCode(t, cli.gitKura(repo, "close", "key1"))
	requireSealClaimed(t, cli, repo, "key1", "tracked.txt")
	assertPathExists(t, expectedWorktreePath(repo, "key1"))
}

func TestCloseBranchCleanupFailureKeepsSeals(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix git worktree behavior")
	}
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := cli.openWorktree(t, repo, "key1")
	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)

	// An unmerged commit leaves the worktree clean (so it is removed) but makes
	// `git branch -d` fail, so close must abort before releasing the seals.
	writeFile(t, filepath.Join(wt1, "newfile.txt"), "content\n")
	git(t, wt1, "add", "newfile.txt")
	git(t, wt1, "commit", "-m", "unmerged commit")

	requireNonZeroExitCode(t, cli.gitKura(repo, "close", "key1"))
	requireSealClaimed(t, cli, repo, "key1", "tracked.txt")
}

func TestCloseReleasesSealsWhenWorktreeDirGone(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := cli.openWorktree(t, repo, "key1")
	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)

	// Simulate a manual deletion of the worktree directory: the seal, branch,
	// and metadata are still present.
	if err := os.RemoveAll(wt1); err != nil {
		t.Fatal(err)
	}

	requireExitCode(t, cli.gitKura(repo, "close", "key1"), 0)
	requireNoSeals(t, cli, repo, "key1")
	assertPathMissing(t, expectedMetadataPath(repo, "key1"))
}

func TestCloseReleasesSealsWhenWorktreeAndBranchGone(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := cli.openWorktree(t, repo, "key1")
	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)

	// Both the worktree directory and the managed branch are gone; only the
	// seal and metadata remain.
	if err := os.RemoveAll(wt1); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "worktree", "prune")
	git(t, repo, "branch", "-D", "key1")

	requireExitCode(t, cli.gitKura(repo, "close", "key1"), 0)
	requireNoSeals(t, cli, repo, "key1")
	assertPathMissing(t, expectedMetadataPath(repo, "key1"))
}

func TestCloseRetriesMetadataOnlyState(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	cli.openWorktree(t, repo, "key1")

	// Remove the worktree and branch the normal way, but leave the git-kura
	// metadata behind to mimic a previous incomplete close.
	git(t, repo, "worktree", "remove", expectedWorktreePath(repo, "key1"))
	git(t, repo, "branch", "-D", "key1")
	assertPathExists(t, expectedMetadataPath(repo, "key1"))

	requireExitCode(t, cli.gitKura(repo, "close", "key1"), 0)
	assertPathMissing(t, expectedMetadataPath(repo, "key1"))
}

func TestCloseLockTimeoutChangesNothing(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	cli.openWorktree(t, repo, "key1")

	// Hold the seal store lock so close cannot acquire it.
	lockPath := sealLockFilePath(t, repo)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	defer func() { _ = os.Remove(lockPath) }()

	// 0 makes close fail immediately while the lock is held, keeping the test
	// fast without depending on the default 5s timeout.
	git(t, repo, "config", "kura.sealLockTimeoutMs", "0")
	result := cli.gitKura(repo, "close", "key1")

	requireExitCode(t, result, int(exitSealLockTimeout))
	requireStderrContains(t, result, "seal-lock-timeout:")

	// Nothing was torn down while the lock was held.
	assertPathExists(t, expectedWorktreePath(repo, "key1"))
	assertPathExists(t, expectedMetadataPath(repo, "key1"))
}

func TestCloseMalformedPathsJSONAborts(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	cli.openWorktree(t, repo, "key1")

	storeFile, _, err := seal.StorePaths(repo)
	if err != nil {
		t.Fatalf("pathsSealStore: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, storeFile, "{not json")

	requireNonZeroExitCode(t, cli.gitKura(repo, "close", "key1"))

	// The worktree, metadata, and the (malformed) store are all untouched.
	assertPathExists(t, expectedWorktreePath(repo, "key1"))
	assertPathExists(t, expectedMetadataPath(repo, "key1"))
	data, err := os.ReadFile(storeFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{not json" {
		t.Fatalf("paths.json = %q, want it left unchanged", string(data))
	}
}

func TestCloseMalformedPathsJSONAbortsWithEnvelope(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	cli.openWorktree(t, repo, "key1")

	storeFile, _, err := seal.StorePaths(repo)
	if err != nil {
		t.Fatalf("pathsSealStore: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, storeFile, "{not json")

	result := cli.gitKura(repo, "close", "key1", "--json")
	requireNonZeroExitCode(t, result)
	requireEmptyStderr(t, result)

	env := requireErrorEnvelope(t, result.stdout, "close")
	errObj := env["error"].(map[string]any)
	details, ok := errObj["details"].(map[string]any)
	if !ok {
		t.Fatalf("error.details missing or not an object: %v", errObj["details"])
	}
	if details["phase"] != "validate-store" {
		t.Fatalf("details.phase = %v, want validate-store", details["phase"])
	}
	storeErr, ok := details["storeError"].(map[string]any)
	if !ok {
		t.Fatalf("details.storeError missing or not an object: %v", details["storeError"])
	}
	if storeErr["status"] != "store-validation-error" {
		t.Fatalf("storeError.status = %v, want store-validation-error", storeErr["status"])
	}
}

func TestCloseAbsentPathsJSONContinues(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	cli.openWorktree(t, repo, "key1")

	storeFile, _, err := seal.StorePaths(repo)
	if err != nil {
		t.Fatalf("pathsSealStore: %v", err)
	}
	assertPathMissing(t, storeFile)

	requireExitCode(t, cli.gitKura(repo, "close", "key1"), 0)
	assertPathMissing(t, expectedWorktreePath(repo, "key1"))
	assertPathMissing(t, expectedMetadataPath(repo, "key1"))
}

// --- ls --json integration tests ---

func TestLsJSONEmptyWorktrees(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "ls", "--json")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "ls", lsDataSchema)
	keys, ok := data["keys"].([]any)
	if !ok {
		t.Fatalf("data.keys is not an array: %v", data["keys"])
	}
	if len(keys) != 0 {
		t.Fatalf("data.keys = %v, want empty array", keys)
	}
}

func TestLsJSONListsOpenWorktrees(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	requireExitCode(t, cli.gitKura(repo, "open", "62"), 0)

	result := cli.gitKura(repo, "ls", "--json")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "ls", lsDataSchema)
	keys, ok := data["keys"].([]any)
	if !ok {
		t.Fatalf("data.keys is not an array: %v", data["keys"])
	}
	if len(keys) != 2 {
		t.Fatalf("data.keys = %v, want 2 entries", keys)
	}
	if keys[0] != "51" || keys[1] != "62" {
		t.Fatalf("data.keys = %v, want [51, 62] (alphabetically sorted)", keys)
	}
}

func TestLsJSONKeysAreSorted(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "z-key"), 0)
	requireExitCode(t, cli.gitKura(repo, "open", "a-key"), 0)
	requireExitCode(t, cli.gitKura(repo, "open", "m-key"), 0)

	result := cli.gitKura(repo, "ls", "--json")
	requireExitCode(t, result, 0)

	data := requireSuccessEnvelopeData(t, result.stdout, "ls", lsDataSchema)
	keys, _ := data["keys"].([]any)
	if len(keys) != 3 || keys[0] != "a-key" || keys[1] != "m-key" || keys[2] != "z-key" {
		t.Fatalf("data.keys = %v, want [a-key, m-key, z-key]", keys)
	}
}

func TestLsJSONFailsOutsideRepo(t *testing.T) {
	cli := newTestCLI(t)
	outside := t.TempDir()

	result := cli.gitKura(outside, "ls", "--json")
	requireNonZeroExitCode(t, result)
	requireEmptyStderr(t, result)

	env := requireErrorEnvelope(t, result.stdout, "ls")
	errObj := env["error"].(map[string]any)
	if !strings.Contains(errObj["message"].(string), "repository") {
		t.Fatalf("error.message = %v, want it to mention the repository", errObj["message"])
	}
}

func TestLsJSONConformsToEnvelopeSchema(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)
	result := cli.gitKura(repo, "ls", "--json")
	requireExitCode(t, result, 0)
	requireConformsToEnvelopeSchema(t, result.stdout)
}

// --- seal ls --json integration tests ---

func TestSealLsJSONEmptyStore(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "ls", "--json")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "seal.ls", sealLsDataSchema)
	if data["filterKey"] != nil {
		t.Fatalf("filterKey = %v, want null", data["filterKey"])
	}
	claims, ok := data["claims"].([]any)
	if !ok {
		t.Fatalf("claims is not an array: %v", data["claims"])
	}
	if len(claims) != 0 {
		t.Fatalf("claims = %v, want empty", claims)
	}
}

func TestSealLsJSONProjectWide(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	commitFile(t, repo, "second.txt", "content\n")
	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")

	requireExitCode(t, cli.gitKura(wt2, "seal", "claim", "second.txt"), 0)
	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)

	result := cli.gitKura(repo, "seal", "ls", "--json")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "seal.ls", sealLsDataSchema)
	if data["filterKey"] != nil {
		t.Fatalf("filterKey = %v, want null", data["filterKey"])
	}
	claims, ok := data["claims"].([]any)
	if !ok || len(claims) != 2 {
		t.Fatalf("claims = %v, want 2 entries", data["claims"])
	}
	c0 := claims[0].(map[string]any)
	c1 := claims[1].(map[string]any)
	if c0["key"] != "key1" || c0["path"] != "tracked.txt" {
		t.Fatalf("claims[0] = %v, want {key:key1, path:tracked.txt}", c0)
	}
	if c1["key"] != "key2" || c1["path"] != "second.txt" {
		t.Fatalf("claims[1] = %v, want {key:key2, path:second.txt}", c1)
	}
}

func TestSealLsJSONKeyFiltered(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	commitFile(t, repo, "second.txt", "content\n")
	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")

	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)
	requireExitCode(t, cli.gitKura(wt2, "seal", "claim", "second.txt"), 0)

	result := cli.gitKura(repo, "seal", "ls", "--json", "key2")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "seal.ls", sealLsDataSchema)
	if data["filterKey"] != "key2" {
		t.Fatalf("filterKey = %v, want key2", data["filterKey"])
	}
	claims, ok := data["claims"].([]any)
	if !ok || len(claims) != 1 {
		t.Fatalf("claims = %v, want 1 entry", data["claims"])
	}
	c := claims[0].(map[string]any)
	if c["key"] != "key2" || c["path"] != "second.txt" {
		t.Fatalf("claims[0] = %v, want {key:key2, path:second.txt}", c)
	}
}

func TestSealLsJSONKeyFilteredEmpty(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := cli.openWorktree(t, repo, "key1")

	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)

	result := cli.gitKura(repo, "seal", "ls", "--json", "key2")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "seal.ls", sealLsDataSchema)
	if data["filterKey"] != "key2" {
		t.Fatalf("filterKey = %v, want key2", data["filterKey"])
	}
	claims, ok := data["claims"].([]any)
	if !ok || len(claims) != 0 {
		t.Fatalf("claims = %v, want empty", data["claims"])
	}
}

func TestSealLsJSONKeyBeforeJSONIsUsageError(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "ls", "key1", "--json")
	requireExitCode(t, result, int(exitUsageError))
	// Must not be a JSON envelope — invalid position is a plain usage error.
	requireEmptyStdout(t, result)
	if result.stderr == "" {
		t.Fatal("stderr = empty, want a usage error message")
	}
}

func TestSealLsJSONFailsOutsideRepo(t *testing.T) {
	cli := newTestCLI(t)
	outside := t.TempDir()

	result := cli.gitKura(outside, "seal", "ls", "--json")
	requireNonZeroExitCode(t, result)
	requireEmptyStderr(t, result)

	env := requireErrorEnvelope(t, result.stdout, "seal.ls")
	errObj := env["error"].(map[string]any)
	if !strings.Contains(errObj["message"].(string), "repository") {
		t.Fatalf("error.message = %v, want it to mention the repository", errObj["message"])
	}
}

func TestSealLsJSONConformsToEnvelopeSchema(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")
	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "tracked.txt"), 0)

	result := cli.gitKura(repo, "seal", "ls", "--json")
	requireExitCode(t, result, 0)
	requireConformsToEnvelopeSchema(t, result.stdout)
}

// --- seal test --json integration tests ---

func TestSealTestJSONAllPassed(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "tracked.txt"), 0)

	// tracked.txt claimed by current key, new-file.txt missing: both safe.
	result := cli.gitKura(wt, "seal", "test", "--json", "tracked.txt", "new-file.txt")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "seal.test", sealTestDataSchema)
	if data["passed"] != true {
		t.Fatalf("passed = %v, want true", data["passed"])
	}
	if data["currentKey"] != "key1" {
		t.Fatalf("currentKey = %v, want key1", data["currentKey"])
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("results = %v, want 2 entries", data["results"])
	}
	r0 := results[0].(map[string]any)
	r1 := results[1].(map[string]any)
	if r0["status"] != "claimed-by-current-key" {
		t.Fatalf("results[0].status = %v, want claimed-by-current-key", r0["status"])
	}
	if r0["safe"] != true {
		t.Fatalf("results[0].safe = %v, want true", r0["safe"])
	}
	if r0["claimedBy"] != "key1" {
		t.Fatalf("results[0].claimedBy = %v, want key1", r0["claimedBy"])
	}
	if r1["status"] != "missing-path" {
		t.Fatalf("results[1].status = %v, want missing-path", r1["status"])
	}
	if r1["safe"] != true {
		t.Fatalf("results[1].safe = %v, want true", r1["safe"])
	}
	if r1["claimedBy"] != nil {
		t.Fatalf("results[1].claimedBy = %v, want null", r1["claimedBy"])
	}
}

func TestSealTestJSONConflictByOtherKey(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")

	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "tracked.txt"), 0)

	result := cli.gitKura(wt2, "seal", "test", "--json", "tracked.txt")
	// Conflict is a business result: ok:true but exit 6.
	requireExitCode(t, result, int(exitSealConflict))
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "seal.test", sealTestDataSchema)
	if data["passed"] != false {
		t.Fatalf("passed = %v, want false", data["passed"])
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %v, want 1 entry", data["results"])
	}
	r0 := results[0].(map[string]any)
	if r0["status"] != "claimed-by-other-key" {
		t.Fatalf("results[0].status = %v, want claimed-by-other-key", r0["status"])
	}
	if r0["safe"] != false {
		t.Fatalf("results[0].safe = %v, want false", r0["safe"])
	}
	if r0["claimedBy"] != "key1" {
		t.Fatalf("results[0].claimedBy = %v, want key1", r0["claimedBy"])
	}
}

func TestSealTestJSONUnclaimed(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	result := cli.gitKura(wt, "seal", "test", "--json", "tracked.txt")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "seal.test", sealTestDataSchema)
	results, ok := data["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %v, want 1 entry", data["results"])
	}
	r0 := results[0].(map[string]any)
	if r0["status"] != "unclaimed" {
		t.Fatalf("results[0].status = %v, want unclaimed", r0["status"])
	}
	if r0["safe"] != true {
		t.Fatalf("results[0].safe = %v, want true", r0["safe"])
	}
	if r0["claimedBy"] != nil {
		t.Fatalf("results[0].claimedBy = %v, want null", r0["claimedBy"])
	}
}

func TestSealTestJSONMissingPath(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	result := cli.gitKura(wt, "seal", "test", "--json", "new-file.txt")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "seal.test", sealTestDataSchema)
	results, ok := data["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %v, want 1 entry", data["results"])
	}
	r0 := results[0].(map[string]any)
	if r0["status"] != "missing-path" {
		t.Fatalf("results[0].status = %v, want missing-path", r0["status"])
	}
	if r0["safe"] != true {
		t.Fatalf("results[0].safe = %v, want true", r0["safe"])
	}
}

func TestSealTestJSONInvalidPath(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	// An absolute path is an input contract violation: ok:false.
	result := cli.gitKura(wt, "seal", "test", "--json", "../outside.txt")
	requireNonZeroExitCode(t, result)
	requireEmptyStderr(t, result)
	requireErrorEnvelope(t, result.stdout, "seal.test")
}

func TestSealTestJSONCurrentKeyUnresolved(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	// The main checkout is not a managed worktree: current key unresolved.
	result := cli.gitKura(repo, "seal", "test", "--json", "tracked.txt")
	requireNonZeroExitCode(t, result)
	requireEmptyStderr(t, result)

	env := requireErrorEnvelope(t, result.stdout, "seal.test")
	errObj := env["error"].(map[string]any)
	if errObj["code"] != "current-key-unresolved" {
		t.Fatalf("error.code = %v, want current-key-unresolved", errObj["code"])
	}
	details, ok := errObj["details"].(map[string]any)
	if !ok {
		t.Fatalf("error.details is not an object: %v", errObj["details"])
	}
	reason, _ := details["reason"].(string)
	if reason == "" {
		t.Fatalf("error.details.reason is empty")
	}
}

func TestSealTestJSONPreservesResultOrder(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	commitFile(t, repo, "second.txt", "content\n")
	commitFile(t, repo, "third.txt", "content\n")
	wt1 := cli.openWorktree(t, repo, "key1")
	wt2 := cli.openWorktree(t, repo, "key2")

	requireExitCode(t, cli.gitKura(wt1, "seal", "claim", "third.txt"), 0)

	result := cli.gitKura(wt2, "seal", "test", "--json", "tracked.txt", "second.txt", "third.txt")
	requireExitCode(t, result, int(exitSealConflict))

	data := requireSuccessEnvelopeData(t, result.stdout, "seal.test", sealTestDataSchema)
	results, ok := data["results"].([]any)
	if !ok || len(results) != 3 {
		t.Fatalf("results = %v, want 3 entries", data["results"])
	}
	// Order must match input order.
	paths := []string{
		results[0].(map[string]any)["path"].(string),
		results[1].(map[string]any)["path"].(string),
		results[2].(map[string]any)["path"].(string),
	}
	if paths[0] != "tracked.txt" || paths[1] != "second.txt" || paths[2] != "third.txt" {
		t.Fatalf("result order = %v, want [tracked.txt second.txt third.txt]", paths)
	}
}

func TestSealTestJSONConformsToEnvelopeSchema(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")

	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "tracked.txt"), 0)

	result := cli.gitKura(wt, "seal", "test", "--json", "tracked.txt")
	requireExitCode(t, result, 0)
	requireConformsToEnvelopeSchema(t, result.stdout)
}

// --- seal doctor --json integration tests ---

func TestSealDoctorJSONHealthy(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "key1")
	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "tracked.txt"), 0)

	result := cli.gitKura(repo, "seal", "doctor", "--json")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "seal.doctor", sealDoctorDataSchema)
	if data["healthy"] != true {
		t.Fatalf("healthy = %v, want true", data["healthy"])
	}
	findings, ok := data["findings"].([]any)
	if !ok || len(findings) != 0 {
		t.Fatalf("findings = %v, want empty", data["findings"])
	}
	summary, ok := data["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary is not an object: %v", data["summary"])
	}
	if summary["checkedClaims"] != float64(1) {
		t.Fatalf("summary.checkedClaims = %v, want 1", summary["checkedClaims"])
	}
}

func TestSealDoctorJSONIntegrityFinding(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	// A non-normalized path is an integrity violation in the store.
	seedSealStore(t, repo, map[string]seal.Entry{
		"src/./a.go": {Key: "key1"},
	})

	result := cli.gitKura(repo, "seal", "doctor", "--json")
	// Integrity finding: ok:true but exit 7 and data.healthy:false.
	requireExitCode(t, result, int(exitSealDoctorError))
	requireEmptyStderr(t, result)

	data := requireSuccessEnvelopeData(t, result.stdout, "seal.doctor", sealDoctorDataSchema)
	if data["healthy"] != false {
		t.Fatalf("healthy = %v, want false", data["healthy"])
	}
	findings, ok := data["findings"].([]any)
	if !ok || len(findings) == 0 {
		t.Fatalf("findings = %v, want at least 1 entry", data["findings"])
	}
	f0 := findings[0].(map[string]any)
	if f0["severity"] != "error" {
		t.Fatalf("findings[0].severity = %v, want error", f0["severity"])
	}
	if f0["code"] == "" {
		t.Fatalf("findings[0].code is empty")
	}
	if f0["message"] == "" {
		t.Fatalf("findings[0].message is empty")
	}
}

func TestSealDoctorJSONMalformedStore(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	storeFile, _, err := seal.StorePaths(repo)
	if err != nil {
		t.Fatalf("pathsSealStore: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storeFile, []byte(`{invalid`), 0o644); err != nil {
		t.Fatal(err)
	}

	result := cli.gitKura(repo, "seal", "doctor", "--json")
	requireExitCode(t, result, int(exitSealDoctorError))
	requireEmptyStderr(t, result)

	env := requireErrorEnvelope(t, result.stdout, "seal.doctor")
	errObj := env["error"].(map[string]any)
	if errObj["code"] != "seal-doctor-error" {
		t.Fatalf("error.code = %v, want seal-doctor-error", errObj["code"])
	}
}

func TestSealDoctorJSONConformsToEnvelopeSchema(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "doctor", "--json")
	requireExitCode(t, result, 0)
	requireConformsToEnvelopeSchema(t, result.stdout)
}

// --- TOON smoke tests for commands with --toon support ---
// These verify that each command's --toon flag routes through the TOON renderer
// and produces a recognisable TOON envelope on stdout with an empty stderr.

func TestOpenTOON(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "open", "51", "--toon")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	for _, want := range []string{"ok", "command", "schemaVersion"} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("toon output missing field %q\noutput: %s", want, result.stdout)
		}
	}
}

func TestCloseTOON(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)

	result := cli.gitKura(repo, "close", "51", "--toon")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	for _, want := range []string{"ok", "command", "schemaVersion"} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("toon output missing field %q\noutput: %s", want, result.stdout)
		}
	}
}

func TestLsTOON(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	requireExitCode(t, cli.gitKura(repo, "open", "51"), 0)

	result := cli.gitKura(repo, "ls", "--toon")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	for _, want := range []string{"ok", "command", "schemaVersion"} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("toon output missing field %q\noutput: %s", want, result.stdout)
		}
	}
}

func TestSealLsTOON(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "ls", "--toon")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	for _, want := range []string{"ok", "command", "schemaVersion"} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("toon output missing field %q\noutput: %s", want, result.stdout)
		}
	}
}

func TestSealClaimTOON(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "51")

	result := cli.gitKura(wt, "seal", "claim", "--toon", "tracked.txt")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	for _, want := range []string{"ok", "command", "schemaVersion"} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("toon output missing field %q\noutput: %s", want, result.stdout)
		}
	}
}

func TestSealUnclaimTOON(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "51")
	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "tracked.txt"), 0)

	result := cli.gitKura(wt, "seal", "unclaim", "--toon", "tracked.txt")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	for _, want := range []string{"ok", "command", "schemaVersion"} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("toon output missing field %q\noutput: %s", want, result.stdout)
		}
	}
}

func TestSealTestTOON(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := cli.openWorktree(t, repo, "51")
	requireExitCode(t, cli.gitKura(wt, "seal", "claim", "tracked.txt"), 0)

	result := cli.gitKura(wt, "seal", "test", "--toon", "tracked.txt")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	for _, want := range []string{"ok", "command", "schemaVersion"} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("toon output missing field %q\noutput: %s", want, result.stdout)
		}
	}
}

func TestSealDoctorTOON(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	result := cli.gitKura(repo, "seal", "doctor", "--toon")
	requireExitCode(t, result, 0)
	requireEmptyStderr(t, result)

	for _, want := range []string{"ok", "command", "schemaVersion"} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("toon output missing field %q\noutput: %s", want, result.stdout)
		}
	}
}
