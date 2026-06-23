package tools

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/tooppoo/git-kura/internal/gitutil"
	"github.com/tooppoo/git-kura/internal/worktree"
)

// PreCommitComponentID is the registry ID of the pre-commit tool component.
const PreCommitComponentID = "pre-commit"

// Install states recorded in the component metadata.
const (
	PreCommitStatePending   = "pending"
	PreCommitStateInstalled = "installed"
)

// PreCommitWrapperScript is the thin Husky-style wrapper Git invokes directly.
const PreCommitWrapperScript = `#!/bin/sh
# git-kura managed pre-commit hook. Do not edit.
# Installed by: git kura tools install pre-commit
# Remove with:  git kura tools uninstall pre-commit
exec git kura tools run pre-commit "$@"
`

//go:embed schema/pre_commit_meta.schema.json
var preCommitMetaSchemaJSON []byte

var (
	preCommitMetaSchemaOnce sync.Once
	preCommitMetaSchemaVal  *jsonschema.Schema
	preCommitMetaSchemaErr  error
)

func getPreCommitMetaSchema() (*jsonschema.Schema, error) {
	preCommitMetaSchemaOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(preCommitMetaSchemaJSON))
		if err != nil {
			preCommitMetaSchemaErr = fmt.Errorf("parse pre-commit meta schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource("pre_commit_meta.schema.json", doc); err != nil {
			preCommitMetaSchemaErr = fmt.Errorf("add pre-commit meta schema resource: %w", err)
			return
		}
		sch, err := c.Compile("pre_commit_meta.schema.json")
		if err != nil {
			preCommitMetaSchemaErr = fmt.Errorf("compile pre-commit meta schema: %w", err)
			return
		}
		preCommitMetaSchemaVal = sch
	})
	return preCommitMetaSchemaVal, preCommitMetaSchemaErr
}

// PreCommitMeta is the pre-commit component's slice of the framework's opaque
// componentMetadata map.
type PreCommitMeta struct {
	InstallState                string `json:"installState"`
	PreviousLocalHooksPathState string `json:"previousLocalHooksPathState"`
	PreviousLocalHooksPathValue string `json:"previousLocalHooksPathValue"`
	PreviousHooksPathState      string `json:"previousHooksPathState"`
	PreviousHooksPathValue      string `json:"previousHooksPathValue"`
	NewHooksPathValue           string `json:"newHooksPathValue"`
	ManagedHooksRoot            string `json:"managedHooksRoot"`
	WrapperPath                 string `json:"wrapperPath"`
	WrapperChecksum             string `json:"wrapperChecksum"`
}

// ToMap converts PreCommitMeta to map[string]any for storage in MetadataEntry.ComponentMetadata.
func (m PreCommitMeta) ToMap() map[string]any {
	data, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

// PreCommitMetaFromEntry decodes and validates the pre-commit component metadata
// from a MetadataEntry.
func PreCommitMetaFromEntry(entry *MetadataEntry) (PreCommitMeta, bool) {
	sch, err := getPreCommitMetaSchema()
	if err != nil {
		return PreCommitMeta{}, false
	}
	if entry == nil || entry.ComponentMetadata == nil {
		return PreCommitMeta{}, false
	}
	data, err := json.Marshal(entry.ComponentMetadata)
	if err != nil {
		return PreCommitMeta{}, false
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return PreCommitMeta{}, false
	}
	if err := sch.Validate(inst); err != nil {
		return PreCommitMeta{}, false
	}
	var m PreCommitMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return PreCommitMeta{}, false
	}
	return m, true
}

// PreCommitManagedRoot is the directory git-kura owns for hook assets.
func PreCommitManagedRoot(commonDir string) string {
	return filepath.Join(filepath.Dir(commonDir), ".kura", "tools", "hooks")
}

// PreCommitHooksDir is the directory core.hooksPath points at.
func PreCommitHooksDir(commonDir string) string {
	return filepath.Join(PreCommitManagedRoot(commonDir), "_")
}

// PreCommitWrapperPath is the path of the managed wrapper script.
func PreCommitWrapperPath(commonDir string) string {
	return filepath.Join(PreCommitHooksDir(commonDir), "pre-commit")
}

// PreCommitComponent installs git-kura's seal guard into Git's pre-commit hook
// path using a Husky-style core.hooksPath wrapper.
type PreCommitComponent struct{}

func (PreCommitComponent) ID() string { return PreCommitComponentID }

// preCommitPrevHook captures the previous hook state at install time.
type preCommitPrevHook struct {
	localState     string
	localValue     string
	effectiveState string
	effectiveValue string
}

// --- install -----------------------------------------------------------------

func (c PreCommitComponent) Install(ctx InstallContext) Outcome {
	commonDir := ctx.CommonDir
	repoRoot := ctx.RepoRoot
	hooksDir := PreCommitHooksDir(commonDir)
	managedRoot := PreCommitManagedRoot(commonDir)
	wrapperPath := PreCommitWrapperPath(commonDir)
	wrapperSum := SHA256Hex([]byte(PreCommitWrapperScript))

	fail := func(reason string) Outcome {
		return Outcome{Result: Result{
			Component: PreCommitComponentID, ReleaseVersion: ctx.ReleaseVersion,
			Destination: hooksDir, Action: ActionFailed, Reason: reason,
		}}
	}

	existing, hasExisting := PreCommitMetaFromEntry(ctx.Entry)
	if hasExisting && existing.InstallState == PreCommitStateInstalled {
		if _, ok := preCommitConsistent(repoRoot, commonDir, wrapperSum); ok {
			return Outcome{Result: Result{
				Component: PreCommitComponentID, ReleaseVersion: ctx.ReleaseVersion,
				Destination: hooksDir, Action: ActionSkipped, Managed: true,
				Reason: "already installed; core.hooksPath and managed wrapper match",
			}}
		}
	}

	if blocker, err := higherPrecedenceHooksPath(repoRoot); err != nil {
		return fail(fmt.Sprintf("preflight core.hooksPath check failed: %v", err))
	} else if blocker != "" {
		return fail(fmt.Sprintf("a higher-precedence core.hooksPath (%s) would override repository local config; not installing", blocker))
	}

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
	if ctx.Entry != nil && ctx.Entry.CreatedAt != "" {
		created = ctx.Entry.CreatedAt
	}
	base := PreCommitMeta{
		PreviousLocalHooksPathState: prev.localState,
		PreviousLocalHooksPathValue: prev.localValue,
		PreviousHooksPathState:      prev.effectiveState,
		PreviousHooksPathValue:      prev.effectiveValue,
		NewHooksPathValue:           hooksDir,
		ManagedHooksRoot:            managedRoot,
		WrapperPath:                 wrapperPath,
		WrapperChecksum:             wrapperSum,
	}
	buildEntry := func(state string) MetadataEntry {
		m := base
		m.InstallState = state
		return MetadataEntry{
			Component:         PreCommitComponentID,
			ReleaseVersion:    ctx.ReleaseVersion,
			InstalledVersion:  ctx.ReleaseVersion,
			DestinationPath:   hooksDir,
			Checksum:          wrapperSum,
			ManagedMode:       ManagedModeConfig,
			ComponentMetadata: m.ToMap(),
			CreatedAt:         created,
			UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
		}
	}

	pendingEntry := buildEntry(PreCommitStatePending)
	if err := persistPreCommitEntry(repoRoot, pendingEntry); err != nil {
		return fail(fmt.Sprintf("write pending metadata: %v", err))
	}

	if err := writeManagedWrapper(wrapperPath); err != nil {
		_ = os.RemoveAll(managedRoot)
		_ = deletePreCommitEntry(repoRoot)
		return fail(fmt.Sprintf("write managed wrapper: %v", err))
	}

	if err := gitutil.SetConfigLocal(repoRoot, "core.hooksPath", hooksDir); err != nil {
		rb := rollbackPreCommit(repoRoot, managedRoot, prev)
		if rb == nil {
			_ = deletePreCommitEntry(repoRoot)
		}
		return fail(rollbackReason(fmt.Sprintf("set core.hooksPath: %v", err), rb))
	}

	if reason, ok := preCommitConsistent(repoRoot, commonDir, wrapperSum); !ok {
		rb := rollbackPreCommit(repoRoot, managedRoot, prev)
		if rb == nil {
			_ = deletePreCommitEntry(repoRoot)
			return fail(fmt.Sprintf("effective core.hooksPath verification failed (%s); rolled back", reason))
		}
		return fail(rollbackReason(fmt.Sprintf("effective core.hooksPath verification failed (%s)", reason), rb))
	}

	installedEntry := buildEntry(PreCommitStateInstalled)
	if err := persistPreCommitEntry(repoRoot, installedEntry); err != nil {
		return fail(fmt.Sprintf("write installed metadata: %v", err))
	}

	action := ActionCreated
	if hasExisting {
		action = ActionUpdated
	}
	return Outcome{
		Result: Result{
			Component: PreCommitComponentID, ReleaseVersion: ctx.ReleaseVersion,
			Destination: hooksDir, Action: action, Managed: true,
			Reason: "core.hooksPath now points at the git-kura managed wrapper",
		},
		SetEntry: &installedEntry,
	}
}

func resolvePreviousHookLookup(repoRoot, commonDir string) (preCommitPrevHook, error) {
	hooksDir := PreCommitHooksDir(commonDir)

	var prev preCommitPrevHook

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
		if !SamePathSafe(localAbs, hooksDir) {
			prev.localState = "set"
			prev.localValue = localValue
		}
	}

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

// ResolvePreviousPreCommitPath resolves the previous pre-commit hook to chain
// for a commit happening in worktreeRoot.
func ResolvePreviousPreCommitPath(meta PreCommitMeta, worktreeRoot, commonDir string) string {
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
	if SamePathSafe(dir, PreCommitHooksDir(commonDir)) || SamePathSafe(preCommitPath, PreCommitWrapperPath(commonDir)) {
		return ""
	}
	return preCommitPath
}

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

func preCommitConsistent(repoRoot, commonDir, wrapperSum string) (string, bool) {
	hooksDir := PreCommitHooksDir(commonDir)
	wrapperPath := PreCommitWrapperPath(commonDir)

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
	if !SamePathSafe(effective, hooksDir) {
		return fmt.Sprintf("effective core.hooksPath %q is not the managed dir %q", effective, hooksDir), false
	}
	data, err := os.ReadFile(wrapperPath)
	if err != nil {
		return fmt.Sprintf("managed wrapper missing: %v", err), false
	}
	if SHA256Hex(data) != wrapperSum {
		return "managed wrapper was modified outside git-kura", false
	}
	return "", true
}

func writeManagedWrapper(wrapperPath string) error {
	if err := os.MkdirAll(filepath.Dir(wrapperPath), 0o755); err != nil {
		return fmt.Errorf("create managed hooks dir: %w", err)
	}
	tmp := wrapperPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(PreCommitWrapperScript), 0o755); err != nil {
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

func rollbackPreCommit(repoRoot, managedRoot string, prev preCommitPrevHook) error {
	restoreErr := restoreLocalHooksPath(repoRoot, prev.localState, prev.localValue)
	rmErr := os.RemoveAll(managedRoot)
	return errors.Join(restoreErr, rmErr)
}

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

// --- uninstall ---------------------------------------------------------------

func (c PreCommitComponent) Uninstall(ctx Context) Outcome {
	commonDir := ctx.CommonDir
	repoRoot := ctx.RepoRoot
	hooksDir := PreCommitHooksDir(commonDir)
	managedRoot := PreCommitManagedRoot(commonDir)

	meta, ok := PreCommitMetaFromEntry(ctx.Entry)
	if !ok {
		return Outcome{Result: Result{
			Component: PreCommitComponentID, Action: ActionNotInstalled,
			Reason: "no install metadata; nothing to remove",
		}}
	}

	localRaw, localSet, err := gitutil.ConfigGetLocal(repoRoot, "core.hooksPath")
	if err != nil {
		return Outcome{Result: Result{
			Component: PreCommitComponentID, Action: ActionFailed,
			Reason: fmt.Sprintf("read local core.hooksPath: %v", err),
		}}
	}
	localValue := strings.TrimRight(localRaw, "\n")
	localAbs := localValue
	if localSet && !filepath.IsAbs(localAbs) {
		localAbs = filepath.Join(repoRoot, localAbs)
	}
	localPointsAtManaged := localSet && SamePathSafe(localAbs, hooksDir)

	if localPointsAtManaged {
		if restoreErr := restoreLocalHooksPath(repoRoot, meta.PreviousLocalHooksPathState, meta.PreviousLocalHooksPathValue); restoreErr != nil {
			return Outcome{Result: Result{
				Component: PreCommitComponentID, Action: ActionFailed,
				Reason: fmt.Sprintf("restore previous local core.hooksPath: %v", restoreErr),
			}}
		}
	}

	if err := os.RemoveAll(managedRoot); err != nil {
		return Outcome{Result: Result{
			Component: PreCommitComponentID, Action: ActionFailed,
			Reason: fmt.Sprintf("remove managed hook files: %v", err),
		}}
	}

	reason := "removed managed wrapper and restored repository-local core.hooksPath"
	if !localPointsAtManaged {
		reason = "removed managed wrapper; repository-local core.hooksPath was not the managed dir and was left untouched"
	}
	return Outcome{
		Result: Result{
			Component: PreCommitComponentID, Destination: hooksDir,
			Action: ActionRemoved, Managed: true, Reason: reason,
		},
		DeleteEntry: true,
	}
}

// --- status ------------------------------------------------------------------

func (c PreCommitComponent) Status(ctx Context) Outcome {
	commonDir := ctx.CommonDir
	repoRoot := ctx.RepoRoot
	hooksDir := PreCommitHooksDir(commonDir)
	wrapperSum := SHA256Hex([]byte(PreCommitWrapperScript))

	meta, hasMeta := PreCommitMetaFromEntry(ctx.Entry)
	if !hasMeta {
		return Outcome{Result: Result{
			Component: PreCommitComponentID, Destination: hooksDir,
			Action: ActionNotInstalled,
			Reason: "not installed; install with \"git kura tools install pre-commit\"",
		}}
	}

	diag := collectPreCommitDiagnostics(repoRoot, commonDir, meta)
	reason, consistent := preCommitConsistent(repoRoot, commonDir, wrapperSum)

	action := ActionInstalled
	switch {
	case meta.InstallState == PreCommitStatePending:
		action = ActionSkipped
		diag.installState = "pending"
	case !consistent:
		action = ActionSkipped
		diag.installState = "inconsistent"
	default:
		diag.installState = "installed"
	}

	return Outcome{Result: Result{
		Component: PreCommitComponentID, ReleaseVersion: ctx.Entry.ReleaseVersion,
		Destination: hooksDir, Action: action, Managed: true,
		Reason: diag.format(reason),
	}}
}

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

func collectPreCommitDiagnostics(repoRoot, commonDir string, meta PreCommitMeta) preCommitDiagnostics {
	d := preCommitDiagnostics{
		managedHooksPath:  PreCommitHooksDir(commonDir),
		previousHooksPath: meta.PreviousLocalHooksPathValue,
		hookWorktreeRoot:  repoRoot,
		hookGitCommonDir:  commonDir,
	}
	if previousPreCommit := ResolvePreviousPreCommitPath(meta, repoRoot, commonDir); previousPreCommit != "" {
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
	fmt.Fprintf(&b, "; currentHooksPath=%s", diagDash(d.currentHooksPath))
	fmt.Fprintf(&b, "; effectiveHooksPath=%s", diagDash(d.effectiveHooksPath))
	fmt.Fprintf(&b, "; managedHooksPath=%s", d.managedHooksPath)
	if d.mismatch {
		b.WriteString("; hooksPathMismatch=true")
	}
	fmt.Fprintf(&b, "; previousHooksPath=%s", diagDash(d.previousHooksPath))
	fmt.Fprintf(&b, "; previousHookDirectory=%s", diagDash(d.previousHookDirectory))
	fmt.Fprintf(&b, "; previousPreCommitPath=%s", diagDash(d.previousPreCommitPath))
	fmt.Fprintf(&b, "; previousPreCommitExists=%t", d.previousPreCommitExists)
	fmt.Fprintf(&b, "; previousPreCommitExecutable=%t", d.previousPreCommitExec)
	fmt.Fprintf(&b, "; hookWorktreeRoot=%s", d.hookWorktreeRoot)
	fmt.Fprintf(&b, "; hookGitCommonDir=%s", d.hookGitCommonDir)
	fmt.Fprintf(&b, "; currentKey=%s", d.currentKey)
	fmt.Fprintf(&b, "; currentKeySource=%s", d.currentKeySource)
	b.WriteString("; bypass: git commit --no-verify")
	return b.String()
}

func diagDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// --- metadata persistence helpers --------------------------------------------

func persistPreCommitEntry(repoRoot string, entry MetadataEntry) error {
	storeFile, _, err := MetadataPaths(repoRoot)
	if err != nil {
		return err
	}
	store, err := ReadMetadata(storeFile)
	if err != nil {
		return err
	}
	store.Components[PreCommitComponentID] = entry
	return WriteMetadata(storeFile, store)
}

func deletePreCommitEntry(repoRoot string) error {
	storeFile, _, err := MetadataPaths(repoRoot)
	if err != nil {
		return err
	}
	store, err := ReadMetadata(storeFile)
	if err != nil {
		return err
	}
	if _, ok := store.Components[PreCommitComponentID]; !ok {
		return nil
	}
	delete(store.Components, PreCommitComponentID)
	return WriteMetadata(storeFile, store)
}
