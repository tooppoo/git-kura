package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/internal/dashboard"
)

func TestParseDashboardArgsNoArgs(t *testing.T) {
	if err := parseDashboardArgs(nil); err != nil {
		t.Fatalf("parseDashboardArgs(nil) = %v, want nil", err)
	}
}

func TestParseDashboardArgsUnexpectedArgumentIsError(t *testing.T) {
	err := parseDashboardArgs([]string{"--json"})
	if err == nil {
		t.Fatalf("parseDashboardArgs(--json) = nil, want usage error")
	}
	if !strings.Contains(err.Error(), "usage: git kura dashboard") {
		t.Fatalf("error = %q, want usage message", err)
	}
}

func TestDashboardHelpFlagPrintsHelp(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		out, err := captureOutput(t, func(r *runner) error {
			return r.run([]string{"dashboard", flag})
		})
		if err != nil {
			t.Fatalf("dashboard %s: %v", flag, err)
		}
		if !strings.Contains(out, "Usage: git kura dashboard") {
			t.Fatalf("help output = %q, want usage header", out)
		}
		if !strings.Contains(out, "read-only") {
			t.Fatalf("help output = %q, want read-only description", out)
		}
	}
}

func TestDashboardUnexpectedArgumentExitsUsageError(t *testing.T) {
	var stdout, stderr strings.Builder
	code := Run([]string{"dashboard", "extra"}, &stdout, &stderr, testVersion)
	if code != exitUsageError {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exitUsageError, stderr.String())
	}
}

func TestDashboardNonInteractiveFailsWithoutEscapeSequences(t *testing.T) {
	var stdout, stderr strings.Builder
	r := &runner{
		stdout:               &stdout,
		stderr:               &stderr,
		version:              testVersion,
		dashboardInteractive: func() bool { return false },
	}
	err := r.cmdDashboard()
	if err == nil {
		t.Fatalf("cmdDashboard = nil, want interactive-terminal error")
	}
	if !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("error = %q, want interactive terminal message", err)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty (no escape sequences)", stdout.String())
	}
}

func TestDashboardOutsideGitRepositoryFails(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir, func() {
		r := newTestRunner()
		r.dashboardInteractive = func() bool { return true }
		r.dashboardRun = func(func() (dashboard.Snapshot, error)) error { return nil }
		err := r.cmdDashboard()
		if err == nil || err.Error() != "not inside a git repository" {
			t.Fatalf("cmdDashboard = %v, want not inside a git repository", err)
		}
	})
}

func TestDashboardLoaderCollectsSealOwnership(t *testing.T) {
	c := newTestCLI(t)
	repo := c.initRepo(t)
	worktreePath := openManagedWorktree(t, repo, "task-a")
	openManagedWorktree(t, repo, "task-b")

	// task-a claims tracked.txt from inside its worktree.
	withWorkingDir(t, worktreePath, func() {
		_, err := captureOutput(t, func(r *runner) error {
			return r.cmdSealClaim(sealClaimOptions{}, []string{"tracked.txt"})
		})
		if err != nil {
			t.Fatalf("seal claim: %v", err)
		}
	})

	var snap dashboard.Snapshot
	withWorkingDir(t, repo, func() {
		r := newTestRunner()
		r.dashboardInteractive = func() bool { return true }
		r.dashboardRun = func(loader func() (dashboard.Snapshot, error)) error {
			var err error
			snap, err = loader()
			return err
		}
		if err := r.cmdDashboard(); err != nil {
			t.Fatalf("cmdDashboard: %v", err)
		}
	})

	if snap.OpenKeys != 2 {
		t.Fatalf("OpenKeys = %d, want 2", snap.OpenKeys)
	}
	keys := make([]string, len(snap.Groups))
	for i, g := range snap.Groups {
		keys[i] = g.Key
	}
	if len(keys) != 2 || keys[0] != "task-a" || keys[1] != "task-b" {
		t.Fatalf("group keys = %v, want [task-a task-b]", keys)
	}
	if len(snap.Groups[0].Paths) != 1 || snap.Groups[0].Paths[0] != "tracked.txt" {
		t.Fatalf("task-a paths = %v, want [tracked.txt]", snap.Groups[0].Paths)
	}
	if len(snap.Groups[1].Paths) != 0 {
		t.Fatalf("task-b paths = %v, want empty", snap.Groups[1].Paths)
	}
}

func TestDashboardRunErrorPropagates(t *testing.T) {
	c := newTestCLI(t)
	repo := c.initRepo(t)

	withWorkingDir(t, repo, func() {
		r := newTestRunner()
		r.dashboardInteractive = func() bool { return true }
		r.dashboardRun = func(func() (dashboard.Snapshot, error)) error {
			return io.ErrUnexpectedEOF
		}
		if err := r.cmdDashboard(); err != io.ErrUnexpectedEOF {
			t.Fatalf("cmdDashboard = %v, want propagated run error", err)
		}
	})
}

// TestStdioIsTerminal exercises the real terminal probe; under go test the
// process stdio is not a terminal, so it must report false rather than panic.
func TestStdioIsTerminal(t *testing.T) {
	_ = stdioIsTerminal()
}
