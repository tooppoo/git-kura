package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/tooppoo/git-kura/internal/gitutil"
	"github.com/tooppoo/git-kura/internal/output"
	"github.com/tooppoo/git-kura/internal/seal"
)

//go:embed schema/commands/seal_ls.schema.json
var sealLsDataSchemaJSON []byte

//go:embed schema/commands/seal_test.schema.json
var sealTestDataSchemaJSON []byte

//go:embed schema/commands/seal_doctor.schema.json
var sealDoctorDataSchemaJSON []byte

//go:embed schema/commands/seal_claim.schema.json
var sealClaimDataSchemaJSON []byte

//go:embed schema/commands/seal_unclaim.schema.json
var sealUnclaimDataSchemaJSON []byte

var sealLsDataSchema = mustCompileSealLsDataSchema()
var sealTestDataSchema = mustCompileSealTestDataSchema()
var sealDoctorDataSchema = mustCompileSealDoctorDataSchema()
var sealClaimDataSchema = mustCompileSchema("seal_claim.schema.json", sealClaimDataSchemaJSON)
var sealUnclaimDataSchema = mustCompileSchema("seal_unclaim.schema.json", sealUnclaimDataSchemaJSON)

func mustCompileSealLsDataSchema() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(sealLsDataSchemaJSON))
	if err != nil {
		panic(fmt.Sprintf("parse seal_ls data schema: %v", err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("seal_ls.schema.json", doc); err != nil {
		panic(fmt.Sprintf("add seal_ls data schema resource: %v", err))
	}
	sch, err := c.Compile("seal_ls.schema.json")
	if err != nil {
		panic(fmt.Sprintf("compile seal_ls data schema: %v", err))
	}
	return sch
}

func mustCompileSealTestDataSchema() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(sealTestDataSchemaJSON))
	if err != nil {
		panic(fmt.Sprintf("parse seal_test data schema: %v", err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("seal_test.schema.json", doc); err != nil {
		panic(fmt.Sprintf("add seal_test data schema resource: %v", err))
	}
	sch, err := c.Compile("seal_test.schema.json")
	if err != nil {
		panic(fmt.Sprintf("compile seal_test data schema: %v", err))
	}
	return sch
}

func mustCompileSealDoctorDataSchema() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(sealDoctorDataSchemaJSON))
	if err != nil {
		panic(fmt.Sprintf("parse seal_doctor data schema: %v", err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("seal_doctor.schema.json", doc); err != nil {
		panic(fmt.Sprintf("add seal_doctor data schema resource: %v", err))
	}
	sch, err := c.Compile("seal_doctor.schema.json")
	if err != nil {
		panic(fmt.Sprintf("compile seal_doctor data schema: %v", err))
	}
	return sch
}

type sealLsOptions struct {
	FilterKey string
	JSON      bool
	Toon      bool
}

func (o sealLsOptions) renderMode() output.RenderMode {
	if o.Toon {
		return output.RenderTOON
	}
	if o.JSON {
		return output.RenderJSON
	}
	return output.RenderHuman
}

type sealTestOptions struct {
	JSON bool
	Toon bool
}

func (o sealTestOptions) renderMode() output.RenderMode {
	if o.Toon {
		return output.RenderTOON
	}
	if o.JSON {
		return output.RenderJSON
	}
	return output.RenderHuman
}

type sealDoctorOptions struct {
	JSON bool
	Toon bool
}

func (o sealDoctorOptions) renderMode() output.RenderMode {
	if o.Toon {
		return output.RenderTOON
	}
	if o.JSON {
		return output.RenderJSON
	}
	return output.RenderHuman
}

type sealClaimOptions struct {
	JSON bool
	Toon bool
}

func (o sealClaimOptions) renderMode() output.RenderMode {
	if o.Toon {
		return output.RenderTOON
	}
	if o.JSON {
		return output.RenderJSON
	}
	return output.RenderHuman
}

type sealUnclaimOptions struct {
	JSON bool
	Toon bool
}

func (o sealUnclaimOptions) renderMode() output.RenderMode {
	if o.Toon {
		return output.RenderTOON
	}
	if o.JSON {
		return output.RenderJSON
	}
	return output.RenderHuman
}

const sealHelp = `Usage: git kura seal <subcommand> [args]

Manage path claims in the repository-wide seal store.

A claim records that the current task — identified by the git-kura managed
worktree you are in — intends to edit a path. This lets conflicting edits
across tasks/worktrees be detected before they reach a merge.

Subcommands:
  ls [key]                       List claimed paths, optionally filtered by key
  claim <path> [path...]         Claim paths for the current key
  unclaim <path> [path...]       Release the current key's claim on paths
  test <path> [path...]          Check paths against the current seal context
  doctor                         Validate the repository-wide seal store

Run "git kura seal <subcommand> --help" for subcommand-specific help.`

const sealLsHelp = `Usage: git kura seal ls [--json] [--toon] [key]

List claimed paths recorded in the seal store.

Without arguments, lists every claimed path across all keys for the whole
repository (the seal store shared by all worktrees). With a key argument,
lists only the paths claimed by that key.

ls is a repository-wide inspection command: it does not derive a current key
from the worktree, so its output is the same regardless of where it is run.
To inspect a single key, pass it explicitly.

Output is one line per claimed path:

  <key>	<path>

Paths are repository-root relative with "/" separators. Lines are sorted
by key, then by path. An empty store produces no output and exits 0.

Flags:
  --json   Print structured output as a JSON envelope. --json must appear
           before the optional key argument.
  --toon   Print structured output as a TOON envelope (experimental; AI-friendly).
           --toon must appear before the optional key argument.`

const sealClaimHelp = `Usage: git kura seal claim [--json] [--toon] <path> [path...]

Claim one or more file paths for the current key in the seal store.

Paths are interpreted relative to the repository root, regardless of the
current working directory. Absolute paths are rejected.
Exits with error if:
  - no current seal key is available (see "Current key" below)
  - any path is absolute or outside the repository
  - any path does not exist or is a directory
  - any path is already claimed by a different key

If a path is already claimed by the current key, it is skipped (idempotent).

Flags:
  --json   Print structured output as a JSON envelope. On error, exits non-zero
           with an ok:false envelope on stdout. --json must appear before the
           path arguments.
  --toon   Print structured output as a TOON envelope (experimental; AI-friendly).
           --toon must appear before the path arguments.

Current key:
  The current key is derived from the git-kura managed worktree you are in:
  run this command from inside the worktree created by "git kura open <key>"
  and that worktree's key becomes the current key. It fails when the current
  directory is not inside a managed worktree, or when that worktree's
  metadata is missing or inconsistent.`

const sealUnclaimHelp = `Usage: git kura seal unclaim [--json] [--toon] <path> [path...]

Release the current key's claim on one or more file paths in the seal store.

Paths are interpreted relative to the repository root, regardless of the
current working directory. Absolute paths are rejected.
Exits with error if:
  - no current seal key is available (see "Current key" below)
  - any path is absolute or outside the repository
  - any path is claimed by a different key

Paths not currently claimed are skipped (idempotent).

Flags:
  --json   Print structured output as a JSON envelope. On error, exits non-zero
           with an ok:false envelope on stdout. --json must appear before the
           path arguments.
  --toon   Print structured output as a TOON envelope (experimental; AI-friendly).
           --toon must appear before the path arguments.

Current key:
  The current key is derived from the git-kura managed worktree you are in:
  run this command from inside the worktree created by "git kura open <key>"
  and that worktree's key becomes the current key. It fails when the current
  directory is not inside a managed worktree, or when that worktree's
  metadata is missing or inconsistent.`

const sealTestHelp = `Usage: git kura seal test [--json] [--toon] <path> [path...]

Check whether one or more paths may be handled in the current seal context.

seal test is read-only: it never modifies the seal store and never takes the
store lock. It answers a single question — given the current key, is every
listed path safe to edit?

Paths are interpreted relative to the repository root, regardless of the
current working directory. Absolute paths and paths outside the repository are
rejected. A path inside the repository that does not exist yet is treated as
unclaimed, so seal test can be used to check a file before creating it.

A path is safe when it is unclaimed, or already claimed by the current key. A
path claimed by a different key is a conflict. seal test exits 0 only when every
path is safe; if any path conflicts it exits with seal-conflict (code 6) and
reports each conflicting path and the key that claims it.

Flags:
  --json   Print structured output as a JSON envelope. On conflict, exits 6 but
           ok is true with data.passed false (conflict is a business result, not
           an execution failure). Current key resolution failures produce an
           ok:false envelope on stdout.
  --toon   Print structured output as a TOON envelope (experimental; AI-friendly).
           Same conflict/failure semantics as --json.

Current key:
  The current key is derived from the git-kura managed worktree you are in:
  run this command from inside the worktree created by "git kura open <key>"
  and that worktree's key becomes the current key. It fails when the current
  directory is not inside a managed worktree, or when that worktree's
  metadata is missing or inconsistent.`

const sealDoctorHelp = `Usage: git kura seal doctor [--json] [--toon]

Validate the repository-wide path seal store.

seal doctor is read-only: it never modifies the seal store and never takes the
store lock. It inspects the seal store attached to the Git repository common
dir, so it can run from any directory inside the repository and does not depend
on the current worktree or current seal key.

An absent store is treated as an empty store. If the store is malformed or
contains invalid paths, seal doctor exits with seal-doctor-error (code 7) and
reports every problematic store entry it finds. On success it prints nothing
and exits 0.

Flags:
  --json   Print structured output as a JSON envelope. A malformed store
           produces an ok:false envelope; integrity violations produce ok:true
           with data.healthy false and data.findings listing each violation.
  --toon   Print structured output as a TOON envelope (experimental; AI-friendly).
           Same semantics as --json.`

func runSeal(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: git kura seal <subcommand> [args]")
	}

	switch args[0] {
	case "-h", "--help":
		fmt.Println(sealHelp)
		return nil
	case "ls":
		if hasHelpFlag(args[1:]) {
			fmt.Println(sealLsHelp)
			return nil
		}
		opts, err := parseSealLsArgs(args[1:])
		if err != nil {
			return err
		}
		return cmdSealLs(opts)
	case "claim":
		return runSealClaim(args[1:])
	case "unclaim":
		return runSealUnclaim(args[1:])
	case "test":
		return runSealTest(args[1:])
	case "doctor":
		return runSealDoctor(args[1:])
	default:
		return fmt.Errorf("unknown seal subcommand: %s", args[0])
	}
}

func runSealClaim(args []string) error {
	if hasHelpFlag(args) {
		fmt.Println(sealClaimHelp)
		return nil
	}
	opts, paths, err := parseSealClaimArgs(args)
	if err != nil {
		return err
	}
	return cmdSealClaim(opts, paths)
}

func runSealUnclaim(args []string) error {
	if hasHelpFlag(args) {
		fmt.Println(sealUnclaimHelp)
		return nil
	}
	opts, paths, err := parseSealUnclaimArgs(args)
	if err != nil {
		return err
	}
	return cmdSealUnclaim(opts, paths)
}

// parseSealClaimArgs parses seal claim arguments. --json/--toon must appear
// before the path arguments. At least one path is required.
func parseSealClaimArgs(args []string) (sealClaimOptions, []string, error) {
	var opts sealClaimOptions
	if len(args) > 0 && args[0] == "--json" {
		opts.JSON = true
		args = args[1:]
	} else if len(args) > 0 && args[0] == "--toon" {
		opts.Toon = true
		args = args[1:]
	}
	if len(args) == 0 {
		return sealClaimOptions{}, nil, exitCodeError(exitUsageError,
			fmt.Errorf("usage: git kura seal claim [--json] [--toon] <path> [path...]"))
	}
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return sealClaimOptions{}, nil, exitCodeError(exitUsageError,
				fmt.Errorf("usage: git kura seal claim [--json] [--toon] <path> [path...]: unknown option %q", a))
		}
	}
	return opts, args, nil
}

// parseSealUnclaimArgs parses seal unclaim arguments. --json/--toon must appear
// before the path arguments. At least one path is required.
func parseSealUnclaimArgs(args []string) (sealUnclaimOptions, []string, error) {
	var opts sealUnclaimOptions
	if len(args) > 0 && args[0] == "--json" {
		opts.JSON = true
		args = args[1:]
	} else if len(args) > 0 && args[0] == "--toon" {
		opts.Toon = true
		args = args[1:]
	}
	if len(args) == 0 {
		return sealUnclaimOptions{}, nil, exitCodeError(exitUsageError,
			fmt.Errorf("usage: git kura seal unclaim [--json] [--toon] <path> [path...]"))
	}
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return sealUnclaimOptions{}, nil, exitCodeError(exitUsageError,
				fmt.Errorf("usage: git kura seal unclaim [--json] [--toon] <path> [path...]: unknown option %q", a))
		}
	}
	return opts, args, nil
}

func runSealTest(args []string) error {
	if hasHelpFlag(args) {
		fmt.Println(sealTestHelp)
		return nil
	}
	opts, paths, err := parseSealTestArgs(args)
	if err != nil {
		return err
	}
	return cmdSealTest(opts, paths)
}

func runSealDoctor(args []string) error {
	if hasHelpFlag(args) {
		fmt.Println(sealDoctorHelp)
		return nil
	}
	opts, err := parseSealDoctorArgs(args)
	if err != nil {
		return err
	}
	return cmdSealDoctor(opts)
}

// parseSealTestArgs parses seal test arguments. --json/--toon must appear
// before the path arguments. At least one path is required.
func parseSealTestArgs(args []string) (sealTestOptions, []string, error) {
	var opts sealTestOptions
	if len(args) > 0 && args[0] == "--json" {
		opts.JSON = true
		args = args[1:]
	} else if len(args) > 0 && args[0] == "--toon" {
		opts.Toon = true
		args = args[1:]
	}
	if len(args) == 0 {
		return sealTestOptions{}, nil, fmt.Errorf("usage: git kura seal test [--json] [--toon] <path> [path...]")
	}
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return sealTestOptions{}, nil, fmt.Errorf("usage: git kura seal test [--json] [--toon] <path> [path...]: unknown option %q", a)
		}
	}
	return opts, args, nil
}

// parseSealDoctorArgs parses seal doctor arguments. --json/--toon are the only
// accepted flags; no positional arguments are accepted.
func parseSealDoctorArgs(args []string) (sealDoctorOptions, error) {
	var opts sealDoctorOptions
	if len(args) > 0 && args[0] == "--json" {
		opts.JSON = true
		args = args[1:]
	} else if len(args) > 0 && args[0] == "--toon" {
		opts.Toon = true
		args = args[1:]
	}
	if len(args) == 0 {
		return opts, nil
	}
	if strings.HasPrefix(args[0], "-") {
		return sealDoctorOptions{}, exitCodeError(exitUsageError, fmt.Errorf("usage: git kura seal doctor [--json] [--toon]: unknown option %q", args[0]))
	}
	return sealDoctorOptions{}, exitCodeError(exitUsageError, fmt.Errorf("usage: git kura seal doctor [--json] [--toon]: unexpected argument %q", args[0]))
}

// parseSealLsArgs parses the argument list for seal ls. --json must appear
// before the optional key argument; "seal ls <key> --json" is a usage error
// and is not treated as a valid JSON request so no ok:false envelope is emitted.
func parseSealLsArgs(args []string) (sealLsOptions, error) {
	var opts sealLsOptions

	if len(args) == 0 {
		return opts, nil
	}

	if args[0] == "--json" {
		opts.JSON = true
		args = args[1:]
	} else if args[0] == "--toon" {
		opts.Toon = true
		args = args[1:]
	} else if strings.HasPrefix(args[0], "-") {
		return sealLsOptions{}, exitCodeError(exitUsageError,
			fmt.Errorf("usage: git kura seal ls [--json] [--toon] [key]: unknown option %q", args[0]))
	}

	if len(args) == 0 {
		return opts, nil
	}

	if strings.HasPrefix(args[0], "-") {
		return sealLsOptions{}, exitCodeError(exitUsageError,
			fmt.Errorf("usage: git kura seal ls [--json] [--toon] [key]: unknown option %q", args[0]))
	}

	if err := validateKey(args[0]); err != nil {
		return sealLsOptions{}, err
	}
	opts.FilterKey = args[0]
	args = args[1:]

	if len(args) > 0 {
		return sealLsOptions{}, exitCodeError(exitUsageError,
			fmt.Errorf("usage: git kura seal ls [--json] [--toon] [key]: unexpected argument %q (--json/--toon must appear before the key)", args[0]))
	}

	return opts, nil
}

// cmdSealLs lists claimed paths from the path seal store. An empty
// opts.FilterKey lists every key. It is repository-wide and read-only.
func cmdSealLs(opts sealLsOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return sealLsFail(opts, fmt.Errorf("not inside a git repository"))
	}
	data, err := seal.Ls(seal.LsInput{RepoRoot: repoRoot, FilterKey: opts.FilterKey})
	if err != nil {
		return sealLsFail(opts, err)
	}

	mode := opts.renderMode()
	if mode != output.RenderHuman {
		if err := validateData(sealLsDataSchema, data); err != nil {
			return err
		}
	}
	return emitResult(mode, output.Result{Command: output.CommandSealLs, Data: data})
}

// sealLsFail routes a seal ls failure to the right output.
func sealLsFail(opts sealLsOptions, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	return emitError(mode, toCommandError(output.CommandSealLs, err))
}

// cmdSealDoctor validates the whole path seal store for the current Git repository.
// It is repository-wide and read-only.
func cmdSealDoctor(opts sealDoctorOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return sealDoctorFail(opts, fmt.Errorf("not inside a git repository"))
	}
	data, err := seal.Doctor(seal.DoctorInput{RepoRoot: repoRoot})
	if err != nil {
		doctorErr := exitCodeError(exitSealDoctorError, fmt.Errorf("seal-doctor-error: %w", err))
		return sealDoctorFail(opts, doctorErr)
	}

	mode := opts.renderMode()
	if mode != output.RenderHuman {
		if err := validateData(sealDoctorDataSchema, data); err != nil {
			return err
		}
		if err := emitResult(mode, output.Result{Command: output.CommandSealDoctor, Data: data}); err != nil {
			return err
		}
		if !data.Healthy {
			return &renderedError{code: exitSealDoctorError}
		}
		return nil
	}

	if err := emitResult(output.RenderHuman, output.Result{Command: output.CommandSealDoctor, Data: data}); err != nil {
		return err
	}
	if !data.Healthy {
		return &renderedError{code: exitSealDoctorError}
	}
	return nil
}

// sealDoctorFail routes a seal doctor failure to the right output.
func sealDoctorFail(opts sealDoctorOptions, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	return emitError(mode, toCommandError(output.CommandSealDoctor, err))
}
