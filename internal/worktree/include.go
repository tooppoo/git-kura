package worktree

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ApplyInclude copies ignored regular files selected by the source worktree's
// .worktreeinclude into a newly created worktree. Existing destinations and
// symlinks are skipped. An error leaves the worktree and any copied files intact.
func ApplyInclude(source, destination string) error {
	src, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	info, err := src.Lstat(".worktreeinclude")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read .worktreeinclude: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf(".worktreeinclude must be a regular file")
	}
	patterns, err := src.ReadFile(".worktreeinclude")
	if err != nil {
		return fmt.Errorf("read .worktreeinclude: %w", err)
	}

	// Give Git a snapshot of the patterns without changing the source index,
	// ignore configuration, or .gitignore files. Git supplies the pattern syntax.
	manifest, err := os.CreateTemp("", "git-kura-worktreeinclude-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(manifest.Name()) }()
	_, writeErr := manifest.Write(patterns)
	closeErr := manifest.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	selected, err := includeFiles(source, "--exclude-from="+manifest.Name())
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		return nil
	}
	ignored, err := includeFiles(source, "--exclude-standard")
	if err != nil {
		return err
	}
	eligible := make(map[string]bool, len(ignored))
	for _, name := range ignored {
		eligible[name] = true
	}
	dst, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()
	for _, name := range selected {
		if !eligible[name] {
			continue
		}
		if err := copyIncludedFile(src, dst, filepath.FromSlash(name)); err != nil {
			return fmt.Errorf("copy %q: %w", name, err)
		}
	}
	return nil
}

func includeFiles(source, exclude string) ([]string, error) {
	// --others excludes tracked paths, even if they match ignore rules. Keep
	// the two exclusion sets separate: combining them would produce a union.
	cmd := exec.Command("git", "ls-files", "--others", "--ignored", "-z", exclude, "--")
	cmd.Dir = source
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("select .worktreeinclude files: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00"), nil
}

func copyIncludedFile(src, dst *os.Root, name string) error {
	if !filepath.IsLocal(name) {
		return fmt.Errorf("path is not worktree-relative")
	}
	for _, part := range strings.Split(name, string(filepath.Separator)) {
		if part == "." || part == ".." || strings.EqualFold(part, ".git") {
			return fmt.Errorf("unsafe worktree path")
		}
	}
	// Do not follow source directory symlinks or destination directory symlinks.
	// os.Root also confines filesystem operations if a path changes during copy.
	ok, err := includeParents(src, filepath.Dir(name), false)
	if err != nil || !ok {
		return err
	}
	info, err := src.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	ok, err = includeParents(dst, filepath.Dir(name), true)
	if err != nil || !ok {
		return err
	}
	if _, err := dst.Lstat(name); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	in, err := src.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := dst.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// includeParents accepts only real directories, creating missing destination
// parents as needed. Existing files and symlinks block copying below that path.
func includeParents(root *os.Root, dir string, create bool) (bool, error) {
	if dir == "." {
		return true, nil
	}
	parent := "."
	for _, part := range strings.Split(dir, string(filepath.Separator)) {
		parent = filepath.Join(parent, part)
		info, err := root.Lstat(parent)
		if os.IsNotExist(err) && create {
			if err := root.Mkdir(parent, 0o755); err != nil && !os.IsExist(err) {
				return false, err
			}
			info, err = root.Lstat(parent)
		}
		if err != nil {
			return false, err
		}
		if !info.IsDir() {
			return false, nil
		}
	}
	return true, nil
}
