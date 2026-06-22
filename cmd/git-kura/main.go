package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/tooppoo/git-kura/internal/gitutil"
	"github.com/tooppoo/git-kura/internal/output"
	"github.com/tooppoo/git-kura/internal/worktree"
)

// resolve by goreleaser
// https://goreleaser.com/resources/cookbooks/using-main.version/
var version string = "0.1.0"

//go:embed schema/get_data.schema.json
var getDataSchemaJSON []byte

//go:embed schema/commands/open.schema.json
var openDataSchemaJSON []byte

//go:embed schema/commands/close.schema.json
var closeDataSchemaJSON []byte

//go:embed schema/commands/ls.schema.json
var lsDataSchemaJSON []byte

// getDataSchema, openDataSchema, closeDataSchema, and lsDataSchema are the
// command-specific schemas for the data payload nested under the common
// envelope's data field. They version independently of the envelope schema.
var (
	getDataSchema   = mustCompileSchema("get_data.schema.json", getDataSchemaJSON)
	openDataSchema  = mustCompileSchema("open.schema.json", openDataSchemaJSON)
	closeDataSchema = mustCompileSchema("close.schema.json", closeDataSchemaJSON)
	lsDataSchema    = mustCompileSchema("ls.schema.json", lsDataSchemaJSON)

	// openDryRunDataSchema is an alias for openDataSchema. The schema is shared
	// by dry-run and actual open; this alias keeps existing references compiling.
	openDryRunDataSchema = openDataSchema
)

func mustCompileSchema(name string, data []byte) *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		panic(fmt.Sprintf("parse %s: %v", name, err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(name, doc); err != nil {
		panic(fmt.Sprintf("add %s resource: %v", name, err))
	}
	sch, err := c.Compile(name)
	if err != nil {
		panic(fmt.Sprintf("compile %s: %v", name, err))
	}
	return sch
}

const topLevelHelp = `Usage: git kura <command> [key] [flags]

Commands:
  get   <key> [flags]  Print worktree path, branch, or structured metadata
  open  <key> [flags]  Create a worktree for <key>
  close <key>          Remove the worktree for <key>
  ls                   List all open worktrees
  seal  <subcommand>   Manage path claims in the repository-wide seal store
  tools <subcommand>   Install, remove, and inspect auxiliary tool components

Run "git kura <command> --help" for command-specific help.`

const getHelp = `Usage: git kura get <key> [flags]

Print worktree information for <key>.

Scalar and structured output require the worktree to be open.

Flags:
  --path          Print the worktree filesystem path (default)
  --branch        Print the branch name
  --root          Print the repository root path
  --json          Print structured metadata as JSON
  --toon          Print structured metadata as TOML-like text
  --format json   Same as --json
  --format toon   Same as --toon`

const openHelp = `Usage: git kura open <key> [flags]

Create a git worktree for <key> on a new branch <key>.

Flags:
  --dry-run       Show the worktree that would be created, without creating it
  --json          Print the result as a JSON envelope
  --toon          Print the result as a TOON envelope (experimental; AI-friendly)

Without --json/--toon, open prints the worktree path and what was created. A dry run never
creates the worktree, branch, or metadata; pre-creation conflicts are reported
as warnings while the command still succeeds.`

const closeHelp = `Usage: git kura close <key> [flags]

Remove the worktree and Kura-managed branch for <key>, and release the path
seals that <key> holds in the repository-wide seal store.

Flags:
  --json   Print structured output as a JSON envelope
  --toon   Print structured output as a TOON envelope (experimental; AI-friendly)

close takes the seal store lock before any cleanup, so it can fail with
seal-lock-timeout (exit code 5) when the lock is held, leaving everything
unchanged.

For the full cleanup order, paths.json handling, and recovery behavior, see
https://github.com/tooppoo/git-kura/blob/main/docs/commands.md`

const lsHelp = `Usage: git kura ls [--json] [--toon]

List all currently open worktrees, one key per line.

Flags:
  --json   Print structured output as a JSON envelope
  --toon   Print structured output as a TOON envelope (experimental; AI-friendly)`

// exitError carries a specific exit code to be used by main.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// renderedError is the exit-code-only sentinel a structured-output command
// returns after a renderer has already written its output (the JSON error
// envelope or the human stderr message). main exits with the carried code
// without printing anything further, so it never appends a second stderr line
// after a JSON error envelope.
type renderedError struct{ code int }

func (e *renderedError) Error() string { return "" }

// exitCodeError wraps err with a specific exit code. Returns nil when err is nil
// so callers can pass through optional errors without a nil check.
func exitCodeError(code int, err error) error {
	if err == nil {
		return nil
	}
	return &exitError{code: code, err: err}
}

// Exit codes for all git kura commands. Keep in sync with the table in
// docs/commands.md.
const (
	exitSuccess         = 0
	exitGeneralError    = 1
	exitUsageError      = 2
	exitUnsafeRefused   = 3
	exitNotFound        = 4
	exitSealLockTimeout = 5
	exitSealConflict    = 6
	exitSealDoctorError = 7
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		// A renderer has already written this failure's output; only carry the
		// exit code, do not print again.
		var re *renderedError
		if errors.As(err, &re) {
			os.Exit(re.code)
		}
		fmt.Fprintln(os.Stderr, err)
		var xe *exitError
		if errors.As(err, &xe) {
			os.Exit(xe.code)
		}
		os.Exit(exitGeneralError)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: git kura <command> [key] [flags]")
	}

	switch args[0] {
	case "-h", "--help":
		fmt.Println(topLevelHelp)
		return nil

	case "-v", "--version":
		fmt.Println(version)
		return nil

	case "get":
		if hasHelpFlag(args[1:]) {
			fmt.Println(getHelp)
			return nil
		}
		key, opts, err := parseGetArgs(args[1:])
		if err != nil {
			return exitCodeError(exitUsageError, err)
		}
		return cmdGet(key, opts)

	case "open":
		if hasHelpFlag(args[1:]) {
			fmt.Println(openHelp)
			return nil
		}
		key, opts, err := parseOpenArgs(args[1:])
		if err != nil {
			return exitCodeError(exitUsageError, err)
		}
		return cmdOpen(key, opts)

	case "close":
		if hasHelpFlag(args[1:]) {
			fmt.Println(closeHelp)
			return nil
		}
		key, opts, err := parseCloseArgs(args[1:])
		if err != nil {
			return err
		}
		return cmdClose(key, opts)

	case "ls":
		if hasHelpFlag(args[1:]) {
			fmt.Println(lsHelp)
			return nil
		}
		opts, err := parseLsArgs(args[1:])
		if err != nil {
			return err
		}
		return cmdLs(opts)

	case "seal":
		return runSeal(args[1:])

	case "tools":
		return runTools(args[1:])

	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// CLI parsing turns raw argv slices into typed command inputs.
// The command functions below should not inspect raw CLI arguments.

type outputMode string

const (
	outputPath   outputMode = "path"
	outputBranch outputMode = "branch"
	outputRoot   outputMode = "root"
	outputJSON   outputMode = "json"
	outputTOON   outputMode = "toon"
)

type getOptions struct {
	OutputMode outputMode
}

type openOptions struct {
	DryRun bool
	JSON   bool
	Toon   bool
}

func (o openOptions) renderMode() output.RenderMode {
	if o.Toon {
		return output.RenderTOON
	}
	if o.JSON {
		return output.RenderJSON
	}
	return output.RenderHuman
}

type closeOptions struct {
	JSON bool
	Toon bool
}

func (o closeOptions) renderMode() output.RenderMode {
	if o.Toon {
		return output.RenderTOON
	}
	if o.JSON {
		return output.RenderJSON
	}
	return output.RenderHuman
}

type lsOptions struct {
	JSON bool
	Toon bool
}

func (o lsOptions) renderMode() output.RenderMode {
	if o.Toon {
		return output.RenderTOON
	}
	if o.JSON {
		return output.RenderJSON
	}
	return output.RenderHuman
}

// lsData is the structured success payload for ls --json.
type lsData struct {
	Keys []string `json:"keys"`
}

func (d lsData) RenderHuman(w io.Writer) error {
	for _, k := range d.Keys {
		if _, err := fmt.Fprintln(w, k); err != nil {
			return err
		}
	}
	return nil
}

func parseGetArgs(args []string) (string, getOptions, error) {
	if len(args) == 0 {
		return "", getOptions{}, fmt.Errorf("usage: git kura get <key> [--path|--branch|--json|--toon|--format <fmt>]")
	}

	key := args[0]
	if err := validateKey(key); err != nil {
		return "", getOptions{}, err
	}

	var mode outputMode
	flags := args[1:]
	for i := 0; i < len(flags); i++ {
		switch flags[i] {
		case "--path":
			if mode != "" {
				return "", getOptions{}, fmt.Errorf("conflict: --%s and --path cannot be used together", mode)
			}
			mode = outputPath
		case "--branch":
			if mode != "" {
				return "", getOptions{}, fmt.Errorf("conflict: --%s and --branch cannot be used together", mode)
			}
			mode = outputBranch
		case "--root":
			if mode != "" {
				return "", getOptions{}, fmt.Errorf("conflict: --%s and --root cannot be used together", mode)
			}
			mode = outputRoot
		case "--json":
			if mode != "" {
				return "", getOptions{}, fmt.Errorf("conflict: --%s and --json cannot be used together", mode)
			}
			mode = outputJSON
		case "--toon":
			if mode != "" {
				return "", getOptions{}, fmt.Errorf("conflict: --%s and --toon cannot be used together", mode)
			}
			mode = outputTOON
		case "--format":
			if i+1 >= len(flags) {
				return "", getOptions{}, fmt.Errorf("--format requires a value (json or toon)")
			}
			i++
			fmtVal := flags[i]
			switch fmtVal {
			case "json":
				if mode != "" {
					return "", getOptions{}, fmt.Errorf("conflict: --%s and --format json cannot be used together", mode)
				}
				mode = outputJSON
			case "toon":
				if mode != "" {
					return "", getOptions{}, fmt.Errorf("conflict: --%s and --format toon cannot be used together", mode)
				}
				mode = outputTOON
			default:
				return "", getOptions{}, fmt.Errorf("unknown format %q: valid formats are json, toon", fmtVal)
			}
		default:
			return "", getOptions{}, fmt.Errorf("unknown flag: %s", flags[i])
		}
	}

	if mode == "" {
		mode = outputPath
	}

	return key, getOptions{OutputMode: mode}, nil
}

func parseOpenArgs(args []string) (string, openOptions, error) {
	if len(args) == 0 {
		return "", openOptions{}, fmt.Errorf("usage: git kura open <key> [--dry-run] [--json] [--toon]")
	}

	key := args[0]
	if err := validateKey(key); err != nil {
		return "", openOptions{}, err
	}

	var opts openOptions
	for _, flag := range args[1:] {
		switch flag {
		case "--dry-run":
			opts.DryRun = true
		case "--json":
			opts.JSON = true
		case "--toon":
			opts.Toon = true
		default:
			return "", openOptions{}, fmt.Errorf("usage: git kura open <key> [--dry-run] [--json] [--toon]: unexpected argument %q", flag)
		}
	}

	if opts.JSON && opts.Toon {
		return "", openOptions{}, exitCodeError(exitUsageError,
			fmt.Errorf("usage: git kura open <key> [--dry-run] [--json] [--toon]: --json and --toon are mutually exclusive"))
	}
	return key, opts, nil
}

func parseCloseArgs(args []string) (string, closeOptions, error) {
	if len(args) == 0 {
		return "", closeOptions{}, fmt.Errorf("usage: git kura close <key> [--json] [--toon]")
	}

	key := args[0]
	if err := validateKey(key); err != nil {
		return "", closeOptions{}, err
	}

	var opts closeOptions
	for _, flag := range args[1:] {
		switch flag {
		case "--json":
			opts.JSON = true
		case "--toon":
			opts.Toon = true
		default:
			return "", closeOptions{}, exitCodeError(exitUsageError,
				fmt.Errorf("usage: git kura close <key> [--json] [--toon]: unexpected argument %q", flag))
		}
	}

	if opts.JSON && opts.Toon {
		return "", closeOptions{}, exitCodeError(exitUsageError,
			fmt.Errorf("usage: git kura close <key> [--json] [--toon]: --json and --toon are mutually exclusive"))
	}
	return key, opts, nil
}

func parseKeyOnlyArgs(command string, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: git kura %s <key>", command)
	}

	key := args[0]
	if err := validateKey(key); err != nil {
		return "", err
	}

	if len(args) > 1 {
		return "", fmt.Errorf("usage: git kura %s <key>: unexpected argument %q", command, args[1])
	}

	return key, nil
}

// Command execution

// cmdGet resolves worktree information for key. Scalar output (--path/--branch/
// --root) keeps its existing bare-value behavior. JSON output (--json/--format
// json) and TOON output (--toon/--format toon) are routed through the output
// framework: success becomes a common envelope and an execution-time failure
// becomes an ok:false envelope, both on stdout.
func cmdGet(key string, opts getOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return getFail(opts, fmt.Errorf("not inside a git repository"))
	}

	branch := worktree.BranchName(key)
	path, err := worktree.Path(repoRoot, key)
	if err != nil {
		return getFail(opts, fmt.Errorf("resolve worktree path: %w", err))
	}

	_, statErr := os.Stat(path)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return getFail(opts, fmt.Errorf("check worktree path: %w", statErr))
	}

	meta, metaErr := worktree.ReadStructuredMetadata(repoRoot, key, path, exists)
	if metaErr != nil {
		return getFail(opts, metaErr)
	}

	switch opts.OutputMode {
	case outputPath:
		fmt.Println(path)
		return nil
	case outputBranch:
		fmt.Println(branch)
		return nil
	case outputRoot:
		fmt.Println(meta.RepositoryRoot)
		return nil
	}

	dirty := false
	if exists {
		if dirty, err = gitutil.WorktreeDirty(path); err != nil {
			return getFail(opts, fmt.Errorf("check worktree status: %w", err))
		}
	}

	data := worktreeJSON{
		SchemaVersion:  1,
		Key:            key,
		Kind:           "worktree",
		Branch:         branch,
		WorktreePath:   path,
		RepositoryRoot: meta.RepositoryRoot,
		BaseBranch:     meta.BaseBranch,
		Exists:         exists,
		Dirty:          dirty,
	}

	switch opts.OutputMode {
	case outputJSON:
		if err := validateData(getDataSchema, data); err != nil {
			return err
		}
		return emitResult(output.RenderJSON, output.Result{Command: output.CommandGet, Data: data})
	case outputTOON:
		if err := validateData(getDataSchema, data); err != nil {
			return err
		}
		return emitResult(output.RenderTOON, output.Result{Command: output.CommandGet, Data: data})
	}

	return nil
}

// getFail routes a get failure to the right output. JSON and TOON requests
// render an ok:false envelope on stdout (and return the exit-code-only
// sentinel); scalar requests keep the existing plain-error behavior.
func getFail(opts getOptions, err error) error {
	if opts.OutputMode != outputJSON && opts.OutputMode != outputTOON {
		return err
	}
	mode := output.RenderJSON
	if opts.OutputMode == outputTOON {
		mode = output.RenderTOON
	}
	return emitError(mode, toCommandError(output.CommandGet, err))
}

func cmdOpen(key string, opts openOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return openFail(opts, fmt.Errorf("not inside a git repository"))
	}

	path, err := worktree.Path(repoRoot, key)
	if err != nil {
		return openFail(opts, fmt.Errorf("resolve worktree path: %w", err))
	}
	branch := worktree.BranchName(key)

	if opts.DryRun {
		return cmdOpenDryRun(opts, repoRoot, key, path, branch)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return openFail(opts, fmt.Errorf("create worktree parent: %w", err))
	}

	cmd := exec.Command("git", "worktree", "add", path, "-b", branch, "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return openFail(opts, fmt.Errorf("git worktree add: %w\n%s", err, out))
	}
	createdWorktree := true
	createdBranch := true

	base, err := gitutil.HeadBranch(repoRoot)
	if err != nil {
		return openFail(opts, fmt.Errorf("get base branch: %w", err))
	}

	meta := worktree.MetadataFile{RepositoryRoot: repoRoot, BaseBranch: base, WorktreePath: path}
	metaPath, err := worktree.MetadataPath(repoRoot, key)
	if err != nil {
		return openFail(opts, fmt.Errorf("resolve metadata path: %w", err))
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		return openFail(opts, fmt.Errorf("create metadata dir: %w", err))
	}
	metaData, _ := json.Marshal(meta)
	if err := os.WriteFile(metaPath, metaData, 0o644); err != nil {
		return openFail(opts, fmt.Errorf("write metadata: %w", err))
	}
	createdMetadata := true

	data := openDataJSON{
		SchemaVersion:   1,
		Key:             key,
		Kind:            "worktree",
		Branch:          branch,
		WorktreePath:    path,
		RepositoryRoot:  repoRoot,
		BaseBranch:      base,
		Exists:          true,
		Dirty:           false,
		CreatedWorktree: &createdWorktree,
		CreatedBranch:   &createdBranch,
		CreatedMetadata: &createdMetadata,
	}
	mode := opts.renderMode()
	if mode != output.RenderHuman {
		if err := validateData(openDataSchema, data); err != nil {
			return err
		}
	}
	return emitResult(mode, output.Result{Command: output.CommandOpen, Data: data})
}

// cmdOpenDryRun evaluates what open would create for key without any side
// effects. It always succeeds (the dry-run evaluation completed): an ok:true
// result, exit code 0. Pre-creation conditions that would collide at real
// creation time are reported as warnings, not failures. --json renders the
// common envelope; otherwise the human renderer prints the planned worktree and
// any warnings.
func cmdOpenDryRun(opts openOptions, repoRoot, key, path, branch string) error {
	base, err := gitutil.HeadBranch(repoRoot)
	if err != nil {
		return openFail(opts, fmt.Errorf("get base branch: %w", err))
	}

	data := openDataJSON{
		SchemaVersion:  1,
		Key:            key,
		Kind:           "worktree",
		Branch:         branch,
		WorktreePath:   path,
		RepositoryRoot: repoRoot,
		BaseBranch:     base,
		Exists:         false,
		Dirty:          false,
	}
	if err := validateData(openDataSchema, data); err != nil {
		return err
	}

	warnings := dryRunConflictWarnings(repoRoot, key, path, branch)

	return emitResult(opts.renderMode(), output.Result{Command: output.CommandOpen, Data: data, Warnings: warnings})
}

// openFail routes an open failure to the right output. JSON/TOON requests
// render an ok:false envelope on stdout; every other open invocation keeps the
// existing plain-error behavior.
func openFail(opts openOptions, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	return emitError(mode, toCommandError(output.CommandOpen, err))
}

// dryRunConflict is one pre-creation collision found by open --dry-run. Type is
// one of worktree-path, branch, or metadata; the remaining fields are populated
// per type.
type dryRunConflict struct {
	Type   string `json:"type"`
	Path   string `json:"path,omitempty"`
	Branch string `json:"branch,omitempty"`
}

// dryRunConflictDetails is the warning details payload for open-dry-run-conflict.
type dryRunConflictDetails struct {
	Conflicts []dryRunConflict `json:"conflicts"`
}

// dryRunConflictWarnings checks, without side effects, the conditions that would
// collide if open actually ran: an existing worktree path, an existing branch,
// or existing metadata. It returns a single open-dry-run-conflict warning
// listing every collision found, or nil when there is none. Detection is
// best-effort: a check that itself errors is treated as "no conflict" so the
// dry-run never fails on it.
func dryRunConflictWarnings(repoRoot, key, path, branch string) []output.Warning {
	var conflicts []dryRunConflict

	if _, err := os.Stat(path); err == nil {
		conflicts = append(conflicts, dryRunConflict{Type: "worktree-path", Path: path})
	}
	if exists, err := gitutil.BranchExists(repoRoot, branch); err == nil && exists {
		conflicts = append(conflicts, dryRunConflict{Type: "branch", Branch: branch})
	}
	if metaPath, err := worktree.MetadataPath(repoRoot, key); err == nil {
		if _, err := os.Stat(metaPath); err == nil {
			conflicts = append(conflicts, dryRunConflict{Type: "metadata", Path: metaPath})
		}
	}

	if len(conflicts) == 0 {
		return nil
	}

	types := make([]string, len(conflicts))
	for i, c := range conflicts {
		types[i] = c.Type
	}
	return []output.Warning{{
		Code:    "open-dry-run-conflict",
		Message: fmt.Sprintf("the planned worktree may conflict with existing state: %s", strings.Join(types, ", ")),
		Details: dryRunConflictDetails{Conflicts: conflicts},
	}}
}

// emitResult renders a successful Result through the selected renderer, writing
// to the process stdout/stderr. Returns nil on success.
func emitResult(mode output.RenderMode, r output.Result) error {
	return output.SelectRenderer(mode).RenderResult(os.Stdout, os.Stderr, r)
}

// emitError renders a CommandError through the selected renderer and returns the
// exit-code-only sentinel, so main exits with the command's code without
// printing the failure again.
func emitError(mode output.RenderMode, cerr *output.CommandError) error {
	if err := output.SelectRenderer(mode).RenderError(os.Stdout, os.Stderr, cerr); err != nil {
		return err
	}
	return &renderedError{code: cerr.ExitCode}
}

// toCommandError converts a plain command failure into the framework's
// CommandError, preserving the existing exit-code contract. An *exitError keeps
// its code and maps to the matching hyphen-case reason token; any other error is
// a general error (exit code 1).
func toCommandError(cmd output.Command, err error) *output.CommandError {
	code := "general-error"
	exitCode := exitGeneralError
	var xe *exitError
	if errors.As(err, &xe) {
		exitCode = xe.code
		code = reasonForExitCode(xe.code)
	}
	return &output.CommandError{
		Command:  cmd,
		Code:     code,
		Message:  err.Error(),
		ExitCode: exitCode,
	}
}

// reasonForExitCode maps an exit code to the hyphen-case reason token used as the
// envelope error code, keeping the JSON error code aligned with the exit-code
// names.
func reasonForExitCode(code int) string {
	switch code {
	case exitUsageError:
		return "usage-error"
	case exitUnsafeRefused:
		return "unsafe-refused"
	case exitNotFound:
		return "not-found"
	case exitSealLockTimeout:
		return "seal-lock-timeout"
	case exitSealConflict:
		return "seal-conflict"
	case exitSealDoctorError:
		return "seal-doctor-error"
	default:
		return "general-error"
	}
}

// validateData marshals a command's data payload and validates it against its
// command-specific schema before it is wrapped in an envelope. A failure is an
// internal contract violation: the command produced data that does not match its
// declared schema.
func validateData(schema *jsonschema.Schema, data any) error {
	out, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("internal: marshal data: %w", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(out))
	if err != nil {
		return fmt.Errorf("internal: parse data: %w", err)
	}
	if err := schema.Validate(inst); err != nil {
		return fmt.Errorf("internal: json output does not conform to schema: %w", err)
	}
	return nil
}

func parseLsArgs(args []string) (lsOptions, error) {
	var opts lsOptions
	for _, a := range args {
		switch a {
		case "--json":
			opts.JSON = true
		case "--toon":
			opts.Toon = true
		default:
			return lsOptions{}, fmt.Errorf("usage: git kura ls [--json] [--toon]: unexpected argument %q", a)
		}
	}
	if opts.JSON && opts.Toon {
		return lsOptions{}, exitCodeError(exitUsageError,
			fmt.Errorf("usage: git kura ls [--json] [--toon]: --json and --toon are mutually exclusive"))
	}
	return opts, nil
}

func cmdLs(opts lsOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return lsFail(opts, fmt.Errorf("not inside a git repository"))
	}

	dir, err := worktree.StateDir(repoRoot)
	if err != nil {
		return lsFail(opts, fmt.Errorf("resolve state dir: %w", err))
	}

	entries, err := os.ReadDir(filepath.Join(dir, "meta", "worktrees"))
	if err != nil {
		if os.IsNotExist(err) {
			entries = nil
		} else {
			return lsFail(opts, fmt.Errorf("read metadata dir: %w", err))
		}
	}

	var keys []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		keys = append(keys, strings.TrimSuffix(name, ".json"))
	}
	// os.ReadDir returns entries sorted by name, but normalise explicitly.
	sort.Strings(keys)

	data := lsData{Keys: keys}
	if data.Keys == nil {
		data.Keys = []string{}
	}

	mode := opts.renderMode()
	if mode != output.RenderHuman {
		if err := validateData(lsDataSchema, data); err != nil {
			return err
		}
	}
	return emitResult(mode, output.Result{Command: output.CommandLs, Data: data})
}

// lsFail routes an ls failure to the right output. JSON/TOON requests render an
// ok:false envelope on stdout; plain requests keep the existing error behavior.
func lsFail(opts lsOptions, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	return emitError(mode, toCommandError(output.CommandLs, err))
}

// cmdClose tears down the worktree for key and releases every path seal that
// key holds, so a closed worktree never leaves stale claims that would block
// other worktrees from claiming the same paths.
//
// The whole teardown runs under the seal store lock so that seal release is
// atomic with worktree/branch/metadata removal. The cleanup steps run in a
// fixed order — worktree, branch, seals, metadata — and abort before touching
// paths.json if the worktree or branch cleanup fails, so a key's seals are only
// released once its worktree and branch are actually gone.
func cmdClose(key string, opts closeOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return closeFail(opts, nil, "preflight", fmt.Errorf("not inside a git repository"))
	}

	path, err := worktree.Path(repoRoot, key)
	if err != nil {
		return closeFail(opts, nil, "preflight", fmt.Errorf("resolve worktree path: %w", err))
	}
	branch := worktree.BranchName(key)

	partial := &closeDataJSON{Key: key, WorktreePath: path, Branch: branch}

	storeFile, lockFile, err := pathsSealStore(repoRoot)
	if err != nil {
		return closeFail(opts, partial, "preflight", err)
	}

	// Acquire the seal store lock before any destructive cleanup. A lock that
	// cannot be acquired fails with seal-lock-timeout (code 5) and leaves the
	// worktree, branch, paths.json, and metadata untouched.
	timeout, err := resolveSealLockTimeout(repoRoot)
	if err != nil {
		return closeFail(opts, partial, "preflight", err)
	}
	release, err := acquireSealLock(lockFile, timeout)
	if err != nil {
		return closeFail(opts, partial, "preflight", err)
	}
	defer release()

	// Read and validate paths.json before destructive cleanup. An absent store
	// is treated as empty and cleanup continues; a malformed or schema-invalid
	// store aborts close before any worktree/branch/paths.json/metadata change.
	store, err := readSealStore(storeFile)
	if err != nil {
		phase := "read-store"
		if errors.As(err, new(sealStoreValidationErr)) {
			phase = "validate-store"
		}
		return closeStoreFail(opts, partial, phase, storeFile, err)
	}

	// 1. Worktree cleanup. A missing worktree directory is a no-op, so stale
	//    state (a manually removed directory or an earlier incomplete close) can
	//    still be recovered. When the directory is gone, prune the dangling
	//    administrative entry so Git no longer treats the branch as checked out.
	if _, statErr := os.Stat(path); statErr == nil {
		cmd := exec.Command("git", "worktree", "remove", path)
		cmd.Dir = repoRoot
		out, removeErr := cmd.CombinedOutput()
		if removeErr != nil {
			return closeFail(opts, partial, "remove-worktree", fmt.Errorf("git worktree remove: %w\n%s", removeErr, out))
		}
		partial.RemovedWorktree = true
	} else if os.IsNotExist(statErr) {
		if err := gitutil.PruneWorktrees(repoRoot); err != nil {
			return closeFail(opts, partial, "remove-worktree", err)
		}
	} else {
		return closeFail(opts, partial, "remove-worktree", fmt.Errorf("check worktree path: %w", statErr))
	}

	// 2. Kura-managed branch cleanup. A branch that is already gone is a no-op; a
	//    branch that exists but cannot be deleted (e.g. unmerged commits) aborts
	//    close before paths.json is updated, so the key's seals are preserved.
	branchExists, err := gitutil.BranchExists(repoRoot, branch)
	if err != nil {
		return closeFail(opts, partial, "remove-branch", err)
	}
	if branchExists {
		if err := gitutil.DeleteBranch(repoRoot, branch); err != nil {
			return closeFail(opts, partial, "remove-branch", err)
		}
		partial.RemovedBranch = true
	}

	// 3. Release every seal claimed by this key, leaving other keys' claims
	//    untouched. Only rewrite paths.json when something actually changed.
	releasedCount := 0
	for sealedPath, entry := range store.Paths {
		if entry.Key == key {
			delete(store.Paths, sealedPath)
			releasedCount++
		}
	}
	if releasedCount > 0 {
		if err := writeSealStore(storeFile, store); err != nil {
			return closeFail(opts, partial, "release-seals", fmt.Errorf("update seal store: %w", err))
		}
	}
	partial.ReleasedSealCount = releasedCount

	// 4. Metadata cleanup. Failure here does not roll back the completed
	//    worktree/branch/seal cleanup; re-running close retries just this step,
	//    which recovers state left behind by a manual deletion or earlier
	//    incomplete close.
	metaPath, err := worktree.MetadataPath(repoRoot, key)
	if err != nil {
		return closeFail(opts, partial, "remove-metadata", fmt.Errorf("resolve metadata path: %w", err))
	}
	if err := os.Remove(metaPath); err != nil {
		if !os.IsNotExist(err) {
			return closeFail(opts, partial, "remove-metadata", fmt.Errorf("remove metadata: %w", err))
		}
		// already absent — RemovedMetadata stays false
	} else {
		partial.RemovedMetadata = true
	}

	mode := opts.renderMode()
	if mode != output.RenderHuman {
		if err := validateData(closeDataSchema, *partial); err != nil {
			return err
		}
	}
	return emitResult(mode, output.Result{Command: output.CommandClose, Data: *partial})
}

// closeFail routes a close failure to the right output. JSON/TOON requests
// render an ok:false envelope on stdout with partial-effect details; non-JSON/
// TOON requests keep the existing plain-error behavior.
func closeFail(opts closeOptions, partial *closeDataJSON, phase string, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	var details *closeErrorDetails
	if partial != nil {
		details = &closeErrorDetails{Phase: phase, PartialResult: partial}
	} else {
		details = &closeErrorDetails{Phase: phase}
	}
	cerr := toCommandError(output.CommandClose, err)
	cerr.Details = details
	return emitError(mode, cerr)
}

// closeStoreFail routes a close store-level failure (read-store or validate-store)
// to the right output, including error.details.storeError as required by Issue #63.
func closeStoreFail(opts closeOptions, partial *closeDataJSON, phase string, storeFile string, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	statusMap := map[string]string{
		"read-store":     "store-read-error",
		"validate-store": "store-validation-error",
	}
	se := &sealStoreError{Status: statusMap[phase], Path: storeFile}
	var details *closeErrorDetails
	if partial != nil {
		details = &closeErrorDetails{Phase: phase, PartialResult: partial, StoreError: se}
	} else {
		details = &closeErrorDetails{Phase: phase, StoreError: se}
	}
	cerr := toCommandError(output.CommandClose, err)
	cerr.Details = details
	return emitError(mode, cerr)
}

type worktreeJSON struct {
	SchemaVersion  int    `json:"schemaVersion"  toon:"schemaVersion"`
	Key            string `json:"key"            toon:"key"`
	Kind           string `json:"kind"           toon:"kind"`
	Branch         string `json:"branch"         toon:"branch"`
	WorktreePath   string `json:"worktreePath"   toon:"worktreePath"`
	RepositoryRoot string `json:"repositoryRoot" toon:"repositoryRoot"`
	BaseBranch     string `json:"baseBranch"     toon:"baseBranch"`
	Exists         bool   `json:"exists"         toon:"exists"`
	Dirty          bool   `json:"dirty"          toon:"dirty"`
}

// openDataJSON is the data payload for open --json (both dry-run and actual).
// Effect fields (CreatedWorktree, CreatedBranch, CreatedMetadata) are present
// only for actual open; dry-run omits them.
type openDataJSON struct {
	SchemaVersion   int    `json:"schemaVersion"`
	Key             string `json:"key"`
	Kind            string `json:"kind"`
	Branch          string `json:"branch"`
	WorktreePath    string `json:"worktreePath"`
	RepositoryRoot  string `json:"repositoryRoot"`
	BaseBranch      string `json:"baseBranch"`
	Exists          bool   `json:"exists"`
	Dirty           bool   `json:"dirty"`
	CreatedWorktree *bool  `json:"createdWorktree,omitempty"`
	CreatedBranch   *bool  `json:"createdBranch,omitempty"`
	CreatedMetadata *bool  `json:"createdMetadata,omitempty"`
}

func (d openDataJSON) RenderHuman(w io.Writer) error {
	if d.CreatedWorktree != nil || d.CreatedBranch != nil || d.CreatedMetadata != nil {
		// Actual open: report what was created, consistent with close format.
		if _, err := fmt.Fprintf(w, "opened: %s (branch: %s)\n", d.WorktreePath, d.Branch); err != nil {
			return err
		}
		if d.CreatedWorktree != nil && *d.CreatedWorktree {
			if _, err := fmt.Fprintln(w, "  created worktree"); err != nil {
				return err
			}
		}
		if d.CreatedBranch != nil && *d.CreatedBranch {
			if _, err := fmt.Fprintln(w, "  created branch"); err != nil {
				return err
			}
		}
		if d.CreatedMetadata != nil && *d.CreatedMetadata {
			if _, err := fmt.Fprintln(w, "  created metadata"); err != nil {
				return err
			}
		}
		return nil
	}
	// Dry-run: show all metadata so the user can review what would be created.
	_, err := fmt.Fprintf(w,
		"worktree path:   %s\nbranch:          %s\nrepository root: %s\nbase branch:     %s\n",
		d.WorktreePath, d.Branch, d.RepositoryRoot, d.BaseBranch)
	return err
}

// closeDataJSON is the success payload for close --json.
type closeDataJSON struct {
	Key               string `json:"key"`
	WorktreePath      string `json:"worktreePath"`
	Branch            string `json:"branch"`
	RemovedWorktree   bool   `json:"removedWorktree"`
	RemovedBranch     bool   `json:"removedBranch"`
	RemovedMetadata   bool   `json:"removedMetadata"`
	ReleasedSealCount int    `json:"releasedSealCount"`
}

func (d closeDataJSON) RenderHuman(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "closed: %s (branch: %s)\n", d.WorktreePath, d.Branch); err != nil {
		return err
	}
	if d.RemovedWorktree {
		if _, err := fmt.Fprintln(w, "  removed worktree"); err != nil {
			return err
		}
	}
	if d.RemovedBranch {
		if _, err := fmt.Fprintln(w, "  removed branch"); err != nil {
			return err
		}
	}
	if d.RemovedMetadata {
		if _, err := fmt.Fprintln(w, "  removed metadata"); err != nil {
			return err
		}
	}
	if d.ReleasedSealCount > 0 {
		if _, err := fmt.Fprintf(w, "  released %d seal(s)\n", d.ReleasedSealCount); err != nil {
			return err
		}
	}
	return nil
}

// closeErrorDetails carries partial effects from a failed close --json.
type closeErrorDetails struct {
	Phase         string          `json:"phase"`
	PartialResult *closeDataJSON  `json:"partialResult,omitempty"`
	StoreError    *sealStoreError `json:"storeError,omitempty"`
}

// RenderHuman writes the human-readable form of the worktree data. It is used by
// the human renderer for open --dry-run and prints at least the planned worktree
// path, branch, repository root, and base branch.
func (d worktreeJSON) RenderHuman(w io.Writer) error {
	_, err := fmt.Fprintf(w,
		"worktree path:   %s\nbranch:          %s\nrepository root: %s\nbase branch:     %s\n",
		d.WorktreePath, d.Branch, d.RepositoryRoot, d.BaseBranch)
	return err
}

// Validation

var validKeyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func validateKey(key string) error {
	if !validKeyRe.MatchString(key) {
		return fmt.Errorf("invalid key %q: key must match [A-Za-z0-9][A-Za-z0-9._-]{0,127}", key)
	}
	if strings.Contains(key, "..") {
		return fmt.Errorf("invalid key %q: key must not contain \"..\"", key)
	}
	if strings.HasSuffix(key, ".") {
		return fmt.Errorf("invalid key %q: key must not end with \".\"", key)
	}
	if strings.HasSuffix(key, ".lock") {
		return fmt.Errorf("invalid key %q: key must not end with \".lock\"", key)
	}
	return nil
}
