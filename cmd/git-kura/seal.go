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

var sealLsDataSchema = mustCompileSealLsDataSchema()

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

type sealLsOptions struct {
	FilterKey string
	JSON      bool
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

const sealLsHelp = `Usage: git kura seal ls [--json] [key]

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
           before the optional key argument.`

const sealClaimHelp = `Usage: git kura seal claim <path> [path...]

Claim one or more file paths for the current key in the seal store.

Paths are interpreted relative to the repository root, regardless of the
current working directory. Absolute paths are rejected.
Exits with error if:
  - no current seal key is available (see "Current key" below)
  - any path is absolute or outside the repository
  - any path does not exist or is a directory
  - any path is already claimed by a different key

If a path is already claimed by the current key, it is skipped (idempotent).

Current key:
  The current key is derived from the git-kura managed worktree you are in:
  run this command from inside the worktree created by "git kura open <key>"
  and that worktree's key becomes the current key. It fails when the current
  directory is not inside a managed worktree, or when that worktree's
  metadata is missing or inconsistent.`

const sealUnclaimHelp = `Usage: git kura seal unclaim <path> [path...]

Release the current key's claim on one or more file paths in the seal store.

Paths are interpreted relative to the repository root, regardless of the
current working directory. Absolute paths are rejected.
Exits with error if:
  - no current seal key is available (see "Current key" below)
  - any path is absolute or outside the repository
  - any path is claimed by a different key

Paths not currently claimed are skipped (idempotent).

Current key:
  The current key is derived from the git-kura managed worktree you are in:
  run this command from inside the worktree created by "git kura open <key>"
  and that worktree's key becomes the current key. It fails when the current
  directory is not inside a managed worktree, or when that worktree's
  metadata is missing or inconsistent.`

const sealTestHelp = `Usage: git kura seal test <path> [path...]

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

Current key:
  The current key is derived from the git-kura managed worktree you are in:
  run this command from inside the worktree created by "git kura open <key>"
  and that worktree's key becomes the current key. It fails when the current
  directory is not inside a managed worktree, or when that worktree's
  metadata is missing or inconsistent.`

const sealDoctorHelp = `Usage: git kura seal doctor

Validate the repository-wide path seal store.

seal doctor is read-only: it never modifies the seal store and never takes the
store lock. It inspects the seal store attached to the Git repository common
dir, so it can run from any directory inside the repository and does not depend
on the current worktree or current seal key.

An absent store is treated as an empty store. If the store is malformed or
contains invalid paths, seal doctor exits with seal-doctor-error (code 7) and
reports every problematic store entry it finds. On success it prints nothing
and exits 0.`

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
	if len(args) == 0 {
		return fmt.Errorf("usage: git kura seal claim <path> [path...]")
	}
	return cmdSealClaim(args)
}

func runSealUnclaim(args []string) error {
	if hasHelpFlag(args) {
		fmt.Println(sealUnclaimHelp)
		return nil
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: git kura seal unclaim <path> [path...]")
	}
	return cmdSealUnclaim(args)
}

func runSealTest(args []string) error {
	if hasHelpFlag(args) {
		fmt.Println(sealTestHelp)
		return nil
	}
	paths, err := parseSealTestArgs(args)
	if err != nil {
		return err
	}
	return cmdSealTest(paths)
}

func runSealDoctor(args []string) error {
	if hasHelpFlag(args) {
		fmt.Println(sealDoctorHelp)
		return nil
	}
	if err := parseSealDoctorArgs(args); err != nil {
		return err
	}
	return cmdSealDoctor()
}

// parseSealTestArgs requires at least one positional path and rejects any
// option. seal test is intentionally option-free in v0: the not-yet-defined
// --all / --unsealed / --staged modes must error rather than be silently
// ignored, so a future release can add them without changing behavior.
func parseSealTestArgs(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: git kura seal test <path> [path...]")
	}
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return nil, fmt.Errorf("usage: git kura seal test <path> [path...]: unknown option %q", a)
		}
	}
	return args, nil
}

// parseSealDoctorArgs rejects every argument and option. doctor is
// intentionally option-free in v0: future fix or formatting modes should be
// added explicitly without silently accepting placeholder flags today.
func parseSealDoctorArgs(args []string) error {
	if len(args) == 0 {
		return nil
	}
	if strings.HasPrefix(args[0], "-") {
		return exitCodeError(exitUsageError, fmt.Errorf("usage: git kura seal doctor: unknown option %q", args[0]))
	}
	return exitCodeError(exitUsageError, fmt.Errorf("usage: git kura seal doctor: unexpected argument %q", args[0]))
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
	} else if strings.HasPrefix(args[0], "-") {
		return sealLsOptions{}, exitCodeError(exitUsageError,
			fmt.Errorf("usage: git kura seal ls [--json] [key]: unknown option %q", args[0]))
	}

	if len(args) == 0 {
		return opts, nil
	}

	if strings.HasPrefix(args[0], "-") {
		return sealLsOptions{}, exitCodeError(exitUsageError,
			fmt.Errorf("usage: git kura seal ls [--json] [key]: unknown option %q", args[0]))
	}

	if err := validateKey(args[0]); err != nil {
		return sealLsOptions{}, err
	}
	opts.FilterKey = args[0]
	args = args[1:]

	if len(args) > 0 {
		return sealLsOptions{}, exitCodeError(exitUsageError,
			fmt.Errorf("usage: git kura seal ls [--json] [key]: unexpected argument %q (--json must appear before the key)", args[0]))
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

	if opts.JSON {
		if err := validateData(sealLsDataSchema, data); err != nil {
			return err
		}
		return emitResult(renderJSON, Result{Command: commandSealLs, Data: data})
	}
	return emitResult(renderHuman, Result{Command: commandSealLs, Data: data})
}

// sealLsFail routes a seal ls failure to the right output. JSON requests
// render an ok:false envelope on stdout; plain requests keep existing behavior.
func sealLsFail(opts sealLsOptions, err error) error {
	if !opts.JSON {
		return err
	}
	return emitError(renderJSON, toCommandError(commandSealLs, err))
}

// cmdSealDoctor validates the whole path seal store for the current Git
// repository. It is repository-wide and read-only: it does not derive a current
// key, inspect git-kura worktree metadata, or acquire paths.lock.
func cmdSealDoctor() error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository")
	}
	storeFile, _, err := pathsSealStore(repoRoot)
	if err != nil {
		return err
	}
	if err := doctorSealStore(storeFile); err != nil {
		return exitCodeError(exitSealDoctorError, fmt.Errorf("seal-doctor-error: %w", err))
	}
	return nil
}
