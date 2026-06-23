package tools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type serviceTestComponent struct {
	id          string
	statusFn    func(Context) Outcome
	installFn   func(InstallContext) Outcome
	uninstallFn func(Context) Outcome
}

func (c serviceTestComponent) ID() string { return c.id }

func (c serviceTestComponent) Status(ctx Context) Outcome {
	if c.statusFn != nil {
		return c.statusFn(ctx)
	}
	return Outcome{Result: Result{Component: c.id, Action: ActionNotInstalled}}
}

func (c serviceTestComponent) Install(ctx InstallContext) Outcome {
	if c.installFn != nil {
		return c.installFn(ctx)
	}
	return Outcome{Result: Result{Component: c.id, Action: ActionSkipped}}
}

func (c serviceTestComponent) Uninstall(ctx Context) Outcome {
	if c.uninstallFn != nil {
		return c.uninstallFn(ctx)
	}
	return Outcome{Result: Result{Component: c.id, Action: ActionNotInstalled}}
}

type hookFetcher struct {
	base *fakeFetcher
	hook func()
}

func (f hookFetcher) FetchSidecar(tag, version string) ([]byte, error) {
	if f.hook != nil {
		f.hook()
	}
	return f.base.FetchSidecar(tag, version)
}

func (f hookFetcher) FetchArchive(tag, name string) ([]byte, error) {
	return f.base.FetchArchive(tag, name)
}

func serviceTestFetcher(t *testing.T, version string, files map[string][]byte) *fakeFetcher {
	t.Helper()
	components := map[string]ArchiveManifestComponent{}
	for path, data := range files {
		componentID := strings.Split(path, "/")[0]
		comp := components[componentID]
		if comp.Files == nil {
			comp.Files = map[string]string{}
		}
		comp.Files[path] = SHA256Hex(data)
		components[componentID] = comp
	}
	manifest := ArchiveManifest{SchemaVersion: 1, Components: components}
	archiveName := "git-kura-tools_" + version + ".tar.gz"
	archive := makeTestArchive(t, manifest, files)
	sidecar := makeTestSidecar(t, version, archiveName, ChecksumAlgorithmSHA256, archive)
	return &fakeFetcher{sidecar: sidecar, archive: archive}
}

func TestEntryForReturnsCopy(t *testing.T) {
	store := MetadataStore{Components: map[string]MetadataEntry{
		"alpha": {Component: "alpha", InstalledVersion: "1.2.3"},
	}}
	entry := entryFor(store, "alpha")
	if entry == nil || entry.InstalledVersion != "1.2.3" {
		t.Fatalf("entryFor = %#v, want alpha entry", entry)
	}
	entry.InstalledVersion = "changed"
	if got := store.Components["alpha"].InstalledVersion; got != "1.2.3" {
		t.Fatalf("entryFor returned store alias; version = %q", got)
	}
	if got := entryFor(store, "missing"); got != nil {
		t.Fatalf("missing entry = %#v, want nil", got)
	}
}

func TestVerifyActionRejectsInvalidCommandAction(t *testing.T) {
	comp := serviceTestComponent{id: "alpha"}
	valid := verifyAction(CmdInstall, comp, Outcome{Result: Result{Component: "alpha", Action: ActionCreated}})
	if valid.Result.Action != ActionCreated {
		t.Fatalf("valid install action was rejected: %#v", valid.Result)
	}

	invalid := verifyAction(CmdInstall, comp, Outcome{Result: Result{Component: "alpha", Action: ActionRemoved}})
	if invalid.Result.Action != ActionFailed {
		t.Fatalf("invalid action = %q, want failed", invalid.Result.Action)
	}
	if !strings.Contains(invalid.Result.Reason, "not valid for install") {
		t.Fatalf("reason = %q, want command validation detail", invalid.Result.Reason)
	}
}

func TestApplyOutcomeMutations(t *testing.T) {
	store := MetadataStore{Components: map[string]MetadataEntry{
		"alpha": {Component: "alpha", InstalledVersion: "old"},
	}}
	if changed := applyOutcome(&store, Outcome{Result: Result{Component: "beta"}}); changed {
		t.Fatal("empty outcome should not change the store")
	}
	entry := &MetadataEntry{Component: "beta", InstalledVersion: "new"}
	if changed := applyOutcome(&store, Outcome{Result: Result{Component: "beta"}, SetEntry: entry}); !changed {
		t.Fatal("SetEntry should report a change")
	}
	if got := store.Components["beta"].InstalledVersion; got != "new" {
		t.Fatalf("stored beta version = %q", got)
	}
	if changed := applyOutcome(&store, Outcome{Result: Result{Component: "alpha"}, DeleteEntry: true}); !changed {
		t.Fatal("DeleteEntry should report a change for existing entry")
	}
	if _, ok := store.Components["alpha"]; ok {
		t.Fatal("alpha entry should have been deleted")
	}
	if changed := applyOutcome(&store, Outcome{Result: Result{Component: "missing"}, DeleteEntry: true}); changed {
		t.Fatal("DeleteEntry should not change store for absent entry")
	}
}

func TestServiceStatusReadsMetadataAndValidatesActions(t *testing.T) {
	repo := toolsTestRepo(t)
	storeFile, _, err := MetadataPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteMetadata(storeFile, MetadataStore{Components: map[string]MetadataEntry{
		"alpha": {Component: "alpha", InstalledVersion: "1.0.0", ManagedMode: ManagedModeFile},
	}}); err != nil {
		t.Fatal(err)
	}

	targets := []Component{
		serviceTestComponent{id: "alpha", statusFn: func(ctx Context) Outcome {
			if ctx.Entry == nil || ctx.Entry.InstalledVersion != "1.0.0" {
				t.Fatalf("status context entry = %#v", ctx.Entry)
			}
			if ctx.CommonDir == "" {
				t.Fatal("status context should include common dir")
			}
			return Outcome{Result: Result{Component: "alpha", Action: ActionInstalled, Managed: true}}
		}},
		serviceTestComponent{id: "beta", statusFn: func(ctx Context) Outcome {
			if ctx.Entry != nil {
				t.Fatalf("beta entry = %#v, want nil", ctx.Entry)
			}
			return Outcome{Result: Result{Component: "beta", Action: ActionCreated}}
		}},
	}

	results, err := Status(repo, targets)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results len = %d", len(results))
	}
	if results[0].Action != ActionInstalled {
		t.Fatalf("alpha action = %q", results[0].Action)
	}
	if results[1].Action != ActionFailed || !strings.Contains(results[1].Reason, "not valid for status") {
		t.Fatalf("beta result = %#v, want invalid action failure", results[1])
	}
}

func TestServiceStatusErrorsOutsideGitRepo(t *testing.T) {
	if _, err := Status(t.TempDir(), nil); err == nil {
		t.Fatal("Status outside a git repo should fail")
	}
}

func TestServiceStatusErrorsOnInvalidMetadata(t *testing.T) {
	repo := toolsTestRepo(t)
	storeFile, _, err := MetadataPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, storeFile, "{not json")
	if _, err := Status(repo, []Component{serviceTestComponent{id: "alpha"}}); err == nil || !strings.Contains(err.Error(), "read tools metadata") {
		t.Fatalf("Status invalid metadata err = %v", err)
	}
}

func TestServiceInstallFetchFailureReturnsFailedResults(t *testing.T) {
	repo := toolsTestRepo(t)
	targets := []Component{serviceTestComponent{id: "alpha"}, serviceTestComponent{id: "beta"}}

	result, err := Install(repo, "1.0.0", time.Second, &fakeFetcher{sidecarErr: errors.New("network down")}, targets)
	if err != nil {
		t.Fatalf("Install fetch failure should be reported as component results: %v", err)
	}
	if len(result.Results) != 2 {
		t.Fatalf("results len = %d", len(result.Results))
	}
	for _, r := range result.Results {
		if r.Action != ActionFailed || !strings.Contains(r.Reason, "network down") {
			t.Fatalf("result = %#v, want fetch failure", r)
		}
	}
}

func TestServiceInstallAppliesMetadataAndValidatesActions(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher := serviceTestFetcher(t, "1.0.0", map[string][]byte{"alpha/tool.txt": []byte("payload\n")})
	targets := []Component{
		serviceTestComponent{id: "alpha", installFn: func(ctx InstallContext) Outcome {
			if ctx.ReleaseVersion != "1.0.0" || ctx.Asset == nil {
				t.Fatalf("install context release=%q asset=%v", ctx.ReleaseVersion, ctx.Asset)
			}
			return Outcome{
				Result: Result{Component: "alpha", ReleaseVersion: ctx.ReleaseVersion, Action: ActionCreated},
				SetEntry: &MetadataEntry{
					Component:         "alpha",
					InstalledVersion:  ctx.ReleaseVersion,
					ReleaseVersion:    ctx.ReleaseVersion,
					DestinationPath:   filepath.Join(repo, "alpha.txt"),
					Checksum:          SHA256Hex([]byte("payload\n")),
					ManagedMode:       ManagedModeFile,
					CreatedAt:         "2026-06-23T00:00:00Z",
					UpdatedAt:         "2026-06-23T00:00:00Z",
					SourceAssetID:     "alpha/tool.txt",
					ComponentMetadata: map[string]any{"ok": true},
				},
			}
		}},
		serviceTestComponent{id: "beta", installFn: func(InstallContext) Outcome {
			return Outcome{Result: Result{Component: "beta", Action: ActionRemoved}}
		}},
	}

	result, err := Install(repo, "1.0.0", time.Second, fetcher, targets)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got := fetcher.sidecarCalls; got != 1 {
		t.Fatalf("sidecarCalls = %d, want 1", got)
	}
	if result.Results[0].Action != ActionCreated {
		t.Fatalf("alpha result = %#v", result.Results[0])
	}
	if result.Results[1].Action != ActionFailed || !strings.Contains(result.Results[1].Reason, "not valid for install") {
		t.Fatalf("beta result = %#v, want invalid action failure", result.Results[1])
	}

	storeFile, _, err := MetadataPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ReadMetadata(storeFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Components["alpha"].InstalledVersion; got != "1.0.0" {
		t.Fatalf("persisted version = %q", got)
	}
	if _, ok := store.Components["beta"]; ok {
		t.Fatal("invalid beta outcome should not write metadata")
	}
}

func TestServiceInstallReturnsLockReleaseWarning(t *testing.T) {
	repo := toolsTestRepo(t)
	_, lockFile, err := MetadataPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := serviceTestFetcher(t, "1.0.0", map[string][]byte{"alpha/tool.txt": []byte("payload\n")})
	targets := []Component{serviceTestComponent{id: "alpha", installFn: func(InstallContext) Outcome {
		if err := os.Remove(lockFile); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(lockFile, "child"), 0o755); err != nil {
			t.Fatal(err)
		}
		return Outcome{Result: Result{Component: "alpha", Action: ActionSkipped}}
	}}}

	result, err := Install(repo, "1.0.0", time.Second, fetcher, targets)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "failed to release tools metadata lock") {
		t.Fatalf("warnings = %#v, want lock release warning", result.Warnings)
	}
}

func TestServiceInstallReturnsLockReleaseWarningWithPrimaryError(t *testing.T) {
	repo := toolsTestRepo(t)
	storeFile, lockFile, err := MetadataPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, storeFile, "{not json")
	base := serviceTestFetcher(t, "1.0.0", map[string][]byte{"alpha/tool.txt": []byte("payload\n")})
	fetcher := hookFetcher{base: base, hook: func() {
		if err := os.Remove(lockFile); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(lockFile, "child"), 0o755); err != nil {
			t.Fatal(err)
		}
	}}

	result, err := Install(repo, "1.0.0", time.Second, fetcher, []Component{serviceTestComponent{id: "alpha"}})
	if err == nil || !strings.Contains(err.Error(), "read tools metadata") {
		t.Fatalf("Install err = %v, want metadata read error", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "failed to release tools metadata lock") {
		t.Fatalf("warnings = %#v, want lock release warning", result.Warnings)
	}
}

func TestServiceInstallLockTimeout(t *testing.T) {
	repo := toolsTestRepo(t)
	_, lockFile, err := MetadataPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	release, err := acquireLock(lockFile, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = release() }()

	_, err = Install(repo, "1.0.0", 0, serviceTestFetcher(t, "1.0.0", map[string][]byte{"alpha/tool.txt": []byte("x")}), nil)
	if err == nil {
		t.Fatal("Install should fail when metadata lock is held")
	}
	if !strings.Contains(err.Error(), "tools-metadata-lock-timeout") {
		t.Fatalf("error = %q, want lock timeout", err.Error())
	}
}

func TestServiceInstallErrorsOnInvalidMetadata(t *testing.T) {
	repo := toolsTestRepo(t)
	storeFile, _, err := MetadataPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, storeFile, "{not json")

	fetcher := serviceTestFetcher(t, "1.0.0", map[string][]byte{"alpha/tool.txt": []byte("x")})
	_, err = Install(repo, "1.0.0", time.Second, fetcher, []Component{serviceTestComponent{id: "alpha"}})
	if err == nil || !strings.Contains(err.Error(), "read tools metadata") {
		t.Fatalf("Install invalid metadata err = %v", err)
	}
}

func TestServiceUninstallDeletesMetadataAndValidatesActions(t *testing.T) {
	repo := toolsTestRepo(t)
	storeFile, _, err := MetadataPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteMetadata(storeFile, MetadataStore{Components: map[string]MetadataEntry{
		"alpha": {Component: "alpha", InstalledVersion: "1.0.0", ManagedMode: ManagedModeFile},
		"beta":  {Component: "beta", InstalledVersion: "1.0.0", ManagedMode: ManagedModeFile},
	}}); err != nil {
		t.Fatal(err)
	}

	targets := []Component{
		serviceTestComponent{id: "alpha", uninstallFn: func(ctx Context) Outcome {
			if ctx.Entry == nil || ctx.Entry.Component != "alpha" {
				t.Fatalf("alpha uninstall entry = %#v", ctx.Entry)
			}
			return Outcome{Result: Result{Component: "alpha", Action: ActionRemoved}, DeleteEntry: true}
		}},
		serviceTestComponent{id: "beta", uninstallFn: func(Context) Outcome {
			return Outcome{Result: Result{Component: "beta", Action: ActionCreated}}
		}},
	}

	result, err := Uninstall(repo, time.Second, targets)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if result.Results[0].Action != ActionRemoved {
		t.Fatalf("alpha result = %#v", result.Results[0])
	}
	if result.Results[1].Action != ActionFailed || !strings.Contains(result.Results[1].Reason, "not valid for uninstall") {
		t.Fatalf("beta result = %#v, want invalid action failure", result.Results[1])
	}
	store, err := ReadMetadata(storeFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Components["alpha"]; ok {
		t.Fatal("alpha metadata should be deleted")
	}
	if _, ok := store.Components["beta"]; !ok {
		t.Fatal("beta metadata should remain after invalid action")
	}
}

func TestServiceUninstallReturnsLockReleaseWarning(t *testing.T) {
	repo := toolsTestRepo(t)
	_, lockFile, err := MetadataPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	targets := []Component{serviceTestComponent{id: "alpha", uninstallFn: func(Context) Outcome {
		if err := os.Remove(lockFile); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(lockFile, "child"), 0o755); err != nil {
			t.Fatal(err)
		}
		return Outcome{Result: Result{Component: "alpha", Action: ActionNotInstalled}}
	}}}

	result, err := Uninstall(repo, time.Second, targets)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "failed to release tools metadata lock") {
		t.Fatalf("warnings = %#v, want lock release warning", result.Warnings)
	}
}

func TestServiceUninstallLockTimeout(t *testing.T) {
	repo := toolsTestRepo(t)
	_, lockFile, err := MetadataPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	release, err := acquireLock(lockFile, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = release() }()

	_, err = Uninstall(repo, 0, nil)
	if err == nil {
		t.Fatal("Uninstall should fail when metadata lock is held")
	}
	if !strings.Contains(err.Error(), "tools-metadata-lock-timeout") {
		t.Fatalf("error = %q, want lock timeout", err.Error())
	}
}

func TestServiceUninstallErrorsOnInvalidMetadata(t *testing.T) {
	repo := toolsTestRepo(t)
	storeFile, _, err := MetadataPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, storeFile, "{not json")

	_, err = Uninstall(repo, time.Second, []Component{serviceTestComponent{id: "alpha"}})
	if err == nil || !strings.Contains(err.Error(), "read tools metadata") {
		t.Fatalf("Uninstall invalid metadata err = %v", err)
	}
}
