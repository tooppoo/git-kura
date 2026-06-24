package cli

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tooppoo/git-kura/internal/gitutil"
	"github.com/tooppoo/git-kura/internal/output"
	"github.com/tooppoo/git-kura/internal/seal"
	"github.com/tooppoo/git-kura/internal/worktree"
)

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
// envelope's data field.
var (
	getDataSchema   = mustCompileSchema("get_data.schema.json", getDataSchemaJSON)
	openDataSchema  = mustCompileSchema("open.schema.json", openDataSchemaJSON)
	closeDataSchema = mustCompileSchema("close.schema.json", closeDataSchemaJSON)
	lsDataSchema    = mustCompileSchema("ls.schema.json", lsDataSchemaJSON)

	// openDryRunDataSchema is an alias for openDataSchema. The schema is shared
	// by dry-run and actual open; this alias keeps existing references compiling.
	openDryRunDataSchema = openDataSchema
)

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

func (r *runner) run(args []string) error {
	var helpWriteErr error
	root := r.buildRootCmd()
	root.SetHelpFunc(func(_ *cobra.Command, _ []string) {
		_, helpWriteErr = fmt.Fprintln(r.stdout, topLevelHelp)
	})
	root.SetArgs(args)
	root.SetOut(r.stdout)
	root.SetErr(r.stderr)
	if err := root.Execute(); err != nil {
		return err
	}
	return helpWriteErr
}

// buildRootCmd constructs the full Cobra command tree. SilenceErrors and
// SilenceUsage suppress Cobra's own error printing so Run() controls all
// output. SetFlagErrorFunc converts unknown-flag errors to exitUsageError so
// they receive exit code 2 consistently with the hand-parsed commands.
func (r *runner) buildRootCmd() *cobra.Command {
	var versionFlag bool

	root := &cobra.Command{
		Use:           "git-kura",
		Args:          cobra.ArbitraryArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if versionFlag {
				_, err := fmt.Fprintln(r.stdout, r.version)
				return err
			}
			if len(args) == 0 {
				return exitCodeError(exitUsageError, fmt.Errorf("usage: git kura <command> [key] [flags]"))
			}
			return exitCodeError(exitUsageError, fmt.Errorf("unknown command: %s", args[0]))
		},
	}

	root.Flags().BoolVarP(&versionFlag, "version", "v", false, "print version and exit")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return exitCodeError(exitUsageError, err)
	})

	root.AddCommand(
		r.buildGetCmd(),
		r.buildOpenCmd(),
		r.buildCloseCmd(),
		r.buildLsCmd(),
		r.buildSealCmd(),
		r.buildToolsCmd(),
	)

	return root
}

// buildGetCmd returns the cobra command for "git kura get".
// DisableFlagParsing passes all args to RunE so the existing parseGetArgs
// function handles flag parsing; this preserves tests that call parseGetArgs
// directly and maintains flag-placement behaviour.
func (r *runner) buildGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "get <key>",
		Short:              "Print worktree path, branch, or structured metadata",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if hasHelpFlag(args) {
				_, err := fmt.Fprintln(r.stdout, getHelp)
				return err
			}
			key, opts, err := parseGetArgs(args)
			if err != nil {
				return exitCodeError(exitUsageError, err)
			}
			return r.cmdGet(key, opts)
		},
	}
}

// buildOpenCmd returns the cobra command for "git kura open".
func (r *runner) buildOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "open <key>",
		Short:              "Create a worktree for <key>",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if hasHelpFlag(args) {
				_, err := fmt.Fprintln(r.stdout, openHelp)
				return err
			}
			key, opts, err := parseOpenArgs(args)
			if err != nil {
				return exitCodeError(exitUsageError, err)
			}
			return r.cmdOpen(key, opts)
		},
	}
}

// buildCloseCmd returns the cobra command for "git kura close".
func (r *runner) buildCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "close <key>",
		Short:              "Remove the worktree for <key>",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if hasHelpFlag(args) {
				_, err := fmt.Fprintln(r.stdout, closeHelp)
				return err
			}
			key, opts, err := parseCloseArgs(args)
			if err != nil {
				return exitCodeError(exitUsageError, err)
			}
			return r.cmdClose(key, opts)
		},
	}
}

// buildLsCmd returns the cobra command for "git kura ls".
func (r *runner) buildLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "ls",
		Short:              "List all open worktrees",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if hasHelpFlag(args) {
				_, err := fmt.Fprintln(r.stdout, lsHelp)
				return err
			}
			opts, err := parseLsArgs(args)
			if err != nil {
				return exitCodeError(exitUsageError, err)
			}
			return r.cmdLs(opts)
		},
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

// cmdGet resolves worktree information for key.
func (r *runner) cmdGet(key string, opts getOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return r.getFail(opts, fmt.Errorf("not inside a git repository"))
	}

	branch := worktree.BranchName(key)
	path, err := worktree.Path(repoRoot, key)
	if err != nil {
		return r.getFail(opts, fmt.Errorf("resolve worktree path: %w", err))
	}

	_, statErr := os.Stat(path)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return r.getFail(opts, fmt.Errorf("check worktree path: %w", statErr))
	}

	meta, metaErr := worktree.ReadStructuredMetadata(repoRoot, key, path, exists)
	if metaErr != nil {
		return r.getFail(opts, metaErr)
	}

	switch opts.OutputMode {
	case outputPath:
		if _, err := fmt.Fprintln(r.stdout, path); err != nil {
			return err
		}
		return nil
	case outputBranch:
		if _, err := fmt.Fprintln(r.stdout, branch); err != nil {
			return err
		}
		return nil
	case outputRoot:
		if _, err := fmt.Fprintln(r.stdout, meta.RepositoryRoot); err != nil {
			return err
		}
		return nil
	}

	dirty := false
	if exists {
		if dirty, err = gitutil.WorktreeDirty(path); err != nil {
			return r.getFail(opts, fmt.Errorf("check worktree status: %w", err))
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
		return r.emitResult(output.RenderJSON, output.Result{Command: output.CommandGet, Data: data})
	case outputTOON:
		if err := validateData(getDataSchema, data); err != nil {
			return err
		}
		return r.emitResult(output.RenderTOON, output.Result{Command: output.CommandGet, Data: data})
	}

	return nil
}

func (r *runner) getFail(opts getOptions, err error) error {
	if opts.OutputMode != outputJSON && opts.OutputMode != outputTOON {
		return err
	}
	mode := output.RenderJSON
	if opts.OutputMode == outputTOON {
		mode = output.RenderTOON
	}
	return r.emitError(mode, toCommandError(output.CommandGet, err))
}

func (r *runner) cmdOpen(key string, opts openOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return r.openFail(opts, fmt.Errorf("not inside a git repository"))
	}

	path, err := worktree.Path(repoRoot, key)
	if err != nil {
		return r.openFail(opts, fmt.Errorf("resolve worktree path: %w", err))
	}
	branch := worktree.BranchName(key)

	if opts.DryRun {
		return r.cmdOpenDryRun(opts, repoRoot, key, path, branch)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return r.openFail(opts, fmt.Errorf("create worktree parent: %w", err))
	}

	cmd := exec.Command("git", "worktree", "add", path, "-b", branch, "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return r.openFail(opts, fmt.Errorf("git worktree add: %w\n%s", err, out))
	}
	createdWorktree := true
	createdBranch := true

	base, err := gitutil.HeadBranch(repoRoot)
	if err != nil {
		return r.openFail(opts, fmt.Errorf("get base branch: %w", err))
	}

	meta := worktree.MetadataFile{RepositoryRoot: repoRoot, BaseBranch: base, WorktreePath: path}
	metaPath, err := worktree.MetadataPath(repoRoot, key)
	if err != nil {
		return r.openFail(opts, fmt.Errorf("resolve metadata path: %w", err))
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		return r.openFail(opts, fmt.Errorf("create metadata dir: %w", err))
	}
	metaData, _ := json.Marshal(meta)
	if err := os.WriteFile(metaPath, metaData, 0o644); err != nil {
		return r.openFail(opts, fmt.Errorf("write metadata: %w", err))
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
	return r.emitResult(mode, output.Result{Command: output.CommandOpen, Data: data})
}

// cmdOpenDryRun evaluates what open would create for key without any side
// effects.
func (r *runner) cmdOpenDryRun(opts openOptions, repoRoot, key, path, branch string) error {
	base, err := gitutil.HeadBranch(repoRoot)
	if err != nil {
		return r.openFail(opts, fmt.Errorf("get base branch: %w", err))
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

	return r.emitResult(opts.renderMode(), output.Result{Command: output.CommandOpen, Data: data, Warnings: warnings})
}

func (r *runner) openFail(opts openOptions, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	return r.emitError(mode, toCommandError(output.CommandOpen, err))
}

// dryRunConflict is one pre-creation collision found by open --dry-run.
type dryRunConflict struct {
	Type   string `json:"type"`
	Path   string `json:"path,omitempty"`
	Branch string `json:"branch,omitempty"`
}

// dryRunConflictDetails is the warning details payload for open-dry-run-conflict.
type dryRunConflictDetails struct {
	Conflicts []dryRunConflict `json:"conflicts"`
}

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

func (r *runner) cmdLs(opts lsOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return r.lsFail(opts, fmt.Errorf("not inside a git repository"))
	}

	dir, err := worktree.StateDir(repoRoot)
	if err != nil {
		return r.lsFail(opts, fmt.Errorf("resolve state dir: %w", err))
	}

	entries, err := os.ReadDir(filepath.Join(dir, "meta", "worktrees"))
	if err != nil {
		if os.IsNotExist(err) {
			entries = nil
		} else {
			return r.lsFail(opts, fmt.Errorf("read metadata dir: %w", err))
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
	return r.emitResult(mode, output.Result{Command: output.CommandLs, Data: data})
}

func (r *runner) lsFail(opts lsOptions, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	return r.emitError(mode, toCommandError(output.CommandLs, err))
}

// cmdClose tears down the worktree for key and releases every path seal that
// key holds, so a closed worktree never leaves stale claims that would block
// other worktrees from claiming the same paths.
//
// The whole teardown runs under the seal store lock so that seal release is
// atomic with worktree/branch/metadata removal.
func (r *runner) cmdClose(key string, opts closeOptions) error {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return r.closeFail(opts, nil, "preflight", fmt.Errorf("not inside a git repository"))
	}

	path, err := worktree.Path(repoRoot, key)
	if err != nil {
		return r.closeFail(opts, nil, "preflight", fmt.Errorf("resolve worktree path: %w", err))
	}
	branch := worktree.BranchName(key)

	partial := &closeDataJSON{Key: key, WorktreePath: path, Branch: branch}

	storeFile, lockFile, err := seal.StorePaths(repoRoot)
	if err != nil {
		return r.closeFail(opts, partial, "preflight", err)
	}

	timeout, err := seal.ResolveLockTimeout(repoRoot)
	if err != nil {
		return r.closeFail(opts, partial, "preflight", err)
	}
	release, err := seal.AcquireLock(lockFile, timeout)
	if err != nil {
		if errors.As(err, new(seal.LockTimeoutErr)) {
			err = exitCodeError(exitSealLockTimeout, err)
		}
		return r.closeFail(opts, partial, "preflight", err)
	}
	defer func() {
		if releaseErr := release(); releaseErr != nil {
			_, _ = fmt.Fprintf(r.stderr, "warning: %s\n", releaseErr)
		}
	}()

	store, err := seal.ReadStore(storeFile)
	if err != nil {
		phase := "read-store"
		if errors.As(err, new(seal.StoreValidationErr)) {
			phase = "validate-store"
		}
		return r.closeStoreFail(opts, partial, phase, storeFile, err)
	}

	// 1. Worktree cleanup.
	if _, statErr := os.Stat(path); statErr == nil {
		cmd := exec.Command("git", "worktree", "remove", path)
		cmd.Dir = repoRoot
		out, removeErr := cmd.CombinedOutput()
		if removeErr != nil {
			return r.closeFail(opts, partial, "remove-worktree", fmt.Errorf("git worktree remove: %w\n%s", removeErr, out))
		}
		partial.RemovedWorktree = true
	} else if os.IsNotExist(statErr) {
		if err := gitutil.PruneWorktrees(repoRoot); err != nil {
			return r.closeFail(opts, partial, "remove-worktree", err)
		}
	} else {
		return r.closeFail(opts, partial, "remove-worktree", fmt.Errorf("check worktree path: %w", statErr))
	}

	// 2. Kura-managed branch cleanup.
	branchExists, err := gitutil.BranchExists(repoRoot, branch)
	if err != nil {
		return r.closeFail(opts, partial, "remove-branch", err)
	}
	if branchExists {
		if err := gitutil.DeleteBranch(repoRoot, branch); err != nil {
			return r.closeFail(opts, partial, "remove-branch", err)
		}
		partial.RemovedBranch = true
	}

	// 3. Release every seal claimed by this key.
	releasedCount := 0
	for sealedPath, entry := range store.Paths {
		if entry.Key == key {
			delete(store.Paths, sealedPath)
			releasedCount++
		}
	}
	if releasedCount > 0 {
		if err := seal.WriteStore(storeFile, store); err != nil {
			return r.closeFail(opts, partial, "release-seals", fmt.Errorf("update seal store: %w", err))
		}
	}
	partial.ReleasedSealCount = releasedCount

	// 4. Metadata cleanup.
	metaPath, err := worktree.MetadataPath(repoRoot, key)
	if err != nil {
		return r.closeFail(opts, partial, "remove-metadata", fmt.Errorf("resolve metadata path: %w", err))
	}
	if err := os.Remove(metaPath); err != nil {
		if !os.IsNotExist(err) {
			return r.closeFail(opts, partial, "remove-metadata", fmt.Errorf("remove metadata: %w", err))
		}
	} else {
		partial.RemovedMetadata = true
	}

	mode := opts.renderMode()
	if mode != output.RenderHuman {
		if err := validateData(closeDataSchema, *partial); err != nil {
			return err
		}
	}
	return r.emitResult(mode, output.Result{Command: output.CommandClose, Data: *partial})
}

func (r *runner) closeFail(opts closeOptions, partial *closeDataJSON, phase string, err error) error {
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
	return r.emitError(mode, cerr)
}

func (r *runner) closeStoreFail(opts closeOptions, partial *closeDataJSON, phase string, storeFile string, err error) error {
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
	return r.emitError(mode, cerr)
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

func (d worktreeJSON) RenderHuman(w io.Writer) error {
	_, err := fmt.Fprintf(w,
		"worktree path:   %s\nbranch:          %s\nrepository root: %s\nbase branch:     %s\n",
		d.WorktreePath, d.Branch, d.RepositoryRoot, d.BaseBranch)
	return err
}

// openDataJSON is the data payload for open --json (both dry-run and actual).
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

// sealStoreError describes a store-wide failure in error.details.storeError.
type sealStoreError struct {
	Status string `json:"status"`
	Path   string `json:"path,omitempty"`
}
