package worktree_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/internal/worktree"
)

func TestApplyIncludePatterns(t *testing.T) {
	for _, tc := range []struct {
		name, patterns string
		want           []string
	}{
		{"literal at any depth", ".env\n", []string{".env", "nested/.env"}},
		{"root anchored", "/.env\n", []string{".env"}},
		{"wildcard and negation", ".env*\n!.env.local\n", []string{".env", "nested/.env"}},
		{"directory", "config/\n", []string{"config/secret.json", "config/nested/key", "config/debug.log", "config/nested/debug.log"}},
		{"recursive with negation", "config/**\n!config/**/\n!config/**/*.log\n", []string{"config/secret.json", "config/nested/key"}},
		{"last match wins", ".env*\n!.env*\n.env.local\n", []string{".env.local"}},
		{"comments and CRLF", "# comment\r\n\r\n.env.local\r\n", []string{".env.local"}},
		{"escaped patterns", "\\#secret\n\\!secret\nspace name\n", []string{"#secret", "!secret", "space name"}},
		{"double star", "**/key\n", []string{"config/nested/key"}},
		{"character class and question mark", ".env.loca[l]\nconfig/nested/ke?\n", []string{".env.local", "config/nested/key"}},
		{"parent stays excluded", "config/\n!config/nested/key\n", []string{"config/secret.json", "config/nested/key", "config/debug.log", "config/nested/debug.log"}},
		{"missing source", "missing\n", nil},
		{"empty manifest", "", nil},
		{"comments only", "# nothing to copy\n\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := initRepo(t)
			dst := t.TempDir()
			files := []string{".env", ".env.local", "nested/.env", "config/secret.json", "config/nested/key", "config/debug.log", "config/nested/debug.log", "#secret", "!secret", "space name"}
			writeFile(t, filepath.Join(repo, ".gitignore"), "*\n")
			writeFile(t, filepath.Join(repo, ".worktreeinclude"), tc.patterns)
			for _, name := range files {
				writeIncludedFile(t, repo, name, "contents of "+name)
			}
			if err := worktree.ApplyInclude(repo, dst); err != nil {
				t.Fatal(err)
			}
			wanted := make(map[string]bool)
			for _, name := range tc.want {
				wanted[name] = true
			}
			for _, name := range files {
				if wanted[name] {
					requireIncludedContent(t, dst, name, "contents of "+name)
				} else {
					requireNotIncluded(t, dst, name)
				}
			}
		})
	}
}

func TestApplyIncludeRequiresIgnoredAndUntracked(t *testing.T) {
	repo := initRepoWithCommit(t)
	dst := t.TempDir()
	writeFile(t, filepath.Join(repo, ".worktreeinclude"), "*\n")
	writeFile(t, filepath.Join(repo, ".gitignore"), ".env\ntracked.txt\n*.ignored\n")
	writeIncludedFile(t, repo, "nested/.gitignore", "local.secret\n")
	writeIncludedFile(t, repo, "nested/local.secret", "nested ignore")
	writeFile(t, filepath.Join(repo, ".git/info/exclude"), "info.secret\n")
	global := filepath.Join(t.TempDir(), "global-ignore")
	writeFile(t, global, "global.secret\n")
	gitCmd(t, repo, "config", "core.excludesFile", global)
	names := []string{".env", "info.secret", "global.secret", "space name.ignored"}
	if runtime.GOOS != "windows" {
		names = append(names, "tab\tname.ignored", "line\nname.ignored")
	}
	for _, name := range names {
		writeIncludedFile(t, repo, name, name)
	}
	writeFile(t, filepath.Join(repo, "tracked.txt"), "modified tracked file")
	writeFile(t, filepath.Join(repo, "untracked.txt"), "unignored file")
	writeFile(t, filepath.Join(repo, "staged.ignored"), "staged ignored file")
	gitCmd(t, repo, "add", "-f", "staged.ignored")
	if err := worktree.ApplyInclude(repo, dst); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		requireIncludedContent(t, dst, name, name)
	}
	requireIncludedContent(t, dst, "nested/local.secret", "nested ignore")
	for _, name := range []string{"tracked.txt", "staged.ignored", "untracked.txt", ".git", ".gitignore", ".worktreeinclude"} {
		requireNotIncluded(t, dst, name)
	}
}

func TestApplyIncludePreservesExistingTargets(t *testing.T) {
	repo := initRepo(t)
	dst := t.TempDir()
	writeFile(t, filepath.Join(repo, ".worktreeinclude"), "*\n")
	writeFile(t, filepath.Join(repo, ".gitignore"), "*.local\n")
	for _, name := range []string{"file.local", "dir.local", "blocked.local/child", "merged.local/new", "merged.local/old"} {
		writeIncludedFile(t, repo, name, "source")
	}
	writeIncludedFile(t, dst, "file.local", "existing file")
	writeIncludedFile(t, dst, "dir.local/keep", "existing directory")
	writeIncludedFile(t, dst, "blocked.local", "blocking file")
	writeIncludedFile(t, dst, "merged.local/old", "old contents")
	for range 2 {
		if err := worktree.ApplyInclude(repo, dst); err != nil {
			t.Fatal(err)
		}
	}
	requireIncludedContent(t, dst, "file.local", "existing file")
	requireIncludedContent(t, dst, "dir.local/keep", "existing directory")
	requireIncludedContent(t, dst, "blocked.local", "blocking file")
	requireIncludedContent(t, dst, "merged.local/old", "old contents")
	requireIncludedContent(t, dst, "merged.local/new", "source")
}

func TestApplyIncludeDoesNotFollowSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	repo := initRepo(t)
	dst := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(repo, ".worktreeinclude"), "*\n")
	writeFile(t, filepath.Join(repo, ".gitignore"), "*.local\n")
	writeIncludedFile(t, outside, "secret", "outside data")
	for _, link := range []struct{ root, name, target string }{
		{repo, "file.local", filepath.Join(outside, "secret")},
		{repo, "dir.local", outside},
		{repo, "dangling.local", filepath.Join(outside, "absent")},
		{dst, "target.local", filepath.Join(outside, "secret")},
		{dst, "parent.local", outside},
		{dst, "broken.local", filepath.Join(outside, "missing")},
	} {
		if err := os.Symlink(link.target, filepath.Join(link.root, link.name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"target.local", "parent.local/new", "broken.local"} {
		writeIncludedFile(t, repo, name, "must not escape")
	}
	if err := worktree.ApplyInclude(repo, dst); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"file.local", "dir.local", "dangling.local"} {
		requireNotIncluded(t, dst, name)
	}
	requireIncludedContent(t, outside, "secret", "outside data")
	requireNotIncluded(t, outside, "new")
	requireNotIncluded(t, outside, "missing")
	for _, name := range []string{"target.local", "parent.local", "broken.local"} {
		info, err := os.Lstat(filepath.Join(dst, name))
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("destination symlink %q changed: %v", name, err)
		}
	}
}

func TestApplyIncludeMissingManifest(t *testing.T) {
	// No manifest is a no-op even without a Git repository or destination.
	if err := worktree.ApplyInclude(t.TempDir(), "missing-destination"); err != nil {
		t.Fatal(err)
	}
}

func TestApplyIncludeErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*testing.T, string) string
		want  string
	}{
		{"directory manifest", func(t *testing.T, repo string) string {
			if err := os.Mkdir(filepath.Join(repo, ".worktreeinclude"), 0o755); err != nil {
				t.Fatal(err)
			}
			return t.TempDir()
		}, "regular file"},
		{"missing destination", func(t *testing.T, repo string) string {
			writeIncludedFile(t, repo, ".worktreeinclude", "*.local\n")
			writeIncludedFile(t, repo, ".gitignore", "*.local\n")
			writeIncludedFile(t, repo, "file.local", "local")
			return filepath.Join(t.TempDir(), "missing")
		}, "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := initRepo(t)
			dst := tc.setup(t, repo)
			err := worktree.ApplyInclude(repo, dst)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
	t.Run("not a repository", func(t *testing.T) {
		repo := t.TempDir()
		writeFile(t, filepath.Join(repo, ".worktreeinclude"), "*\n")
		if err := worktree.ApplyInclude(repo, t.TempDir()); err == nil || !strings.Contains(err.Error(), "select") {
			t.Fatalf("error = %v, want selection error", err)
		}
	})
}

func TestApplyIncludePermissionsAndPartialFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix permissions")
	}
	repo := initRepo(t)
	dst := t.TempDir()
	writeFile(t, filepath.Join(repo, ".worktreeinclude"), "*.local\n")
	writeFile(t, filepath.Join(repo, ".gitignore"), "*.local\n")
	writeIncludedFile(t, repo, "a.local", "executable")
	writeIncludedFile(t, repo, "z.local", "unreadable")
	if err := os.Chmod(filepath.Join(repo, "a.local"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(repo, "z.local"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(repo, "z.local"), 0o600) })
	if _, err := os.ReadFile(filepath.Join(repo, "z.local")); err == nil {
		t.Skip("current user can read permission-denied files")
	}
	err := worktree.ApplyInclude(repo, dst)
	if err == nil || !strings.Contains(err.Error(), "z.local") {
		t.Fatalf("error = %v, want failed path", err)
	}
	requireIncludedContent(t, dst, "a.local", "executable")
	info, err := os.Stat(filepath.Join(dst, "a.local"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("copied mode = %v, error = %v, want 0700", info, err)
	}
	requireNotIncluded(t, dst, "z.local")
}

func writeIncludedFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, content)
}

func requireIncludedContent(t *testing.T, root, name, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil || string(got) != want {
		t.Fatalf("%q content = %q, error = %v, want %q", name, got, err, want)
	}
}

func requireNotIncluded(t *testing.T, root, name string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name))); !os.IsNotExist(err) {
		t.Fatalf("%q should not be copied: %v", name, err)
	}
}
