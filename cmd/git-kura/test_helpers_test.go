package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// Test helpers

type cliResult struct {
	stdout string
	stderr string
	code   int
}

type testCLI struct {
	t       *testing.T
	binDir  string
	envPath string
}

func newTestCLI(t *testing.T) *testCLI {
	t.Helper()

	binDir := t.TempDir()
	bin := filepath.Join(binDir, "git-kura")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = "."
	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("build git-kura: %v\n%s", err, output)
	}

	envPath := binDir + string(os.PathListSeparator) + os.Getenv("PATH")
	return &testCLI{t: t, binDir: binDir, envPath: envPath}
}

func (c *testCLI) initRepo(t *testing.T) string {
	t.Helper()

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.email", "kura-test@example.com")
	git(t, repo, "config", "user.name", "Kura Test")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "initial\n")
	git(t, repo, "add", "tracked.txt")
	git(t, repo, "commit", "-m", "initial")
	return repo
}

func (c *testCLI) gitKura(dir string, args ...string) cliResult {
	c.t.Helper()
	return c.run(dir, append([]string{"kura"}, args...)...)
}

// openWorktree creates a managed worktree for key in repo via the built CLI and
// returns its path. Seal commands derive the current key from the worktree they
// run in, so tests operate from the returned path rather than from the main
// checkout.
func (c *testCLI) openWorktree(t *testing.T, repo, key string) string {
	t.Helper()
	res := c.gitKura(repo, "open", key)
	requireExitCode(t, res, 0)
	return strings.TrimSpace(res.stdout)
}

// openManagedWorktree creates a managed worktree for key in repo using the
// in-process cmdOpen and returns its path. Used by in-process unit tests that
// call cmdSealClaim/cmdSealUnclaim directly, which derive the key from the current
// worktree.
func openManagedWorktree(t *testing.T, repo, key string) string {
	t.Helper()
	var path string
	withWorkingDir(t, repo, func() {
		out, err := captureStdout(t, func() error { return cmdOpen(key, openOptions{}) })
		if err != nil {
			t.Fatalf("cmdOpen %q: %v", key, err)
		}
		path = strings.TrimSpace(out)
	})
	return path
}

func (c *testCLI) posixShell(dir, script string) cliResult {
	c.t.Helper()
	cmd := exec.Command("sh", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+c.envPath)
	return runCommand(cmd)
}

func (c *testCLI) windowsCommand(dir, script string) cliResult {
	c.t.Helper()
	cmd := exec.Command("cmd", "/C", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+c.envPath)
	return runCommand(cmd)
}

func (c *testCLI) run(dir string, args ...string) cliResult {
	c.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+c.envPath)
	return runCommand(cmd)
}

func runCommand(cmd *exec.Cmd) cliResult {
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	code := 0
	if err := cmd.Run(); err != nil {
		code = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
	}
	return cliResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func withWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	}()
	fn()
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write

	fnErr := fn()

	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old

	out, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	return string(out), fnErr
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func gitRefs(t *testing.T, repo string) string {
	t.Helper()
	return git(t, repo, "show-ref", "--heads")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commitFile writes name into repo and commits it, so the file is present in
// every managed worktree opened afterward.
func commitFile(t *testing.T, repo, name, content string) {
	t.Helper()
	writeFile(t, filepath.Join(repo, name), content)
	git(t, repo, "add", name)
	git(t, repo, "commit", "-m", "add "+name)
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := file.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func expectedWorktreePath(repo, key string) string {
	return filepath.Join(expectedStateDir(repo), "worktrees", key)
}

func expectedMetadataPath(repo, key string) string {
	return filepath.Join(expectedStateDir(repo), "meta", "worktrees", key+".json")
}

func expectedStateDir(repo string) string {
	return filepath.Join(repo, ".git", "kura")
}

func printableName(value string) string {
	if value == "" {
		return "(empty)"
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '\x00', '\n', '\t', '/', '\\':
			return '_'
		default:
			return r
		}
	}, value)
}

func requireExitCode(t *testing.T, result cliResult, want int) {
	t.Helper()
	if result.code != want {
		t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", result.code, want, result.stdout, result.stderr)
	}
}

func requireNonZeroExitCode(t *testing.T, result cliResult) {
	t.Helper()
	if result.code == 0 {
		t.Fatalf("exit code = 0, want non-zero\nstdout:\n%s\nstderr:\n%s", result.stdout, result.stderr)
	}
}

func requireEmptyStdout(t *testing.T, result cliResult) {
	t.Helper()
	if result.stdout != "" {
		t.Fatalf("stdout = %q, want empty", result.stdout)
	}
}

func requireEmptyStderr(t *testing.T, result cliResult) {
	t.Helper()
	if result.stderr != "" {
		t.Fatalf("stderr = %q, want empty", result.stderr)
	}
}

func requireStdoutLine(t *testing.T, result cliResult, want string) {
	t.Helper()
	requireExitCode(t, result, 0)
	if strings.TrimSuffix(result.stdout, "\n") != want {
		t.Fatalf("stdout = %q, want %q\nstderr:\n%s", result.stdout, want+"\n", result.stderr)
	}
}

func requireCleanValueStdout(t *testing.T, result cliResult) {
	t.Helper()
	if strings.Count(result.stdout, "\n") != 1 {
		t.Fatalf("stdout should contain exactly one line, got %q", result.stdout)
	}
	for _, forbidden := range []string{": ", "\"", "'", "warning"} {
		if strings.Contains(strings.ToLower(result.stdout), forbidden) {
			t.Fatalf("stdout contains non-value text %q in %q", forbidden, result.stdout)
		}
	}
}

func requireStderrContains(t *testing.T, result cliResult, want string) {
	t.Helper()
	if !strings.Contains(strings.ToLower(result.stderr), strings.ToLower(want)) {
		t.Fatalf("stderr = %q, want it to contain %q", result.stderr, want)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("%s exists, want missing", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s should exist: %v", path, err)
	}
}

func requireJSONMetadata(t *testing.T, output string) map[string]any {
	t.Helper()
	var metadata map[string]any
	if err := json.Unmarshal([]byte(output), &metadata); err != nil {
		t.Fatalf("json output is not parseable: %v\n%s", err, output)
	}
	return metadata
}

func requireJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read json file %s: %v", path, err)
	}
	return requireJSONMetadata(t, string(data))
}

func requireStdoutContainsLine(t *testing.T, result cliResult, want string) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimRight(result.stdout, "\n"), "\n") {
		if line == want {
			return
		}
	}
	t.Fatalf("stdout = %q, want a line %q\nstderr:\n%s", result.stdout, want, result.stderr)
}

func requireStdoutNotContainsLine(t *testing.T, result cliResult, notWant string) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimRight(result.stdout, "\n"), "\n") {
		if line == notWant {
			t.Fatalf("stdout = %q, want no line %q", result.stdout, notWant)
		}
	}
}

func requireConformsToOutputSchema(t *testing.T, jsonOutput string) {
	t.Helper()

	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(jsonOutput))
	if err != nil {
		t.Fatalf("parse json output: %v\noutput: %s", err, jsonOutput)
	}

	if err := outputSchema.Validate(inst); err != nil {
		t.Fatalf("json output does not conform to schema:\n%v\noutput: %s", err, jsonOutput)
	}
}
