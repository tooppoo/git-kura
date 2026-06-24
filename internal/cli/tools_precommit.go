package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/tooppoo/git-kura/internal/gitutil"
	"github.com/tooppoo/git-kura/internal/seal"
	"github.com/tooppoo/git-kura/internal/tools"
	"github.com/tooppoo/git-kura/internal/worktree"
)

const toolsRunHelp = `Usage: git kura tools run pre-commit [hook-args...]

Internal command invoked by the git-kura managed pre-commit hook wrapper. It is
not a primary user command; install, uninstall, and inspect the pre-commit
component with "git kura tools install|uninstall|status pre-commit".

It runs the same path-level seal decision as "git kura seal test" against the
staged files, chains any previously configured pre-commit hook, and re-checks
the staged files afterward. The commit is rejected when any check fails. This is
a local safety guard only; "git commit --no-verify" bypasses it.`

func (r *runner) runToolsRun(args []string) error {
	if hasHelpFlag(args) {
		if _, err := fmt.Fprintln(r.stdout, toolsRunHelp); err != nil {
			return err
		}
		return nil
	}
	if len(args) == 0 {
		return usageError(fmt.Errorf("usage: git kura tools run <hook> [hook-args...]"))
	}
	switch args[0] {
	case "pre-commit":
		return r.runPreCommitHook(args[1:])
	default:
		return usageError(fmt.Errorf("unknown tools run target %q: only \"pre-commit\" is supported", args[0]))
	}
}

// failClosed wraps a hook context/store failure as a general (exit 1) error so
// the commit is rejected rather than silently allowed.
func failClosed(format string, a ...any) error {
	return exitCodeError(exitGeneralError, fmt.Errorf(format, a...))
}

func (r *runner) runPreCommitHook(hookArgs []string) error {
	worktreeRoot, err := gitutil.RepoRoot()
	if err != nil {
		return failClosed("pre-commit hook: not inside a git repository")
	}
	commonDir, err := gitutil.CommonDir(worktreeRoot)
	if err != nil {
		return failClosed("pre-commit hook: resolve git common dir: %v", err)
	}

	currentKey, err := resolveHookCurrentKey(commonDir, worktreeRoot)
	if err != nil {
		return err
	}

	if err := preCommitSealCheck(worktreeRoot, currentKey, "pre-hook"); err != nil {
		return err
	}

	if err := r.runPreviousPreCommit(worktreeRoot, commonDir, hookArgs); err != nil {
		return err
	}

	if err := preCommitSealCheck(worktreeRoot, currentKey, "post-hook"); err != nil {
		return err
	}
	return nil
}

func resolveHookCurrentKey(commonDir, worktreeRoot string) (string, error) {
	stateDir := filepath.Join(commonDir, "kura")
	keys, err := worktree.ResolveKeyForWorktreeRoot(stateDir, worktreeRoot)
	if err != nil {
		return "", failClosed("pre-commit hook: resolve current key: %v", err)
	}
	switch len(keys) {
	case 0:
		return seal.KeyNone, nil
	case 1:
		return keys[0], nil
	default:
		return "", failClosed("pre-commit hook: %d managed worktrees match %s; refusing to guess current key", len(keys), worktreeRoot)
	}
}

func preCommitSealCheck(worktreeRoot, currentKey, phase string) error {
	staged, err := gitutil.StagedFiles(worktreeRoot)
	if err != nil {
		return failClosed("pre-commit hook (%s): %v", phase, err)
	}
	conflicts, err := seal.EvaluatePaths(worktreeRoot, currentKey, staged)
	if err != nil {
		return failClosed("pre-commit hook (%s): cannot read seal store; run \"git kura seal doctor\": %v", phase, err)
	}
	if len(conflicts) > 0 {
		return sealConflictError(conflicts)
	}
	return nil
}

// runPreviousPreCommit chains the user-defined pre-commit hook recorded at
// install time.
//
// Design exception: the subprocess is wired to os.Stdin/Stdout/Stderr directly
// rather than to r.stdout/r.stderr. This is intentional — the chained hook must
// run in the real terminal context (interactive prompts, coloured output, pagers)
// and its I/O cannot be captured through the injected writers. All diagnostic
// output emitted by runPreviousPreCommit itself still goes through r.stderr.
func (r *runner) runPreviousPreCommit(worktreeRoot, commonDir string, hookArgs []string) error {
	storeFile, _, err := tools.MetadataPaths(worktreeRoot)
	if err != nil {
		return failClosed("pre-commit hook: %v", err)
	}
	store, err := tools.ReadMetadata(storeFile)
	if err != nil {
		return failClosed("pre-commit hook: read tools metadata: %v", err)
	}
	entry, ok := store.Components[tools.PreCommitComponentID]
	if !ok {
		return nil
	}
	meta, ok := tools.PreCommitMetaFromEntry(&entry)
	if !ok {
		return nil
	}
	prevPath := tools.ResolvePreviousPreCommitPath(meta, worktreeRoot, commonDir)
	if prevPath == "" {
		return nil
	}

	info, err := os.Stat(prevPath)
	if err != nil {
		return nil
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		_, _ = fmt.Fprintf(r.stderr, "git-kura: previous pre-commit hook %s is not executable; skipping\n", prevPath)
		return nil
	}

	cmd := exec.Command(prevPath, hookArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	var exitErr *exec.ExitError
	if err := cmd.Run(); err != nil {
		if errors.As(err, &exitErr) {
			return exitCodeError(ExitCode(exitErr.ExitCode()), fmt.Errorf("previous pre-commit hook failed"))
		}
		return failClosed("run previous pre-commit hook %s: %v", prevPath, err)
	}
	return nil
}
