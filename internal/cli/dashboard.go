package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/tooppoo/git-kura/internal/dashboard"
	"github.com/tooppoo/git-kura/internal/gitutil"
)

const dashboardHelp = `Usage: git kura dashboard

Open a read-only TUI that groups sealed paths under each managed worktree
key. It shows the union of open worktrees and keys holding seal claims,
including zero-claim worktrees and orphaned claims whose worktree is gone.

The dashboard requires an interactive terminal on stdin and stdout. It never
takes the seal store writer lock and never modifies any state.

Keys:
  up/down     Select the previous / next row
  left/right  Collapse / expand the selected key group
  /           Filter keys and claimed paths (case-insensitive substring)
  r           Reload now
  q, Ctrl-C   Quit`

// buildDashboardCmd returns the cobra command for "git kura dashboard".
func (r *runner) buildDashboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "dashboard",
		Short:              "Open a read-only TUI overview of seal ownership",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if hasHelpFlag(args) {
				_, err := fmt.Fprintln(r.stdout, dashboardHelp)
				return err
			}
			if err := parseDashboardArgs(args); err != nil {
				return exitCodeError(exitUsageError, err)
			}
			return r.cmdDashboard()
		},
	}
}

func parseDashboardArgs(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: git kura dashboard: unexpected argument %q", args[0])
	}
	return nil
}

// cmdDashboard starts the read-only seal ownership TUI. The interactivity
// check runs before anything touches the terminal, so a non-interactive
// invocation fails without emitting any escape sequence.
func (r *runner) cmdDashboard() error {
	interactive := r.dashboardInteractive
	if interactive == nil {
		interactive = stdioIsTerminal
	}
	if !interactive() {
		return fmt.Errorf("dashboard requires an interactive terminal on stdin and stdout")
	}

	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository")
	}
	loader := func() (dashboard.Snapshot, error) { return dashboard.Collect(repoRoot) }

	run := r.dashboardRun
	if run == nil {
		run = runDashboardOnStdio
	}
	return run(loader)
}

func stdioIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func runDashboardOnStdio(loader func() (dashboard.Snapshot, error)) error {
	return dashboard.Run(loader, dashboard.DefaultInterval, os.Stdin, os.Stdout)
}
