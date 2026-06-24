package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tooppoo/git-kura/internal/gitutil"
	"github.com/tooppoo/git-kura/internal/seal"
	"github.com/tooppoo/git-kura/internal/tools"
)

// exitRepositoryContextError is the exit code for commands that require a git
// repository context but were run outside one.
const exitRepositoryContextError ExitCode = 8

const toolsHelp = `Usage: git kura tools <subcommand> [component...] [flags]

Install, remove, and inspect git-kura auxiliary tool components.

A component is a self-contained helper (for example a pre-commit hook or an
editor skill) that git-kura can install into the repository from a verified
release asset and later remove. Each operation reports, per component, the
release version, source asset, destination, action, managed status, and a
reason.

Subcommands:
  status [component...]      Show the install state of components
  install <component...>     Install one or more components
  uninstall <component...>   Remove one or more components

Flags:
  --all                      With install/uninstall, target every component

status with no component shows every registered component. install and
uninstall require at least one component or --all; --all may not be combined
with explicit component names. status does not accept --all and never accesses
the network.

Run "git kura tools <subcommand> --help" for subcommand-specific help.`

const toolsStatusHelp = `Usage: git kura tools status [component...]

Show the install state of tool components from local metadata, the filesystem,
and git config only. status never accesses the network and does not check
whether the expected release asset is available remotely.

With no component, status reports every registered component. With one or more
components, it reports only those. An unknown component is a usage error.

Per component, status reports an action of installed, not-installed, skipped, or
failed.`

const toolsInstallHelp = `Usage: git kura tools install <component...>
       git kura tools install --all

Install one or more tool components from the tools release asset that matches
this binary's release version (the "latest" release is never used).

Installation downloads the tools asset archive and its sidecar manifest, then
verifies the archive checksum against the sidecar manifest before extracting it.
Only sha256 checksums are supported. A verified archive is cached under the git
common dir and reused on later installs. install requires an official release
binary; go install and source builds cannot download release assets.

At least one component or --all is required. --all may not be combined with
explicit component names. An unknown component is a usage error.

Per component, install reports an action of created, updated, skipped, or
failed. Components are processed independently: one failure does not stop the
rest, and the command exits non-zero if any component failed.`

const toolsUninstallHelp = `Usage: git kura tools uninstall <component...>
       git kura tools uninstall --all

Remove one or more installed tool components, using local metadata to decide
what is safe to remove. A component with no metadata is reported not-installed
and nothing is changed. A file whose checksum no longer matches the recorded
value is treated as user-modified and is skipped rather than removed; a config
value that no longer matches is likewise left untouched.

At least one component or --all is required. --all may not be combined with
explicit component names. An unknown component is a usage error.

Per component, uninstall reports an action of removed, not-installed, skipped,
or failed. Components are processed independently: one failure does not stop the
rest, and the command exits non-zero if any component failed.`

// toolsDeps holds the framework dependencies that production wiring fills in
// and tests override.
type toolsDeps struct {
	registry *tools.Registry
	fetcher  tools.Fetcher
	version  string
}

func (r *runner) runTools(args []string) error {
	reg, err := tools.ProductionRegistry()
	if err != nil {
		panic(fmt.Sprintf("production tools registry: %v", err))
	}
	deps := toolsDeps{
		registry: reg,
		fetcher:  tools.NewGithubReleaseFetcher(),
		version:  r.version,
	}
	return r.runToolsWith(deps, args)
}

func (r *runner) runToolsWith(deps toolsDeps, args []string) error {
	if len(args) == 0 {
		return usageError(fmt.Errorf("usage: git kura tools <subcommand> [component...]"))
	}
	switch args[0] {
	case "-h", "--help":
		if _, err := fmt.Fprintln(r.stdout, toolsHelp); err != nil {
			return err
		}
		return nil
	case "status":
		if hasHelpFlag(args[1:]) {
			if _, err := fmt.Fprintln(r.stdout, toolsStatusHelp); err != nil {
				return err
			}
			return nil
		}
		targets, err := parseToolsTargets(deps.registry, tools.CmdStatus, args[1:])
		if err != nil {
			return err
		}
		return r.cmdToolsStatus(deps, targets)
	case "install":
		if hasHelpFlag(args[1:]) {
			if _, err := fmt.Fprintln(r.stdout, toolsInstallHelp); err != nil {
				return err
			}
			return nil
		}
		targets, err := parseToolsTargets(deps.registry, tools.CmdInstall, args[1:])
		if err != nil {
			return err
		}
		return r.cmdToolsInstall(deps, targets)
	case "uninstall":
		if hasHelpFlag(args[1:]) {
			if _, err := fmt.Fprintln(r.stdout, toolsUninstallHelp); err != nil {
				return err
			}
			return nil
		}
		targets, err := parseToolsTargets(deps.registry, tools.CmdUninstall, args[1:])
		if err != nil {
			return err
		}
		return r.cmdToolsUninstall(deps, targets)
	case "run":
		return r.runToolsRun(args[1:])
	default:
		return usageError(fmt.Errorf("unknown tools subcommand: %s", args[0]))
	}
}

// usageError tags err as a usage error (exit code 2).
func usageError(err error) error {
	return exitCodeError(exitUsageError, err)
}

func parseToolsTargets(reg *tools.Registry, cmd tools.Command, args []string) ([]tools.Component, error) {
	var all bool
	var names []string
	for _, a := range args {
		switch {
		case a == "--all":
			if cmd == tools.CmdStatus {
				return nil, usageError(fmt.Errorf("git kura tools status does not support --all; run it with no component to show every component"))
			}
			all = true
		case strings.HasPrefix(a, "-"):
			return nil, usageError(fmt.Errorf("git kura tools %s: unknown flag %q", cmd, a))
		default:
			names = append(names, a)
		}
	}

	if all && len(names) > 0 {
		return nil, usageError(fmt.Errorf("git kura tools %s: --all cannot be combined with explicit component names", cmd))
	}

	switch cmd {
	case tools.CmdInstall, tools.CmdUninstall:
		if !all && len(names) == 0 {
			return nil, usageError(fmt.Errorf("git kura tools %s: specify at least one component or --all", cmd))
		}
	}

	if all || (cmd == tools.CmdStatus && len(names) == 0) {
		return registryComponents(reg, reg.IDs())
	}
	return registryComponents(reg, names)
}

func registryComponents(reg *tools.Registry, ids []string) ([]tools.Component, error) {
	targets := make([]tools.Component, 0, len(ids))
	for _, id := range ids {
		c, ok := reg.Get(id)
		if !ok {
			return nil, usageError(fmt.Errorf("unknown component %q: run \"git kura tools status\" to list components", id))
		}
		targets = append(targets, c)
	}
	return targets, nil
}

func (r *runner) cmdToolsStatus(deps toolsDeps, targets []tools.Component) error {
	repoRoot, err := r.toolsRepoRoot()
	if err != nil {
		return err
	}
	results, err := tools.Status(repoRoot, targets)
	if err != nil {
		return err
	}
	return r.reportToolsResults(results)
}

func (r *runner) cmdToolsInstall(deps toolsDeps, targets []tools.Component) error {
	repoRoot, err := r.toolsRepoRoot()
	if err != nil {
		return err
	}
	timeout, err := seal.ResolveLockTimeout(repoRoot)
	if err != nil {
		return err
	}
	result, err := tools.Install(repoRoot, deps.version, timeout, deps.fetcher, targets)
	r.printToolsWarnings(result.Warnings)
	if err != nil {
		if errors.Is(err, tools.ErrLockTimeout) {
			return exitCodeError(exitSealLockTimeout, err)
		}
		return err
	}
	return r.reportToolsResults(result.Results)
}

func (r *runner) cmdToolsUninstall(deps toolsDeps, targets []tools.Component) error {
	repoRoot, err := r.toolsRepoRoot()
	if err != nil {
		return err
	}
	timeout, err := seal.ResolveLockTimeout(repoRoot)
	if err != nil {
		return err
	}
	result, err := tools.Uninstall(repoRoot, timeout, targets)
	r.printToolsWarnings(result.Warnings)
	if err != nil {
		if errors.Is(err, tools.ErrLockTimeout) {
			return exitCodeError(exitSealLockTimeout, err)
		}
		return err
	}
	return r.reportToolsResults(result.Results)
}

func (r *runner) toolsRepoRoot() (string, error) {
	root, err := gitutil.RepoRoot()
	if err != nil {
		return "", exitCodeError(exitRepositoryContextError,
			fmt.Errorf("not-in-git-repository: not inside a git repository"))
	}
	return root, nil
}

func (r *runner) reportToolsResults(results []tools.Result) error {
	r.printToolsResults(results)
	failed := 0
	for _, res := range results {
		if res.IsFailure() {
			failed++
		}
	}
	if failed > 0 {
		return exitCodeError(exitGeneralError, fmt.Errorf("%d of %d components failed", failed, len(results)))
	}
	return nil
}

func (r *runner) printToolsResults(results []tools.Result) {
	var b strings.Builder
	for i, res := range results {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s\n", res.Component)
		fmt.Fprintf(&b, "  release version: %s\n", dashIfEmpty(res.ReleaseVersion))
		fmt.Fprintf(&b, "  source asset:    %s\n", dashIfEmpty(res.SourceAsset))
		fmt.Fprintf(&b, "  destination:     %s\n", dashIfEmpty(res.Destination))
		fmt.Fprintf(&b, "  action:          %s\n", res.Action)
		fmt.Fprintf(&b, "  managed:         %t\n", res.Managed)
		fmt.Fprintf(&b, "  reason:          %s\n", dashIfEmpty(res.Reason))
	}
	_, _ = fmt.Fprint(r.stdout, b.String())
}

func (r *runner) printToolsWarnings(warnings []string) {
	for _, w := range warnings {
		_, _ = fmt.Fprintln(r.stderr, w)
	}
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
