package gitutil

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func RepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ConfigScopeValue is one value of a config key together with the scope it was
// read from (system, global, local, worktree, or command).
type ConfigScopeValue struct {
	Scope string
	Value string
}

// ConfigGetAllWithScope returns every value configured for key together with its
// scope, in Git's precedence order (lowest precedence first, so the last entry
// is the effective value). It uses "git config -z --show-scope --get-all" so the
// scope and value are parsed NUL-safely. An unset key returns an empty slice and
// a nil error.
func ConfigGetAllWithScope(repoRoot, key string) ([]ConfigScopeValue, error) {
	cmd := exec.Command("git", "config", "-z", "--show-scope", "--get-all", key)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		// `git config --get-all` exits 1 when the key is unset; treat that as no
		// values rather than an error.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("read git config scopes for %q: %w", key, err)
	}
	// The -z --show-scope stream is a flat sequence of NUL-terminated fields:
	// scope, value, scope, value, ... A trailing NUL leaves an empty final field
	// that must be dropped.
	fields := strings.Split(string(out), "\x00")
	if n := len(fields); n > 0 && fields[n-1] == "" {
		fields = fields[:n-1]
	}
	var values []ConfigScopeValue
	for i := 0; i+1 < len(fields); i += 2 {
		values = append(values, ConfigScopeValue{Scope: fields[i], Value: fields[i+1]})
	}
	return values, nil
}

// ConfigGetLocal reads a single value from the repository-local config only
// ("git config --local --get"), ignoring global/system/worktree/command scopes.
// An unset key returns configured=false with a nil error.
func ConfigGetLocal(repoRoot, key string) (value string, configured bool, err error) {
	cmd := exec.Command("git", "config", "--local", "--get", key)
	cmd.Dir = repoRoot
	out, runErr := cmd.Output()
	if runErr == nil {
		return string(out), true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1 {
		return "", false, nil
	}
	return "", false, fmt.Errorf("read local git config %q: %w", key, runErr)
}

// SetConfigLocal sets key to value in the repository-local config
// ("git config --local").
func SetConfigLocal(repoRoot, key, value string) error {
	cmd := exec.Command("git", "config", "--local", key, value)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("set git config %q: %w\n%s", key, err, out)
	}
	return nil
}

// UnsetConfigLocal removes key from the repository-local config. A key that is
// already unset is treated as a no-op (git exits 5 in that case).
func UnsetConfigLocal(repoRoot, key string) error {
	cmd := exec.Command("git", "config", "--local", "--unset", key)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	// `git config --unset` exits 5 when the key (or section) is absent; that is
	// the desired post-condition, so report success.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 5 {
		return nil
	}
	return fmt.Errorf("unset git config %q: %w\n%s", key, err, out)
}

// StagedFiles returns all repository-relative paths affected by staged changes.
// For renames and copies both the old and new path are included, so a claim on
// the old path cannot be bypassed by a staged rename.
func StagedFiles(dir string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--cached", "--name-status", "--find-renames", "-z")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read staged files: %w", err)
	}
	parts := strings.Split(string(out), "\x00")
	var paths []string
	for i := 0; i < len(parts); {
		token := parts[i]
		if token == "" {
			i++
			continue
		}
		// Renames (R<score>) and copies (C<score>) produce two path tokens.
		if strings.HasPrefix(token, "R") || strings.HasPrefix(token, "C") {
			if i+1 < len(parts) && parts[i+1] != "" {
				paths = append(paths, parts[i+1])
			}
			if i+2 < len(parts) && parts[i+2] != "" {
				paths = append(paths, parts[i+2])
			}
			i += 3
		} else {
			if i+1 < len(parts) && parts[i+1] != "" {
				paths = append(paths, parts[i+1])
			}
			i += 2
		}
	}
	return paths, nil
}

func HeadBranch(repoRoot string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func DeleteBranch(repoRoot, branch string) error {
	cmd := exec.Command("git", "branch", "-d", branch)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("delete branch %q: %w\n%s", branch, err, out)
	}
	return nil
}

// BranchExists reports whether refs/heads/<branch> exists in the repository.
// It lets callers treat an already-absent branch as a no-op instead of an error
// (e.g. when cleaning up stale state where the branch was deleted manually).
func BranchExists(repoRoot, branch string) (bool, error) {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = repoRoot
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	// git show-ref exits 1 specifically when the ref is absent; fatal errors
	// (e.g. run outside a git repository) exit 128. Only exit 1 is a clean
	// "branch does not exist"; everything else is surfaced as a real error.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("check branch %q: %w", branch, err)
}

// PruneWorktrees removes administrative entries for worktrees whose working
// directory no longer exists. This unblocks branch deletion after a worktree
// directory was removed manually (Git otherwise refuses to delete a branch it
// still considers checked out in the now-missing worktree).
func PruneWorktrees(repoRoot string) error {
	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("prune worktrees: %w\n%s", err, out)
	}
	return nil
}

func CommonDir(repoRoot string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	dir := strings.TrimSpace(string(out))
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir), nil
	}
	return filepath.Clean(filepath.Join(repoRoot, dir)), nil
}

func WorktreeDirty(path string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// ConfigValue reads a single Git config value via `git config --get <key>`,
// following Git's standard scope resolution (local / global / system). It does
// not restrict the lookup to any single scope.
//
// The value is returned exactly as Git emits it (`git config` appends a trailing
// newline, which the caller is responsible for trimming). When the key is unset,
// `git config --get` exits with status 1, which is reported as configured=false
// with a nil error so callers can fall back to a default. Any other failure is
// returned as err.
func ConfigValue(repoRoot, key string) (value string, configured bool, err error) {
	cmd := exec.Command("git", "config", "--get", key)
	cmd.Dir = repoRoot
	out, runErr := cmd.Output()
	if runErr == nil {
		return string(out), true, nil
	}
	// `git config --get` exits 1 specifically when the key is unset; any other
	// exit status (or a failure to run git at all) is a real error.
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1 {
		return "", false, nil
	}
	return "", false, fmt.Errorf("read git config %q: %w", key, runErr)
}
