package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// Unit tests cover pure parsing and validation helpers without creating Git
// repositories or invoking the compiled binary.

func TestValidateKeyAcceptsValidKeys(t *testing.T) {
	for _, key := range []string{
		"51",
		"051",
		"ABC-123",
		"abc-123",
		"task-51",
		"bugfix_login",
		"release-2026-06",
		"a",
		"Z",
		"0",
	} {
		t.Run(key, func(t *testing.T) {
			if err := validateKey(key); err != nil {
				t.Fatalf("validateKey(%q) = %v, want nil", key, err)
			}
		})
	}
}

func TestValidateKeyRejectsInvalidKeys(t *testing.T) {
	for _, key := range []string{
		"../x",
		".git",
		"feature..x",
		"feature.lock",
		"feature.",
		"a/b",
		"a b",
		"",
		".hidden",
		"@{upstream}",
	} {
		t.Run(printableName(key), func(t *testing.T) {
			if err := validateKey(key); err == nil {
				t.Fatalf("validateKey(%q) = nil, want error", key)
			}
		})
	}
}

func TestParseGetArgs(t *testing.T) {
	t.Run("default output mode is path", func(t *testing.T) {
		key, opts, err := parseGetArgs([]string{"51"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "51" {
			t.Fatalf("key = %q, want %q", key, "51")
		}
		if opts.OutputMode != outputPath {
			t.Fatalf("OutputMode = %q, want %q", opts.OutputMode, outputPath)
		}
	})

	t.Run("--path produces path output mode", func(t *testing.T) {
		_, opts, err := parseGetArgs([]string{"51", "--path"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.OutputMode != outputPath {
			t.Fatalf("OutputMode = %q, want %q", opts.OutputMode, outputPath)
		}
	})

	t.Run("--branch produces branch output mode", func(t *testing.T) {
		_, opts, err := parseGetArgs([]string{"51", "--branch"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.OutputMode != outputBranch {
			t.Fatalf("OutputMode = %q, want %q", opts.OutputMode, outputBranch)
		}
	})

	t.Run("--json and --format json produce same output mode", func(t *testing.T) {
		_, shortOpts, err := parseGetArgs([]string{"51", "--json"})
		if err != nil {
			t.Fatalf("--json: unexpected error: %v", err)
		}
		_, formatOpts, err := parseGetArgs([]string{"51", "--format", "json"})
		if err != nil {
			t.Fatalf("--format json: unexpected error: %v", err)
		}
		if shortOpts.OutputMode != formatOpts.OutputMode {
			t.Fatalf("--json mode %q != --format json mode %q", shortOpts.OutputMode, formatOpts.OutputMode)
		}
		if shortOpts.OutputMode != outputJSON {
			t.Fatalf("OutputMode = %q, want %q", shortOpts.OutputMode, outputJSON)
		}
	})

	t.Run("--root produces root output mode", func(t *testing.T) {
		_, opts, err := parseGetArgs([]string{"51", "--root"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.OutputMode != outputRoot {
			t.Fatalf("OutputMode = %q, want %q", opts.OutputMode, outputRoot)
		}
	})

	t.Run("--toon and --format toon produce same output mode", func(t *testing.T) {
		_, shortOpts, err := parseGetArgs([]string{"51", "--toon"})
		if err != nil {
			t.Fatalf("--toon: unexpected error: %v", err)
		}
		_, formatOpts, err := parseGetArgs([]string{"51", "--format", "toon"})
		if err != nil {
			t.Fatalf("--format toon: unexpected error: %v", err)
		}
		if shortOpts.OutputMode != formatOpts.OutputMode {
			t.Fatalf("--toon mode %q != --format toon mode %q", shortOpts.OutputMode, formatOpts.OutputMode)
		}
		if shortOpts.OutputMode != outputTOON {
			t.Fatalf("OutputMode = %q, want %q", shortOpts.OutputMode, outputTOON)
		}
	})

	t.Run("unknown format is error", func(t *testing.T) {
		_, _, err := parseGetArgs([]string{"51", "--format", "xml"})
		if err == nil {
			t.Fatal("expected error for unknown format, got nil")
		}
		for _, want := range []string{"format", "json", "toon"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q does not mention %q", err.Error(), want)
			}
		}
	})

	t.Run("missing format value is error", func(t *testing.T) {
		_, _, err := parseGetArgs([]string{"51", "--format"})
		if err == nil {
			t.Fatal("expected error for missing format value, got nil")
		}
		if !strings.Contains(err.Error(), "--format") {
			t.Fatalf("error %q does not mention --format", err.Error())
		}
	})

	for _, args := range [][]string{
		{"51", "--path", "--path"},
		{"51", "--path", "--branch"},
		{"51", "--path", "--root"},
		{"51", "--root", "--path"},
		{"51", "--root", "--branch"},
		{"51", "--root", "--json"},
		{"51", "--root", "--toon"},
		{"51", "--json", "--toon"},
		{"51", "--path", "--json"},
		{"51", "--branch", "--json"},
		{"51", "--path", "--format", "json"},
		{"51", "--branch", "--format", "toon"},
	} {
		args := args
		t.Run("conflict: "+strings.Join(args[1:], " "), func(t *testing.T) {
			_, _, err := parseGetArgs(args)
			if err == nil {
				t.Fatal("expected conflict error, got nil")
			}
			if !strings.Contains(strings.ToLower(err.Error()), "conflict") {
				t.Fatalf("error %q does not mention 'conflict'", err.Error())
			}
		})
	}

	t.Run("no key is usage error", func(t *testing.T) {
		_, _, err := parseGetArgs([]string{})
		if err == nil {
			t.Fatal("expected error for missing key, got nil")
		}
	})

	t.Run("unknown flag is error", func(t *testing.T) {
		_, _, err := parseGetArgs([]string{"51", "--unknown"})
		if err == nil {
			t.Fatal("expected error for unknown flag, got nil")
		}
	})

	t.Run("invalid key is error", func(t *testing.T) {
		_, _, err := parseGetArgs([]string{"../x"})
		if err == nil {
			t.Fatal("expected error for invalid key, got nil")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "key") {
			t.Fatalf("error %q does not mention 'key'", err.Error())
		}
	})
}

func TestParseOpenArgs(t *testing.T) {
	t.Run("valid key succeeds", func(t *testing.T) {
		key, opts, err := parseOpenArgs([]string{"51"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "51" {
			t.Fatalf("key = %q, want 51", key)
		}
		if opts.DryRun {
			t.Fatal("DryRun = true, want false")
		}
	})

	t.Run("dry-run flag", func(t *testing.T) {
		key, opts, err := parseOpenArgs([]string{"51", "--dry-run"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "51" {
			t.Fatalf("key = %q, want 51", key)
		}
		if !opts.DryRun {
			t.Fatal("DryRun = false, want true")
		}
	})

	t.Run("dry-run and json in either order", func(t *testing.T) {
		for _, args := range [][]string{
			{"51", "--dry-run", "--json"},
			{"51", "--json", "--dry-run"},
		} {
			_, opts, err := parseOpenArgs(args)
			if err != nil {
				t.Fatalf("parseOpenArgs(%v) unexpected error: %v", args, err)
			}
			if !opts.DryRun || !opts.JSON {
				t.Fatalf("parseOpenArgs(%v) = %+v, want DryRun and JSON true", args, opts)
			}
		}
	})

	t.Run("json without dry-run is usage error", func(t *testing.T) {
		_, _, err := parseOpenArgs([]string{"51", "--json"})
		if err == nil {
			t.Fatal("expected error for --json without --dry-run, got nil")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "dry-run") {
			t.Fatalf("error %q does not mention --dry-run", err.Error())
		}
	})

	t.Run("no key is usage error", func(t *testing.T) {
		_, _, err := parseOpenArgs([]string{})
		if err == nil {
			t.Fatal("expected error for missing key, got nil")
		}
	})

	t.Run("extra argument is error", func(t *testing.T) {
		_, _, err := parseOpenArgs([]string{"51", "--extra"})
		if err == nil {
			t.Fatal("expected error for extra argument, got nil")
		}
	})

	t.Run("invalid key is error", func(t *testing.T) {
		_, _, err := parseOpenArgs([]string{"../x"})
		if err == nil {
			t.Fatal("expected error for invalid key, got nil")
		}
	})
}

func TestParseKeyOnlyArgs(t *testing.T) {
	t.Run("valid key succeeds", func(t *testing.T) {
		key, err := parseKeyOnlyArgs("open", []string{"51"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "51" {
			t.Fatalf("key = %q, want %q", key, "51")
		}
	})

	t.Run("no key is usage error", func(t *testing.T) {
		_, err := parseKeyOnlyArgs("open", []string{})
		if err == nil {
			t.Fatal("expected error for missing key, got nil")
		}
	})

	t.Run("extra argument is error", func(t *testing.T) {
		_, err := parseKeyOnlyArgs("open", []string{"51", "--extra"})
		if err == nil {
			t.Fatal("expected error for extra argument, got nil")
		}
	})

	t.Run("invalid key is error", func(t *testing.T) {
		_, err := parseKeyOnlyArgs("open", []string{"../x"})
		if err == nil {
			t.Fatal("expected error for invalid key, got nil")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "key") {
			t.Fatalf("error %q does not mention 'key'", err.Error())
		}
	})
}

func TestValidateDataRejectsInvalidData(t *testing.T) {
	// A zero worktreeJSON violates the get data schema (empty required strings),
	// so validateData must reject it before it is wrapped in an envelope.
	if err := validateData(getDataSchema, worktreeJSON{}); err == nil {
		t.Fatal("validateData invalid data error = nil, want error")
	}
}

func TestValidateDataRejectsUnmarshalableData(t *testing.T) {
	// A channel cannot be marshaled to JSON, so validateData must surface the
	// marshal failure rather than panic or produce a malformed payload.
	if err := validateData(getDataSchema, make(chan int)); err == nil {
		t.Fatal("validateData unmarshalable data error = nil, want error")
	}
}

func TestValidateDataAcceptsValidData(t *testing.T) {
	data := worktreeJSON{
		SchemaVersion:  1,
		Key:            "51",
		Kind:           "worktree",
		Branch:         "51",
		WorktreePath:   "/repo/.git/kura/worktrees/51",
		RepositoryRoot: "/repo",
		BaseBranch:     "main",
		Exists:         true,
		Dirty:          false,
	}
	if err := validateData(getDataSchema, data); err != nil {
		t.Fatalf("validateData valid data error = %v, want nil", err)
	}
	// The same shape is valid against the dry-run schema when exists/dirty are
	// false, which a dry run guarantees.
	data.Exists = false
	if err := validateData(openDryRunDataSchema, data); err != nil {
		t.Fatalf("validateData dry-run data error = %v, want nil", err)
	}
}

func TestRenderedErrorErrorIsEmpty(t *testing.T) {
	// The sentinel carries only an exit code; its message is intentionally empty
	// so main never prints it after the renderer has already written the failure.
	if got := (&renderedError{code: exitUnsafeRefused}).Error(); got != "" {
		t.Fatalf("renderedError.Error() = %q, want empty", got)
	}
}

func TestExitErrorUnwrap(t *testing.T) {
	inner := fmt.Errorf("inner")
	xe := &exitError{code: exitNotFound, err: inner}
	if !errors.Is(xe, inner) {
		t.Fatal("errors.Is(exitError, inner) = false, want true")
	}
}

func TestReasonForExitCode(t *testing.T) {
	cases := map[int]string{
		exitUsageError:      "usage-error",
		exitUnsafeRefused:   "unsafe-refused",
		exitNotFound:        "not-found",
		exitSealLockTimeout: "seal-lock-timeout",
		exitSealConflict:    "seal-conflict",
		exitSealDoctorError: "seal-doctor-error",
		exitGeneralError:    "general-error",
		9999:                "general-error",
	}
	for code, want := range cases {
		if got := reasonForExitCode(code); got != want {
			t.Fatalf("reasonForExitCode(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestToCommandError(t *testing.T) {
	t.Run("plain error is a general error", func(t *testing.T) {
		ce := toCommandError(commandGet, fmt.Errorf("boom"))
		if ce.Code != "general-error" || ce.ExitCode != exitGeneralError {
			t.Fatalf("toCommandError plain = %+v, want general-error/%d", ce, exitGeneralError)
		}
		if ce.Message != "boom" || ce.Command != commandGet {
			t.Fatalf("toCommandError plain = %+v, want message boom, command get", ce)
		}
	})

	t.Run("exitError keeps its code and reason", func(t *testing.T) {
		ce := toCommandError(commandOpen, exitCodeError(exitNotFound, fmt.Errorf("missing")))
		if ce.Code != "not-found" || ce.ExitCode != exitNotFound {
			t.Fatalf("toCommandError exitError = %+v, want not-found/%d", ce, exitNotFound)
		}
	})
}

func TestPrintTOONFormat(t *testing.T) {
	data := worktreeJSON{
		SchemaVersion:  1,
		Key:            "test-51",
		Kind:           "worktree",
		Branch:         "test-51",
		WorktreePath:   "/repo/.git/kura/worktrees/test-51",
		RepositoryRoot: "/repo",
		BaseBranch:     "main",
		Exists:         true,
		Dirty:          false,
	}

	stdout, err := captureStdout(t, func() error { return printTOON(data) })
	if err != nil {
		t.Fatalf("printTOON error = %v", err)
	}

	for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		if !strings.Contains(line, ": ") {
			t.Errorf("line %q does not use ': ' separator", line)
		}
	}
}

func TestPrintTOONFields(t *testing.T) {
	data := worktreeJSON{
		SchemaVersion:  1,
		Key:            "test-51",
		Kind:           "worktree",
		Branch:         "test-51",
		WorktreePath:   "/repo/.git/kura/worktrees/test-51",
		RepositoryRoot: "/repo",
		BaseBranch:     "main",
		Exists:         true,
		Dirty:          false,
	}

	stdout, err := captureStdout(t, func() error { return printTOON(data) })
	if err != nil {
		t.Fatalf("printTOON error = %v", err)
	}

	for field, want := range map[string]string{
		"schemaVersion":  "schemaVersion: 1",
		"key":            "key: test-51",
		"kind":           "kind: worktree",
		"branch":         "branch: test-51",
		"worktreePath":   "worktreePath: /repo/.git/kura/worktrees/test-51",
		"repositoryRoot": "repositoryRoot: /repo",
		"baseBranch":     "baseBranch: main",
		"exists":         "exists: true",
		"dirty":          "dirty: false",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("field %q: stdout does not contain %q\nfull output:\n%s", field, want, stdout)
		}
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 9 {
		t.Errorf("line count = %d, want 9\nfull output:\n%s", len(lines), stdout)
	}
}

func TestRequireCleanValueStdoutAcceptsWindowsPath(t *testing.T) {
	requireCleanValueStdout(t, cliResult{stdout: `C:\repo.kura\worktrees\51` + "\n"})
}

// --- seal path store unit tests ---

func TestNormalizeSealPathAbsoluteRejected(t *testing.T) {
	root := t.TempDir()
	_, err := normalizeSealPath(root, filepath.Join(root, "src", "foo.go"))
	if err == nil {
		t.Fatal("expected error for absolute path, got nil")
	}
}

func TestNormalizeSealPathRootRelative(t *testing.T) {
	root := t.TempDir()
	path, err := normalizeSealPath(root, "src/foo.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != filepath.Join("src", "foo.go") {
		t.Fatalf("got %q, want %q", path, filepath.Join("src", "foo.go"))
	}
}

func TestNormalizeSealPathIgnoresWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Even when the caller's cwd is a subdirectory, the argument is resolved
	// against the repository root, not the cwd.
	withWorkingDir(t, sub, func() {
		path, err := normalizeSealPath(root, "src/foo.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if path != filepath.Join("src", "foo.go") {
			t.Fatalf("got %q, want %q", path, filepath.Join("src", "foo.go"))
		}
	})
}

func TestNormalizeSealPathEscapesRepo(t *testing.T) {
	root := t.TempDir()
	_, err := normalizeSealPath(root, "../escape.go")
	if err == nil {
		t.Fatal("expected error for path outside repo, got nil")
	}
}

func TestNormalizeSealPathDotDotPrefixInsideRepo(t *testing.T) {
	root := t.TempDir()
	// A file like "..foo/bar" starts with ".." but is inside the repo.
	path, err := normalizeSealPath(root, "..foo/bar")
	if err != nil {
		t.Fatalf("unexpected error for path inside repo starting with '..': %v", err)
	}
	if path != filepath.Join("..foo", "bar") {
		t.Fatalf("got %q, want %q", path, filepath.Join("..foo", "bar"))
	}
}

func TestNormalizeSealPathRepoRootItself(t *testing.T) {
	root := t.TempDir()
	path, err := normalizeSealPath(root, ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "." {
		t.Fatalf("got %q, want %q", path, ".")
	}
}

func TestReadSealStoreNotExist(t *testing.T) {
	store, err := readSealStore(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if len(store.Paths) != 0 {
		t.Fatalf("expected empty paths, got %v", store.Paths)
	}
}

func TestReadSealStoreCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "seals.json"), []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readSealStore(filepath.Join(dir, "seals.json"))
	if err == nil {
		t.Fatal("expected error for corrupt store, got nil")
	}
}

func TestReadSealStoreRejectsSchemaViolations(t *testing.T) {
	for name, content := range map[string]string{
		// valid JSON, but entries must be objects with a "key" field
		"bare string entry": `{"schemaVersion":1,"paths":{"src/a.go":"key1"}}`,
		// unsupported schema version
		"wrong schemaVersion": `{"schemaVersion":2,"paths":{}}`,
		// missing required fields
		"missing paths": `{"schemaVersion":1}`,
		// empty key violates minLength
		"empty key": `{"schemaVersion":1,"paths":{"src/a.go":{"key":""}}}`,
		// unknown top-level field violates additionalProperties
		"unknown field": `{"schemaVersion":1,"paths":{},"extra":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "paths.json")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := readSealStore(path); err == nil {
				t.Fatalf("expected schema validation error for %s, got nil", name)
			}
		})
	}
}

func TestDoctorSealStoreAcceptsMissingEmptyAndValidStores(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	storeFile, _, err := pathsSealStore(repo)
	if err != nil {
		t.Fatalf("pathsSealStore: %v", err)
	}

	if err := doctorSealStore(storeFile); err != nil {
		t.Fatalf("doctor missing store: %v", err)
	}
	if _, err := os.Stat(storeFile); !os.IsNotExist(err) {
		t.Fatalf("doctor should not create missing store (stat err: %v)", err)
	}

	if err := writeSealStore(storeFile, sealPathStore{Paths: map[string]sealEntry{}}); err != nil {
		t.Fatalf("write empty store: %v", err)
	}
	if err := doctorSealStore(storeFile); err != nil {
		t.Fatalf("doctor empty store: %v", err)
	}

	if err := writeSealStore(storeFile, sealPathStore{
		Paths: map[string]sealEntry{"src/a.go": {Key: "key1"}},
	}); err != nil {
		t.Fatalf("write valid store: %v", err)
	}
	if err := doctorSealStore(storeFile); err != nil {
		t.Fatalf("doctor valid store: %v", err)
	}
}

func TestDoctorSealStoreRejectsInvalidStoreStructure(t *testing.T) {
	for name, content := range map[string]string{
		"not json":            `{`,
		"wrong schemaVersion": `{"schemaVersion":2,"paths":{}}`,
		"missing paths":       `{"schemaVersion":1}`,
		"entry key missing":   `{"schemaVersion":1,"paths":{"src/a.go":{}}}`,
		"entry key empty":     `{"schemaVersion":1,"paths":{"src/a.go":{"key":""}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			cli := newTestCLI(t)
			repo := cli.initRepo(t)
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

			if err := doctorSealStore(storeFile); err == nil {
				t.Fatal("doctor invalid store error = nil, want error")
			}
		})
	}
}

func TestDoctorSealStoreRejectsInvalidStoredPaths(t *testing.T) {
	for name, paths := range map[string]map[string]sealEntry{
		"outside repo": {
			"../outside.txt": {Key: "key1"},
		},
		"backslash separator": {
			`src\a.go`: {Key: "key1"},
		},
		"not normalized": {
			"src/./a.go": {Key: "key1"},
		},
		"normalized duplicate": {
			"src/a.go":  {Key: "key1"},
			"src//a.go": {Key: "key1"},
			"src/other": {Key: "key1"},
		},
		"same canonical path different keys": {
			"src/a.go":  {Key: "key1"},
			"src//a.go": {Key: "key2"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			cli := newTestCLI(t)
			repo := cli.initRepo(t)
			storeFile := seedSealStore(t, repo, paths)

			err := doctorSealStore(storeFile)
			if err == nil {
				t.Fatal("doctor invalid path error = nil, want error")
			}
			for _, want := range []string{"src", "outside", `\`} {
				if strings.Contains(name, want) && !strings.Contains(err.Error(), want) {
					t.Fatalf("doctor error %q should mention %q", err.Error(), want)
				}
			}
		})
	}
}

// TestDoctorSealStoreReportsEveryViolation verifies that doctor does not stop at
// the first bad entry: a store with several independent problems must surface
// all of them so the user can fix them in one pass.
func TestDoctorSealStoreReportsEveryViolation(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	storeFile := seedSealStore(t, repo, map[string]sealEntry{
		`bad\sep.go`:    {Key: "key1"}, // backslash separator
		"../escape.txt": {Key: "key1"}, // escapes the repository root
		"src/./b.go":    {Key: "key1"}, // not normalized
	})

	err := doctorSealStore(storeFile)
	if err == nil {
		t.Fatal("doctor error = nil, want error")
	}
	// Each substring uniquely identifies one of the seeded bad entries.
	for _, want := range []string{"sep.go", "escape.txt", "src/./b.go"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("doctor error %q should mention every violation, missing %q", err.Error(), want)
		}
	}
}

func TestCanonicalStoredSealPath(t *testing.T) {
	for _, tc := range []struct {
		name      string
		rawPath   string
		want      string
		wantError string
	}{
		{name: "valid", rawPath: "src/a.go", want: "src/a.go"},
		{name: "empty", rawPath: "", wantError: "empty path"},
		{name: "absolute", rawPath: "/tmp/a.go", wantError: "repository-relative"},
		{name: "escape", rawPath: "src/../../a.go", wantError: "escapes"},
		{name: "backslash", rawPath: `src\a.go`, wantError: "non-/"},
		{name: "canonical", rawPath: "src/./a.go", want: "src/a.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := canonicalStoredSealPath(tc.rawPath)
			if tc.wantError != "" {
				if err == nil {
					t.Fatalf("canonicalStoredSealPath(%q) error = nil, want %q", tc.rawPath, tc.wantError)
				}
				if !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("canonicalStoredSealPath(%q) error = %q, want it to contain %q", tc.rawPath, err.Error(), tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("canonicalStoredSealPath(%q) error = %v, want nil", tc.rawPath, err)
			}
			if got != tc.want {
				t.Fatalf("canonicalStoredSealPath(%q) = %q, want %q", tc.rawPath, got, tc.want)
			}
		})
	}
}

func TestCmdSealDoctorOutsideRepoFails(t *testing.T) {
	outside := t.TempDir()
	withWorkingDir(t, outside, func() {
		if err := cmdSealDoctor(); err == nil {
			t.Fatal("cmdSealDoctor outside repo error = nil, want error")
		}
	})
}

func TestCmdSealDoctorReturnsExitCode7ForIntegrityFailures(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	seedSealStore(t, repo, map[string]sealEntry{
		`src\a.go`: {Key: "key1"},
	})

	withWorkingDir(t, repo, func() {
		err := cmdSealDoctor()
		if err == nil {
			t.Fatal("cmdSealDoctor invalid store error = nil, want error")
		}
		var xe *exitError
		if !errors.As(err, &xe) || xe.code != exitSealDoctorError {
			t.Fatalf("cmdSealDoctor exit code = %v, want %d (err: %v)", xe, exitSealDoctorError, err)
		}
		if !strings.Contains(err.Error(), "seal-doctor-error:") {
			t.Fatalf("doctor error should include stable token: %v", err)
		}
	})
}

func TestWriteSealStoreRejectsSchemaViolations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paths.json")
	// An empty key violates the schema's minLength constraint; the write
	// must be refused and nothing persisted.
	err := writeSealStore(path, sealPathStore{
		Paths: map[string]sealEntry{"src/a.go": {Key: ""}},
	})
	if err == nil {
		t.Fatal("expected schema validation error, got nil")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("store file should not exist after refused write (stat err: %v)", statErr)
	}
}

func TestWriteSealStoreNormalizesNilPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paths.json")
	if err := writeSealStore(path, sealPathStore{}); err != nil {
		t.Fatalf("write with nil Paths should normalize to empty map: %v", err)
	}
	got, err := readSealStore(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Paths == nil || len(got.Paths) != 0 {
		t.Fatalf("expected empty paths map, got %+v", got.Paths)
	}
}

func TestReadSealStoreUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission tests are Unix-specific")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root: permission restrictions don't apply")
	}
	dir := t.TempDir()
	storePath := filepath.Join(dir, "seals.json")
	if err := os.WriteFile(storePath, []byte(`{"schemaVersion":1,"paths":{}}`), 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(storePath, 0o644) }()
	_, err := readSealStore(storePath)
	if err == nil {
		t.Fatal("expected error for unreadable file, got nil")
	}
}

func TestWriteReadSealStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paths.json")
	original := sealPathStore{
		Paths: map[string]sealEntry{
			"src/a.go":      {Key: "key1"},
			"internal/b.go": {Key: "key2"},
		},
	}
	if err := writeSealStore(path, original); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readSealStore(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got.Paths) != len(original.Paths) {
		t.Fatalf("round-trip length mismatch: got %d, want %d", len(got.Paths), len(original.Paths))
	}
	if got.Paths["src/a.go"].Key != "key1" || got.Paths["internal/b.go"].Key != "key2" {
		t.Fatalf("round-trip content mismatch: got %+v", got.Paths)
	}
	if got.SchemaVersion != sealPathSchemaVersion {
		t.Fatalf("schema version: got %d, want %d", got.SchemaVersion, sealPathSchemaVersion)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestWrittenSealStoreConformsToSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paths.json")
	store := sealPathStore{
		Paths: map[string]sealEntry{
			"src/a.go": {Key: "key1"},
		},
	}
	if err := writeSealStore(path, store); err != nil {
		t.Fatalf("write: %v", err)
	}

	schemaDoc, err := jsonschema.UnmarshalJSON(strings.NewReader(readFileString(t, filepath.Join("schema", "seal_store.schema.json"))))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("seal_store.schema.json", schemaDoc); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	schema, err := compiler.Compile("seal_store.schema.json")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(readFileString(t, path)))
	if err != nil {
		t.Fatalf("parse written store: %v", err)
	}
	if err := schema.Validate(inst); err != nil {
		t.Fatalf("written store does not conform to schema: %v", err)
	}
}

func TestWriteSealStoreMkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	// Create a file where MkdirAll expects to create a directory
	if err := os.WriteFile(filepath.Join(dir, "not-a-dir"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := writeSealStore(filepath.Join(dir, "not-a-dir", "paths.json"), sealPathStore{Paths: map[string]sealEntry{}})
	if err == nil {
		t.Fatal("expected error when MkdirAll cannot create dir, got nil")
	}
}

func TestPathsSealStoreOutsideRepo(t *testing.T) {
	_, _, err := pathsSealStore(t.TempDir())
	if err == nil {
		t.Fatal("pathsSealStore outside git repo: error = nil, want error")
	}
}

func TestExitCodeValuesMatchDocs(t *testing.T) {
	// Keep in sync with the exit code table in docs/commands.md.
	if exitSuccess != 0 || exitGeneralError != 1 || exitUsageError != 2 ||
		exitUnsafeRefused != 3 || exitNotFound != 4 ||
		exitSealLockTimeout != 5 || exitSealConflict != 6 ||
		exitSealDoctorError != 7 {
		t.Fatal("exit code constants must match the table in docs/commands.md")
	}
}

func TestExitCodeErrorNilPassthrough(t *testing.T) {
	if err := exitCodeError(exitSealConflict, nil); err != nil {
		t.Fatalf("exitCodeError(code, nil) = %v, want nil", err)
	}
	err := exitCodeError(exitSealConflict, errors.New("boom"))
	var xe *exitError
	if !errors.As(err, &xe) || xe.code != exitSealConflict {
		t.Fatalf("expected exitError with code %d, got: %v", exitSealConflict, err)
	}
}

func TestAcquireSealLockBasic(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "paths.lock")

	release, err := acquireSealLock(lockPath, defaultSealLockTimeout)
	if err != nil {
		t.Fatalf("acquireSealLock: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file should exist: %v", err)
	}
	release()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatal("lock file should be removed after release")
	}
}

func TestAcquireSealLockTimeout(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "paths.lock")

	// Hold the lock by creating the file manually.
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	defer func() { _ = os.Remove(lockPath) }()

	// Use a short timeout for the test.
	_, err = acquireSealLock(lockPath, 150*time.Millisecond)
	if err == nil {
		t.Fatal("expected lock-timeout error, got nil")
	}
	var xe *exitError
	if !errors.As(err, &xe) || xe.code != exitSealLockTimeout {
		t.Fatalf("expected exitError with code %d, got: %v", exitSealLockTimeout, err)
	}
	if !strings.Contains(err.Error(), "seal-lock-timeout:") {
		t.Fatalf("expected 'seal-lock-timeout:' prefix in error: %s", err.Error())
	}
}

// TestAcquireSealLockZeroTimeoutTriesOnce verifies the timeout=0 boundary:
// acquisition is attempted exactly once and, when the lock is already held,
// fails immediately with seal-lock-timeout rather than retrying.
func TestAcquireSealLockZeroTimeoutTriesOnce(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "paths.lock")

	// Hold the lock so the single attempt must fail.
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	defer func() { _ = os.Remove(lockPath) }()

	start := time.Now()
	_, err = acquireSealLock(lockPath, 0)
	if err == nil {
		t.Fatal("expected lock-timeout error, got nil")
	}
	// A single attempt must not sleep for a retry interval.
	if elapsed := time.Since(start); elapsed >= sealStoreLockInterval {
		t.Fatalf("zero timeout retried instead of failing immediately: took %s", elapsed)
	}
	var xe *exitError
	if !errors.As(err, &xe) || xe.code != exitSealLockTimeout {
		t.Fatalf("expected exitError with code %d, got: %v", exitSealLockTimeout, err)
	}
	if !strings.Contains(err.Error(), "seal-lock-timeout:") {
		t.Fatalf("expected 'seal-lock-timeout:' prefix in error: %s", err.Error())
	}
}

// TestAcquireSealLockZeroTimeoutSucceedsWhenFree verifies that a zero timeout
// still acquires the lock when it is free (the single attempt succeeds).
func TestAcquireSealLockZeroTimeoutSucceedsWhenFree(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "paths.lock")

	release, err := acquireSealLock(lockPath, 0)
	if err != nil {
		t.Fatalf("acquireSealLock with free lock and zero timeout: %v", err)
	}
	release()
}

func TestResolveSealLockTimeout(t *testing.T) {
	t.Run("unset uses default", func(t *testing.T) {
		repo := newTestCLI(t).initRepo(t)
		got, err := resolveSealLockTimeout(repo)
		if err != nil {
			t.Fatalf("resolveSealLockTimeout: %v", err)
		}
		if got != defaultSealLockTimeout {
			t.Fatalf("got %s, want default %s", got, defaultSealLockTimeout)
		}
	})

	t.Run("valid values", func(t *testing.T) {
		cases := map[string]time.Duration{
			"0":    0,
			"5000": 5000 * time.Millisecond,
			"1":    1 * time.Millisecond,
		}
		for value, want := range cases {
			repo := newTestCLI(t).initRepo(t)
			git(t, repo, "config", sealLockTimeoutConfigKey, value)
			got, err := resolveSealLockTimeout(repo)
			if err != nil {
				t.Fatalf("resolveSealLockTimeout(%q): %v", value, err)
			}
			if got != want {
				t.Fatalf("value %q: got %s, want %s", value, got, want)
			}
		}
	})

	t.Run("the cap itself is accepted", func(t *testing.T) {
		repo := newTestCLI(t).initRepo(t)
		git(t, repo, "config", sealLockTimeoutConfigKey, strconv.FormatInt(maxSealLockTimeoutMs, 10))
		got, err := resolveSealLockTimeout(repo)
		if err != nil {
			t.Fatalf("resolveSealLockTimeout at cap: %v", err)
		}
		if got != maxSealLockTimeout {
			t.Fatalf("got %s, want %s", got, maxSealLockTimeout)
		}
	})

	t.Run("values above the cap are rejected", func(t *testing.T) {
		for _, value := range []string{
			strconv.FormatInt(maxSealLockTimeoutMs+1, 10), // just over the cap
			"9223372036855",              // parses as int64 but overflows when multiplied
			"99999999999999999999999999", // too large to fit in int64
		} {
			repo := newTestCLI(t).initRepo(t)
			git(t, repo, "config", sealLockTimeoutConfigKey, value)
			_, err := resolveSealLockTimeout(repo)
			if err == nil {
				t.Fatalf("value %q: expected error, got nil", value)
			}
			if !strings.Contains(err.Error(), sealLockTimeoutConfigKey) {
				t.Fatalf("value %q: error should name the config key, got: %v", value, err)
			}
		}
	})

	t.Run("config read failure is propagated", func(t *testing.T) {
		repo := newTestCLI(t).initRepo(t)
		// A non-existent working directory makes git fail to run; the error must
		// surface rather than being swallowed as an unset key.
		_, err := resolveSealLockTimeout(filepath.Join(repo, "does-not-exist"))
		if err == nil {
			t.Fatal("expected error when git config cannot run, got nil")
		}
	})

	t.Run("invalid values are rejected", func(t *testing.T) {
		for _, value := range []string{"+5", " 5", "5 ", "5s", "abc", "-1", "", "1.5", "0x5"} {
			repo := newTestCLI(t).initRepo(t)
			// git config preserves the value (including leading/trailing
			// whitespace and the empty string) exactly when set via the CLI.
			git(t, repo, "config", sealLockTimeoutConfigKey, value)
			_, err := resolveSealLockTimeout(repo)
			if err == nil {
				t.Fatalf("value %q: expected error, got nil", value)
			}
			if !strings.Contains(err.Error(), sealLockTimeoutConfigKey) {
				t.Fatalf("value %q: error should name the config key, got: %v", value, err)
			}
		}
	})
}

func TestAcquireSealLockUnwritableDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission tests are Unix-specific")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root: permission restrictions don't apply")
	}
	dir := filepath.Join(t.TempDir(), "seals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o755) }()

	_, err := acquireSealLock(filepath.Join(dir, "paths.lock"), defaultSealLockTimeout)
	if err == nil {
		t.Fatal("expected error creating lock in unwritable dir, got nil")
	}
	var xe *exitError
	if errors.As(err, &xe) {
		t.Fatalf("permission error must not be reported as lock timeout: %v", err)
	}
}

func TestWriteSealStoreUnwritableDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission tests are Unix-specific")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root: permission restrictions don't apply")
	}
	dir := filepath.Join(t.TempDir(), "seals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o755) }()

	err := writeSealStore(filepath.Join(dir, "paths.json"), sealPathStore{Paths: map[string]sealEntry{}})
	if err == nil {
		t.Fatal("expected error writing store in unwritable dir, got nil")
	}
}

func TestSealLockReleaseReportsRemoveFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission tests are Unix-specific")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root: permission restrictions don't apply")
	}
	dir := filepath.Join(t.TempDir(), "seals")
	lockPath := filepath.Join(dir, "paths.lock")

	release, err := acquireSealLock(lockPath, defaultSealLockTimeout)
	if err != nil {
		t.Fatalf("acquireSealLock: %v", err)
	}

	// Make the directory read-only so the lock file cannot be removed.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o755) }()

	release() // must not panic; reports the failure on stderr

	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file should still exist after failed release: %v", err)
	}
}

func TestReadSealContextInsideWorktree(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		key, err := readSealContext()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "key1" {
			t.Fatalf("got %q, want %q", key, "key1")
		}
	})
}

func TestReadSealContextOutsideWorktree(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	// The main checkout is a git repository but not a managed worktree.
	withWorkingDir(t, repo, func() {
		if _, err := readSealContext(); err == nil {
			t.Fatal("expected error outside a managed worktree, got nil")
		}
	})
}

// --- cmdSealClaim / cmdSealUnclaim in-process tests (need a real git repo) ---

func TestCmdSealClaimAndRemoveInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := cmdSealClaim([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
		// idempotent: same key, same path
		if err := cmdSealClaim([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim idempotent: %v", err)
		}
		if err := cmdSealUnclaim([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealUnclaim: %v", err)
		}
		// idempotent: not present
		if err := cmdSealUnclaim([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealUnclaim idempotent: %v", err)
		}
	})
}

func TestCmdSealClaimMultiplePathsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	commitFile(t, repo, "second.txt", "content\n")
	commitFile(t, repo, "third.txt", "content\n")
	wt1 := openManagedWorktree(t, repo, "key1")
	wt2 := openManagedWorktree(t, repo, "key2")

	withWorkingDir(t, wt1, func() {
		if err := cmdSealClaim([]string{"tracked.txt", "second.txt", "third.txt"}); err != nil {
			t.Fatalf("cmdSealClaim multiple paths: %v", err)
		}
	})
	// All three should be blocked for a different key
	withWorkingDir(t, wt2, func() {
		if err := cmdSealClaim([]string{"second.txt"}); err == nil {
			t.Fatal("expected conflict for second.txt, got nil")
		}
	})
}

func TestCmdSealClaimRejectsDifferentKeyInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := openManagedWorktree(t, repo, "key1")
	wt2 := openManagedWorktree(t, repo, "key2")

	withWorkingDir(t, wt1, func() {
		if err := cmdSealClaim([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
	})

	withWorkingDir(t, wt2, func() {
		err := cmdSealClaim([]string{"tracked.txt"})
		if err == nil {
			t.Fatal("expected error when adding path under different key, got nil")
		}
		var xe *exitError
		if !errors.As(err, &xe) || xe.code != exitSealConflict {
			t.Fatalf("expected exitError code %d, got: %v", exitSealConflict, err)
		}
		if !strings.Contains(err.Error(), "seal-conflict:") {
			t.Fatalf("expected 'seal-conflict:' prefix, got: %s", err.Error())
		}
	})
}

func TestCmdSealUnclaimRejectsDifferentKeyInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := openManagedWorktree(t, repo, "key1")
	wt2 := openManagedWorktree(t, repo, "key2")

	withWorkingDir(t, wt1, func() {
		if err := cmdSealClaim([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
	})

	withWorkingDir(t, wt2, func() {
		err := cmdSealUnclaim([]string{"tracked.txt"})
		if err == nil {
			t.Fatal("expected error when removing path owned by different key, got nil")
		}
		var xe *exitError
		if !errors.As(err, &xe) || xe.code != exitSealConflict {
			t.Fatalf("expected exitError code %d, got: %v", exitSealConflict, err)
		}
		if !strings.Contains(err.Error(), "seal-conflict:") {
			t.Fatalf("expected 'seal-conflict:' prefix, got: %s", err.Error())
		}
	})

	// key1's seal must still be intact
	withWorkingDir(t, wt1, func() {
		if err := cmdSealClaim([]string{"tracked.txt"}); err != nil {
			t.Fatalf("seal should still be owned by key1 after failed removal: %v", err)
		}
	})
}

func TestCmdSealClaimReportsAllConflictsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	commitFile(t, repo, "second.txt", "content\n")
	commitFile(t, repo, "third.txt", "content\n")
	wt1 := openManagedWorktree(t, repo, "key1")
	wt2 := openManagedWorktree(t, repo, "key2")
	wt3 := openManagedWorktree(t, repo, "key3")
	wt4 := openManagedWorktree(t, repo, "key4")

	withWorkingDir(t, wt1, func() {
		if err := cmdSealClaim([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim tracked.txt: %v", err)
		}
	})
	withWorkingDir(t, wt2, func() {
		if err := cmdSealClaim([]string{"second.txt"}); err != nil {
			t.Fatalf("cmdSealClaim second.txt: %v", err)
		}
	})

	// key3 tries to add all three: the error must list both conflicting
	// paths with the keys that seal them.
	withWorkingDir(t, wt3, func() {
		err := cmdSealClaim([]string{"tracked.txt", "second.txt", "third.txt"})
		if err == nil {
			t.Fatal("expected conflict error, got nil")
		}
		msg := err.Error()
		for _, want := range []string{"seal-conflict:", "tracked.txt", "key1", "second.txt", "key2"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("conflict error missing %q: %s", want, msg)
			}
		}
	})

	// All-or-nothing: third.txt must not have been sealed.
	withWorkingDir(t, wt4, func() {
		if err := cmdSealClaim([]string{"third.txt"}); err != nil {
			t.Fatalf("third.txt should not have been claimed by the failed claim: %v", err)
		}
	})
}

func TestCmdSealClaimRejectsDirectoryInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")
	if err := os.Mkdir(filepath.Join(wt, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	withWorkingDir(t, wt, func() {
		err := cmdSealClaim([]string{"subdir"})
		if err == nil {
			t.Fatal("expected error for directory target, got nil")
		}
		if !strings.Contains(err.Error(), "directory") {
			t.Fatalf("expected directory rejection message, got: %v", err)
		}
	})
}

func TestCmdSealClaimNonExistentFileInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := cmdSealClaim([]string{"nosuchfile.txt"}); err == nil {
			t.Fatal("expected error for non-existent file, got nil")
		}
	})
}

func TestCmdSealClaimOutsideRepoInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := cmdSealClaim([]string{"../outside.txt"}); err == nil {
			t.Fatal("expected error for path outside repo, got nil")
		}
	})
}

func TestCmdSealClaimFailsOutsideGitRepo(t *testing.T) {
	withWorkingDir(t, t.TempDir(), func() {
		if err := cmdSealClaim([]string{"tracked.txt"}); err == nil {
			t.Fatal("expected error outside git repo, got nil")
		}
	})
}

func TestCmdSealUnclaimFailsOutsideGitRepo(t *testing.T) {
	withWorkingDir(t, t.TempDir(), func() {
		if err := cmdSealUnclaim([]string{"tracked.txt"}); err == nil {
			t.Fatal("expected error outside git repo, got nil")
		}
	})
}

func TestCmdSealClaimFailsOutsideManagedWorktree(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	// A plain git checkout that is not a managed worktree must be rejected.
	withWorkingDir(t, repo, func() {
		if err := cmdSealClaim([]string{"tracked.txt"}); err == nil {
			t.Fatal("expected error outside a managed worktree, got nil")
		}
	})
}

func TestCmdSealUnclaimOutsideRepoInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := cmdSealUnclaim([]string{"../outside.txt"}); err == nil {
			t.Fatal("expected error for path outside repo, got nil")
		}
	})
}

func TestCmdSealUnclaimAllowsDifferentKeyAfterRemovalInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := openManagedWorktree(t, repo, "key1")
	wt2 := openManagedWorktree(t, repo, "key2")

	withWorkingDir(t, wt1, func() {
		if err := cmdSealClaim([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
		if err := cmdSealUnclaim([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealUnclaim: %v", err)
		}
	})
	// After removal, a different key can now seal the same path
	withWorkingDir(t, wt2, func() {
		if err := cmdSealClaim([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim after removal: %v", err)
		}
	})
}

func TestCmdSealUnclaimFromMultiPathStoreInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	commitFile(t, repo, "second.txt", "content\n")
	wt1 := openManagedWorktree(t, repo, "key1")
	wt2 := openManagedWorktree(t, repo, "key2")

	withWorkingDir(t, wt1, func() {
		if err := cmdSealClaim([]string{"tracked.txt", "second.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
		if err := cmdSealUnclaim([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealUnclaim tracked.txt: %v", err)
		}
	})
	// second.txt is still sealed under key1
	withWorkingDir(t, wt2, func() {
		if err := cmdSealClaim([]string{"second.txt"}); err == nil {
			t.Fatal("expected conflict error for second.txt still sealed under key1, got nil")
		}
	})
}

func TestRunSealClaimUnclaimInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := run([]string{"seal", "claim", "tracked.txt"}); err != nil {
			t.Fatalf("seal claim via run: %v", err)
		}
		if err := run([]string{"seal", "unclaim", "tracked.txt"}); err != nil {
			t.Fatalf("seal unclaim via run: %v", err)
		}
	})
}

func TestRunSealClaimMultiplePathsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	commitFile(t, repo, "second.txt", "content\n")
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := run([]string{"seal", "claim", "tracked.txt", "second.txt"}); err != nil {
			t.Fatalf("seal claim multiple paths via run: %v", err)
		}
		if err := run([]string{"seal", "unclaim", "tracked.txt", "second.txt"}); err != nil {
			t.Fatalf("seal unclaim multiple paths via run: %v", err)
		}
	})
}

func TestRunSealClaimMissingArgInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	// The missing-argument check runs before the seal context is resolved, so
	// it fails regardless of the current worktree.
	withWorkingDir(t, repo, func() {
		if err := run([]string{"seal", "claim"}); err == nil {
			t.Fatal("expected error for missing path arg, got nil")
		}
		if err := run([]string{"seal", "unclaim"}); err == nil {
			t.Fatal("expected error for missing path arg, got nil")
		}
	})
}

func TestRunSealClaimUnclaimHelpInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		stdout, err := captureStdout(t, func() error {
			return run([]string{"seal", "claim", "--help"})
		})
		if err != nil {
			t.Fatalf("seal claim --help: %v", err)
		}
		if !strings.Contains(stdout, "managed worktree") {
			t.Fatalf("seal claim --help should describe worktree-derived key resolution: %s", stdout)
		}

		stdout, err = captureStdout(t, func() error {
			return run([]string{"seal", "unclaim", "--help"})
		})
		if err != nil {
			t.Fatalf("seal unclaim --help: %v", err)
		}
		if !strings.Contains(stdout, "managed worktree") {
			t.Fatalf("seal unclaim --help should describe worktree-derived key resolution: %s", stdout)
		}
	})
}

// --- cmdSealTest in-process tests (need a real git repo) ---

func TestCmdSealTestUnsealedSucceedsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := cmdSealTest([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealTest on unsealed path: %v", err)
		}
	})
}

func TestCmdSealTestNonExistentPathSucceedsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	// A path inside the repository that does not exist is treated as unclaimed,
	// so it can be checked before the file is created.
	withWorkingDir(t, wt, func() {
		if err := cmdSealTest([]string{"new-file.txt"}); err != nil {
			t.Fatalf("cmdSealTest on non-existent in-repo path: %v", err)
		}
	})
}

func TestCmdSealTestCurrentKeySucceedsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := cmdSealClaim([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
		// A path claimed by the current key is safe.
		if err := cmdSealTest([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealTest on own claim: %v", err)
		}
	})
}

func TestCmdSealTestRejectsDifferentKeyInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := openManagedWorktree(t, repo, "key1")
	wt2 := openManagedWorktree(t, repo, "key2")

	withWorkingDir(t, wt1, func() {
		if err := cmdSealClaim([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
	})

	withWorkingDir(t, wt2, func() {
		err := cmdSealTest([]string{"tracked.txt"})
		if err == nil {
			t.Fatal("expected conflict error for path claimed by different key, got nil")
		}
		var xe *exitError
		if !errors.As(err, &xe) || xe.code != exitSealConflict {
			t.Fatalf("expected exitError code %d, got: %v", exitSealConflict, err)
		}
		for _, want := range []string{"seal-conflict:", "tracked.txt", "key1"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("conflict error missing %q: %s", want, err.Error())
			}
		}
	})
}

func TestCmdSealTestReportsAllConflictsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	commitFile(t, repo, "second.txt", "content\n")
	commitFile(t, repo, "third.txt", "content\n")
	wt1 := openManagedWorktree(t, repo, "key1")
	wt2 := openManagedWorktree(t, repo, "key2")
	wt3 := openManagedWorktree(t, repo, "key3")

	withWorkingDir(t, wt1, func() {
		if err := cmdSealClaim([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim tracked.txt: %v", err)
		}
	})
	withWorkingDir(t, wt2, func() {
		if err := cmdSealClaim([]string{"second.txt"}); err != nil {
			t.Fatalf("cmdSealClaim second.txt: %v", err)
		}
	})

	// key3 tests all three: third.txt is safe, but the two foreign claims must
	// both be reported with the keys that hold them.
	withWorkingDir(t, wt3, func() {
		err := cmdSealTest([]string{"tracked.txt", "second.txt", "third.txt"})
		if err == nil {
			t.Fatal("expected conflict error, got nil")
		}
		for _, want := range []string{"seal-conflict:", "tracked.txt", "key1", "second.txt", "key2"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("conflict error missing %q: %s", want, err.Error())
			}
		}
	})
}

func TestCmdSealTestDoesNotMutateStoreInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := cmdSealTest([]string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealTest: %v", err)
		}
	})

	// seal test is read-only: it must not create the store file.
	storeFile, _, err := pathsSealStore(repo)
	if err != nil {
		t.Fatalf("pathsSealStore: %v", err)
	}
	if _, err := os.Stat(storeFile); !os.IsNotExist(err) {
		t.Fatalf("seal store %s should not exist after seal test (stat err: %v)", storeFile, err)
	}
}

func TestCmdSealTestOutsideRepoPathInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := cmdSealTest([]string{"../outside.txt"}); err == nil {
			t.Fatal("expected error for path outside repo, got nil")
		}
	})
}

func TestCmdSealTestFailsOutsideManagedWorktreeInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	// The main checkout is a git repo but not a managed worktree: the context
	// error must be distinct from a seal-conflict.
	withWorkingDir(t, repo, func() {
		err := cmdSealTest([]string{"tracked.txt"})
		if err == nil {
			t.Fatal("expected error outside a managed worktree, got nil")
		}
		if strings.Contains(err.Error(), "seal-conflict:") {
			t.Fatalf("context error must not look like a seal-conflict: %s", err.Error())
		}
		var xe *exitError
		if errors.As(err, &xe) && xe.code == exitSealConflict {
			t.Fatalf("context error must not carry the seal-conflict exit code: %v", err)
		}
	})
}

func TestRunSealTestInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := run([]string{"seal", "test", "tracked.txt"}); err != nil {
			t.Fatalf("seal test via run: %v", err)
		}
	})
}

func TestRunSealTestMissingArgInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		if err := run([]string{"seal", "test"}); err == nil {
			t.Fatal("expected error for missing path arg, got nil")
		}
	})
}

func TestRunSealTestRejectsUndefinedOptionsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	// --all / --unsealed / --staged are not defined in v0 and must error. The
	// option check happens before the seal context is resolved, but run it from
	// the worktree so a context failure can't mask an accepted option.
	withWorkingDir(t, wt, func() {
		for _, opt := range []string{"--all", "--unsealed", "--staged"} {
			if err := run([]string{"seal", "test", opt, "tracked.txt"}); err == nil {
				t.Fatalf("expected error for undefined option %q, got nil", opt)
			}
		}
	})
}

func TestRunSealTestHelpInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		stdout, err := captureStdout(t, func() error {
			return run([]string{"seal", "test", "--help"})
		})
		if err != nil {
			t.Fatalf("seal test --help: %v", err)
		}
		if !strings.Contains(stdout, "managed worktree") {
			t.Fatalf("seal test --help should describe worktree-derived key resolution: %s", stdout)
		}
	})
}

// --- parseLsArgs unit tests ---

func TestParseLsArgsNoFlags(t *testing.T) {
	opts, err := parseLsArgs([]string{})
	if err != nil {
		t.Fatalf("parseLsArgs([]) error = %v, want nil", err)
	}
	if opts.JSON {
		t.Fatal("JSON = true, want false")
	}
}

func TestParseLsArgsJSON(t *testing.T) {
	opts, err := parseLsArgs([]string{"--json"})
	if err != nil {
		t.Fatalf("parseLsArgs([--json]) error = %v, want nil", err)
	}
	if !opts.JSON {
		t.Fatal("JSON = false, want true")
	}
}

func TestParseLsArgsUnknownFlagIsError(t *testing.T) {
	for _, args := range [][]string{
		{"--format"},
		{"--all"},
		{"unexpected"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, err := parseLsArgs(args); err == nil {
				t.Fatalf("parseLsArgs(%v) error = nil, want error", args)
			}
		})
	}
}

// --- parseSealLsArgs unit tests ---

func TestParseSealLsArgsNoArgs(t *testing.T) {
	opts, err := parseSealLsArgs([]string{})
	if err != nil {
		t.Fatalf("parseSealLsArgs([]) error = %v, want nil", err)
	}
	if opts.JSON || opts.FilterKey != "" {
		t.Fatalf("opts = %+v, want zero", opts)
	}
}

func TestParseSealLsArgsKeyOnly(t *testing.T) {
	opts, err := parseSealLsArgs([]string{"key1"})
	if err != nil {
		t.Fatalf("parseSealLsArgs([key1]) error = %v, want nil", err)
	}
	if opts.FilterKey != "key1" || opts.JSON {
		t.Fatalf("opts = %+v, want FilterKey=key1 JSON=false", opts)
	}
}

func TestParseSealLsArgsJSONOnly(t *testing.T) {
	opts, err := parseSealLsArgs([]string{"--json"})
	if err != nil {
		t.Fatalf("parseSealLsArgs([--json]) error = %v, want nil", err)
	}
	if !opts.JSON || opts.FilterKey != "" {
		t.Fatalf("opts = %+v, want JSON=true FilterKey=''", opts)
	}
}

func TestParseSealLsArgsJSONThenKey(t *testing.T) {
	opts, err := parseSealLsArgs([]string{"--json", "key1"})
	if err != nil {
		t.Fatalf("parseSealLsArgs([--json key1]) error = %v, want nil", err)
	}
	if !opts.JSON || opts.FilterKey != "key1" {
		t.Fatalf("opts = %+v, want JSON=true FilterKey=key1", opts)
	}
}

func TestParseSealLsArgsKeyBeforeJSONIsUsageError(t *testing.T) {
	_, err := parseSealLsArgs([]string{"key1", "--json"})
	if err == nil {
		t.Fatal("parseSealLsArgs([key1 --json]) error = nil, want usage error")
	}
	var xe *exitError
	if !errors.As(err, &xe) || xe.code != exitUsageError {
		t.Fatalf("expected exitUsageError, got: %v", err)
	}
}

func TestParseSealLsArgsUnknownOptionIsUsageError(t *testing.T) {
	for _, args := range [][]string{
		{"--all"},
		{"--key", "key1"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := parseSealLsArgs(args)
			if err == nil {
				t.Fatalf("parseSealLsArgs(%v) error = nil, want error", args)
			}
			var xe *exitError
			if !errors.As(err, &xe) || xe.code != exitUsageError {
				t.Fatalf("expected exitUsageError, got: %v", err)
			}
		})
	}
}

func TestParseSealLsArgsExtraArgAfterKeyIsUsageError(t *testing.T) {
	_, err := parseSealLsArgs([]string{"--json", "key1", "extra"})
	if err == nil {
		t.Fatal("parseSealLsArgs([--json key1 extra]) error = nil, want usage error")
	}
	var xe *exitError
	if !errors.As(err, &xe) || xe.code != exitUsageError {
		t.Fatalf("expected exitUsageError, got: %v", err)
	}
}

func TestRunSealLsJSONInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := run([]string{"seal", "claim", "tracked.txt"}); err != nil {
			t.Fatalf("seal claim: %v", err)
		}
	})

	var out string
	withWorkingDir(t, repo, func() {
		var err error
		out, err = captureStdout(t, func() error { return run([]string{"seal", "ls", "--json"}) })
		if err != nil {
			t.Fatalf("seal ls --json: %v", err)
		}
	})

	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("seal ls --json output = %q, want ok:true envelope", out)
	}
	if !strings.Contains(out, `"filterKey":null`) {
		t.Fatalf("seal ls --json output = %q, want filterKey:null", out)
	}
}

func TestRunLsJSONInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	openManagedWorktree(t, repo, "key1")

	var out string
	withWorkingDir(t, repo, func() {
		var err error
		out, err = captureStdout(t, func() error { return run([]string{"ls", "--json"}) })
		if err != nil {
			t.Fatalf("ls --json: %v", err)
		}
	})

	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("ls --json output = %q, want ok:true envelope", out)
	}
	if !strings.Contains(out, `"keys"`) {
		t.Fatalf("ls --json output = %q, want keys field", out)
	}
}
