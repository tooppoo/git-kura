package tools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitConfigLocalStr reads a git local config value; returns ("", false) when unset.
func gitConfigLocalStr(t *testing.T, repo, key string) (string, bool) {
	t.Helper()
	cmd := exec.Command("git", "config", "--local", "--get", key)
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(out), "\n"), true
}

func TestRollbackReason(t *testing.T) {
	if got := rollbackReason("primary", nil); got != "primary" {
		t.Fatalf("nil rollback err: got %q", got)
	}
	got := rollbackReason("primary", os.ErrPermission)
	if !strings.Contains(got, "rollback also failed") {
		t.Fatalf("rollback failure should be surfaced: %q", got)
	}
}

func TestRollbackPreCommitRestoresConfig(t *testing.T) {
	repo := toolsTestRepo(t)
	managedRoot := PreCommitManagedRoot(filepath.Join(repo, ".git"))
	if err := os.MkdirAll(managedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "config", "--local", "core.hooksPath", filepath.Join(repo, "managed"))

	prev := preCommitPrevHook{localState: "set", localValue: filepath.Join(repo, "old")}
	if err := rollbackPreCommit(repo, managedRoot, prev); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	got, ok := gitConfigLocalStr(t, repo, "core.hooksPath")
	if !ok || got != prev.localValue {
		t.Fatalf("core.hooksPath = %q (ok=%v), want %q", got, ok, prev.localValue)
	}
	if _, err := os.Stat(managedRoot); !os.IsNotExist(err) {
		t.Fatalf("managed root should be removed: %v", err)
	}

	if err := rollbackPreCommit(repo, managedRoot, preCommitPrevHook{localState: "unset"}); err != nil {
		t.Fatalf("rollback unset: %v", err)
	}
	if _, ok := gitConfigLocalStr(t, repo, "core.hooksPath"); ok {
		t.Fatal("core.hooksPath should be unset")
	}
}

func TestResolvePreviousHookLookupRecursionGuard(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	testGit(t, repo, "config", "--local", "core.hooksPath", PreCommitHooksDir(commonDir))

	prev, err := resolvePreviousHookLookup(repo, commonDir)
	if err != nil {
		t.Fatalf("resolvePreviousHookLookup: %v", err)
	}
	if prev.localState != "unset" {
		t.Fatalf("orphaned local value must be treated as unset, got %q", prev.localState)
	}
	meta := PreCommitMeta{PreviousHooksPathState: prev.effectiveState, PreviousHooksPathValue: prev.effectiveValue}
	if got := ResolvePreviousPreCommitPath(meta, repo, commonDir); got != "" {
		t.Fatalf("managed dir must not chain as previous hook, got %q", got)
	}
}

func TestResolvePreviousHookLookupErrorsOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	if _, err := resolvePreviousHookLookup(dir, filepath.Join(dir, ".git")); err == nil {
		t.Fatal("resolvePreviousHookLookup should error when git config cannot run")
	}
}

func TestResolvePreviousHookLookupOrphanLocalValue(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	testGit(t, repo, "config", "--local", "core.hooksPath", PreCommitHooksDir(commonDir))

	prev, err := resolvePreviousHookLookup(repo, commonDir)
	if err != nil {
		t.Fatalf("resolvePreviousHookLookup: %v", err)
	}
	if prev.localState != "unset" || prev.localValue != "" {
		t.Fatalf("orphaned local value must be treated as unset, got %q/%q", prev.localState, prev.localValue)
	}
}

func TestPersistAndDeleteEntryErrorOnBadStore(t *testing.T) {
	repo := toolsTestRepo(t)
	toolsDir := filepath.Join(repo, ".git", "kura", "tools")
	if err := os.MkdirAll(filepath.Dir(toolsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolsDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := persistPreCommitEntry(repo, MetadataEntry{Component: PreCommitComponentID, ManagedMode: ManagedModeConfig}); err == nil {
		t.Fatal("persist should fail on an unreadable store")
	}
	if err := deletePreCommitEntry(repo); err == nil {
		t.Fatal("delete should fail on an unreadable store")
	}
}

func TestPersistDeleteEntryNonRepoError(t *testing.T) {
	dir := t.TempDir()
	if err := persistPreCommitEntry(dir, MetadataEntry{}); err == nil {
		t.Fatal("persistPreCommitEntry should fail outside a git repo")
	}
	if err := deletePreCommitEntry(dir); err == nil {
		t.Fatal("deletePreCommitEntry should fail outside a git repo")
	}
}

func TestDeletePreCommitEntry(t *testing.T) {
	repo := toolsTestRepo(t)
	entry := MetadataEntry{
		Component:         PreCommitComponentID,
		ManagedMode:       ManagedModeConfig,
		ComponentMetadata: PreCommitMeta{InstallState: PreCommitStatePending}.ToMap(),
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if err := persistPreCommitEntry(repo, entry); err != nil {
		t.Fatalf("persist: %v", err)
	}

	storeFile, _, err := MetadataPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ReadMetadata(storeFile)
	if err != nil || len(store.Components) == 0 {
		t.Fatal("entry should be present after persist")
	}

	if err := deletePreCommitEntry(repo); err != nil {
		t.Fatalf("delete: %v", err)
	}
	store2, err := ReadMetadata(storeFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store2.Components[PreCommitComponentID]; ok {
		t.Fatal("entry should be gone after delete")
	}
	// Deleting an absent entry is a no-op.
	if err := deletePreCommitEntry(repo); err != nil {
		t.Fatalf("delete absent: %v", err)
	}
}

func TestPreCommitConsistentVariants(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	sum := SHA256Hex([]byte(PreCommitWrapperScript))

	if reason, ok := preCommitConsistent(repo, commonDir, sum); ok || !strings.Contains(reason, "unset") {
		t.Fatalf("unset core.hooksPath: reason=%q ok=%v", reason, ok)
	}

	testGit(t, repo, "config", "--local", "core.hooksPath", filepath.Join(repo, "elsewhere"))
	if reason, ok := preCommitConsistent(repo, commonDir, sum); ok || !strings.Contains(reason, "not the managed dir") {
		t.Fatalf("non-managed: reason=%q ok=%v", reason, ok)
	}

	testGit(t, repo, "config", "--local", "core.hooksPath", "relhooks")
	if reason, ok := preCommitConsistent(repo, commonDir, sum); ok || !strings.Contains(reason, "not the managed dir") {
		t.Fatalf("relative non-managed: reason=%q ok=%v", reason, ok)
	}

	testGit(t, repo, "config", "--local", "core.hooksPath", PreCommitHooksDir(commonDir))
	if reason, ok := preCommitConsistent(repo, commonDir, sum); ok || !strings.Contains(reason, "missing") {
		t.Fatalf("missing wrapper: reason=%q ok=%v", reason, ok)
	}
}

func TestCollectDiagnosticsKeySource(t *testing.T) {
	repo := toolsTestRepo(t)
	commitFile(t, repo, "seed.txt", "seed\n")
	commonDir := filepath.Join(repo, ".git")

	// Unmanaged worktree root.
	d := collectPreCommitDiagnostics(repo, commonDir, PreCommitMeta{})
	if d.currentKeySource != "unmanaged-worktree" {
		t.Fatalf("currentKeySource = %q, want unmanaged-worktree", d.currentKeySource)
	}

	wt := openManagedWorktreeForTest(t, repo, "alpha")

	// Inject a duplicate worktree metadata entry → ambiguous.
	dupe := filepath.Join(commonDir, "kura", "meta", "worktrees", "beta.json")
	dupeData, _ := json.Marshal(map[string]any{
		"repositoryRoot": repo,
		"baseBranch":     "main",
		"worktreePath":   wt,
	})
	if err := os.WriteFile(dupe, dupeData, 0o644); err != nil {
		t.Fatal(err)
	}
	d = collectPreCommitDiagnostics(wt, commonDir, PreCommitMeta{})
	if d.currentKeySource != "ambiguous" {
		t.Fatalf("currentKeySource = %q, want ambiguous", d.currentKeySource)
	}

	// Replace the worktrees dir with a plain file so os.ReadDir fails.
	worktreesDir := filepath.Join(commonDir, "kura", "meta", "worktrees")
	if err := os.RemoveAll(worktreesDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(worktreesDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d = collectPreCommitDiagnostics(repo, commonDir, PreCommitMeta{})
	if d.currentKeySource != "error" {
		t.Fatalf("currentKeySource = %q, want error", d.currentKeySource)
	}
}

func preCommitInstallCtx(repo string) InstallContext {
	return InstallContext{
		Context:        Context{RepoRoot: repo, CommonDir: filepath.Join(repo, ".git")},
		ReleaseVersion: "1.2.3",
	}
}

func readPreCommitEntryForTest(t *testing.T, repo string) *MetadataEntry {
	t.Helper()
	storeFile, _, err := MetadataPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ReadMetadata(storeFile)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := store.Components[PreCommitComponentID]
	if !ok {
		return nil
	}
	return &entry
}

func TestPreCommitComponentLifecycle(t *testing.T) {
	repo := toolsTestRepo(t)
	comp := PreCommitComponent{}

	install := comp.Install(preCommitInstallCtx(repo))
	if install.Result.Action != ActionCreated || install.SetEntry == nil {
		t.Fatalf("install = %#v, entry=%#v", install.Result, install.SetEntry)
	}
	if got, ok := gitConfigLocalStr(t, repo, "core.hooksPath"); !ok || !SamePathSafe(got, PreCommitHooksDir(filepath.Join(repo, ".git"))) {
		t.Fatalf("core.hooksPath = %q ok=%v", got, ok)
	}
	if _, err := os.Stat(PreCommitWrapperPath(filepath.Join(repo, ".git"))); err != nil {
		t.Fatalf("managed wrapper missing: %v", err)
	}

	entry := readPreCommitEntryForTest(t, repo)
	status := comp.Status(Context{RepoRoot: repo, CommonDir: filepath.Join(repo, ".git"), Entry: entry})
	if status.Result.Action != ActionInstalled || !strings.Contains(status.Result.Reason, "installState=installed") {
		t.Fatalf("status = %#v", status.Result)
	}

	reinstallCtx := preCommitInstallCtx(repo)
	reinstallCtx.Entry = entry
	reinstall := comp.Install(reinstallCtx)
	if reinstall.Result.Action != ActionSkipped {
		t.Fatalf("reinstall = %#v, want skipped", reinstall.Result)
	}

	uninstall := comp.Uninstall(Context{RepoRoot: repo, CommonDir: filepath.Join(repo, ".git"), Entry: entry})
	if uninstall.Result.Action != ActionRemoved || !uninstall.DeleteEntry {
		t.Fatalf("uninstall = %#v delete=%v", uninstall.Result, uninstall.DeleteEntry)
	}
	if _, ok := gitConfigLocalStr(t, repo, "core.hooksPath"); ok {
		t.Fatal("core.hooksPath should be unset after uninstall")
	}
	if _, err := os.Stat(PreCommitManagedRoot(filepath.Join(repo, ".git"))); !os.IsNotExist(err) {
		t.Fatalf("managed root should be removed: %v", err)
	}
}

func TestPreCommitComponentCapturesAndRestoresPreviousHook(t *testing.T) {
	repo := toolsTestRepo(t)
	prevDir := filepath.Join(repo, "hooks")
	if err := os.MkdirAll(prevDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prevHook := filepath.Join(prevDir, "pre-commit")
	if err := os.WriteFile(prevHook, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "config", "--local", "core.hooksPath", prevDir)

	comp := PreCommitComponent{}
	install := comp.Install(preCommitInstallCtx(repo))
	if install.Result.Action != ActionCreated {
		t.Fatalf("install = %#v", install.Result)
	}
	entry := readPreCommitEntryForTest(t, repo)
	meta, ok := PreCommitMetaFromEntry(entry)
	if !ok {
		t.Fatal("expected pre-commit metadata")
	}
	if meta.PreviousLocalHooksPathState != "set" || meta.PreviousLocalHooksPathValue != prevDir {
		t.Fatalf("previous local = %q/%q, want set/%q", meta.PreviousLocalHooksPathState, meta.PreviousLocalHooksPathValue, prevDir)
	}
	if got := ResolvePreviousPreCommitPath(meta, repo, filepath.Join(repo, ".git")); !SamePathSafe(got, prevHook) {
		t.Fatalf("previous pre-commit = %q, want %q", got, prevHook)
	}

	uninstall := comp.Uninstall(Context{RepoRoot: repo, CommonDir: filepath.Join(repo, ".git"), Entry: entry})
	if uninstall.Result.Action != ActionRemoved {
		t.Fatalf("uninstall = %#v", uninstall.Result)
	}
	if got, ok := gitConfigLocalStr(t, repo, "core.hooksPath"); !ok || got != prevDir {
		t.Fatalf("core.hooksPath = %q ok=%v, want %q", got, ok, prevDir)
	}
}

func TestPreCommitComponentReinstallRepairsInconsistentInstall(t *testing.T) {
	repo := toolsTestRepo(t)
	comp := PreCommitComponent{}

	install := comp.Install(preCommitInstallCtx(repo))
	if install.Result.Action != ActionCreated {
		t.Fatalf("install = %#v", install.Result)
	}
	entry := readPreCommitEntryForTest(t, repo)
	meta, ok := PreCommitMetaFromEntry(entry)
	if !ok {
		t.Fatal("expected metadata after install")
	}
	if meta.PreviousLocalHooksPathState != "unset" {
		t.Fatalf("initial previous local state = %q", meta.PreviousLocalHooksPathState)
	}

	testGit(t, repo, "config", "--local", "--unset", "core.hooksPath")
	repairCtx := preCommitInstallCtx(repo)
	repairCtx.Entry = entry
	repair := comp.Install(repairCtx)
	if repair.Result.Action != ActionUpdated || repair.SetEntry == nil {
		t.Fatalf("repair = %#v entry=%#v, want updated with entry", repair.Result, repair.SetEntry)
	}
	got, ok := gitConfigLocalStr(t, repo, "core.hooksPath")
	if !ok || !SamePathSafe(got, PreCommitHooksDir(filepath.Join(repo, ".git"))) {
		t.Fatalf("core.hooksPath after repair = %q ok=%v", got, ok)
	}
	repairedMeta, ok := PreCommitMetaFromEntry(repair.SetEntry)
	if !ok {
		t.Fatal("expected repaired metadata")
	}
	if repairedMeta.PreviousLocalHooksPathState != "unset" {
		t.Fatalf("repair should preserve previous local state, got %q", repairedMeta.PreviousLocalHooksPathState)
	}
}

func TestPreCommitComponentInstallCapturesGlobalPreviousHookForChaining(t *testing.T) {
	repo := toolsTestRepo(t)
	globalCfg := filepath.Join(t.TempDir(), "gitconfig")
	globalHooks := filepath.Join(repo, "global-hooks")
	if err := os.WriteFile(globalCfg, []byte("[core]\n\thooksPath = "+globalHooks+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalCfg)

	install := PreCommitComponent{}.Install(preCommitInstallCtx(repo))
	if install.Result.Action != ActionCreated {
		t.Fatalf("install = %#v", install.Result)
	}
	meta, ok := PreCommitMetaFromEntry(readPreCommitEntryForTest(t, repo))
	if !ok {
		t.Fatal("expected metadata")
	}
	if meta.PreviousLocalHooksPathState != "unset" {
		t.Fatalf("local state = %q, want unset", meta.PreviousLocalHooksPathState)
	}
	if meta.PreviousHooksPathState != "set" || !SamePathSafe(meta.PreviousHooksPathValue, globalHooks) {
		t.Fatalf("effective previous hooks = %q/%q, want set/%q", meta.PreviousHooksPathState, meta.PreviousHooksPathValue, globalHooks)
	}
}

func TestPreCommitComponentStatusVariants(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	comp := PreCommitComponent{}

	notInstalled := comp.Status(Context{RepoRoot: repo, CommonDir: commonDir})
	if notInstalled.Result.Action != ActionNotInstalled {
		t.Fatalf("not installed status = %#v", notInstalled.Result)
	}

	pending := MetadataEntry{
		Component:         PreCommitComponentID,
		ReleaseVersion:    "1.2.3",
		InstalledVersion:  "1.2.3",
		ManagedMode:       ManagedModeConfig,
		ComponentMetadata: PreCommitMeta{InstallState: PreCommitStatePending}.ToMap(),
	}
	pendingStatus := comp.Status(Context{RepoRoot: repo, CommonDir: commonDir, Entry: &pending})
	if pendingStatus.Result.Action != ActionSkipped || !strings.Contains(pendingStatus.Result.Reason, "installState=pending") {
		t.Fatalf("pending status = %#v", pendingStatus.Result)
	}

	install := comp.Install(preCommitInstallCtx(repo))
	if install.Result.Action != ActionCreated {
		t.Fatalf("install = %#v", install.Result)
	}
	entry := readPreCommitEntryForTest(t, repo)
	if err := os.WriteFile(PreCommitWrapperPath(commonDir), []byte("#!/bin/sh\necho changed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	modified := comp.Status(Context{RepoRoot: repo, CommonDir: commonDir, Entry: entry})
	if modified.Result.Action != ActionSkipped || !strings.Contains(modified.Result.Reason, "modified outside git-kura") {
		t.Fatalf("modified status = %#v", modified.Result)
	}
}

func TestPreCommitComponentInstallFailurePaths(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	comp := PreCommitComponent{}

	testGit(t, repo, "config", "--local", "extensions.worktreeConfig", "true")
	testGit(t, repo, "config", "--worktree", "core.hooksPath", filepath.Join(repo, "elsewhere"))
	blocked := comp.Install(preCommitInstallCtx(repo))
	if blocked.Result.Action != ActionFailed || !strings.Contains(blocked.Result.Reason, "higher-precedence") {
		t.Fatalf("higher precedence install = %#v", blocked.Result)
	}

	testGit(t, repo, "config", "--worktree", "--unset", "core.hooksPath")
	toolsDir := filepath.Join(commonDir, "kura", "tools")
	if err := os.MkdirAll(filepath.Dir(toolsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolsDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	pendingFailure := comp.Install(preCommitInstallCtx(repo))
	if pendingFailure.Result.Action != ActionFailed || !strings.Contains(pendingFailure.Result.Reason, "pending metadata") {
		t.Fatalf("pending metadata failure = %#v", pendingFailure.Result)
	}
}

func TestPreCommitComponentInstallFailsWritingWrapper(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	managedRoot := PreCommitManagedRoot(commonDir)
	if err := os.MkdirAll(filepath.Dir(managedRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedRoot, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome := PreCommitComponent{}.Install(preCommitInstallCtx(repo))
	if outcome.Result.Action != ActionFailed || !strings.Contains(outcome.Result.Reason, "write managed wrapper") {
		t.Fatalf("install = %#v, want wrapper write failure", outcome.Result)
	}
	if entry := readPreCommitEntryForTest(t, repo); entry != nil {
		t.Fatalf("pending metadata should be cleaned up, got %#v", entry)
	}
	if _, ok := gitConfigLocalStr(t, repo, "core.hooksPath"); ok {
		t.Fatal("core.hooksPath should not be set when wrapper write fails")
	}
}

func TestPreCommitComponentUninstallVariants(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	comp := PreCommitComponent{}

	missing := comp.Uninstall(Context{RepoRoot: repo, CommonDir: commonDir})
	if missing.Result.Action != ActionNotInstalled {
		t.Fatalf("missing uninstall = %#v", missing.Result)
	}

	install := comp.Install(preCommitInstallCtx(repo))
	if install.Result.Action != ActionCreated {
		t.Fatalf("install = %#v", install.Result)
	}
	entry := readPreCommitEntryForTest(t, repo)
	other := filepath.Join(repo, "other-hooks")
	testGit(t, repo, "config", "--local", "core.hooksPath", other)
	uninstall := comp.Uninstall(Context{RepoRoot: repo, CommonDir: commonDir, Entry: entry})
	if uninstall.Result.Action != ActionRemoved || !strings.Contains(uninstall.Result.Reason, "left untouched") {
		t.Fatalf("uninstall with user-changed hooksPath = %#v", uninstall.Result)
	}
	if got, ok := gitConfigLocalStr(t, repo, "core.hooksPath"); !ok || got != other {
		t.Fatalf("core.hooksPath = %q ok=%v, want %q", got, ok, other)
	}
}

func TestPreCommitComponentErrorsOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()
	commonDir := filepath.Join(dir, ".git")
	comp := PreCommitComponent{}

	install := comp.Install(InstallContext{
		Context:        Context{RepoRoot: dir, CommonDir: commonDir},
		ReleaseVersion: "1.2.3",
	})
	if install.Result.Action != ActionFailed || !strings.Contains(install.Result.Reason, "resolve previous hook lookup") {
		t.Fatalf("install outside git repo = %#v", install.Result)
	}

	entry := &MetadataEntry{
		Component:       PreCommitComponentID,
		ManagedMode:     ManagedModeConfig,
		CreatedAt:       "2026-06-23T00:00:00Z",
		UpdatedAt:       "2026-06-23T00:00:00Z",
		DestinationPath: PreCommitHooksDir(commonDir),
		ComponentMetadata: PreCommitMeta{
			InstallState:                PreCommitStateInstalled,
			PreviousLocalHooksPathState: "unset",
			PreviousHooksPathState:      "unset",
			NewHooksPathValue:           PreCommitHooksDir(commonDir),
			ManagedHooksRoot:            PreCommitManagedRoot(commonDir),
			WrapperPath:                 PreCommitWrapperPath(commonDir),
			WrapperChecksum:             SHA256Hex([]byte(PreCommitWrapperScript)),
		}.ToMap(),
	}
	uninstall := comp.Uninstall(Context{RepoRoot: dir, CommonDir: commonDir, Entry: entry})
	if uninstall.Result.Action != ActionFailed || !strings.Contains(uninstall.Result.Reason, "read local core.hooksPath") {
		t.Fatalf("uninstall outside git repo = %#v", uninstall.Result)
	}
}

func TestPreCommitDiagnosticsFormatAndMetaDecode(t *testing.T) {
	if got := diagDash(""); got != "-" {
		t.Fatalf("diagDash empty = %q", got)
	}
	if got := diagDash("x"); got != "x" {
		t.Fatalf("diagDash x = %q", got)
	}

	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	reason := preCommitDiagnostics{
		currentHooksPath:        "local",
		effectiveHooksPath:      "effective",
		managedHooksPath:        PreCommitHooksDir(commonDir),
		previousHooksPath:       "prev",
		previousHookDirectory:   filepath.Join(repo, "prev"),
		previousPreCommitPath:   filepath.Join(repo, "prev", "pre-commit"),
		previousPreCommitExists: true,
		previousPreCommitExec:   true,
		hookWorktreeRoot:        repo,
		hookGitCommonDir:        commonDir,
		currentKey:              "alpha",
		currentKeySource:        "managed-worktree",
		installState:            "installed",
		mismatch:                true,
	}.format("consistent enough")
	for _, want := range []string{"installState=installed", "hooksPathMismatch=true", "previousPreCommitExecutable=true", "bypass: git commit --no-verify"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("formatted diagnostics missing %q: %s", want, reason)
		}
	}

	if _, ok := PreCommitMetaFromEntry(nil); ok {
		t.Fatal("nil entry should not decode")
	}
	if _, ok := PreCommitMetaFromEntry(&MetadataEntry{}); ok {
		t.Fatal("entry without component metadata should not decode")
	}
	meta := PreCommitMeta{InstallState: PreCommitStateInstalled, PreviousLocalHooksPathState: "set"}
	got, ok := PreCommitMetaFromEntry(&MetadataEntry{ComponentMetadata: meta.ToMap()})
	if !ok || got.InstallState != PreCommitStateInstalled || got.PreviousLocalHooksPathState != "set" {
		t.Fatalf("decoded meta = %+v ok=%v", got, ok)
	}
	if _, ok := PreCommitMetaFromEntry(&MetadataEntry{ComponentMetadata: map[string]any{"installState": 123}}); ok {
		t.Fatal("bad metadata shape should not decode")
	}
}
