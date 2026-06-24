package cli

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/tooppoo/git-kura/internal/gitutil"
	"github.com/tooppoo/git-kura/internal/output"
	"github.com/tooppoo/git-kura/internal/seal"
	"github.com/tooppoo/git-kura/internal/worktree"
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

func (r *runner) runSeal(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: git kura seal <subcommand> [args]")
	}

	switch args[0] {
	case "-h", "--help":
		if _, err := fmt.Fprintln(r.stdout, sealHelp); err != nil {
			return err
		}
		return nil
	case "ls":
		if hasHelpFlag(args[1:]) {
			if _, err := fmt.Fprintln(r.stdout, sealLsHelp); err != nil {
				return err
			}
			return nil
		}
		opts, err := parseSealLsArgs(args[1:])
		if err != nil {
			return err
		}
		return r.cmdSealLs(opts)
	case "claim":
		return r.runSealClaim(args[1:])
	case "unclaim":
		return r.runSealUnclaim(args[1:])
	case "test":
		return r.runSealTest(args[1:])
	case "doctor":
		return r.runSealDoctor(args[1:])
	default:
		return fmt.Errorf("unknown seal subcommand: %s", args[0])
	}
}

func (r *runner) runSealClaim(args []string) error {
	if hasHelpFlag(args) {
		if _, err := fmt.Fprintln(r.stdout, sealClaimHelp); err != nil {
			return err
		}
		return nil
	}
	opts, paths, err := parseSealClaimArgs(args)
	if err != nil {
		return err
	}
	return r.cmdSealClaim(opts, paths)
}

func (r *runner) runSealUnclaim(args []string) error {
	if hasHelpFlag(args) {
		if _, err := fmt.Fprintln(r.stdout, sealUnclaimHelp); err != nil {
			return err
		}
		return nil
	}
	opts, paths, err := parseSealUnclaimArgs(args)
	if err != nil {
		return err
	}
	return r.cmdSealUnclaim(opts, paths)
}

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

func (r *runner) runSealTest(args []string) error {
	if hasHelpFlag(args) {
		if _, err := fmt.Fprintln(r.stdout, sealTestHelp); err != nil {
			return err
		}
		return nil
	}
	opts, paths, err := parseSealTestArgs(args)
	if err != nil {
		return exitCodeError(exitUsageError, err)
	}
	return r.cmdSealTest(opts, paths)
}

func (r *runner) runSealDoctor(args []string) error {
	if hasHelpFlag(args) {
		if _, err := fmt.Fprintln(r.stdout, sealDoctorHelp); err != nil {
			return err
		}
		return nil
	}
	opts, err := parseSealDoctorArgs(args)
	if err != nil {
		return err
	}
	return r.cmdSealDoctor(opts)
}

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

func (r *runner) cmdSealLs(opts sealLsOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return r.sealLsFail(opts, fmt.Errorf("not inside a git repository"))
	}
	data, err := seal.Ls(seal.LsInput{RepoRoot: repoRoot, FilterKey: opts.FilterKey})
	if err != nil {
		return r.sealLsFail(opts, err)
	}

	mode := opts.renderMode()
	if mode != output.RenderHuman {
		if err := validateData(sealLsDataSchema, data); err != nil {
			return err
		}
	}
	return r.emitResult(mode, output.Result{Command: output.CommandSealLs, Data: data})
}

func (r *runner) sealLsFail(opts sealLsOptions, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	return r.emitError(mode, toCommandError(output.CommandSealLs, err))
}

func (r *runner) cmdSealDoctor(opts sealDoctorOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return r.sealDoctorFail(opts, fmt.Errorf("not inside a git repository"))
	}
	data, err := seal.Doctor(seal.DoctorInput{RepoRoot: repoRoot})
	if err != nil {
		doctorErr := exitCodeError(exitSealDoctorError, fmt.Errorf("seal-doctor-error: %w", err))
		return r.sealDoctorFail(opts, doctorErr)
	}

	mode := opts.renderMode()
	if mode != output.RenderHuman {
		if err := validateData(sealDoctorDataSchema, data); err != nil {
			return err
		}
		if err := r.emitResult(mode, output.Result{Command: output.CommandSealDoctor, Data: data}); err != nil {
			return err
		}
		if !data.Healthy {
			return &renderedError{code: int(exitSealDoctorError)}
		}
		return nil
	}

	if err := r.emitResult(output.RenderHuman, output.Result{Command: output.CommandSealDoctor, Data: data}); err != nil {
		return err
	}
	if !data.Healthy {
		return &renderedError{code: int(exitSealDoctorError)}
	}
	return nil
}

func (r *runner) sealDoctorFail(opts sealDoctorOptions, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	return r.emitError(mode, toCommandError(output.CommandSealDoctor, err))
}

// --- seal_path.go logic (cmdSealClaim, cmdSealUnclaim, cmdSealTest) ---

// sealMutationErrorDetails is the error.details payload for seal claim/unclaim
// failures.
type sealMutationErrorDetails struct {
	Phase      string                  `json:"phase,omitempty"`
	CurrentKey string                  `json:"currentKey,omitempty"`
	Paths      []seal.MutationPathItem `json:"paths,omitempty"`
	Conflicts  []seal.ConflictItem     `json:"conflicts,omitempty"`
	Duplicates []seal.DuplicateItem    `json:"duplicates,omitempty"`
	StoreError *sealStoreError         `json:"storeError,omitempty"`
}

// currentKeyUnresolvedDetails is the error.details payload when current key
// resolution fails for seal test --json.
type currentKeyUnresolvedDetails struct {
	Reason         string  `json:"reason"`
	RepositoryRoot *string `json:"repositoryRoot"`
	MetadataPath   *string `json:"metadataPath"`
}

// readSealContext resolves the current seal key from the active git-kura managed worktree.
func readSealContext() (string, error) {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return worktree.CurrentKey(repoRoot)
}

// sealConflictError builds the seal-conflict error listing every conflicting path.
func sealConflictError(conflicts []seal.PathConflict) error {
	parts := make([]string, 0, len(conflicts))
	for _, c := range conflicts {
		parts = append(parts, fmt.Sprintf("path %q is already claimed by key %q", c.Path, c.SealedBy))
	}
	return exitCodeError(exitSealConflict,
		fmt.Errorf("seal-conflict: %s", strings.Join(parts, "; ")))
}

func (r *runner) cmdSealClaim(opts sealClaimOptions, rawPaths []string) error {
	key, err := readSealContext()
	if err != nil {
		return r.sealClaimFail(opts, "preflight", err)
	}

	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return r.sealClaimFail(opts, "preflight", fmt.Errorf("not inside a git repository"))
	}

	result, claimWarnings, claimErr := seal.Claim(seal.ClaimInput{
		RepoRoot:   repoRoot,
		CurrentKey: key,
		RawPaths:   rawPaths,
	})
	for _, w := range claimWarnings {
		_, _ = fmt.Fprintf(r.stderr, "warning: %s\n", w)
	}
	if claimErr != nil {
		var lockErr seal.LockTimeoutErr
		if errors.As(claimErr, &lockErr) {
			return r.sealClaimFail(opts, "preflight", exitCodeError(exitSealLockTimeout, lockErr))
		}
		var storeErr seal.StoreErr
		if errors.As(claimErr, &storeErr) {
			return r.sealClaimStoreFail(opts, storeErr.Phase, storeErr.StorePath, storeErr.Cause)
		}
		var conflictErr seal.ConflictErr
		if errors.As(claimErr, &conflictErr) {
			details := sealMutationErrorDetails{
				Phase:      conflictErr.Phase,
				CurrentKey: conflictErr.CurrentKey,
				Paths:      conflictErr.Paths,
				Conflicts:  conflictErr.Conflicts,
				Duplicates: conflictErr.Duplicates,
			}
			cerr := &output.CommandError{
				Command:  output.CommandSealClaim,
				Code:     "seal-conflict",
				Message:  "seal-conflict: one or more paths could not be claimed",
				ExitCode: int(exitSealConflict),
				Details:  details,
			}
			if opts.renderMode() == output.RenderHuman {
				var pConflicts []seal.PathConflict
				for _, item := range conflictErr.Paths {
					if !item.Blocking {
						continue
					}
					if item.Status == "owned-by-other" {
						pConflicts = append(pConflicts, seal.PathConflict{Path: item.Path, SealedBy: item.OwnerKey})
					} else {
						return fmt.Errorf("%s", item.HumanError)
					}
				}
				if len(pConflicts) > 0 {
					return sealConflictError(pConflicts)
				}
			}
			return r.emitError(opts.renderMode(), cerr)
		}
		return r.sealClaimFail(opts, "preflight", claimErr)
	}

	mode := opts.renderMode()
	if mode != output.RenderHuman {
		if err := validateData(sealClaimDataSchema, result); err != nil {
			return err
		}
	}
	return r.emitResult(mode, output.Result{Command: output.CommandSealClaim, Data: result})
}

func (r *runner) sealClaimFail(opts sealClaimOptions, phase string, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	details := sealMutationErrorDetails{Phase: phase}
	cerr := toCommandError(output.CommandSealClaim, err)
	cerr.Details = details
	return r.emitError(mode, cerr)
}

func (r *runner) sealClaimStoreFail(opts sealClaimOptions, phase string, storeFile string, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	statusMap := map[string]string{
		"read-store":     "store-read-error",
		"validate-store": "store-validation-error",
		"write-store":    "store-write-error",
	}
	details := sealMutationErrorDetails{
		Phase:      phase,
		StoreError: &sealStoreError{Status: statusMap[phase], Path: storeFile},
	}
	cerr := toCommandError(output.CommandSealClaim, err)
	cerr.Details = details
	return r.emitError(mode, cerr)
}

func (r *runner) cmdSealUnclaim(opts sealUnclaimOptions, rawPaths []string) error {
	key, err := readSealContext()
	if err != nil {
		return r.sealUnclaimFail(opts, "preflight", err)
	}

	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return r.sealUnclaimFail(opts, "preflight", fmt.Errorf("not inside a git repository"))
	}

	result, unclaimWarnings, unclaimErr := seal.Unclaim(seal.UnclaimInput{
		RepoRoot:   repoRoot,
		CurrentKey: key,
		RawPaths:   rawPaths,
	})
	for _, w := range unclaimWarnings {
		_, _ = fmt.Fprintf(r.stderr, "warning: %s\n", w)
	}
	if unclaimErr != nil {
		var lockErr seal.LockTimeoutErr
		if errors.As(unclaimErr, &lockErr) {
			return r.sealUnclaimFail(opts, "preflight", exitCodeError(exitSealLockTimeout, lockErr))
		}
		var storeErr seal.StoreErr
		if errors.As(unclaimErr, &storeErr) {
			return r.sealUnclaimStoreFail(opts, storeErr.Phase, storeErr.StorePath, storeErr.Cause)
		}
		var conflictErr seal.ConflictErr
		if errors.As(unclaimErr, &conflictErr) {
			details := sealMutationErrorDetails{
				Phase:      conflictErr.Phase,
				CurrentKey: conflictErr.CurrentKey,
				Paths:      conflictErr.Paths,
				Conflicts:  conflictErr.Conflicts,
				Duplicates: conflictErr.Duplicates,
			}
			cerr := &output.CommandError{
				Command:  output.CommandSealUnclaim,
				Code:     "seal-conflict",
				Message:  "seal-conflict: one or more paths could not be released",
				ExitCode: int(exitSealConflict),
				Details:  details,
			}
			if opts.renderMode() == output.RenderHuman {
				var pConflicts []seal.PathConflict
				for _, item := range conflictErr.Paths {
					if !item.Blocking {
						continue
					}
					if item.Status == "owned-by-other" {
						pConflicts = append(pConflicts, seal.PathConflict{Path: item.Path, SealedBy: item.OwnerKey})
					} else {
						return fmt.Errorf("%s", item.HumanError)
					}
				}
				if len(pConflicts) > 0 {
					return sealConflictError(pConflicts)
				}
			}
			return r.emitError(opts.renderMode(), cerr)
		}
		return r.sealUnclaimFail(opts, "preflight", unclaimErr)
	}

	mode := opts.renderMode()
	if mode != output.RenderHuman {
		if err := validateData(sealUnclaimDataSchema, result); err != nil {
			return err
		}
	}
	return r.emitResult(mode, output.Result{Command: output.CommandSealUnclaim, Data: result})
}

func (r *runner) sealUnclaimFail(opts sealUnclaimOptions, phase string, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	details := sealMutationErrorDetails{Phase: phase}
	cerr := toCommandError(output.CommandSealUnclaim, err)
	cerr.Details = details
	return r.emitError(mode, cerr)
}

func (r *runner) sealUnclaimStoreFail(opts sealUnclaimOptions, phase string, storeFile string, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	statusMap := map[string]string{
		"read-store":     "store-read-error",
		"validate-store": "store-validation-error",
		"write-store":    "store-write-error",
	}
	details := sealMutationErrorDetails{
		Phase:      phase,
		StoreError: &sealStoreError{Status: statusMap[phase], Path: storeFile},
	}
	cerr := toCommandError(output.CommandSealUnclaim, err)
	cerr.Details = details
	return r.emitError(mode, cerr)
}

func (r *runner) cmdSealTest(opts sealTestOptions, rawPaths []string) error {
	repoTop, err := gitutil.RepoRoot()
	if err != nil {
		return r.sealTestCurrentKeyFail(opts, fmt.Errorf("not inside a git repository"), "")
	}

	key, keyErr := worktree.CurrentKey(repoTop)
	if keyErr != nil {
		return r.sealTestCurrentKeyFail(opts, keyErr, repoTop)
	}

	data, testErr := seal.Test(seal.TestInput{
		RepoRoot:   repoTop,
		CurrentKey: key,
		RawPaths:   rawPaths,
	})
	if testErr != nil {
		return r.sealTestFail(opts, testErr)
	}

	mode := opts.renderMode()
	if mode != output.RenderHuman {
		if err := validateData(sealTestDataSchema, data); err != nil {
			return err
		}
		if err := r.emitResult(mode, output.Result{Command: output.CommandSealTest, Data: data}); err != nil {
			return err
		}
		if !data.Passed {
			return &renderedError{code: int(exitSealConflict)}
		}
		return nil
	}

	if err := r.emitResult(output.RenderHuman, output.Result{Command: output.CommandSealTest, Data: data}); err != nil {
		return err
	}
	if !data.Passed {
		return &renderedError{code: int(exitSealConflict)}
	}
	return nil
}

func (r *runner) sealTestFail(opts sealTestOptions, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	return r.emitError(mode, toCommandError(output.CommandSealTest, err))
}

func (r *runner) sealTestCurrentKeyFail(opts sealTestOptions, keyErr error, repoTop string) error {
	reason, metaPath := classifyCurrentKeyError(keyErr, repoTop)
	msg := fmt.Sprintf("current-key-unresolved: %s", keyErr.Error())

	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return fmt.Errorf("%s", msg)
	}

	var repoTopPtr *string
	if repoTop != "" {
		repoTopCopy := repoTop
		repoTopPtr = &repoTopCopy
	}
	cerr := &output.CommandError{
		Command:  output.CommandSealTest,
		Code:     "current-key-unresolved",
		Message:  msg,
		ExitCode: int(exitGeneralError),
		Details: currentKeyUnresolvedDetails{
			Reason:         reason,
			RepositoryRoot: repoTopPtr,
			MetadataPath:   metaPath,
		},
	}
	return r.emitError(mode, cerr)
}

func classifyCurrentKeyError(err error, repoTop string) (reason string, metaPath *string) {
	msg := err.Error()
	if strings.Contains(msg, "not inside a git repository") {
		return "not-inside-git-repository", nil
	}
	if strings.Contains(msg, "not inside a git-kura managed worktree") {
		return "not-in-managed-worktree", nil
	}
	if strings.Contains(msg, "has no git-kura metadata") {
		if mp := tryResolveWorktreeMetadataPath(repoTop); mp != "" {
			return "metadata-missing", &mp
		}
		return "metadata-missing", nil
	}
	return "metadata-inconsistent", nil
}

func tryResolveWorktreeMetadataPath(repoTop string) string {
	commonDir, err := gitutil.CommonDir(repoTop)
	if err != nil {
		return ""
	}
	stateDir := filepath.Join(commonDir, "kura")
	worktreesDir := filepath.Join(stateDir, "worktrees")
	rel, err := filepath.Rel(worktreesDir, repoTop)
	if err != nil || strings.ContainsRune(rel, filepath.Separator) || rel == "." || rel == ".." {
		return ""
	}
	return worktree.MetadataPathInStateDir(stateDir, rel)
}
