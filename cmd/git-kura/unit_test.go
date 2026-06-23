package main

import (
	"bytes"
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
	"github.com/tooppoo/git-kura/internal/output"
	"github.com/tooppoo/git-kura/internal/seal"
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

	t.Run("json without dry-run is valid", func(t *testing.T) {
		_, opts, err := parseOpenArgs([]string{"51", "--json"})
		if err != nil {
			t.Fatalf("parseOpenArgs with --json (no --dry-run) returned unexpected error: %v", err)
		}
		if !opts.JSON {
			t.Fatal("expected opts.JSON true for --json flag")
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
		ce := toCommandError(output.CommandGet, fmt.Errorf("boom"))
		if ce.Code != "general-error" || ce.ExitCode != exitGeneralError {
			t.Fatalf("toCommandError plain = %+v, want general-error/%d", ce, exitGeneralError)
		}
		if ce.Message != "boom" || ce.Command != output.CommandGet {
			t.Fatalf("toCommandError plain = %+v, want message boom, command get", ce)
		}
	})

	t.Run("exitError keeps its code and reason", func(t *testing.T) {
		ce := toCommandError(output.CommandOpen, exitCodeError(exitNotFound, fmt.Errorf("missing")))
		if ce.Code != "not-found" || ce.ExitCode != exitNotFound {
			t.Fatalf("toCommandError exitError = %+v, want not-found/%d", ce, exitNotFound)
		}
	})
}

func TestTOONRendererFormat(t *testing.T) {
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

	var buf bytes.Buffer
	r := output.SelectRenderer(output.RenderTOON)
	if err := r.RenderResult(&buf, &bytes.Buffer{}, output.Result{Command: output.CommandGet, Data: data}); err != nil {
		t.Fatalf("RenderResult error = %v", err)
	}
	out := buf.String()

	// Lines that end with ':' (section headers like "data:" or "warnings[0]:")
	// are valid TOON syntax and do not carry a value on the same line.
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, ":") {
			continue
		}
		if !strings.Contains(line, ": ") {
			t.Errorf("line %q does not use ': ' separator", line)
		}
	}
}

func TestTOONRendererFields(t *testing.T) {
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

	var buf bytes.Buffer
	r := output.SelectRenderer(output.RenderTOON)
	if err := r.RenderResult(&buf, &bytes.Buffer{}, output.Result{Command: output.CommandGet, Data: data}); err != nil {
		t.Fatalf("RenderResult error = %v", err)
	}
	out := buf.String()

	// Data fields must appear somewhere in the TOON envelope output.
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
		if !strings.Contains(out, want) {
			t.Errorf("field %q: output does not contain %q\nfull output:\n%s", field, want, out)
		}
	}
}

func TestRequireCleanValueStdoutAcceptsWindowsPath(t *testing.T) {
	requireCleanValueStdout(t, cliResult{stdout: `C:\repo.kura\worktrees\51` + "\n"})
}

// --- seal path store unit tests ---

func TestNormalizeSealPathAbsoluteRejected(t *testing.T) {
	root := t.TempDir()
	_, err := seal.NormalizePath(root, filepath.Join(root, "src", "foo.go"))
	if err == nil {
		t.Fatal("expected error for absolute path, got nil")
	}
}

func TestNormalizeSealPathRootRelative(t *testing.T) {
	root := t.TempDir()
	path, err := seal.NormalizePath(root, "src/foo.go")
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
		path, err := seal.NormalizePath(root, "src/foo.go")
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
	_, err := seal.NormalizePath(root, "../escape.go")
	if err == nil {
		t.Fatal("expected error for path outside repo, got nil")
	}
}

func TestNormalizeSealPathDotDotPrefixInsideRepo(t *testing.T) {
	root := t.TempDir()
	// A file like "..foo/bar" starts with ".." but is inside the repo.
	path, err := seal.NormalizePath(root, "..foo/bar")
	if err != nil {
		t.Fatalf("unexpected error for path inside repo starting with '..': %v", err)
	}
	if path != filepath.Join("..foo", "bar") {
		t.Fatalf("got %q, want %q", path, filepath.Join("..foo", "bar"))
	}
}

func TestNormalizeSealPathRepoRootItself(t *testing.T) {
	root := t.TempDir()
	path, err := seal.NormalizePath(root, ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "." {
		t.Fatalf("got %q, want %q", path, ".")
	}
}

func TestReadSealStoreNotExist(t *testing.T) {
	store, err := seal.ReadStore(filepath.Join(t.TempDir(), "missing.json"))
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
	_, err := seal.ReadStore(filepath.Join(dir, "seals.json"))
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
			if _, err := seal.ReadStore(path); err == nil {
				t.Fatalf("expected schema validation error for %s, got nil", name)
			}
		})
	}
}

func TestDoctorSealStoreAcceptsMissingEmptyAndValidStores(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	storeFile, _, err := seal.StorePaths(repo)
	if err != nil {
		t.Fatalf("pathsSealStore: %v", err)
	}

	if err := seal.DoctorStore(storeFile); err != nil {
		t.Fatalf("doctor missing store: %v", err)
	}
	if _, err := os.Stat(storeFile); !os.IsNotExist(err) {
		t.Fatalf("doctor should not create missing store (stat err: %v)", err)
	}

	if err := seal.WriteStore(storeFile, seal.PathStore{Paths: map[string]seal.Entry{}}); err != nil {
		t.Fatalf("write empty store: %v", err)
	}
	if err := seal.DoctorStore(storeFile); err != nil {
		t.Fatalf("doctor empty store: %v", err)
	}

	if err := seal.WriteStore(storeFile, seal.PathStore{
		Paths: map[string]seal.Entry{"src/a.go": {Key: "key1"}},
	}); err != nil {
		t.Fatalf("write valid store: %v", err)
	}
	if err := seal.DoctorStore(storeFile); err != nil {
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
			storeFile, _, err := seal.StorePaths(repo)
			if err != nil {
				t.Fatalf("pathsSealStore: %v", err)
			}
			if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(storeFile, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}

			if err := seal.DoctorStore(storeFile); err == nil {
				t.Fatal("doctor invalid store error = nil, want error")
			}
		})
	}
}

func TestDoctorSealStoreRejectsInvalidStoredPaths(t *testing.T) {
	for name, paths := range map[string]map[string]seal.Entry{
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

			err := seal.DoctorStore(storeFile)
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
	storeFile := seedSealStore(t, repo, map[string]seal.Entry{
		`bad\sep.go`:    {Key: "key1"}, // backslash separator
		"../escape.txt": {Key: "key1"}, // escapes the repository root
		"src/./b.go":    {Key: "key1"}, // not normalized
	})

	err := seal.DoctorStore(storeFile)
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
			got, err := seal.CanonicalStoredPath(tc.rawPath)
			if tc.wantError != "" {
				if err == nil {
					t.Fatalf("seal.CanonicalStoredPath(%q) error = nil, want %q", tc.rawPath, tc.wantError)
				}
				if !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("seal.CanonicalStoredPath(%q) error = %q, want it to contain %q", tc.rawPath, err.Error(), tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("seal.CanonicalStoredPath(%q) error = %v, want nil", tc.rawPath, err)
			}
			if got != tc.want {
				t.Fatalf("seal.CanonicalStoredPath(%q) = %q, want %q", tc.rawPath, got, tc.want)
			}
		})
	}
}

func TestCmdSealDoctorOutsideRepoFails(t *testing.T) {
	outside := t.TempDir()
	withWorkingDir(t, outside, func() {
		if err := cmdSealDoctor(sealDoctorOptions{}); err == nil {
			t.Fatal("cmdSealDoctor outside repo error = nil, want error")
		}
	})
}

func TestCmdSealDoctorReturnsExitCode7ForIntegrityFailures(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	seedSealStore(t, repo, map[string]seal.Entry{
		`src\a.go`: {Key: "key1"},
	})

	withWorkingDir(t, repo, func() {
		out, err := captureStdout(t, func() error {
			return cmdSealDoctor(sealDoctorOptions{})
		})
		if err == nil {
			t.Fatal("cmdSealDoctor invalid store error = nil, want error")
		}
		var re *renderedError
		if !errors.As(err, &re) || re.code != exitSealDoctorError {
			t.Fatalf("cmdSealDoctor exit code = %v, want %d (err: %v)", re, exitSealDoctorError, err)
		}
		if !strings.Contains(out, "seal-doctor-error:") {
			t.Fatalf("doctor output should include stable token: %s", out)
		}
	})
}

func TestWriteSealStoreRejectsSchemaViolations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paths.json")
	// An empty key violates the schema's minLength constraint; the write
	// must be refused and nothing persisted.
	err := seal.WriteStore(path, seal.PathStore{
		Paths: map[string]seal.Entry{"src/a.go": {Key: ""}},
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
	if err := seal.WriteStore(path, seal.PathStore{}); err != nil {
		t.Fatalf("write with nil Paths should normalize to empty map: %v", err)
	}
	got, err := seal.ReadStore(path)
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
	_, err := seal.ReadStore(storePath)
	if err == nil {
		t.Fatal("expected error for unreadable file, got nil")
	}
}

func TestWriteReadSealStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paths.json")
	original := seal.PathStore{
		Paths: map[string]seal.Entry{
			"src/a.go":      {Key: "key1"},
			"internal/b.go": {Key: "key2"},
		},
	}
	if err := seal.WriteStore(path, original); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := seal.ReadStore(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got.Paths) != len(original.Paths) {
		t.Fatalf("round-trip length mismatch: got %d, want %d", len(got.Paths), len(original.Paths))
	}
	if got.Paths["src/a.go"].Key != "key1" || got.Paths["internal/b.go"].Key != "key2" {
		t.Fatalf("round-trip content mismatch: got %+v", got.Paths)
	}
	if got.SchemaVersion != seal.SchemaVersion {
		t.Fatalf("schema version: got %d, want %d", got.SchemaVersion, seal.SchemaVersion)
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
	store := seal.PathStore{
		Paths: map[string]seal.Entry{
			"src/a.go": {Key: "key1"},
		},
	}
	if err := seal.WriteStore(path, store); err != nil {
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
	err := seal.WriteStore(filepath.Join(dir, "not-a-dir", "paths.json"), seal.PathStore{Paths: map[string]seal.Entry{}})
	if err == nil {
		t.Fatal("expected error when MkdirAll cannot create dir, got nil")
	}
}

func TestPathsSealStoreOutsideRepo(t *testing.T) {
	_, _, err := seal.StorePaths(t.TempDir())
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

	release, err := seal.AcquireLock(lockPath, seal.DefaultLockTimeout)
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
	_, err = seal.AcquireLock(lockPath, 150*time.Millisecond)
	if err == nil {
		t.Fatal("expected lock-timeout error, got nil")
	}
	var lte seal.LockTimeoutErr
	if !errors.As(err, &lte) {
		t.Fatalf("expected LockTimeoutErr, got: %v", err)
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
	_, err = seal.AcquireLock(lockPath, 0)
	if err == nil {
		t.Fatal("expected lock-timeout error, got nil")
	}
	// A single attempt must not sleep for a retry interval.
	if elapsed := time.Since(start); elapsed >= seal.LockInterval {
		t.Fatalf("zero timeout retried instead of failing immediately: took %s", elapsed)
	}
	var lte seal.LockTimeoutErr
	if !errors.As(err, &lte) {
		t.Fatalf("expected LockTimeoutErr, got: %v", err)
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

	release, err := seal.AcquireLock(lockPath, 0)
	if err != nil {
		t.Fatalf("acquireSealLock with free lock and zero timeout: %v", err)
	}
	release()
}

func TestResolveSealLockTimeout(t *testing.T) {
	t.Run("unset uses default", func(t *testing.T) {
		repo := newTestCLI(t).initRepo(t)
		got, err := seal.ResolveLockTimeout(repo)
		if err != nil {
			t.Fatalf("resolveSealLockTimeout: %v", err)
		}
		if got != seal.DefaultLockTimeout {
			t.Fatalf("got %s, want default %s", got, seal.DefaultLockTimeout)
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
			git(t, repo, "config", seal.LockTimeoutConfigKey, value)
			got, err := seal.ResolveLockTimeout(repo)
			if err != nil {
				t.Fatalf("seal.ResolveLockTimeout(%q): %v", value, err)
			}
			if got != want {
				t.Fatalf("value %q: got %s, want %s", value, got, want)
			}
		}
	})

	t.Run("the cap itself is accepted", func(t *testing.T) {
		repo := newTestCLI(t).initRepo(t)
		git(t, repo, "config", seal.LockTimeoutConfigKey, strconv.FormatInt(seal.MaxLockTimeoutMs, 10))
		got, err := seal.ResolveLockTimeout(repo)
		if err != nil {
			t.Fatalf("resolveSealLockTimeout at cap: %v", err)
		}
		if got != seal.MaxLockTimeout {
			t.Fatalf("got %s, want %s", got, seal.MaxLockTimeout)
		}
	})

	t.Run("values above the cap are rejected", func(t *testing.T) {
		for _, value := range []string{
			strconv.FormatInt(seal.MaxLockTimeoutMs+1, 10), // just over the cap
			"9223372036855",              // parses as int64 but overflows when multiplied
			"99999999999999999999999999", // too large to fit in int64
		} {
			repo := newTestCLI(t).initRepo(t)
			git(t, repo, "config", seal.LockTimeoutConfigKey, value)
			_, err := seal.ResolveLockTimeout(repo)
			if err == nil {
				t.Fatalf("value %q: expected error, got nil", value)
			}
			if !strings.Contains(err.Error(), seal.LockTimeoutConfigKey) {
				t.Fatalf("value %q: error should name the config key, got: %v", value, err)
			}
		}
	})

	t.Run("config read failure is propagated", func(t *testing.T) {
		repo := newTestCLI(t).initRepo(t)
		// A non-existent working directory makes git fail to run; the error must
		// surface rather than being swallowed as an unset key.
		_, err := seal.ResolveLockTimeout(filepath.Join(repo, "does-not-exist"))
		if err == nil {
			t.Fatal("expected error when git config cannot run, got nil")
		}
	})

	t.Run("invalid values are rejected", func(t *testing.T) {
		for _, value := range []string{"+5", " 5", "5 ", "5s", "abc", "-1", "", "1.5", "0x5"} {
			repo := newTestCLI(t).initRepo(t)
			// git config preserves the value (including leading/trailing
			// whitespace and the empty string) exactly when set via the CLI.
			git(t, repo, "config", seal.LockTimeoutConfigKey, value)
			_, err := seal.ResolveLockTimeout(repo)
			if err == nil {
				t.Fatalf("value %q: expected error, got nil", value)
			}
			if !strings.Contains(err.Error(), seal.LockTimeoutConfigKey) {
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

	_, err := seal.AcquireLock(filepath.Join(dir, "paths.lock"), seal.DefaultLockTimeout)
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

	err := seal.WriteStore(filepath.Join(dir, "paths.json"), seal.PathStore{Paths: map[string]seal.Entry{}})
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

	release, err := seal.AcquireLock(lockPath, seal.DefaultLockTimeout)
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
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
		// idempotent: same key, same path
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim idempotent: %v", err)
		}
		if err := cmdSealUnclaim(sealUnclaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealUnclaim: %v", err)
		}
		// idempotent: not present
		if err := cmdSealUnclaim(sealUnclaimOptions{}, []string{"tracked.txt"}); err != nil {
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
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt", "second.txt", "third.txt"}); err != nil {
			t.Fatalf("cmdSealClaim multiple paths: %v", err)
		}
	})
	// All three should be blocked for a different key
	withWorkingDir(t, wt2, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"second.txt"}); err == nil {
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
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
	})

	withWorkingDir(t, wt2, func() {
		err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"})
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
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
	})

	withWorkingDir(t, wt2, func() {
		err := cmdSealUnclaim(sealUnclaimOptions{}, []string{"tracked.txt"})
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
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err != nil {
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
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim tracked.txt: %v", err)
		}
	})
	withWorkingDir(t, wt2, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"second.txt"}); err != nil {
			t.Fatalf("cmdSealClaim second.txt: %v", err)
		}
	})

	// key3 tries to add all three: the error must list both conflicting
	// paths with the keys that seal them.
	withWorkingDir(t, wt3, func() {
		err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt", "second.txt", "third.txt"})
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
		if err := cmdSealClaim(sealClaimOptions{}, []string{"third.txt"}); err != nil {
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
		err := cmdSealClaim(sealClaimOptions{}, []string{"subdir"})
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
		if err := cmdSealClaim(sealClaimOptions{}, []string{"nosuchfile.txt"}); err == nil {
			t.Fatal("expected error for non-existent file, got nil")
		}
	})
}

func TestCmdSealClaimOutsideRepoInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"../outside.txt"}); err == nil {
			t.Fatal("expected error for path outside repo, got nil")
		}
	})
}

func TestCmdSealClaimFailsOutsideGitRepo(t *testing.T) {
	withWorkingDir(t, t.TempDir(), func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err == nil {
			t.Fatal("expected error outside git repo, got nil")
		}
	})
}

func TestCmdSealUnclaimFailsOutsideGitRepo(t *testing.T) {
	withWorkingDir(t, t.TempDir(), func() {
		if err := cmdSealUnclaim(sealUnclaimOptions{}, []string{"tracked.txt"}); err == nil {
			t.Fatal("expected error outside git repo, got nil")
		}
	})
}

func TestCmdSealClaimFailsOutsideManagedWorktree(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	// A plain git checkout that is not a managed worktree must be rejected.
	withWorkingDir(t, repo, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err == nil {
			t.Fatal("expected error outside a managed worktree, got nil")
		}
	})
}

func TestCmdSealUnclaimOutsideRepoInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := cmdSealUnclaim(sealUnclaimOptions{}, []string{"../outside.txt"}); err == nil {
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
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
		if err := cmdSealUnclaim(sealUnclaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealUnclaim: %v", err)
		}
	})
	// After removal, a different key can now seal the same path
	withWorkingDir(t, wt2, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err != nil {
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
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt", "second.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
		if err := cmdSealUnclaim(sealUnclaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealUnclaim tracked.txt: %v", err)
		}
	})
	// second.txt is still sealed under key1
	withWorkingDir(t, wt2, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"second.txt"}); err == nil {
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

// TestCmdSealClaimJSONSuccessInProcess exercises the JSON success code path in
// cmdSealClaim (subprocess integration tests cannot contribute to in-process coverage).
func TestCmdSealClaimJSONSuccessInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		out, err := captureStdout(t, func() error {
			return cmdSealClaim(sealClaimOptions{JSON: true}, []string{"tracked.txt"})
		})
		if err != nil {
			t.Fatalf("cmdSealClaim JSON success: %v", err)
		}
		if !strings.Contains(out, `"ok":true`) {
			t.Errorf("expected ok:true, got: %s", out)
		}
		if !strings.Contains(out, `"claimed"`) {
			t.Errorf("expected claimed status, got: %s", out)
		}
	})
}

// TestCmdSealClaimJSONConflictInProcess exercises the JSON error envelope path in
// cmdSealClaim when a path is owned by another key.
func TestCmdSealClaimJSONConflictInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := openManagedWorktree(t, repo, "key1")
	wt2 := openManagedWorktree(t, repo, "key2")

	withWorkingDir(t, wt1, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("initial claim: %v", err)
		}
	})

	withWorkingDir(t, wt2, func() {
		out, err := captureStdout(t, func() error {
			return cmdSealClaim(sealClaimOptions{JSON: true}, []string{"tracked.txt"})
		})
		if err == nil {
			t.Fatal("expected conflict error, got nil")
		}
		if !strings.Contains(out, `"ok":false`) {
			t.Errorf("expected ok:false envelope, got: %s", out)
		}
		if !strings.Contains(out, `"owned-by-other"`) {
			t.Errorf("expected owned-by-other status in paths, got: %s", out)
		}
		if !strings.Contains(out, `"conflicts"`) {
			t.Errorf("expected conflicts array in error details, got: %s", out)
		}
		if !strings.Contains(out, `"currentKey"`) {
			t.Errorf("expected currentKey in error details, got: %s", out)
		}
	})
}

// TestCmdSealUnclaimJSONSuccessInProcess exercises the JSON success code path in
// cmdSealUnclaim (subprocess integration tests cannot contribute to in-process coverage).
func TestCmdSealUnclaimJSONSuccessInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("initial claim: %v", err)
		}
		out, err := captureStdout(t, func() error {
			return cmdSealUnclaim(sealUnclaimOptions{JSON: true}, []string{"tracked.txt"})
		})
		if err != nil {
			t.Fatalf("cmdSealUnclaim JSON success: %v", err)
		}
		if !strings.Contains(out, `"ok":true`) {
			t.Errorf("expected ok:true, got: %s", out)
		}
		if !strings.Contains(out, `"released"`) {
			t.Errorf("expected released status, got: %s", out)
		}
	})
}

// --- cmdSealTest in-process tests (need a real git repo) ---

func TestCmdSealTestUnsealedSucceedsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := cmdSealTest(sealTestOptions{}, []string{"tracked.txt"}); err != nil {
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
		if err := cmdSealTest(sealTestOptions{}, []string{"new-file.txt"}); err != nil {
			t.Fatalf("cmdSealTest on non-existent in-repo path: %v", err)
		}
	})
}

func TestCmdSealTestCurrentKeySucceedsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
		// A path claimed by the current key is safe.
		if err := cmdSealTest(sealTestOptions{}, []string{"tracked.txt"}); err != nil {
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
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
	})

	withWorkingDir(t, wt2, func() {
		out, err := captureStdout(t, func() error {
			return cmdSealTest(sealTestOptions{}, []string{"tracked.txt"})
		})
		if err == nil {
			t.Fatal("expected conflict error for path claimed by different key, got nil")
		}
		var re *renderedError
		if !errors.As(err, &re) || re.code != exitSealConflict {
			t.Fatalf("expected renderedError code %d, got: %v", exitSealConflict, err)
		}
		for _, want := range []string{"seal-conflict:", "tracked.txt", "key1"} {
			if !strings.Contains(out, want) {
				t.Fatalf("conflict stdout missing %q: %s", want, out)
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
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim tracked.txt: %v", err)
		}
	})
	withWorkingDir(t, wt2, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"second.txt"}); err != nil {
			t.Fatalf("cmdSealClaim second.txt: %v", err)
		}
	})

	// key3 tests all three: third.txt is safe, but the two foreign claims must
	// both be reported with the keys that hold them.
	withWorkingDir(t, wt3, func() {
		out, err := captureStdout(t, func() error {
			return cmdSealTest(sealTestOptions{}, []string{"tracked.txt", "second.txt", "third.txt"})
		})
		if err == nil {
			t.Fatal("expected conflict error, got nil")
		}
		var re *renderedError
		if !errors.As(err, &re) || re.code != exitSealConflict {
			t.Fatalf("expected renderedError code %d, got: %v", exitSealConflict, err)
		}
		for _, want := range []string{"seal-conflict:", "tracked.txt", "key1", "second.txt", "key2"} {
			if !strings.Contains(out, want) {
				t.Fatalf("conflict stdout missing %q: %s", want, out)
			}
		}
	})
}

func TestCmdSealTestDoesNotMutateStoreInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	withWorkingDir(t, wt, func() {
		if err := cmdSealTest(sealTestOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealTest: %v", err)
		}
	})

	// seal test is read-only: it must not create the store file.
	storeFile, _, err := seal.StorePaths(repo)
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
		if err := cmdSealTest(sealTestOptions{}, []string{"../outside.txt"}); err == nil {
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
		err := cmdSealTest(sealTestOptions{}, []string{"tracked.txt"})
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

// --- RenderHuman direct tests ---

func TestSealTestDataRenderHumanPrintsConflicts(t *testing.T) {
	claimedBy := "key2"
	data := seal.TestResult{
		CurrentKey: "key1",
		Passed:     false,
		Results: []seal.TestResultItem{
			{Path: "safe.go", Status: "claimed-by-current-key", Safe: true, ClaimedBy: &claimedBy},
			{Path: "conflict.go", Status: "claimed-by-other-key", Safe: false, ClaimedBy: &claimedBy},
			{Path: "free.go", Status: "unclaimed", Safe: true},
		},
	}
	var sb strings.Builder
	if err := data.RenderHuman(&sb); err != nil {
		t.Fatalf("RenderHuman error: %v", err)
	}
	got := sb.String()
	if !strings.Contains(got, "conflict.go") {
		t.Fatalf("expected conflict.go in RenderHuman output, got: %q", got)
	}
	if strings.Contains(got, "safe.go") || strings.Contains(got, "free.go") {
		t.Fatalf("unexpected non-conflict paths in RenderHuman output: %q", got)
	}
}

func TestSealDoctorDataRenderHumanEmitsFindings(t *testing.T) {
	data := seal.DoctorResult{
		Healthy:  false,
		Summary:  seal.DoctorSummary{CheckedClaims: 1, ErrorCount: 1},
		Findings: []seal.DoctorFinding{{Severity: "error", Code: "invalid-stored-path", Message: "bad path"}},
	}
	var sb strings.Builder
	if err := data.RenderHuman(&sb); err != nil {
		t.Fatalf("RenderHuman error: %v", err)
	}
	got := sb.String()
	if !strings.Contains(got, "seal-doctor-error:") {
		t.Fatalf("seal.DoctorResult.RenderHuman should emit seal-doctor-error: token, got: %q", got)
	}
	if !strings.Contains(got, "bad path") {
		t.Fatalf("seal.DoctorResult.RenderHuman should emit finding message, got: %q", got)
	}
}

// --- parseSealTestArgs / parseSealDoctorArgs error paths ---

func TestParseSealTestArgsJSONFlag(t *testing.T) {
	opts, paths, err := parseSealTestArgs([]string{"--json", "path.go"})
	if err != nil {
		t.Fatalf("parseSealTestArgs --json: %v", err)
	}
	if !opts.JSON {
		t.Fatal("opts.JSON = false, want true")
	}
	if len(paths) != 1 || paths[0] != "path.go" {
		t.Fatalf("paths = %v, want [path.go]", paths)
	}
}

func TestParseSealTestArgsUnknownFlagIsError(t *testing.T) {
	_, _, err := parseSealTestArgs([]string{"path.go", "--unknown"})
	if err == nil {
		t.Fatal("parseSealTestArgs with unknown flag: error = nil, want non-nil")
	}
}

func TestParseSealDoctorArgsJSONFlag(t *testing.T) {
	opts, err := parseSealDoctorArgs([]string{"--json"})
	if err != nil {
		t.Fatalf("parseSealDoctorArgs --json: %v", err)
	}
	if !opts.JSON {
		t.Fatal("opts.JSON = false, want true")
	}
}

func TestParseSealDoctorArgsUnknownFlagIsUsageError(t *testing.T) {
	_, err := parseSealDoctorArgs([]string{"--unknown"})
	if err == nil {
		t.Fatal("parseSealDoctorArgs with unknown flag: error = nil, want non-nil")
	}
	var xe *exitError
	if !errors.As(err, &xe) || xe.code != exitUsageError {
		t.Fatalf("parseSealDoctorArgs unknown flag: expected usage error, got: %v", err)
	}
}

func TestParseSealDoctorArgsUnexpectedArgIsUsageError(t *testing.T) {
	_, err := parseSealDoctorArgs([]string{"unexpected"})
	if err == nil {
		t.Fatal("parseSealDoctorArgs with unexpected arg: error = nil, want non-nil")
	}
	var xe *exitError
	if !errors.As(err, &xe) || xe.code != exitUsageError {
		t.Fatalf("parseSealDoctorArgs unexpected arg: expected usage error, got: %v", err)
	}
}

// --- sealTestFail / sealDoctorFail JSON mode ---

func TestSealTestFailJSONEmitsErrorEnvelope(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return sealTestFail(sealTestOptions{JSON: true}, fmt.Errorf("test-error"))
	})
	if err == nil {
		t.Fatal("sealTestFail JSON mode: error = nil, want non-nil")
	}
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("sealTestFail JSON mode: expected ok:false in output, got: %q", out)
	}
}

func TestSealDoctorFailJSONEmitsErrorEnvelope(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return sealDoctorFail(sealDoctorOptions{JSON: true}, fmt.Errorf("test-error"))
	})
	if err == nil {
		t.Fatal("sealDoctorFail JSON mode: error = nil, want non-nil")
	}
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("sealDoctorFail JSON mode: expected ok:false in output, got: %q", out)
	}
}

// --- classifyCurrentKeyError branches ---

func TestClassifyCurrentKeyErrorNotInsideGitRepository(t *testing.T) {
	err := fmt.Errorf("not inside a git repository")
	reason, meta := classifyCurrentKeyError(err, "")
	if reason != "not-inside-git-repository" {
		t.Fatalf("reason = %q, want not-inside-git-repository", reason)
	}
	if meta != nil {
		t.Fatalf("meta = %v, want nil", meta)
	}
}

func TestClassifyCurrentKeyErrorNotInManagedWorktree(t *testing.T) {
	err := fmt.Errorf("current directory is not inside a git-kura managed worktree")
	reason, meta := classifyCurrentKeyError(err, t.TempDir())
	if reason != "not-in-managed-worktree" {
		t.Fatalf("reason = %q, want not-in-managed-worktree", reason)
	}
	if meta != nil {
		t.Fatalf("meta = %v, want nil", meta)
	}
}

func TestClassifyCurrentKeyErrorMetadataMissing(t *testing.T) {
	err := fmt.Errorf("directory /tmp/x has no git-kura metadata")
	reason, _ := classifyCurrentKeyError(err, t.TempDir())
	if reason != "metadata-missing" {
		t.Fatalf("reason = %q, want metadata-missing", reason)
	}
}

func TestClassifyCurrentKeyErrorMetadataInconsistent(t *testing.T) {
	err := fmt.Errorf("some unrecognized worktree error")
	reason, meta := classifyCurrentKeyError(err, t.TempDir())
	if reason != "metadata-inconsistent" {
		t.Fatalf("reason = %q, want metadata-inconsistent", reason)
	}
	if meta != nil {
		t.Fatalf("meta = %v, want nil", meta)
	}
}

// --- tryResolveWorktreeMetadataPath ---

func TestTryResolveWorktreeMetadataPathNonGitDirReturnsEmpty(t *testing.T) {
	result := tryResolveWorktreeMetadataPath(t.TempDir())
	if result != "" {
		t.Fatalf("tryResolveWorktreeMetadataPath non-git dir = %q, want empty", result)
	}
}

func TestTryResolveWorktreeMetadataPathFromRepoRootReturnsEmpty(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	// The repo root is not under the kura worktrees dir, so the result is empty.
	result := tryResolveWorktreeMetadataPath(repo)
	if result != "" {
		t.Fatalf("tryResolveWorktreeMetadataPath from repo root = %q, want empty", result)
	}
}

// --- cmdSealDoctor JSON mode in-process ---

func TestCmdSealDoctorJSONHealthyInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	var out string
	withWorkingDir(t, repo, func() {
		var err error
		out, err = captureStdout(t, func() error {
			return cmdSealDoctor(sealDoctorOptions{JSON: true})
		})
		if err != nil {
			t.Fatalf("cmdSealDoctor JSON healthy: %v", err)
		}
	})
	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("expected ok:true, got: %q", out)
	}
	if !strings.Contains(out, `"healthy":true`) {
		t.Fatalf("expected healthy:true, got: %q", out)
	}
}

func TestCmdSealDoctorJSONIntegrityViolationInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	seedSealStore(t, repo, map[string]seal.Entry{
		`src/./a.go`: {Key: "key1"},
	})

	var out string
	withWorkingDir(t, repo, func() {
		var err error
		out, err = captureStdout(t, func() error {
			return cmdSealDoctor(sealDoctorOptions{JSON: true})
		})
		if err == nil {
			t.Fatal("expected error for integrity violation, got nil")
		}
		var re *renderedError
		if !errors.As(err, &re) || re.code != exitSealDoctorError {
			t.Fatalf("expected renderedError with exitSealDoctorError, got: %v", err)
		}
	})
	if !strings.Contains(out, `"healthy":false`) {
		t.Fatalf("expected healthy:false, got: %q", out)
	}
}

func TestCmdSealDoctorJSONMalformedStoreInProcess(t *testing.T) {
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

	var out string
	withWorkingDir(t, repo, func() {
		out, err = captureStdout(t, func() error {
			return cmdSealDoctor(sealDoctorOptions{JSON: true})
		})
		if err == nil {
			t.Fatal("expected error for malformed store, got nil")
		}
	})
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("expected ok:false, got: %q", out)
	}
}

// --- cmdSealTest JSON mode in-process ---

func TestCmdSealTestJSONPassedInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	var out string
	withWorkingDir(t, wt, func() {
		var err error
		out, err = captureStdout(t, func() error {
			return cmdSealTest(sealTestOptions{JSON: true}, []string{"tracked.txt"})
		})
		if err != nil {
			t.Fatalf("cmdSealTest JSON passed: %v", err)
		}
	})
	if !strings.Contains(out, `"passed":true`) {
		t.Fatalf("expected passed:true, got: %q", out)
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("expected ok:true, got: %q", out)
	}
}

func TestCmdSealTestJSONConflictInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := openManagedWorktree(t, repo, "key1")
	wt2 := openManagedWorktree(t, repo, "key2")

	withWorkingDir(t, wt1, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
	})

	var out string
	withWorkingDir(t, wt2, func() {
		var err error
		out, err = captureStdout(t, func() error {
			return cmdSealTest(sealTestOptions{JSON: true}, []string{"tracked.txt"})
		})
		if err == nil {
			t.Fatal("expected error for conflict, got nil")
		}
		var re *renderedError
		if !errors.As(err, &re) || re.code != exitSealConflict {
			t.Fatalf("expected renderedError with exitSealConflict, got: %v", err)
		}
	})
	if !strings.Contains(out, `"passed":false`) {
		t.Fatalf("expected passed:false, got: %q", out)
	}
}

func TestCmdSealTestJSONCurrentKeyUnresolvedInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	var out string
	withWorkingDir(t, repo, func() {
		var err error
		out, err = captureStdout(t, func() error {
			return cmdSealTest(sealTestOptions{JSON: true}, []string{"tracked.txt"})
		})
		if err == nil {
			t.Fatal("expected error outside managed worktree, got nil")
		}
	})
	if !strings.Contains(out, `"current-key-unresolved"`) {
		t.Fatalf("expected current-key-unresolved in output, got: %q", out)
	}
}

func TestCmdSealTestJSONCurrentKeyUnresolvedOutsideGitInProcess(t *testing.T) {
	outside := t.TempDir()

	var out string
	withWorkingDir(t, outside, func() {
		var err error
		out, err = captureStdout(t, func() error {
			return cmdSealTest(sealTestOptions{JSON: true}, []string{"any.txt"})
		})
		if err == nil {
			t.Fatal("expected error outside git repo, got nil")
		}
	})
	if !strings.Contains(out, `"current-key-unresolved"`) {
		t.Fatalf("expected current-key-unresolved in output, got: %q", out)
	}
	if !strings.Contains(out, `"not-inside-git-repository"`) {
		t.Fatalf("expected not-inside-git-repository reason, got: %q", out)
	}
}

func TestCmdSealTestJSONConflictForMissingFileInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt1 := openManagedWorktree(t, repo, "key1")
	wt2 := openManagedWorktree(t, repo, "key2")

	withWorkingDir(t, wt1, func() {
		if err := cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"}); err != nil {
			t.Fatalf("cmdSealClaim: %v", err)
		}
	})

	// Delete the file — it is still claimed by key1 in the store.
	if err := os.Remove(filepath.Join(repo, "tracked.txt")); err != nil {
		t.Fatalf("remove tracked.txt: %v", err)
	}

	var out string
	withWorkingDir(t, wt2, func() {
		var err error
		out, err = captureStdout(t, func() error {
			return cmdSealTest(sealTestOptions{JSON: true}, []string{"tracked.txt"})
		})
		if err == nil {
			t.Fatal("expected conflict error for missing-but-claimed file, got nil")
		}
		var re *renderedError
		if !errors.As(err, &re) || re.code != exitSealConflict {
			t.Fatalf("expected exitSealConflict, got: %v", err)
		}
	})
	if !strings.Contains(out, `"passed":false`) {
		t.Fatalf("expected passed:false for claimed-by-other-key even when file missing, got: %q", out)
	}
	if !strings.Contains(out, `"claimed-by-other-key"`) {
		t.Fatalf("expected claimed-by-other-key status, got: %q", out)
	}
}

// --- Parse arg coverage ---

func TestParseSealClaimArgsUnknownFlag(t *testing.T) {
	_, _, err := parseSealClaimArgs([]string{"--unknown", "foo.txt"})
	if err == nil {
		t.Fatal("expected error for unknown flag, got nil")
	}
	if !strings.Contains(err.Error(), "unknown option") {
		t.Fatalf("error %q does not mention unknown option", err.Error())
	}
}

func TestParseSealUnclaimArgsUnknownFlag(t *testing.T) {
	_, _, err := parseSealUnclaimArgs([]string{"--unknown", "foo.txt"})
	if err == nil {
		t.Fatal("expected error for unknown flag, got nil")
	}
	if !strings.Contains(err.Error(), "unknown option") {
		t.Fatalf("error %q does not mention unknown option", err.Error())
	}
}

func TestParseCloseArgsUnknownFlag(t *testing.T) {
	_, _, err := parseCloseArgs([]string{"51", "--unknown"})
	if err == nil {
		t.Fatal("expected error for unknown flag, got nil")
	}
}

// --- RenderHuman no-op coverage ---

func TestCloseDataJSONRenderHuman(t *testing.T) {
	d := closeDataJSON{Key: "k", WorktreePath: "/p", Branch: "k"}
	var buf strings.Builder
	if err := d.RenderHuman(&buf); err != nil {
		t.Fatalf("closeDataJSON.RenderHuman: %v", err)
	}
	if !strings.Contains(buf.String(), "/p") {
		t.Fatalf("RenderHuman output missing worktreePath: %q", buf.String())
	}
}

// TestWorktreeJSONRenderHuman exercises the RenderHuman path for worktreeJSON.
// worktreeJSON is used by cmdGet which emits it only via JSON/TOON modes, but
// it must implement Renderable; this test ensures the method body is exercised.
func TestWorktreeJSONRenderHuman(t *testing.T) {
	d := worktreeJSON{
		WorktreePath:   "/tmp/wt",
		Branch:         "feature",
		RepositoryRoot: "/tmp/repo",
		BaseBranch:     "main",
	}
	var buf strings.Builder
	if err := d.RenderHuman(&buf); err != nil {
		t.Fatalf("worktreeJSON.RenderHuman: %v", err)
	}
	if !strings.Contains(buf.String(), "/tmp/wt") {
		t.Fatalf("output missing worktreePath: %q", buf.String())
	}
}

func TestSealClaimDataRenderHuman(t *testing.T) {
	d := seal.ClaimResult{}
	if err := d.RenderHuman(nil); err != nil {
		t.Fatalf("seal.ClaimResult.RenderHuman: %v", err)
	}
}

func TestSealUnclaimDataRenderHuman(t *testing.T) {
	d := seal.UnclaimResult{}
	if err := d.RenderHuman(nil); err != nil {
		t.Fatalf("seal.UnclaimResult.RenderHuman: %v", err)
	}
}

// --- Fail-path JSON mode coverage ---

// TestCloseFailJSONNilPartial exercises closeFail with JSON=true and nil partial
// (early-phase failure before any partial state is built).
func TestCloseFailJSONNilPartial(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return closeFail(closeOptions{JSON: true}, nil, "preflight", fmt.Errorf("no git repo"))
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("expected ok:false envelope, got: %s", out)
	}
	if !strings.Contains(out, `"phase":"preflight"`) {
		t.Fatalf("expected phase:preflight in details, got: %s", out)
	}
}

// TestCloseFailJSONWithPartial exercises closeFail with JSON=true and a non-nil
// partial (failure after some close steps completed).
func TestCloseFailJSONWithPartial(t *testing.T) {
	partial := &closeDataJSON{Key: "51", WorktreePath: "/tmp/51", Branch: "51"}
	out, err := captureStdout(t, func() error {
		return closeFail(closeOptions{JSON: true}, partial, "remove-branch", fmt.Errorf("branch error"))
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("expected ok:false envelope, got: %s", out)
	}
	if !strings.Contains(out, `"partialResult"`) {
		t.Fatalf("expected partialResult in details, got: %s", out)
	}
}

// TestSealClaimFailJSON exercises sealClaimFail with JSON=true (store-level failure).
func TestSealClaimFailJSON(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return sealClaimFail(sealClaimOptions{JSON: true}, "preflight", fmt.Errorf("preflight error"))
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("expected ok:false envelope, got: %s", out)
	}
	if !strings.Contains(out, `"phase":"preflight"`) {
		t.Fatalf("expected phase:preflight in details, got: %s", out)
	}
}

// TestSealClaimStoreFailJSON exercises sealClaimStoreFail with JSON=true,
// verifying that storeError is included in error.details.
func TestSealClaimStoreFailJSON(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return sealClaimStoreFail(sealClaimOptions{JSON: true}, "read-store", "/path/to/seals.json", fmt.Errorf("read error"))
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("expected ok:false envelope, got: %s", out)
	}
	if !strings.Contains(out, `"phase":"read-store"`) {
		t.Fatalf("expected phase:read-store in details, got: %s", out)
	}
	if !strings.Contains(out, `"storeError"`) {
		t.Fatalf("expected storeError in details, got: %s", out)
	}
	if !strings.Contains(out, `"store-read-error"`) {
		t.Fatalf("expected store-read-error status, got: %s", out)
	}
}

// TestSealUnclaimFailJSON exercises sealUnclaimFail with JSON=true.
func TestSealUnclaimFailJSON(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return sealUnclaimFail(sealUnclaimOptions{JSON: true}, "preflight", fmt.Errorf("preflight error"))
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("expected ok:false envelope, got: %s", out)
	}
	if !strings.Contains(out, `"phase":"preflight"`) {
		t.Fatalf("expected phase:preflight in details, got: %s", out)
	}
}

// TestSealUnclaimStoreFailJSON exercises sealUnclaimStoreFail with JSON=true,
// verifying that storeError is included in error.details.
func TestSealUnclaimStoreFailJSON(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return sealUnclaimStoreFail(sealUnclaimOptions{JSON: true}, "write-store", "/path/to/seals.json", fmt.Errorf("write error"))
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("expected ok:false envelope, got: %s", out)
	}
	if !strings.Contains(out, `"phase":"write-store"`) {
		t.Fatalf("expected phase:write-store in details, got: %s", out)
	}
	if !strings.Contains(out, `"storeError"`) {
		t.Fatalf("expected storeError in details, got: %s", out)
	}
	if !strings.Contains(out, `"store-write-error"`) {
		t.Fatalf("expected store-write-error status, got: %s", out)
	}
}

// TestSealStoreValidationErrInterface verifies the Error() and Unwrap() methods
// on seal.StoreValidationErr satisfy the standard error interface.
func TestSealStoreValidationErrInterface(t *testing.T) {
	cause := fmt.Errorf("schema error")
	e := seal.StoreValidationErr{Cause: cause}
	if e.Error() != cause.Error() {
		t.Fatalf("Error() = %q, want %q", e.Error(), cause.Error())
	}
	if e.Unwrap() != cause {
		t.Fatalf("Unwrap() = %v, want %v", e.Unwrap(), cause)
	}
	var target seal.StoreValidationErr
	if !errors.As(e, &target) {
		t.Fatal("errors.As should match seal.StoreValidationErr")
	}
}

// TestCmdSealClaimValidateStoreInProcess exercises the validate-store error path
// in cmdSealClaim when the seal store file contains schema-invalid JSON.
func TestCmdSealClaimValidateStoreInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	storeFile, _, err := seal.StorePaths(repo)
	if err != nil {
		t.Fatalf("pathsSealStore: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, storeFile, `{"paths":{}}`) // valid JSON but fails schema (missing "version")

	withWorkingDir(t, wt, func() {
		out, err := captureStdout(t, func() error {
			return cmdSealClaim(sealClaimOptions{JSON: true}, []string{"tracked.txt"})
		})
		if err == nil {
			t.Fatal("expected error from schema-invalid store, got nil")
		}
		if !strings.Contains(out, `"validate-store"`) {
			t.Fatalf("expected validate-store phase, got: %s", out)
		}
		if !strings.Contains(out, `"store-validation-error"`) {
			t.Fatalf("expected store-validation-error status, got: %s", out)
		}
	})
}

// TestCmdSealUnclaimValidateStoreInProcess exercises the validate-store error path
// in cmdSealUnclaim when the seal store file contains schema-invalid JSON.
func TestCmdSealUnclaimValidateStoreInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)
	wt := openManagedWorktree(t, repo, "key1")

	storeFile, _, err := seal.StorePaths(repo)
	if err != nil {
		t.Fatalf("pathsSealStore: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, storeFile, `{"paths":{}}`) // valid JSON but fails schema (missing "version")

	withWorkingDir(t, wt, func() {
		out, err := captureStdout(t, func() error {
			return cmdSealUnclaim(sealUnclaimOptions{JSON: true}, []string{"tracked.txt"})
		})
		if err == nil {
			t.Fatal("expected error from schema-invalid store, got nil")
		}
		if !strings.Contains(out, `"validate-store"`) {
			t.Fatalf("expected validate-store phase, got: %s", out)
		}
		if !strings.Contains(out, `"store-validation-error"`) {
			t.Fatalf("expected store-validation-error status, got: %s", out)
		}
	})
}

// TestCloseStoreFailJSONWithPartial verifies that closeStoreFail includes
// partialResult when a partial *closeDataJSON is provided.
func TestCloseStoreFailJSONWithPartial(t *testing.T) {
	partial := &closeDataJSON{Key: "key1", WorktreePath: "/wt/key1", Branch: "key1"}
	out, err := captureStdout(t, func() error {
		return closeStoreFail(closeOptions{JSON: true}, partial, "validate-store", "/path/to/paths.json", fmt.Errorf("schema error"))
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(out, `"validate-store"`) {
		t.Fatalf("expected validate-store phase, got: %s", out)
	}
	if !strings.Contains(out, `"partialResult"`) {
		t.Fatalf("expected partialResult in details, got: %s", out)
	}
	if !strings.Contains(out, `"store-validation-error"`) {
		t.Fatalf("expected store-validation-error status, got: %s", out)
	}
}

// TestSealClaimValidateStoreFailJSON verifies that sealClaimStoreFail maps
// "validate-store" to "store-validation-error" in the storeError field.
func TestSealClaimValidateStoreFailJSON(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return sealClaimStoreFail(sealClaimOptions{JSON: true}, "validate-store", "/path/to/seals.json", fmt.Errorf("schema error"))
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(out, `"phase":"validate-store"`) {
		t.Fatalf("expected phase:validate-store, got: %s", out)
	}
	if !strings.Contains(out, `"store-validation-error"`) {
		t.Fatalf("expected store-validation-error status, got: %s", out)
	}
}

// TestSealUnclaimValidateStoreFailJSON verifies that sealUnclaimStoreFail maps
// "validate-store" to "store-validation-error" in the storeError field.
func TestSealUnclaimValidateStoreFailJSON(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return sealUnclaimStoreFail(sealUnclaimOptions{JSON: true}, "validate-store", "/path/to/seals.json", fmt.Errorf("schema error"))
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(out, `"phase":"validate-store"`) {
		t.Fatalf("expected phase:validate-store, got: %s", out)
	}
	if !strings.Contains(out, `"store-validation-error"`) {
		t.Fatalf("expected store-validation-error status, got: %s", out)
	}
}

// TestCloseStoreFailJSON exercises closeStoreFail with JSON=true, verifying that
// storeError is included in error.details with the correct phase mapping.
func TestCloseStoreFailJSON(t *testing.T) {
	for _, tc := range []struct {
		phase      string
		wantStatus string
	}{
		{"read-store", "store-read-error"},
		{"validate-store", "store-validation-error"},
	} {
		t.Run(tc.phase, func(t *testing.T) {
			out, err := captureStdout(t, func() error {
				return closeStoreFail(closeOptions{JSON: true}, nil, tc.phase, "/path/to/paths.json", fmt.Errorf("store error"))
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(out, `"ok":false`) {
				t.Fatalf("expected ok:false envelope, got: %s", out)
			}
			wantPhase := `"phase":"` + tc.phase + `"`
			if !strings.Contains(out, wantPhase) {
				t.Fatalf("expected %s in details, got: %s", wantPhase, out)
			}
			if !strings.Contains(out, `"storeError"`) {
				t.Fatalf("expected storeError in details, got: %s", out)
			}
			if !strings.Contains(out, tc.wantStatus) {
				t.Fatalf("expected %s in storeError, got: %s", tc.wantStatus, out)
			}
		})
	}
}

// --- Additional parse/fail coverage ---

// errWriter is an io.Writer that always returns an error. Used to test error
// propagation in RenderHuman methods.
type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

// TestLsDataRenderHumanWriteError exercises the error return path of
// lsData.RenderHuman when the underlying writer fails.
func TestLsDataRenderHumanWriteError(t *testing.T) {
	d := lsData{Keys: []string{"key1"}}
	err := d.RenderHuman(errWriter{err: fmt.Errorf("write failed")})
	if err == nil {
		t.Fatal("expected write error, got nil")
	}
}

// TestSealLsDataRenderHumanWriteError exercises the error return path of
// seal.LsResult.RenderHuman when the underlying writer fails.
func TestSealLsDataRenderHumanWriteError(t *testing.T) {
	d := seal.LsResult{Claims: []seal.LsClaim{{Key: "k", Path: "p"}}}
	err := d.RenderHuman(errWriter{err: fmt.Errorf("write failed")})
	if err == nil {
		t.Fatal("expected write error, got nil")
	}
}

// TestSealTestDataRenderHumanWriteError exercises the error return path of
// seal.TestResult.RenderHuman when the underlying writer fails.
func TestSealTestDataRenderHumanWriteError(t *testing.T) {
	owner := "other"
	d := seal.TestResult{
		CurrentKey: "mine",
		Results: []seal.TestResultItem{
			{Path: "foo.ts", Status: "claimed-by-other-key", Safe: false, ClaimedBy: &owner},
		},
	}
	err := d.RenderHuman(errWriter{err: fmt.Errorf("write failed")})
	if err == nil {
		t.Fatal("expected write error, got nil")
	}
}

// TestParseSealClaimArgsJSONFlag verifies that --json sets JSON mode and is
// stripped from the path list.
func TestParseSealClaimArgsJSONFlag(t *testing.T) {
	opts, paths, err := parseSealClaimArgs([]string{"--json", "src/foo.ts"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.JSON {
		t.Error("expected JSON=true")
	}
	if len(paths) != 1 || paths[0] != "src/foo.ts" {
		t.Errorf("unexpected paths: %v", paths)
	}
}

// TestParseSealUnclaimArgsJSONFlag verifies that --json sets JSON mode and is
// stripped from the path list for unclaim.
func TestParseSealUnclaimArgsJSONFlag(t *testing.T) {
	opts, paths, err := parseSealUnclaimArgs([]string{"--json", "src/bar.ts"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.JSON {
		t.Error("expected JSON=true")
	}
	if len(paths) != 1 || paths[0] != "src/bar.ts" {
		t.Errorf("unexpected paths: %v", paths)
	}
}

// TestParseSealClaimArgsJSONOnlyIsUsageError verifies that --json alone
// (no paths) returns a usage error.
func TestParseSealClaimArgsJSONOnlyIsUsageError(t *testing.T) {
	_, _, err := parseSealClaimArgs([]string{"--json"})
	if err == nil {
		t.Fatal("expected usage error, got nil")
	}
}

// TestParseSealUnclaimArgsJSONOnlyIsUsageError verifies that --json alone
// (no paths) returns a usage error.
func TestParseSealUnclaimArgsJSONOnlyIsUsageError(t *testing.T) {
	_, _, err := parseSealUnclaimArgs([]string{"--json"})
	if err == nil {
		t.Fatal("expected usage error, got nil")
	}
}

// TestLsFailJSON verifies that lsFail routes the error through the JSON
// envelope when opts.JSON is true.
func TestLsFailJSON(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return lsFail(lsOptions{JSON: true}, fmt.Errorf("ls error"))
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("expected ok:false envelope, got: %s", out)
	}
}

// TestSealLsFailJSON verifies that sealLsFail routes the error through the
// JSON envelope when opts.JSON is true.
func TestSealLsFailJSON(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return sealLsFail(sealLsOptions{JSON: true}, fmt.Errorf("seal ls error"))
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("expected ok:false envelope, got: %s", out)
	}
}

// TestParseCloseArgsEmpty verifies that parseCloseArgs returns a usage error
// when no arguments are provided.
func TestParseCloseArgsEmpty(t *testing.T) {
	_, _, err := parseCloseArgs([]string{})
	if err == nil {
		t.Fatal("expected usage error, got nil")
	}
}

// TestParseCloseArgsInvalidKey verifies that parseCloseArgs rejects an invalid key.
func TestParseCloseArgsInvalidKey(t *testing.T) {
	_, _, err := parseCloseArgs([]string{"../invalid"})
	if err == nil {
		t.Fatal("expected error for invalid key, got nil")
	}
}

// TestParseCloseArgsSuccess verifies the success path returns correct key and opts.
func TestParseCloseArgsSuccess(t *testing.T) {
	key, opts, err := parseCloseArgs([]string{"51", "--json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "51" {
		t.Errorf("expected key=51, got %q", key)
	}
	if !opts.JSON {
		t.Error("expected JSON=true")
	}
}

// TestEncodeEnvelopeSchemaValidationFailure verifies that encodeEnvelope returns an
// error when the constructed envelope does not conform to the schema (e.g. missing
// required warnings array). This exercises the schema-validation error branch.
func TestEncodeEnvelopeSchemaValidationFailure(t *testing.T) {
	// An Envelope with a zero-value Command ("") and nil Warnings serializes to
	// {"ok":false,"command":"","schemaVersion":0,"warnings":null} which fails the
	// envelope schema (command must be one of the allowed enum values, warnings must
	// be an array not null).
	_, err := output.EncodeEnvelope(output.Envelope{})
	if err == nil {
		t.Fatal("expected schema validation error for empty Envelope, got nil")
	}
}

// TestWriteTOONEnvelopeSchemaError verifies that the TOON renderer surfaces
// EncodeEnvelope errors rather than silently swallowing them. It uses
// WriteEnvelope (which shares the same encode+validate path) since the TOON
// renderer's internal helper is not exported.
func TestWriteTOONEnvelopeSchemaError(t *testing.T) {
	var buf bytes.Buffer
	err := output.WriteEnvelope(&buf, output.Envelope{})
	if err == nil {
		t.Fatal("WriteEnvelope with invalid envelope: expected error, got nil")
	}
}

// TestRenderModeTOON verifies that the renderMode helper on each options struct
// returns renderTOON when Toon is set, renderJSON when only JSON is set, and
// renderHuman when neither is set. These are pure-function tests; no git repo needed.
func TestRenderModeTOON(t *testing.T) {
	requireMode := func(t *testing.T, got, want output.RenderMode, label string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s: got %v, want %v", label, got, want)
		}
	}

	t.Run("openOptions", func(t *testing.T) {
		toon, json, human := openOptions{Toon: true}, openOptions{JSON: true}, openOptions{}
		requireMode(t, toon.renderMode(), output.RenderTOON, "Toon=true")
		requireMode(t, json.renderMode(), output.RenderJSON, "JSON=true")
		requireMode(t, human.renderMode(), output.RenderHuman, "default")
	})

	t.Run("closeOptions", func(t *testing.T) {
		toon, json, human := closeOptions{Toon: true}, closeOptions{JSON: true}, closeOptions{}
		requireMode(t, toon.renderMode(), output.RenderTOON, "Toon=true")
		requireMode(t, json.renderMode(), output.RenderJSON, "JSON=true")
		requireMode(t, human.renderMode(), output.RenderHuman, "default")
	})

	t.Run("lsOptions", func(t *testing.T) {
		toon, json, human := lsOptions{Toon: true}, lsOptions{JSON: true}, lsOptions{}
		requireMode(t, toon.renderMode(), output.RenderTOON, "Toon=true")
		requireMode(t, json.renderMode(), output.RenderJSON, "JSON=true")
		requireMode(t, human.renderMode(), output.RenderHuman, "default")
	})

	t.Run("sealLsOptions", func(t *testing.T) {
		toon, json, human := sealLsOptions{Toon: true}, sealLsOptions{JSON: true}, sealLsOptions{}
		requireMode(t, toon.renderMode(), output.RenderTOON, "Toon=true")
		requireMode(t, json.renderMode(), output.RenderJSON, "JSON=true")
		requireMode(t, human.renderMode(), output.RenderHuman, "default")
	})

	t.Run("sealTestOptions", func(t *testing.T) {
		toon, json, human := sealTestOptions{Toon: true}, sealTestOptions{JSON: true}, sealTestOptions{}
		requireMode(t, toon.renderMode(), output.RenderTOON, "Toon=true")
		requireMode(t, json.renderMode(), output.RenderJSON, "JSON=true")
		requireMode(t, human.renderMode(), output.RenderHuman, "default")
	})

	t.Run("sealDoctorOptions", func(t *testing.T) {
		toon, json, human := sealDoctorOptions{Toon: true}, sealDoctorOptions{JSON: true}, sealDoctorOptions{}
		requireMode(t, toon.renderMode(), output.RenderTOON, "Toon=true")
		requireMode(t, json.renderMode(), output.RenderJSON, "JSON=true")
		requireMode(t, human.renderMode(), output.RenderHuman, "default")
	})

	t.Run("sealClaimOptions", func(t *testing.T) {
		toon, json, human := sealClaimOptions{Toon: true}, sealClaimOptions{JSON: true}, sealClaimOptions{}
		requireMode(t, toon.renderMode(), output.RenderTOON, "Toon=true")
		requireMode(t, json.renderMode(), output.RenderJSON, "JSON=true")
		requireMode(t, human.renderMode(), output.RenderHuman, "default")
	})

	t.Run("sealUnclaimOptions", func(t *testing.T) {
		toon, json, human := sealUnclaimOptions{Toon: true}, sealUnclaimOptions{JSON: true}, sealUnclaimOptions{}
		requireMode(t, toon.renderMode(), output.RenderTOON, "Toon=true")
		requireMode(t, json.renderMode(), output.RenderJSON, "JSON=true")
		requireMode(t, human.renderMode(), output.RenderHuman, "default")
	})
}

// TestParseArgsTOONFlag tests the --toon parsing branch in each command's
// argument parser. These are pure parser tests; no git repo is needed.

func TestParseOpenArgsTOONFlag(t *testing.T) {
	_, opts, err := parseOpenArgs([]string{"51", "--toon"})
	if err != nil {
		t.Fatalf("parseOpenArgs with --toon: %v", err)
	}
	if !opts.Toon {
		t.Fatal("opts.Toon = false, want true")
	}
}

func TestParseOpenArgsJSONAndTOONIsUsageError(t *testing.T) {
	for _, args := range [][]string{
		{"51", "--json", "--toon"},
		{"51", "--toon", "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, _, err := parseOpenArgs(args)
			if err == nil {
				t.Fatal("expected usage error, got nil")
			}
			var xe *exitError
			if !errors.As(err, &xe) || xe.code != exitUsageError {
				t.Fatalf("expected exitUsageError, got: %v", err)
			}
		})
	}
}

func TestParseCloseArgsTOONFlag(t *testing.T) {
	key, opts, err := parseCloseArgs([]string{"51", "--toon"})
	if err != nil {
		t.Fatalf("parseCloseArgs with --toon: %v", err)
	}
	if key != "51" {
		t.Fatalf("key = %q, want 51", key)
	}
	if !opts.Toon {
		t.Fatal("opts.Toon = false, want true")
	}
}

func TestParseCloseArgsJSONAndTOONIsUsageError(t *testing.T) {
	for _, args := range [][]string{
		{"51", "--json", "--toon"},
		{"51", "--toon", "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, _, err := parseCloseArgs(args)
			if err == nil {
				t.Fatal("expected usage error, got nil")
			}
			var xe *exitError
			if !errors.As(err, &xe) || xe.code != exitUsageError {
				t.Fatalf("expected exitUsageError, got: %v", err)
			}
		})
	}
}

func TestParseLsArgsTOONFlag(t *testing.T) {
	opts, err := parseLsArgs([]string{"--toon"})
	if err != nil {
		t.Fatalf("parseLsArgs with --toon: %v", err)
	}
	if !opts.Toon {
		t.Fatal("opts.Toon = false, want true")
	}
}

func TestParseLsArgsJSONAndTOONIsUsageError(t *testing.T) {
	for _, args := range [][]string{
		{"--json", "--toon"},
		{"--toon", "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := parseLsArgs(args)
			if err == nil {
				t.Fatal("expected usage error, got nil")
			}
			var xe *exitError
			if !errors.As(err, &xe) || xe.code != exitUsageError {
				t.Fatalf("expected exitUsageError, got: %v", err)
			}
		})
	}
}

func TestParseSealLsArgsTOONFlag(t *testing.T) {
	opts, err := parseSealLsArgs([]string{"--toon"})
	if err != nil {
		t.Fatalf("parseSealLsArgs with --toon: %v", err)
	}
	if !opts.Toon {
		t.Fatal("opts.Toon = false, want true")
	}
}

func TestParseSealTestArgsTOONFlag(t *testing.T) {
	opts, paths, err := parseSealTestArgs([]string{"--toon", "path.go"})
	if err != nil {
		t.Fatalf("parseSealTestArgs with --toon: %v", err)
	}
	if !opts.Toon {
		t.Fatal("opts.Toon = false, want true")
	}
	if len(paths) != 1 || paths[0] != "path.go" {
		t.Fatalf("paths = %v, want [path.go]", paths)
	}
}

func TestParseSealDoctorArgsTOONFlag(t *testing.T) {
	opts, err := parseSealDoctorArgs([]string{"--toon"})
	if err != nil {
		t.Fatalf("parseSealDoctorArgs with --toon: %v", err)
	}
	if !opts.Toon {
		t.Fatal("opts.Toon = false, want true")
	}
}

func TestParseSealClaimArgsTOONFlag(t *testing.T) {
	opts, paths, err := parseSealClaimArgs([]string{"--toon", "src/foo.ts"})
	if err != nil {
		t.Fatalf("parseSealClaimArgs with --toon: %v", err)
	}
	if !opts.Toon {
		t.Fatal("opts.Toon = false, want true")
	}
	if len(paths) != 1 || paths[0] != "src/foo.ts" {
		t.Fatalf("paths = %v, want [src/foo.ts]", paths)
	}
}

func TestParseSealUnclaimArgsTOONFlag(t *testing.T) {
	opts, paths, err := parseSealUnclaimArgs([]string{"--toon", "src/bar.ts"})
	if err != nil {
		t.Fatalf("parseSealUnclaimArgs with --toon: %v", err)
	}
	if !opts.Toon {
		t.Fatal("opts.Toon = false, want true")
	}
	if len(paths) != 1 || paths[0] != "src/bar.ts" {
		t.Fatalf("paths = %v, want [src/bar.ts]", paths)
	}
}
