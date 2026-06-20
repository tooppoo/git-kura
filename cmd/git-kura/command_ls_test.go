package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunLsIgnoresNonMetadataEntries(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		if err := run([]string{"open", "51"}); err != nil {
			t.Fatalf("open error = %v", err)
		}

		metaDir := filepath.Join(expectedStateDir(repo), "meta", "worktrees")
		writeFile(t, filepath.Join(metaDir, "notjson"), "noise")
		if err := os.Mkdir(filepath.Join(metaDir, "subdir"), 0o755); err != nil {
			t.Fatal(err)
		}

		stdout, err := captureStdout(t, func() error {
			return run([]string{"ls"})
		})
		if err != nil {
			t.Fatalf("ls error = %v", err)
		}
		lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
		if len(lines) != 1 || lines[0] != "51" {
			t.Fatalf("ls stdout = %q, want only line \"51\"", stdout)
		}
	})
}

func TestRunLsInProcess(t *testing.T) {
	cli := newTestCLI(t)
	repo := cli.initRepo(t)

	withWorkingDir(t, repo, func() {
		stdout, err := captureStdout(t, func() error {
			return run([]string{"ls"})
		})
		if err != nil {
			t.Fatalf("ls with no worktrees error = %v, want nil", err)
		}
		if strings.TrimSpace(stdout) != "" {
			t.Fatalf("ls with no worktrees stdout = %q, want empty", stdout)
		}

		if err := run([]string{"open", "51"}); err != nil {
			t.Fatalf("open 51 error = %v", err)
		}
		if err := run([]string{"open", "52"}); err != nil {
			t.Fatalf("open 52 error = %v", err)
		}

		stdout, err = captureStdout(t, func() error {
			return run([]string{"ls"})
		})
		if err != nil {
			t.Fatalf("ls error = %v", err)
		}
		for _, key := range []string{"51", "52"} {
			found := false
			for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
				if line == key {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("ls stdout = %q, want line %q", stdout, key)
			}
		}

		if err := run([]string{"close", "51"}); err != nil {
			t.Fatalf("close 51 error = %v", err)
		}

		stdout, err = captureStdout(t, func() error {
			return run([]string{"ls"})
		})
		if err != nil {
			t.Fatalf("ls after close error = %v", err)
		}
		for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
			if line == "51" {
				t.Fatalf("ls after close stdout = %q, want no line 51", stdout)
			}
		}
		found := false
		for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
			if line == "52" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("ls after close stdout = %q, want line 52", stdout)
		}
	})
}
