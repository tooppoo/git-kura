package main

import (
	"fmt"
	"strings"

	"github.com/tooppoo/git-kura/internal/gitutil"
)

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
// and tests override: the component registry, the release-asset fetcher, and
// the running binary's version. Injecting these lets framework tests use a
// test-only registry and in-memory release assets without touching the
// production registry or the network.
type toolsDeps struct {
	registry *toolsRegistry
	fetcher  releaseFetcher
	version  string
}

func runTools(args []string) error {
	deps := toolsDeps{
		registry: productionToolsRegistry(),
		fetcher:  newGithubReleaseFetcher(),
		version:  version,
	}
	return runToolsWith(deps, args)
}

func runToolsWith(deps toolsDeps, args []string) error {
	if len(args) == 0 {
		return usageError(fmt.Errorf("usage: git kura tools <subcommand> [component...]"))
	}
	switch args[0] {
	case "-h", "--help":
		fmt.Println(toolsHelp)
		return nil
	case "status":
		if hasHelpFlag(args[1:]) {
			fmt.Println(toolsStatusHelp)
			return nil
		}
		targets, err := parseToolsTargets(deps.registry, toolsCmdStatus, args[1:])
		if err != nil {
			return err
		}
		return cmdToolsStatus(deps, targets)
	case "install":
		if hasHelpFlag(args[1:]) {
			fmt.Println(toolsInstallHelp)
			return nil
		}
		targets, err := parseToolsTargets(deps.registry, toolsCmdInstall, args[1:])
		if err != nil {
			return err
		}
		return cmdToolsInstall(deps, targets)
	case "uninstall":
		if hasHelpFlag(args[1:]) {
			fmt.Println(toolsUninstallHelp)
			return nil
		}
		targets, err := parseToolsTargets(deps.registry, toolsCmdUninstall, args[1:])
		if err != nil {
			return err
		}
		return cmdToolsUninstall(deps, targets)
	case "run":
		// Internal command invoked by managed hook wrappers (e.g. the pre-commit
		// wrapper). It resolves its own Git/seal context and does not use the
		// component registry or release assets.
		return runToolsRun(args[1:])
	default:
		return usageError(fmt.Errorf("unknown tools subcommand: %s", args[0]))
	}
}

// usageError tags err as a usage error (exit code 2).
func usageError(err error) error {
	return exitCodeError(exitUsageError, err)
}

// parseToolsTargets parses the component arguments and --all flag for cmd and
// resolves them to concrete components from the registry. It enforces the
// command-specific argument rules and rejects unknown components and flags as
// usage errors.
func parseToolsTargets(reg *toolsRegistry, cmd toolsCommand, args []string) ([]toolsComponent, error) {
	var all bool
	var names []string
	for _, a := range args {
		switch {
		case a == "--all":
			if cmd == toolsCmdStatus {
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
	case toolsCmdInstall, toolsCmdUninstall:
		if !all && len(names) == 0 {
			return nil, usageError(fmt.Errorf("git kura tools %s: specify at least one component or --all", cmd))
		}
	}

	if all || (cmd == toolsCmdStatus && len(names) == 0) {
		return registryComponents(reg, reg.ids())
	}
	return registryComponents(reg, names)
}

// registryComponents resolves ids to components, returning a usage error for
// any unknown ID.
func registryComponents(reg *toolsRegistry, ids []string) ([]toolsComponent, error) {
	targets := make([]toolsComponent, 0, len(ids))
	for _, id := range ids {
		c, ok := reg.get(id)
		if !ok {
			return nil, usageError(fmt.Errorf("unknown component %q: run \"git kura tools status\" to list components", id))
		}
		targets = append(targets, c)
	}
	return targets, nil
}

// entryFor returns a copy of the component's metadata entry, or nil when it has
// none. Returning a copy keeps a component from mutating the store directly; all
// changes flow back through toolsOutcome.
func entryFor(store toolsMetadataStore, id string) *toolsMetadataEntry {
	if e, ok := store.Components[id]; ok {
		return &e
	}
	return nil
}

// verifyAction guards against a component returning an action that is invalid
// for the command. That can only be a component bug, so the result is rewritten
// to failed and any metadata mutation it requested is dropped.
func verifyAction(cmd toolsCommand, comp toolsComponent, o toolsOutcome) toolsOutcome {
	if actionValidFor(cmd, o.result.Action) {
		return o
	}
	return toolsOutcome{result: toolsResult{
		Component: comp.id(),
		Action:    actionFailed,
		Reason:    fmt.Sprintf("internal: component returned action %q which is not valid for %s", o.result.Action, cmd),
	}}
}

// applyOutcome applies a component's requested metadata mutation to store and
// reports whether the store changed.
func applyOutcome(store *toolsMetadataStore, o toolsOutcome) bool {
	id := o.result.Component
	if o.deleteEntry {
		if _, ok := store.Components[id]; ok {
			delete(store.Components, id)
			return true
		}
		return false
	}
	if o.setEntry != nil {
		store.Components[id] = *o.setEntry
		return true
	}
	return false
}

func cmdToolsStatus(deps toolsDeps, targets []toolsComponent) error {
	repoRoot, commonDir, err := toolsRepoDirs()
	if err != nil {
		return err
	}
	storeFile, _, err := toolsMetadataPaths(repoRoot)
	if err != nil {
		return err
	}
	// status is read-only and takes no lock: a held install/uninstall lock must
	// never block status.
	store, err := readToolsMetadata(storeFile)
	if err != nil {
		return err
	}

	results := make([]toolsResult, 0, len(targets))
	for _, comp := range targets {
		ctx := toolsContext{repoRoot: repoRoot, commonDir: commonDir, entry: entryFor(store, comp.id())}
		o := verifyAction(toolsCmdStatus, comp, comp.status(ctx))
		results = append(results, o.result)
	}
	return reportToolsResults(results)
}

func cmdToolsInstall(deps toolsDeps, targets []toolsComponent) error {
	repoRoot, commonDir, err := toolsRepoDirs()
	if err != nil {
		return err
	}

	storeFile, lockFile, err := toolsMetadataPaths(repoRoot)
	if err != nil {
		return err
	}
	timeout, err := resolveSealLockTimeout(repoRoot)
	if err != nil {
		return err
	}
	// Hold the metadata lock across asset resolution and the component installs.
	// Resolution extracts the verified archive into the per-version cache under
	// the git common dir, replacing that directory in place; the same lock that
	// serializes the metadata read-modify-write therefore also serializes cache
	// access, so a concurrent install cannot replace the cache while a component
	// is reading from the verified asset root.
	release, err := acquireToolsLock(lockFile, timeout)
	if err != nil {
		return err
	}
	defer release()

	// A resolution failure (not an official release, unsupported checksum
	// algorithm, checksum mismatch, or a cache that disagrees with the release)
	// means no component can install: report each target as failed and change
	// nothing.
	resolver := &toolsAssetResolver{version: deps.version, commonDir: commonDir, fetcher: deps.fetcher}
	asset, err := resolver.resolve()
	if err != nil {
		results := make([]toolsResult, 0, len(targets))
		for _, comp := range targets {
			results = append(results, toolsResult{
				Component: comp.id(),
				Action:    actionFailed,
				Reason:    err.Error(),
			})
		}
		return reportToolsResults(results)
	}

	store, err := readToolsMetadata(storeFile)
	if err != nil {
		return err
	}

	results := make([]toolsResult, 0, len(targets))
	changed := false
	for _, comp := range targets {
		ctx := toolsInstallContext{
			toolsContext:   toolsContext{repoRoot: repoRoot, commonDir: commonDir, entry: entryFor(store, comp.id())},
			releaseVersion: asset.version,
			asset:          asset,
		}
		o := verifyAction(toolsCmdInstall, comp, comp.install(ctx))
		if applyOutcome(&store, o) {
			changed = true
		}
		results = append(results, o.result)
	}

	if changed {
		if err := writeToolsMetadata(storeFile, store); err != nil {
			return err
		}
	}
	return reportToolsResults(results)
}

func cmdToolsUninstall(deps toolsDeps, targets []toolsComponent) error {
	repoRoot, commonDir, err := toolsRepoDirs()
	if err != nil {
		return err
	}
	storeFile, lockFile, err := toolsMetadataPaths(repoRoot)
	if err != nil {
		return err
	}
	timeout, err := resolveSealLockTimeout(repoRoot)
	if err != nil {
		return err
	}
	release, err := acquireToolsLock(lockFile, timeout)
	if err != nil {
		return err
	}
	defer release()

	store, err := readToolsMetadata(storeFile)
	if err != nil {
		return err
	}

	results := make([]toolsResult, 0, len(targets))
	changed := false
	for _, comp := range targets {
		ctx := toolsContext{repoRoot: repoRoot, commonDir: commonDir, entry: entryFor(store, comp.id())}
		o := verifyAction(toolsCmdUninstall, comp, comp.uninstall(ctx))
		if applyOutcome(&store, o) {
			changed = true
		}
		results = append(results, o.result)
	}

	if changed {
		if err := writeToolsMetadata(storeFile, store); err != nil {
			return err
		}
	}
	return reportToolsResults(results)
}

func toolsRepoDirs() (repoRoot, commonDir string, err error) {
	repoRoot, err = gitutil.RepoRoot()
	if err != nil {
		return "", "", exitCodeError(exitRepositoryContextError,
			fmt.Errorf("not-in-git-repository: not inside a git repository"))
	}
	commonDir, err = gitutil.CommonDir(repoRoot)
	if err != nil {
		return "", "", fmt.Errorf("get git common dir: %w", err)
	}
	return repoRoot, commonDir, nil
}

// reportToolsResults prints every result and returns a non-zero-exit error when
// at least one component failed. skipped and not-installed never make the
// command fail.
func reportToolsResults(results []toolsResult) error {
	printToolsResults(results)
	failed := 0
	for _, r := range results {
		if r.isFailure() {
			failed++
		}
	}
	if failed > 0 {
		return exitCodeError(exitGeneralError, fmt.Errorf("%d of %d components failed", failed, len(results)))
	}
	return nil
}

func printToolsResults(results []toolsResult) {
	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s\n", r.Component)
		fmt.Fprintf(&b, "  release version: %s\n", dashIfEmpty(r.ReleaseVersion))
		fmt.Fprintf(&b, "  source asset:    %s\n", dashIfEmpty(r.SourceAsset))
		fmt.Fprintf(&b, "  destination:     %s\n", dashIfEmpty(r.Destination))
		fmt.Fprintf(&b, "  action:          %s\n", r.Action)
		fmt.Fprintf(&b, "  managed:         %t\n", r.Managed)
		fmt.Fprintf(&b, "  reason:          %s\n", dashIfEmpty(r.Reason))
	}
	fmt.Print(b.String())
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
