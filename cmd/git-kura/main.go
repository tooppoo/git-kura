package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	toon "github.com/toon-format/toon-go"
	"github.com/tooppoo/git-kura/internal/gitutil"
	"github.com/tooppoo/git-kura/internal/worktree"
)

// resolve by goreleaser
// https://goreleaser.com/resources/cookbooks/using-main.version/
var version string = "dev"

//go:embed schema/output.schema.json
var outputSchemaJSON []byte

var outputSchema = mustCompileOutputSchema()

func mustCompileOutputSchema() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(outputSchemaJSON))
	if err != nil {
		panic(fmt.Sprintf("parse output schema: %v", err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("output.schema.json", doc); err != nil {
		panic(fmt.Sprintf("add output schema resource: %v", err))
	}
	sch, err := c.Compile("output.schema.json")
	if err != nil {
		panic(fmt.Sprintf("compile output schema: %v", err))
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
  guard <subcommand>   Manage the cooperative guard for the current worktree

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
  --dry-run       Print the worktree that would be created as JSON`

const closeHelp = `Usage: git kura close <key>

Remove the worktree and Kura-managed branch for <key>, and release the path
seals that <key> holds in the repository-wide seal store.

close takes the seal store lock before any cleanup, so it can fail with
seal-lock-timeout (exit code 5) when the lock is held, leaving everything
unchanged.

For the full cleanup order, paths.json handling, and recovery behavior, see
https://github.com/tooppoo/git-kura/blob/main/docs/commands.md`

const lsHelp = `Usage: git kura ls

List all currently open worktrees, one key per line.`

// exitError carries a specific exit code to be used by main.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

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
	exitGuardConflict   = 8
)

func main() {
	if err := run(os.Args[1:]); err != nil {
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
			return err
		}
		return cmdGet(key, opts)

	case "open":
		if hasHelpFlag(args[1:]) {
			fmt.Println(openHelp)
			return nil
		}
		key, opts, err := parseOpenArgs(args[1:])
		if err != nil {
			return err
		}
		return cmdOpen(key, opts)

	case "close":
		if hasHelpFlag(args[1:]) {
			fmt.Println(closeHelp)
			return nil
		}
		key, err := parseKeyOnlyArgs("close", args[1:])
		if err != nil {
			return err
		}
		return cmdClose(key)

	case "ls":
		if hasHelpFlag(args[1:]) {
			fmt.Println(lsHelp)
			return nil
		}
		if err := parseLsArgs(args[1:]); err != nil {
			return err
		}
		return cmdLs()

	case "seal":
		return runSeal(args[1:])

	case "guard":
		return runGuard(args[1:])

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
		return "", openOptions{}, fmt.Errorf("usage: git kura open <key> [--dry-run]")
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
		default:
			return "", openOptions{}, fmt.Errorf("usage: git kura open <key> [--dry-run]: unexpected argument %q", flag)
		}
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

func cmdGet(key string, opts getOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository")
	}

	branch := worktree.BranchName(key)
	path, err := worktree.Path(repoRoot, key)
	if err != nil {
		return fmt.Errorf("resolve worktree path: %w", err)
	}

	_, statErr := os.Stat(path)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("check worktree path: %w", statErr)
	}

	meta, metaErr := worktree.ReadStructuredMetadata(repoRoot, key, path, exists)
	if metaErr != nil {
		return metaErr
	}

	if opts.OutputMode == outputPath {
		fmt.Println(path)
		return nil
	}
	if opts.OutputMode == outputBranch {
		fmt.Println(branch)
		return nil
	}
	if opts.OutputMode == outputRoot {
		fmt.Println(meta.RepositoryRoot)
		return nil
	}

	dirty := false
	if exists {
		if dirty, err = gitutil.WorktreeDirty(path); err != nil {
			return fmt.Errorf("check worktree status: %w", err)
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
		return printJSON(data)
	case outputTOON:
		return printTOON(data)
	}

	return nil
}

func cmdOpen(key string, opts openOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository")
	}

	path, err := worktree.Path(repoRoot, key)
	if err != nil {
		return fmt.Errorf("resolve worktree path: %w", err)
	}
	branch := worktree.BranchName(key)

	if opts.DryRun {
		base, err := gitutil.HeadBranch(repoRoot)
		if err != nil {
			return fmt.Errorf("get base branch: %w", err)
		}
		data := worktreeJSON{
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
		return printJSON(data)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create worktree parent: %w", err)
	}

	cmd := exec.Command("git", "worktree", "add", path, "-b", branch, "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add: %w\n%s", err, out)
	}

	base, err := gitutil.HeadBranch(repoRoot)
	if err != nil {
		return fmt.Errorf("get base branch: %w", err)
	}

	meta := worktree.MetadataFile{RepositoryRoot: repoRoot, BaseBranch: base, WorktreePath: path}
	metaPath, err := worktree.MetadataPath(repoRoot, key)
	if err != nil {
		return fmt.Errorf("resolve metadata path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		return fmt.Errorf("create metadata dir: %w", err)
	}
	metaData, _ := json.Marshal(meta)
	if err := os.WriteFile(metaPath, metaData, 0o644); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	fmt.Println(path)
	return nil
}

func printJSON(data worktreeJSON) error {
	out, _ := json.Marshal(data)
	inst, _ := jsonschema.UnmarshalJSON(bytes.NewReader(out))
	if err := outputSchema.Validate(inst); err != nil {
		return fmt.Errorf("internal: json output does not conform to schema: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

func printTOON(data worktreeJSON) error {
	out, err := toon.MarshalString(data)
	if err != nil {
		return fmt.Errorf("internal: toon encoding failed: %w", err)
	}
	fmt.Println(out)
	return nil
}

func parseLsArgs(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: git kura ls: unexpected argument %q", args[0])
	}
	return nil
}

func cmdLs() error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository")
	}

	dir, err := worktree.StateDir(repoRoot)
	if err != nil {
		return fmt.Errorf("resolve state dir: %w", err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "meta", "worktrees"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read metadata dir: %w", err)
	}

	// os.ReadDir returns entries sorted by name
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		fmt.Println(strings.TrimSuffix(name, ".json"))
	}
	return nil
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
func cmdClose(key string) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository")
	}

	path, err := worktree.Path(repoRoot, key)
	if err != nil {
		return fmt.Errorf("resolve worktree path: %w", err)
	}

	storeFile, lockFile, err := pathsSealStore(repoRoot)
	if err != nil {
		return err
	}

	// Acquire the seal store lock before any destructive cleanup. A lock that
	// cannot be acquired fails with seal-lock-timeout (code 5) and leaves the
	// worktree, branch, paths.json, and metadata untouched.
	timeout, err := resolveSealLockTimeout(repoRoot)
	if err != nil {
		return err
	}
	release, err := acquireSealLock(lockFile, timeout)
	if err != nil {
		return err
	}
	defer release()

	// Read and validate paths.json before destructive cleanup. An absent store
	// is treated as empty and cleanup continues; a malformed or schema-invalid
	// store aborts close before any worktree/branch/paths.json/metadata change.
	store, err := readSealStore(storeFile)
	if err != nil {
		return err
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
			return fmt.Errorf("git worktree remove: %w\n%s", removeErr, out)
		}
	} else if os.IsNotExist(statErr) {
		if err := gitutil.PruneWorktrees(repoRoot); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("check worktree path: %w", statErr)
	}

	// 2. Kura-managed branch cleanup. A branch that is already gone is a no-op; a
	//    branch that exists but cannot be deleted (e.g. unmerged commits) aborts
	//    close before paths.json is updated, so the key's seals are preserved.
	branch := worktree.BranchName(key)
	branchExists, err := gitutil.BranchExists(repoRoot, branch)
	if err != nil {
		return err
	}
	if branchExists {
		if err := gitutil.DeleteBranch(repoRoot, branch); err != nil {
			return err
		}
	}

	// 3. Release every seal claimed by this key, leaving other keys' claims
	//    untouched. Only rewrite paths.json when something actually changed.
	removed := false
	for sealedPath, entry := range store.Paths {
		if entry.Key == key {
			delete(store.Paths, sealedPath)
			removed = true
		}
	}
	if removed {
		if err := writeSealStore(storeFile, store); err != nil {
			return fmt.Errorf("update seal store: %w", err)
		}
	}

	// 4. Metadata cleanup. Failure here does not roll back the completed
	//    worktree/branch/seal cleanup; re-running close retries just this step,
	//    which recovers state left behind by a manual deletion or earlier
	//    incomplete close.
	meta, err := worktree.MetadataPath(repoRoot, key)
	if err != nil {
		return fmt.Errorf("resolve metadata path: %w", err)
	}
	if err := os.Remove(meta); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove metadata: %w", err)
	}

	return nil
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
