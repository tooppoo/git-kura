package tools

import (
	"fmt"
	"time"

	"github.com/tooppoo/git-kura/internal/gitutil"
)

// ServiceResult carries the results and any non-fatal warnings from a
// mutating service operation (install or uninstall).
type ServiceResult struct {
	Results  []Result
	Warnings []string
}

// Status runs the status operation for the given components and returns their results.
func Status(repoRoot string, targets []Component) ([]Result, error) {
	commonDir, err := gitutil.CommonDir(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("get git common dir: %w", err)
	}

	storeFile, _, err := MetadataPaths(repoRoot)
	if err != nil {
		return nil, err
	}
	store, err := ReadMetadata(storeFile)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(targets))
	for _, comp := range targets {
		ctx := Context{RepoRoot: repoRoot, CommonDir: commonDir, Entry: entryFor(store, comp.ID())}
		o := verifyAction(CmdStatus, comp, comp.Status(ctx))
		results = append(results, o.Result)
	}
	return results, nil
}

// Install runs the install operation for the given components and returns a ServiceResult.
func Install(repoRoot, version string, timeout time.Duration, fetcher Fetcher, targets []Component) (result ServiceResult, err error) {
	commonDir, err := gitutil.CommonDir(repoRoot)
	if err != nil {
		return result, fmt.Errorf("get git common dir: %w", err)
	}

	storeFile, lockFile, err := MetadataPaths(repoRoot)
	if err != nil {
		return result, err
	}

	release, err := acquireLock(lockFile, timeout)
	if err != nil {
		return result, err
	}

	defer func() {
		if releaseErr := release(); releaseErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("warning: %v", releaseErr))
		}
	}()

	resolver := &assetResolver{version: version, commonDir: commonDir, fetcher: fetcher}
	asset, err := resolver.resolve()
	if err != nil {
		results := make([]Result, 0, len(targets))
		for _, comp := range targets {
			results = append(results, Result{Component: comp.ID(), Action: ActionFailed, Reason: err.Error()})
		}
		result.Results = results
		return result, nil
	}

	store, err := ReadMetadata(storeFile)
	if err != nil {
		return result, err
	}

	results := make([]Result, 0, len(targets))
	changed := false
	for _, comp := range targets {
		ctx := InstallContext{
			Context:        Context{RepoRoot: repoRoot, CommonDir: commonDir, Entry: entryFor(store, comp.ID())},
			ReleaseVersion: asset.Version(),
			Asset:          asset,
		}
		o := verifyAction(CmdInstall, comp, comp.Install(ctx))
		if applyOutcome(&store, o) {
			changed = true
		}
		results = append(results, o.Result)
	}

	if changed {
		if err := WriteMetadata(storeFile, store); err != nil {
			return result, err
		}
	}
	result.Results = results
	return result, nil
}

// Uninstall runs the uninstall operation for the given components and returns a ServiceResult.
func Uninstall(repoRoot string, timeout time.Duration, targets []Component) (result ServiceResult, err error) {
	commonDir, err := gitutil.CommonDir(repoRoot)
	if err != nil {
		return result, fmt.Errorf("get git common dir: %w", err)
	}

	storeFile, lockFile, err := MetadataPaths(repoRoot)
	if err != nil {
		return result, err
	}

	release, err := acquireLock(lockFile, timeout)
	if err != nil {
		return result, err
	}

	defer func() {
		if releaseErr := release(); releaseErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("warning: %v", releaseErr))
		}
	}()

	store, err := ReadMetadata(storeFile)
	if err != nil {
		return result, err
	}

	results := make([]Result, 0, len(targets))
	changed := false
	for _, comp := range targets {
		ctx := Context{RepoRoot: repoRoot, CommonDir: commonDir, Entry: entryFor(store, comp.ID())}
		o := verifyAction(CmdUninstall, comp, comp.Uninstall(ctx))
		if applyOutcome(&store, o) {
			changed = true
		}
		results = append(results, o.Result)
	}

	if changed {
		if err := WriteMetadata(storeFile, store); err != nil {
			return result, err
		}
	}
	result.Results = results
	return result, nil
}

// entryFor returns a copy of the component's metadata entry, or nil when absent.
func entryFor(store MetadataStore, id string) *MetadataEntry {
	if e, ok := store.Components[id]; ok {
		return &e
	}
	return nil
}

// verifyAction guards against a component returning an invalid action for the command.
func verifyAction(cmd Command, comp Component, o Outcome) Outcome {
	if actionValidFor(cmd, o.Result.Action) {
		return o
	}
	return Outcome{Result: Result{
		Component: comp.ID(),
		Action:    ActionFailed,
		Reason:    fmt.Sprintf("internal: component returned action %q which is not valid for %s", o.Result.Action, cmd),
	}}
}

// applyOutcome applies a component's metadata mutation to store and reports
// whether the store changed.
func applyOutcome(store *MetadataStore, o Outcome) bool {
	id := o.Result.Component
	if o.DeleteEntry {
		if _, ok := store.Components[id]; ok {
			delete(store.Components, id)
			return true
		}
		return false
	}
	if o.SetEntry != nil {
		store.Components[id] = *o.SetEntry
		return true
	}
	return false
}
