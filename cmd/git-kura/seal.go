package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"sort"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/tooppoo/git-kura/internal/gitutil"
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

func (o sealLsOptions) renderMode() renderMode {
	if o.Toon {
		return renderTOON
	}
	if o.JSON {
		return renderJSON
	}
	return renderHuman
}

type sealTestOptions struct {
	JSON bool
	Toon bool
}

func (o sealTestOptions) renderMode() renderMode {
	if o.Toon {
		return renderTOON
	}
	if o.JSON {
		return renderJSON
	}
	return renderHuman
}

type sealDoctorOptions struct {
	JSON bool
	Toon bool
}

func (o sealDoctorOptions) renderMode() renderMode {
	if o.Toon {
		return renderTOON
	}
	if o.JSON {
		return renderJSON
	}
	return renderHuman
}

type sealClaimOptions struct {
	JSON bool
	Toon bool
}

func (o sealClaimOptions) renderMode() renderMode {
	if o.Toon {
		return renderTOON
	}
	if o.JSON {
		return renderJSON
	}
	return renderHuman
}

type sealUnclaimOptions struct {
	JSON bool
	Toon bool
}

func (o sealUnclaimOptions) renderMode() renderMode {
	if o.Toon {
		return renderTOON
	}
	if o.JSON {
		return renderJSON
	}
	return renderHuman
}

// sealClaimPathItem is one path's result in the seal claim success data.
type sealClaimPathItem struct {
	Path   string `json:"path"`
	Status string `json:"status"` // "claimed" or "already-owned"
}

// sealClaimData is the structured success payload for seal claim --json.
type sealClaimData struct {
	CurrentKey string              `json:"currentKey"`
	Paths      []sealClaimPathItem `json:"paths"`
}

func (d sealClaimData) RenderHuman(w io.Writer) error {
	for _, p := range d.Paths {
		label := p.Status
		if p.Status == "already-owned" {
			label = "already owned"
		}
		if _, err := fmt.Fprintf(w, "%s: %s\n", label, p.Path); err != nil {
			return err
		}
	}
	return nil
}

// sealUnclaimPathItem is one path's result in the seal unclaim success data.
type sealUnclaimPathItem struct {
	Path   string `json:"path"`
	Status string `json:"status"` // "released" or "not-claimed"
}

// sealUnclaimData is the structured success payload for seal unclaim --json.
type sealUnclaimData struct {
	CurrentKey string                `json:"currentKey"`
	Paths      []sealUnclaimPathItem `json:"paths"`
}

func (d sealUnclaimData) RenderHuman(w io.Writer) error {
	for _, p := range d.Paths {
		label := p.Status
		if p.Status == "not-claimed" {
			label = "not claimed"
		}
		if _, err := fmt.Fprintf(w, "%s: %s\n", label, p.Path); err != nil {
			return err
		}
	}
	return nil
}

// sealMutationPathItem is one path item in seal mutation error details.
// Status values for claim errors: "would-claim", "already-owned", "owned-by-other",
// "duplicate", "invalid-path", "outside-repository".
// Status values for unclaim errors: "would-release", "not-claimed", "owned-by-other",
// "duplicate", "invalid-path", "outside-repository".
type sealMutationPathItem struct {
	Path        string `json:"path"`
	Status      string `json:"status"`
	OwnerKey    string `json:"ownerKey,omitempty"`
	DuplicateOf *int   `json:"duplicateOf,omitempty"`
}

// sealMutationErrorDetails is the error.details payload for seal claim/unclaim
// failures. Preflight failures populate Phase, CurrentKey, Paths, Conflicts,
// and Duplicates. Store-level failures populate Phase and StoreError only.
type sealMutationErrorDetails struct {
	Phase      string                 `json:"phase,omitempty"`
	CurrentKey string                 `json:"currentKey,omitempty"`
	Paths      []sealMutationPathItem `json:"paths,omitempty"`
	Conflicts  []sealConflictItem     `json:"conflicts,omitempty"`
	Duplicates []sealDuplicateItem    `json:"duplicates,omitempty"`
	StoreError *sealStoreError        `json:"storeError,omitempty"`
}

// sealConflictItem describes a single ownership conflict in error.details.conflicts[].
type sealConflictItem struct {
	Path         string `json:"path"`
	OwnerKey     string `json:"ownerKey"`
	RequestedKey string `json:"requestedKey"`
}

// sealDuplicateItem describes a duplicate normalized path in error.details.duplicates[].
type sealDuplicateItem struct {
	Path           string `json:"path"`
	FirstIndex     int    `json:"firstIndex"`
	DuplicateIndex int    `json:"duplicateIndex"`
}

// sealStoreError describes a store-wide failure in error.details.storeError.
// It is required when phase is read-store, validate-store, or write-store.
type sealStoreError struct {
	Status string `json:"status"`
	Path   string `json:"path,omitempty"`
}

// sealLsClaim is one entry in the seal ls JSON output.
type sealLsClaim struct {
	Key  string `json:"key"`
	Path string `json:"path"`
}

// sealLsData is the structured success payload for seal ls --json.
type sealLsData struct {
	FilterKey *string       `json:"filterKey"`
	Claims    []sealLsClaim `json:"claims"`
}

func (d sealLsData) RenderHuman(w io.Writer) error {
	for _, c := range d.Claims {
		if _, err := fmt.Fprintf(w, "%s\t%s\n", c.Key, c.Path); err != nil {
			return err
		}
	}
	return nil
}

// sealTestResultItem is one path's inspection result in the seal test output.
type sealTestResultItem struct {
	Path      string  `json:"path"`
	Status    string  `json:"status"`
	Safe      bool    `json:"safe"`
	ClaimedBy *string `json:"claimedBy"`
}

// sealTestData is the structured success payload for seal test --json.
type sealTestData struct {
	CurrentKey string               `json:"currentKey"`
	Passed     bool                 `json:"passed"`
	Results    []sealTestResultItem `json:"results"`
}

func (d sealTestData) RenderHuman(w io.Writer) error {
	// Human mode: emit one line per conflict so the user can see every blocked path.
	// Uses the canonical "seal-conflict:" token, consistent with claim/unclaim errors.
	for _, r := range d.Results {
		if r.Status == "claimed-by-other-key" {
			if _, err := fmt.Fprintf(w, "seal-conflict: path %q is already claimed by key %q\n", r.Path, *r.ClaimedBy); err != nil {
				return err
			}
		}
	}
	return nil
}

// sealDoctorFinding is one integrity finding from seal doctor.
type sealDoctorFinding struct {
	Severity string  `json:"severity"`
	Code     string  `json:"code"`
	Path     *string `json:"path"`
	Message  string  `json:"message"`
}

// sealDoctorSummary aggregates counts from a seal doctor inspection.
type sealDoctorSummary struct {
	CheckedClaims int `json:"checkedClaims"`
	ErrorCount    int `json:"errorCount"`
	WarningCount  int `json:"warningCount"`
}

// sealDoctorData is the structured success payload for seal doctor --json.
type sealDoctorData struct {
	Healthy  bool                `json:"healthy"`
	Summary  sealDoctorSummary   `json:"summary"`
	Findings []sealDoctorFinding `json:"findings"`
}

func (d sealDoctorData) RenderHuman(w io.Writer) error {
	// Healthy: no output. Unhealthy: one line per finding with the stable token.
	for _, f := range d.Findings {
		if _, err := fmt.Fprintf(w, "seal-doctor-error: %s\n", f.Message); err != nil {
			return err
		}
	}
	return nil
}

// currentKeyUnresolvedDetails is the error.details payload when current key
// resolution fails for seal test --json.
type currentKeyUnresolvedDetails struct {
	Reason         string  `json:"reason"`
	RepositoryRoot *string `json:"repositoryRoot"`
	MetadataPath   *string `json:"metadataPath"`
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
// opts.FilterKey lists every key. Per
// docs/adr/20260612T170922Z_seal-command-current-context-and-scope.md,
// ls is always repository-wide: its scope must not depend on the caller's
// current worktree. It also reads the store without acquiring paths.lock,
// so a held lock never blocks listing.
func cmdSealLs(opts sealLsOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return sealLsFail(opts, fmt.Errorf("not inside a git repository"))
	}
	storeFile, _, err := pathsSealStore(repoRoot)
	if err != nil {
		return sealLsFail(opts, err)
	}
	store, err := readSealStore(storeFile)
	if err != nil {
		return sealLsFail(opts, err)
	}

	rawPaths := make([]string, 0, len(store.Paths))
	for p, entry := range store.Paths {
		if opts.FilterKey != "" && entry.Key != opts.FilterKey {
			continue
		}
		rawPaths = append(rawPaths, p)
	}
	sort.Slice(rawPaths, func(i, j int) bool {
		ki, kj := store.Paths[rawPaths[i]].Key, store.Paths[rawPaths[j]].Key
		if ki != kj {
			return ki < kj
		}
		return rawPaths[i] < rawPaths[j]
	})

	claims := make([]sealLsClaim, len(rawPaths))
	for i, p := range rawPaths {
		claims[i] = sealLsClaim{Key: store.Paths[p].Key, Path: p}
	}

	var filterKey *string
	if opts.FilterKey != "" {
		k := opts.FilterKey
		filterKey = &k
	}
	data := sealLsData{FilterKey: filterKey, Claims: claims}

	mode := opts.renderMode()
	if mode != renderHuman {
		if err := validateData(sealLsDataSchema, data); err != nil {
			return err
		}
	}
	return emitResult(mode, Result{Command: commandSealLs, Data: data})
}

// sealLsFail routes a seal ls failure to the right output. JSON/TOON requests
// render an ok:false envelope on stdout; plain requests keep existing behavior.
func sealLsFail(opts sealLsOptions, err error) error {
	mode := opts.renderMode()
	if mode == renderHuman {
		return err
	}
	return emitError(mode, toCommandError(commandSealLs, err))
}

// cmdSealDoctor validates the whole path seal store for the current Git
// repository. It is repository-wide and read-only: it does not derive a current
// key, inspect git-kura worktree metadata, or acquire paths.lock.
func cmdSealDoctor(opts sealDoctorOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return sealDoctorFail(opts, fmt.Errorf("not inside a git repository"))
	}
	storeFile, _, err := pathsSealStore(repoRoot)
	if err != nil {
		return sealDoctorFail(opts, err)
	}

	inspection, err := inspectSealStore(storeFile)
	if err != nil {
		// Store could not be read/parsed: execution failure.
		doctorErr := exitCodeError(exitSealDoctorError, fmt.Errorf("seal-doctor-error: %w", err))
		return sealDoctorFail(opts, doctorErr)
	}

	errorCount := 0
	warningCount := 0
	for _, f := range inspection.findings {
		if f.Severity == "error" {
			errorCount++
		} else {
			warningCount++
		}
	}
	findings := inspection.findings
	if findings == nil {
		findings = []sealDoctorFinding{}
	}
	data := sealDoctorData{
		Healthy: len(inspection.findings) == 0,
		Summary: sealDoctorSummary{
			CheckedClaims: inspection.checkedClaims,
			ErrorCount:    errorCount,
			WarningCount:  warningCount,
		},
		Findings: findings,
	}

	mode := opts.renderMode()
	if mode != renderHuman {
		if err := validateData(sealDoctorDataSchema, data); err != nil {
			return err
		}
		if err := emitResult(mode, Result{Command: commandSealDoctor, Data: data}); err != nil {
			return err
		}
		if !data.Healthy {
			return &renderedError{code: exitSealDoctorError}
		}
		return nil
	}

	// Human mode: route through the framework so JSON/TOON and human render the same data.
	// Unhealthy findings go to stdout as a business result (ok:true, healthy:false).
	if err := emitResult(renderHuman, Result{Command: commandSealDoctor, Data: data}); err != nil {
		return err
	}
	if !data.Healthy {
		return &renderedError{code: exitSealDoctorError}
	}
	return nil
}

// sealDoctorFail routes a seal doctor failure to the right output. JSON/TOON
// requests render an ok:false envelope on stdout; plain requests keep the
// existing error behavior.
func sealDoctorFail(opts sealDoctorOptions, err error) error {
	mode := opts.renderMode()
	if mode == renderHuman {
		return err
	}
	return emitError(mode, toCommandError(commandSealDoctor, err))
}
