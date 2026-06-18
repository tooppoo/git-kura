package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tooppoo/git-kura/internal/gitutil"
	"github.com/tooppoo/git-kura/internal/worktree"
)

// preCommitComponentID is the registry ID of the pre-commit tool component.
const preCommitComponentID = "pre-commit"

// Install states recorded in the component metadata. A pending install has
// changed (or is about to change) core.hooksPath but has not yet verified the
// effective value; an installed component has completed and verified the switch.
const (
	preCommitStatePending   = "pending"
	preCommitStateInstalled = "installed"
)

// preCommitWrapperScript is the thin Husky-style wrapper Git invokes directly.
// It does nothing but hand control to the internal hook command, so all real
// logic stays in the git-kura binary and the on-disk hook never needs changing.
const preCommitWrapperScript = `#!/bin/sh
# git-kura managed pre-commit hook. Do not edit.
# Installed by: git kura tools install pre-commit
# Remove with:  git kura tools uninstall pre-commit
exec git kura tools run pre-commit "$@"
`

// preCommitMeta is the pre-commit component's slice of the framework's opaque
// componentMetadata map. It records what install changed so uninstall and
// rollback can restore the previous state, and the previous hook lookup so the
// runtime hook can chain to a user-defined pre-commit hook.
//
// Two distinct notions of "previous core.hooksPath" are tracked, because
// install only ever writes the repository-local scope:
//
//   - PreviousLocalHooksPath{State,Value} capture the repository-LOCAL config
//     value before install. Uninstall and rollback restore the local scope to
//     exactly this state (unset stays unset), so they never leave a dangling
//     local value or mask a lower-precedence (global/system) value.
//   - PreviousHooksPath{State,Value} capture the EFFECTIVE core.hooksPath (which
//     may have come from any scope) as the RAW value Git was configured with.
//     A relative value is stored verbatim and resolved at hook runtime against
//     the worktree the hook is running in — the way Git itself resolves a
//     relative core.hooksPath — so commits from a linked worktree chain that
//     worktree's hook, not the install worktree's.
type preCommitMeta struct {
	InstallState                string `json:"installState"`
	PreviousLocalHooksPathState string `json:"previousLocalHooksPathState"` // "set" | "unset"
	PreviousLocalHooksPathValue string `json:"previousLocalHooksPathValue"`
	PreviousHooksPathState      string `json:"previousHooksPathState"` // effective "set" | "unset"
	PreviousHooksPathValue      string `json:"previousHooksPathValue"` // raw effective value (may be relative)
	NewHooksPathValue           string `json:"newHooksPathValue"`
	ManagedHooksRoot            string `json:"managedHooksRoot"`
	WrapperPath                 string `json:"wrapperPath"`
	WrapperChecksum             string `json:"wrapperChecksum"`
}

func (m preCommitMeta) toMap() map[string]any {
	data, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

func preCommitMetaFromEntry(entry *toolsMetadataEntry) (preCommitMeta, bool) {
	if entry == nil || entry.ComponentMetadata == nil {
		return preCommitMeta{}, false
	}
	data, err := json.Marshal(entry.ComponentMetadata)
	if err != nil {
		return preCommitMeta{}, false
	}
	var m preCommitMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return preCommitMeta{}, false
	}
	return m, true
}

// preCommitManagedRoot is the directory tree git-kura owns for hook assets,
// "<git-common-dir>/kura/tools/hooks". Uninstall removes this whole tree.
func preCommitManagedRoot(commonDir string) string {
	return filepath.Join(commonDir, "kura", "tools", "hooks")
}

// preCommitHooksDir is the directory core.hooksPath points at,
// "<git-common-dir>/kura/tools/hooks/_". It holds the managed wrapper.
func preCommitHooksDir(commonDir string) string {
	return filepath.Join(preCommitManagedRoot(commonDir), "_")
}

func preCommitWrapperPath(commonDir string) string {
	return filepath.Join(preCommitHooksDir(commonDir), "pre-commit")
}

// preCommitComponent installs git-kura's seal guard into Git's pre-commit hook
// path using a Husky-style core.hooksPath wrapper. It never edits existing
// user-defined hook files.
type preCommitComponent struct{}

func (preCommitComponent) id() string { return preCommitComponentID }

// preCommitPrevHook is the previous hook state captured at install time. It
// separates the repository-local config value (what install/uninstall/rollback
// manage) from the effective core.hooksPath (what the runtime hook chains to,
// resolved against the running worktree at hook time).
type preCommitPrevHook struct {
	localState     string // "set" | "unset" — repository-local scope only
	localValue     string
	effectiveState string // "set" | "unset" — highest-precedence scope
	effectiveValue string // raw effective value (may be relative)
}

// --- install ---------------------------------------------------------------

func (c preCommitComponent) install(ctx toolsInstallContext) toolsOutcome {
	commonDir := ctx.commonDir
	repoRoot := ctx.repoRoot
	hooksDir := preCommitHooksDir(commonDir)
	managedRoot := preCommitManagedRoot(commonDir)
	wrapperPath := preCommitWrapperPath(commonDir)
	wrapperSum := sha256hex([]byte(preCommitWrapperScript))

	fail := func(reason string) toolsOutcome {
		return toolsOutcome{result: toolsResult{
			Component: preCommitComponentID, ReleaseVersion: ctx.releaseVersion,
			Destination: hooksDir, Action: actionFailed, Reason: reason,
		}}
	}

	// Idempotency: a fully installed and consistent component is a no-op that
	// must not disturb the recorded previous-hook metadata.
	existing, hasExisting := preCommitMetaFromEntry(ctx.entry)
	if hasExisting && existing.InstallState == preCommitStateInstalled {
		if _, ok := preCommitConsistent(repoRoot, commonDir, wrapperSum); ok {
			return toolsOutcome{result: toolsResult{
				Component: preCommitComponentID, ReleaseVersion: ctx.releaseVersion,
				Destination: hooksDir, Action: actionSkipped, Managed: true,
				Reason: "already installed; core.hooksPath and managed wrapper match",
			}}
		}
		// Installed but inconsistent: fall through to repair (re-install).
	}

	// Preflight: a higher-precedence core.hooksPath would shadow the value we
	// write to local config, so fail before changing anything persistent.
	if blocker, err := higherPrecedenceHooksPath(repoRoot); err != nil {
		return fail(fmt.Sprintf("preflight core.hooksPath check failed: %v", err))
	} else if blocker != "" {
		return fail(fmt.Sprintf("a higher-precedence core.hooksPath (%s) would override repository local config; not installing", blocker))
	}

	// Resolve the previous hook lookup before switching. On a re-install/repair
	// that already recorded previous metadata, reuse it rather than recapturing
	// git-kura's own wrapper (which would break the chain to the user hook).
	var prev preCommitPrevHook
	if hasExisting && (existing.PreviousLocalHooksPathState != "" || existing.PreviousHooksPathState != "") {
		prev = preCommitPrevHook{
			localState:     existing.PreviousLocalHooksPathState,
			localValue:     existing.PreviousLocalHooksPathValue,
			effectiveState: existing.PreviousHooksPathState,
			effectiveValue: existing.PreviousHooksPathValue,
		}
	} else {
		p, err := resolvePreviousHookLookup(repoRoot, commonDir)
		if err != nil {
			return fail(fmt.Sprintf("resolve previous hook lookup: %v", err))
		}
		prev = p
	}

	created := time.Now().UTC().Format(time.RFC3339)
	if ctx.entry != nil && ctx.entry.CreatedAt != "" {
		created = ctx.entry.CreatedAt
	}
	base := preCommitMeta{
		PreviousLocalHooksPathState: prev.localState,
		PreviousLocalHooksPathValue: prev.localValue,
		PreviousHooksPathState:      prev.effectiveState,
		PreviousHooksPathValue:      prev.effectiveValue,
		NewHooksPathValue:           hooksDir,
		ManagedHooksRoot:            managedRoot,
		WrapperPath:                 wrapperPath,
		WrapperChecksum:             wrapperSum,
	}
	buildEntry := func(state string) toolsMetadataEntry {
		m := base
		m.InstallState = state
		return toolsMetadataEntry{
			Component:         preCommitComponentID,
			ReleaseVersion:    ctx.releaseVersion,
			InstalledVersion:  ctx.releaseVersion,
			DestinationPath:   hooksDir,
			Checksum:          wrapperSum,
			ManagedMode:       managedModeConfig,
			ComponentMetadata: m.toMap(),
			CreatedAt:         created,
			UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
		}
	}

	// Persist restoration metadata in pending state before any config change, so
	// a crash never leaves core.hooksPath switched without recovery info.
	pendingEntry := buildEntry(preCommitStatePending)
	if err := persistPreCommitEntry(repoRoot, pendingEntry); err != nil {
		return fail(fmt.Sprintf("write pending metadata: %v", err))
	}

	// Write managed hook files atomically.
	if err := writeManagedWrapper(wrapperPath); err != nil {
		_ = os.RemoveAll(managedRoot)
		_ = deletePreCommitEntry(repoRoot)
		return fail(fmt.Sprintf("write managed wrapper: %v", err))
	}

	// Point repository local core.hooksPath at the managed dir (absolute path).
	if err := gitutil.SetConfigLocal(repoRoot, "core.hooksPath", hooksDir); err != nil {
		rb := rollbackPreCommit(repoRoot, managedRoot, prev)
		if rb == nil {
			_ = deletePreCommitEntry(repoRoot)
		}
		return fail(rollbackReason(fmt.Sprintf("set core.hooksPath: %v", err), rb))
	}

	// Verify the effective value resolves to the managed dir.
	if reason, ok := preCommitConsistent(repoRoot, commonDir, wrapperSum); !ok {
		rb := rollbackPreCommit(repoRoot, managedRoot, prev)
		if rb == nil {
			_ = deletePreCommitEntry(repoRoot)
			return fail(fmt.Sprintf("effective core.hooksPath verification failed (%s); rolled back", reason))
		}
		// Rollback failed: keep pending metadata so the state can be repaired and
		// surface the inconsistency instead of hiding it.
		return fail(rollbackReason(fmt.Sprintf("effective core.hooksPath verification failed (%s)", reason), rb))
	}

	// Mark installed.
	installedEntry := buildEntry(preCommitStateInstalled)
	if err := persistPreCommitEntry(repoRoot, installedEntry); err != nil {
		return fail(fmt.Sprintf("write installed metadata: %v", err))
	}

	action := actionCreated
	if hasExisting {
		action = actionUpdated
	}
	return toolsOutcome{
		result: toolsResult{
			Component: preCommitComponentID, ReleaseVersion: ctx.releaseVersion,
			Destination: hooksDir, Action: action, Managed: true,
			Reason: "core.hooksPath now points at the git-kura managed wrapper",
		},
		setEntry: &installedEntry,
	}
}

// resolvePreviousHookLookup captures the state install needs to restore later.
//
// It records two things from distinct config scopes:
//   - the repository-LOCAL core.hooksPath (set/unset and value), because install
//     only writes the local scope and uninstall/rollback must return the local
//     scope to exactly this state — never inventing a local value that masks a
//     lower-precedence (global/system) value;
//   - the EFFECTIVE core.hooksPath as the RAW value Git was configured with. A
//     relative value is stored verbatim, not resolved here, because Git resolves
//     a relative core.hooksPath against the worktree the hook runs in. Resolving
//     it against the install worktree would make linked worktrees chain the
//     wrong previous hook (see resolvePreviousPreCommitPath, called at runtime).
//
// When the local value already points at git-kura's own managed dir (an orphaned
// or repeated install), it is treated as unset so we never record ourselves as
// the local restore target.
func resolvePreviousHookLookup(repoRoot, commonDir string) (preCommitPrevHook, error) {
	hooksDir := preCommitHooksDir(commonDir)

	var prev preCommitPrevHook

	// Repository-local scope, for config restoration.
	localRaw, localSet, err := gitutil.ConfigGetLocal(repoRoot, "core.hooksPath")
	if err != nil {
		return preCommitPrevHook{}, err
	}
	prev.localState = "unset"
	if localSet {
		localValue := strings.TrimRight(localRaw, "\n")
		localAbs := localValue
		if !filepath.IsAbs(localAbs) {
			localAbs = filepath.Join(repoRoot, localAbs)
		}
		if !samePathSafe(localAbs, hooksDir) {
			prev.localState = "set"
			prev.localValue = localValue
		}
		// else: local already points at our managed dir; keep state "unset" so
		// uninstall restores to a clean unset rather than to ourselves.
	}

	// Effective value (raw), for runtime hook chaining.
	effRaw, effConfigured, err := gitutil.ConfigValue(repoRoot, "core.hooksPath")
	if err != nil {
		return preCommitPrevHook{}, err
	}
	prev.effectiveState = "unset"
	if effConfigured {
		prev.effectiveState = "set"
		prev.effectiveValue = strings.TrimRight(effRaw, "\n")
	}
	return prev, nil
}

// resolvePreviousPreCommitPath resolves the previous pre-commit hook to chain
// for a commit happening in worktreeRoot, applying Git's own runtime resolution
// rules: an unset previous core.hooksPath falls back to "<git-common-dir>/hooks",
// an absolute value is used as-is, and a relative value is resolved against the
// worktree the hook is running in (worktreeRoot) — not the install worktree.
//
// It returns "" when there is no previous hook to chain, i.e. when the resolved
// directory is git-kura's own managed dir (recursion guard).
func resolvePreviousPreCommitPath(meta preCommitMeta, worktreeRoot, commonDir string) string {
	var dir string
	if meta.PreviousHooksPathState == "set" {
		value := meta.PreviousHooksPathValue
		if filepath.IsAbs(value) {
			dir = filepath.Clean(value)
		} else {
			dir = filepath.Clean(filepath.Join(worktreeRoot, value))
		}
	} else {
		dir = filepath.Join(commonDir, "hooks")
	}
	preCommitPath := filepath.Join(dir, "pre-commit")
	if samePathSafe(dir, preCommitHooksDir(commonDir)) || samePathSafe(preCommitPath, preCommitWrapperPath(commonDir)) {
		return ""
	}
	return preCommitPath
}

// higherPrecedenceHooksPath reports a core.hooksPath value configured in a scope
// that overrides repository local config (worktree or command scope). An empty
// return means repository local config can control the effective value.
func higherPrecedenceHooksPath(repoRoot string) (string, error) {
	values, err := gitutil.ConfigGetAllWithScope(repoRoot, "core.hooksPath")
	if err != nil {
		return "", err
	}
	for _, v := range values {
		if v.Scope == "worktree" || v.Scope == "command" {
			return fmt.Sprintf("%s scope: %s", v.Scope, v.Value), nil
		}
	}
	return "", nil
}

// preCommitConsistent reports whether the effective core.hooksPath resolves to
// the managed hooks dir and the managed wrapper exists with the expected
// content. The returned string explains the first inconsistency found.
func preCommitConsistent(repoRoot, commonDir, wrapperSum string) (string, bool) {
	hooksDir := preCommitHooksDir(commonDir)
	wrapperPath := preCommitWrapperPath(commonDir)

	raw, configured, err := gitutil.ConfigValue(repoRoot, "core.hooksPath")
	if err != nil {
		return fmt.Sprintf("read core.hooksPath: %v", err), false
	}
	if !configured {
		return "core.hooksPath is unset", false
	}
	effective := strings.TrimRight(raw, "\n")
	if !filepath.IsAbs(effective) {
		effective = filepath.Join(repoRoot, effective)
	}
	if !samePathSafe(effective, hooksDir) {
		return fmt.Sprintf("effective core.hooksPath %q is not the managed dir %q", effective, hooksDir), false
	}
	data, err := os.ReadFile(wrapperPath)
	if err != nil {
		return fmt.Sprintf("managed wrapper missing: %v", err), false
	}
	if sha256hex(data) != wrapperSum {
		return "managed wrapper was modified outside git-kura", false
	}
	return "", true
}

// writeManagedWrapper atomically writes the executable wrapper script.
func writeManagedWrapper(wrapperPath string) error {
	if err := os.MkdirAll(filepath.Dir(wrapperPath), 0o755); err != nil {
		return fmt.Errorf("create managed hooks dir: %w", err)
	}
	tmp := wrapperPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(preCommitWrapperScript), 0o755); err != nil {
		return fmt.Errorf("write wrapper: %w", err)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod wrapper: %w", err)
	}
	if err := os.Rename(tmp, wrapperPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit wrapper: %w", err)
	}
	return nil
}

// rollbackPreCommit restores the repository-local core.hooksPath to its previous
// state and removes the managed hook files. It returns nil on full success, or
// the error that prevented a clean rollback.
func rollbackPreCommit(repoRoot, managedRoot string, prev preCommitPrevHook) error {
	restoreErr := restoreLocalHooksPath(repoRoot, prev.localState, prev.localValue)
	rmErr := os.RemoveAll(managedRoot)
	return errors.Join(restoreErr, rmErr)
}

// restoreLocalHooksPath returns the repository-local core.hooksPath to the
// captured state: a recorded "set" value is rewritten, while "unset" removes the
// local value entirely (so a lower-precedence global/system value is no longer
// masked).
func restoreLocalHooksPath(repoRoot, state, value string) error {
	if state == "set" {
		return gitutil.SetConfigLocal(repoRoot, "core.hooksPath", value)
	}
	return gitutil.UnsetConfigLocal(repoRoot, "core.hooksPath")
}

func rollbackReason(primary string, rb error) string {
	if rb == nil {
		return primary
	}
	return fmt.Sprintf("%s; rollback also failed: %v. State may be inconsistent; run \"git kura tools status pre-commit\" or \"git kura seal doctor\"", primary, rb)
}

// --- uninstall -------------------------------------------------------------

func (c preCommitComponent) uninstall(ctx toolsContext) toolsOutcome {
	commonDir := ctx.commonDir
	repoRoot := ctx.repoRoot
	hooksDir := preCommitHooksDir(commonDir)
	managedRoot := preCommitManagedRoot(commonDir)

	meta, ok := preCommitMetaFromEntry(ctx.entry)
	if !ok {
		return toolsOutcome{result: toolsResult{
			Component: preCommitComponentID, Action: actionNotInstalled,
			Reason: "no install metadata; nothing to remove",
		}}
	}

	// Decide restoration on the repository-LOCAL value install actually wrote,
	// not the effective value: a higher-precedence (worktree/command) override
	// can shadow the effective value while local config still points at the
	// managed dir, and leaving that dangling local value would re-break commits
	// once the override is removed. If a user changed the local value, leave it.
	localRaw, localSet, err := gitutil.ConfigGetLocal(repoRoot, "core.hooksPath")
	if err != nil {
		return toolsOutcome{result: toolsResult{
			Component: preCommitComponentID, Action: actionFailed,
			Reason: fmt.Sprintf("read local core.hooksPath: %v", err),
		}}
	}
	localValue := strings.TrimRight(localRaw, "\n")
	localAbs := localValue
	if localSet && !filepath.IsAbs(localAbs) {
		localAbs = filepath.Join(repoRoot, localAbs)
	}
	localPointsAtManaged := localSet && samePathSafe(localAbs, hooksDir)

	if localPointsAtManaged {
		if restoreErr := restoreLocalHooksPath(repoRoot, meta.PreviousLocalHooksPathState, meta.PreviousLocalHooksPathValue); restoreErr != nil {
			return toolsOutcome{result: toolsResult{
				Component: preCommitComponentID, Action: actionFailed,
				Reason: fmt.Sprintf("restore previous local core.hooksPath: %v", restoreErr),
			}}
		}
	}

	// Remove only git-kura managed hook files. User-defined hooks are never
	// touched; previous hook files were never modified, so nothing is restored.
	if err := os.RemoveAll(managedRoot); err != nil {
		return toolsOutcome{result: toolsResult{
			Component: preCommitComponentID, Action: actionFailed,
			Reason: fmt.Sprintf("remove managed hook files: %v", err),
		}}
	}

	reason := "removed managed wrapper and restored repository-local core.hooksPath"
	if !localPointsAtManaged {
		reason = "removed managed wrapper; repository-local core.hooksPath was not the managed dir and was left untouched"
	}
	return toolsOutcome{
		result: toolsResult{
			Component: preCommitComponentID, Destination: hooksDir,
			Action: actionRemoved, Managed: true, Reason: reason,
		},
		deleteEntry: true,
	}
}

// --- status ----------------------------------------------------------------

func (c preCommitComponent) status(ctx toolsContext) toolsOutcome {
	commonDir := ctx.commonDir
	repoRoot := ctx.repoRoot
	hooksDir := preCommitHooksDir(commonDir)
	wrapperSum := sha256hex([]byte(preCommitWrapperScript))

	meta, hasMeta := preCommitMetaFromEntry(ctx.entry)
	if !hasMeta {
		return toolsOutcome{result: toolsResult{
			Component: preCommitComponentID, Destination: hooksDir,
			Action: actionNotInstalled,
			Reason: "not installed; install with \"git kura tools install pre-commit\"",
		}}
	}

	diag := collectPreCommitDiagnostics(repoRoot, commonDir, meta)
	reason, consistent := preCommitConsistent(repoRoot, commonDir, wrapperSum)

	action := actionInstalled
	switch {
	case meta.InstallState == preCommitStatePending:
		action = actionSkipped
		diag.installState = "pending"
	case !consistent:
		action = actionSkipped
		diag.installState = "inconsistent"
	default:
		diag.installState = "installed"
	}

	return toolsOutcome{result: toolsResult{
		Component: preCommitComponentID, ReleaseVersion: ctx.entry.ReleaseVersion,
		Destination: hooksDir, Action: action, Managed: true,
		Reason: diag.format(reason),
	}}
}

// preCommitDiagnostics is the diagnostic view status reports. The worktree/key
// fields are derived from the status command's own context and are only
// approximations of what the hook resolves at runtime.
type preCommitDiagnostics struct {
	currentHooksPath        string
	effectiveHooksPath      string
	managedHooksPath        string
	previousHooksPath       string
	previousHookDirectory   string
	previousPreCommitPath   string
	previousPreCommitExists bool
	previousPreCommitExec   bool
	hookWorktreeRoot        string
	hookGitCommonDir        string
	currentKey              string
	currentKeySource        string
	installState            string
	mismatch                bool
}

func collectPreCommitDiagnostics(repoRoot, commonDir string, meta preCommitMeta) preCommitDiagnostics {
	d := preCommitDiagnostics{
		managedHooksPath:  preCommitHooksDir(commonDir),
		previousHooksPath: meta.PreviousLocalHooksPathValue,
		hookWorktreeRoot:  repoRoot,
		hookGitCommonDir:  commonDir,
	}
	// previousPreCommitPath is resolved against the status command's own worktree
	// (a diagnostic approximation); the hook resolves it again at runtime against
	// the worktree the commit happens in. An empty result means no previous hook
	// would be chained.
	if previousPreCommit := resolvePreviousPreCommitPath(meta, repoRoot, commonDir); previousPreCommit != "" {
		d.previousPreCommitPath = previousPreCommit
		d.previousHookDirectory = filepath.Dir(previousPreCommit)
		if info, err := os.Stat(previousPreCommit); err == nil {
			d.previousPreCommitExists = true
			d.previousPreCommitExec = !info.IsDir() && info.Mode().Perm()&0o111 != 0
		}
	}
	if local, ok, _ := gitutil.ConfigGetLocal(repoRoot, "core.hooksPath"); ok {
		d.currentHooksPath = strings.TrimRight(local, "\n")
	}
	if eff, ok, _ := gitutil.ConfigValue(repoRoot, "core.hooksPath"); ok {
		d.effectiveHooksPath = strings.TrimRight(eff, "\n")
	}
	d.mismatch = d.currentHooksPath != d.effectiveHooksPath

	stateDir := filepath.Join(commonDir, "kura")
	keys, err := worktree.ResolveKeyForWorktreeRoot(stateDir, repoRoot)
	switch {
	case err != nil:
		d.currentKey = "none"
		d.currentKeySource = "error"
	case len(keys) == 1:
		d.currentKey = keys[0]
		d.currentKeySource = "managed-worktree"
	case len(keys) == 0:
		d.currentKey = "none"
		d.currentKeySource = "unmanaged-worktree"
	default:
		d.currentKey = "none"
		d.currentKeySource = "ambiguous"
	}
	return d
}

func (d preCommitDiagnostics) format(consistencyReason string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "installState=%s", d.installState)
	if consistencyReason != "" {
		fmt.Fprintf(&b, " (%s)", consistencyReason)
	}
	fmt.Fprintf(&b, "; currentHooksPath=%s", dashIfEmpty(d.currentHooksPath))
	fmt.Fprintf(&b, "; effectiveHooksPath=%s", dashIfEmpty(d.effectiveHooksPath))
	fmt.Fprintf(&b, "; managedHooksPath=%s", d.managedHooksPath)
	if d.mismatch {
		b.WriteString("; hooksPathMismatch=true")
	}
	fmt.Fprintf(&b, "; previousHooksPath=%s", dashIfEmpty(d.previousHooksPath))
	fmt.Fprintf(&b, "; previousHookDirectory=%s", dashIfEmpty(d.previousHookDirectory))
	fmt.Fprintf(&b, "; previousPreCommitPath=%s", dashIfEmpty(d.previousPreCommitPath))
	fmt.Fprintf(&b, "; previousPreCommitExists=%t", d.previousPreCommitExists)
	fmt.Fprintf(&b, "; previousPreCommitExecutable=%t", d.previousPreCommitExec)
	fmt.Fprintf(&b, "; hookWorktreeRoot=%s", d.hookWorktreeRoot)
	fmt.Fprintf(&b, "; hookGitCommonDir=%s", d.hookGitCommonDir)
	fmt.Fprintf(&b, "; currentKey=%s", d.currentKey)
	fmt.Fprintf(&b, "; currentKeySource=%s", d.currentKeySource)
	b.WriteString("; bypass: git commit --no-verify")
	return b.String()
}

// --- metadata persistence helpers ------------------------------------------
//
// install/uninstall hold the tools metadata lock, so these read-modify-write
// helpers are race-free. They let the component persist a pending entry before
// changing config (durability for crash recovery) independently of the
// framework's final whole-store write.

func persistPreCommitEntry(repoRoot string, entry toolsMetadataEntry) error {
	storeFile, _, err := toolsMetadataPaths(repoRoot)
	if err != nil {
		return err
	}
	store, err := readToolsMetadata(storeFile)
	if err != nil {
		return err
	}
	store.Components[preCommitComponentID] = entry
	return writeToolsMetadata(storeFile, store)
}

func deletePreCommitEntry(repoRoot string) error {
	storeFile, _, err := toolsMetadataPaths(repoRoot)
	if err != nil {
		return err
	}
	store, err := readToolsMetadata(storeFile)
	if err != nil {
		return err
	}
	if _, ok := store.Components[preCommitComponentID]; !ok {
		return nil
	}
	delete(store.Components, preCommitComponentID)
	return writeToolsMetadata(storeFile, store)
}

// samePathSafe is a non-panicking path equality check tolerant of symlinked
// prefixes; empty inputs never match.
func samePathSafe(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// --- runtime hook: git kura tools run pre-commit ---------------------------

const toolsRunHelp = `Usage: git kura tools run pre-commit [hook-args...]

Internal command invoked by the git-kura managed pre-commit hook wrapper. It is
not a primary user command; install, uninstall, and inspect the pre-commit
component with "git kura tools install|uninstall|status pre-commit".

It runs the same path-level seal decision as "git kura seal test" against the
staged files, chains any previously configured pre-commit hook, and re-checks
the staged files afterward. The commit is rejected when any check fails. This is
a local safety guard only; "git commit --no-verify" bypasses it.`

func runToolsRun(args []string) error {
	if hasHelpFlag(args) {
		fmt.Println(toolsRunHelp)
		return nil
	}
	if len(args) == 0 {
		return usageError(fmt.Errorf("usage: git kura tools run <hook> [hook-args...]"))
	}
	switch args[0] {
	case "pre-commit":
		return runPreCommitHook(args[1:])
	default:
		return usageError(fmt.Errorf("unknown tools run target %q: only \"pre-commit\" is supported", args[0]))
	}
}

// failClosed wraps a hook context/store failure as a general (exit 1) error so
// the commit is rejected rather than silently allowed.
func failClosed(format string, a ...any) error {
	return exitCodeError(exitGeneralError, fmt.Errorf(format, a...))
}

func runPreCommitHook(hookArgs []string) error {
	// Resolve Git context from the hook process. Git runs pre-commit hooks from
	// the working-tree top, so the process cwd is the worktree root.
	worktreeRoot, err := gitutil.RepoRoot()
	if err != nil {
		return failClosed("pre-commit hook: not inside a git repository")
	}
	commonDir, err := gitutil.CommonDir(worktreeRoot)
	if err != nil {
		return failClosed("pre-commit hook: resolve git common dir: %v", err)
	}

	currentKey, err := resolveHookCurrentKey(commonDir, worktreeRoot)
	if err != nil {
		return err
	}

	storeFile, _, err := pathsSealStore(worktreeRoot)
	if err != nil {
		return failClosed("pre-commit hook: %v", err)
	}

	// Pre-hook check: reject before the previous hook can touch files another
	// key owns.
	if err := preCommitSealCheck(storeFile, currentKey, worktreeRoot, "pre-hook"); err != nil {
		return err
	}

	// Chain a previously configured pre-commit hook, if any.
	if err := runPreviousPreCommit(worktreeRoot, commonDir, hookArgs); err != nil {
		return err
	}

	// Post-hook check: validate the final staged state after the previous hook
	// may have modified or added files.
	if err := preCommitSealCheck(storeFile, currentKey, worktreeRoot, "post-hook"); err != nil {
		return err
	}
	return nil
}

// resolveHookCurrentKey maps the worktree Git is running the hook for to a
// managed key. No match is "none" (sealKeyNone); an ambiguous match or a
// metadata read error is fail-closed.
func resolveHookCurrentKey(commonDir, worktreeRoot string) (string, error) {
	stateDir := filepath.Join(commonDir, "kura")
	keys, err := worktree.ResolveKeyForWorktreeRoot(stateDir, worktreeRoot)
	if err != nil {
		return "", failClosed("pre-commit hook: resolve current key: %v", err)
	}
	switch len(keys) {
	case 0:
		return sealKeyNone, nil
	case 1:
		return keys[0], nil
	default:
		return "", failClosed("pre-commit hook: %d managed worktrees match %s; refusing to guess current key", len(keys), worktreeRoot)
	}
}

func preCommitSealCheck(storeFile, currentKey, worktreeRoot, phase string) error {
	staged, err := gitutil.StagedFiles(worktreeRoot)
	if err != nil {
		return failClosed("pre-commit hook (%s): %v", phase, err)
	}
	store, err := readSealStore(storeFile)
	if err != nil {
		return failClosed("pre-commit hook (%s): cannot read seal store; run \"git kura seal doctor\": %v", phase, err)
	}
	conflicts := evaluateSealedPaths(store, currentKey, staged)
	if len(conflicts) > 0 {
		return sealConflictError(conflicts)
	}
	return nil
}

// runPreviousPreCommit chains the user-defined pre-commit hook recorded at
// install time, preserving Git hook semantics: it runs only an executable file
// directly (never through a shell), inherits the process's working directory,
// argv, standard streams, and environment, and skips a missing or non-executable
// hook (warning in the latter case). It returns the previous hook's exit code on
// failure.
func runPreviousPreCommit(worktreeRoot, commonDir string, hookArgs []string) error {
	storeFile, _, err := toolsMetadataPaths(worktreeRoot)
	if err != nil {
		return failClosed("pre-commit hook: %v", err)
	}
	store, err := readToolsMetadata(storeFile)
	if err != nil {
		return failClosed("pre-commit hook: read tools metadata: %v", err)
	}
	entry, ok := store.Components[preCommitComponentID]
	if !ok {
		return nil
	}
	meta, ok := preCommitMetaFromEntry(&entry)
	if !ok {
		return nil
	}
	// Resolve the previous hook against THIS worktree, so a relative previous
	// core.hooksPath chains the hook for the worktree the commit is happening in
	// (Git's own semantics), not the worktree install ran in. The resolver also
	// applies the recursion guard against git-kura's managed wrapper.
	prevPath := resolvePreviousPreCommitPath(meta, worktreeRoot, commonDir)
	if prevPath == "" {
		return nil
	}

	info, err := os.Stat(prevPath)
	if err != nil {
		// Missing previous hook: skip silently, matching Git semantics.
		return nil
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		fmt.Fprintf(os.Stderr, "git-kura: previous pre-commit hook %s is not executable; skipping\n", prevPath)
		return nil
	}

	cmd := exec.Command(prevPath, hookArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Inherit cwd (cmd.Dir == "") and environment (cmd.Env == nil) unchanged.
	var exitErr *exec.ExitError
	if err := cmd.Run(); err != nil {
		if errors.As(err, &exitErr) {
			return exitCodeError(exitErr.ExitCode(), fmt.Errorf("previous pre-commit hook failed"))
		}
		return failClosed("run previous pre-commit hook %s: %v", prevPath, err)
	}
	return nil
}
