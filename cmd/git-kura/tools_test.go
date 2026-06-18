package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- shared test helpers ---------------------------------------------------

func toolsTestRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.email", "kura-test@example.com")
	git(t, repo, "config", "user.name", "Kura Test")
	return repo
}

// runToolsCLI runs runToolsWith from inside repo, capturing stdout and the
// returned error.
func runToolsCLI(t *testing.T, repo string, deps toolsDeps, args ...string) (string, error) {
	t.Helper()
	var out string
	var err error
	withWorkingDir(t, repo, func() {
		out, err = captureStdout(t, func() error { return runToolsWith(deps, args) })
	})
	return out, err
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var xe *exitError
	if errors.As(err, &xe) {
		return xe.code
	}
	return exitGeneralError
}

func requireToolsExit(t *testing.T, err error, want int) {
	t.Helper()
	if got := exitCodeOf(err); got != want {
		t.Fatalf("exit code = %d, want %d (err: %v)", got, want, err)
	}
}

// --- in-memory release asset construction ----------------------------------

func makeToolsArchive(t *testing.T, manifest archiveManifest, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	writeArchiveEntry := func(name string, data []byte) {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	mj, _ := json.Marshal(manifest)
	writeArchiveEntry(toolsArchiveManifestName, mj)
	for name, data := range files {
		writeArchiveEntry(name, data)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeSidecar(t *testing.T, version, archiveName, algorithm string, archive []byte) []byte {
	t.Helper()
	s := sidecarManifest{
		ArchiveName:       archiveName,
		ArchiveChecksum:   sha256hex(archive),
		ChecksumAlgorithm: algorithm,
		Version:           version,
	}
	b, _ := json.Marshal(s)
	return b
}

type fakeFetcher struct {
	sidecar      []byte
	archive      []byte
	sidecarErr   error
	archiveErr   error
	sidecarCalls int
	archiveCalls int
}

func (f *fakeFetcher) fetchSidecar(tag, version string) ([]byte, error) {
	f.sidecarCalls++
	if f.sidecarErr != nil {
		return nil, f.sidecarErr
	}
	return f.sidecar, nil
}

func (f *fakeFetcher) fetchArchive(tag, name string) ([]byte, error) {
	f.archiveCalls++
	if f.archiveErr != nil {
		return nil, f.archiveErr
	}
	return f.archive, nil
}

// --- fixture components -----------------------------------------------------

// fileFixture is a file-managed component used to exercise the framework. It
// copies one archive file to a destination under the repo root and records a
// checksum so user modifications can be detected.
type fileFixture struct {
	componentID string
	archiveRel  string // path inside the archive
	destRel     string // destination path relative to repo root
}

func (c *fileFixture) id() string { return c.componentID }

func (c *fileFixture) destAbs(repoRoot string) string {
	return filepath.Join(repoRoot, filepath.FromSlash(c.destRel))
}

func (c *fileFixture) install(ctx toolsInstallContext) toolsOutcome {
	data, err := os.ReadFile(ctx.asset.path(c.archiveRel))
	if err != nil {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Action: actionFailed, Reason: "read asset: " + err.Error()}}
	}
	sum := sha256hex(data)
	action := actionCreated
	if ctx.entry != nil {
		if ctx.entry.Checksum == sum {
			return toolsOutcome{result: toolsResult{
				Component: c.componentID, ReleaseVersion: ctx.releaseVersion,
				SourceAsset: c.archiveRel, Destination: c.destRel,
				Action: actionSkipped, Managed: true, Reason: "already installed; checksum matches",
			}}
		}
		action = actionUpdated
	}
	dest := c.destAbs(ctx.repoRoot)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Action: actionFailed, Reason: err.Error()}}
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Action: actionFailed, Reason: err.Error()}}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	created := now
	if ctx.entry != nil {
		created = ctx.entry.CreatedAt
	}
	entry := &toolsMetadataEntry{
		Component: c.componentID, SourceAssetID: c.archiveRel,
		ReleaseVersion: ctx.releaseVersion, DestinationPath: c.destRel,
		InstalledVersion: ctx.releaseVersion, Checksum: sum,
		ManagedMode: managedModeFile, CreatedAt: created, UpdatedAt: now,
	}
	return toolsOutcome{
		result: toolsResult{
			Component: c.componentID, ReleaseVersion: ctx.releaseVersion,
			SourceAsset: c.archiveRel, Destination: c.destRel,
			Action: action, Managed: true, Reason: "",
		},
		setEntry: entry,
	}
}

func (c *fileFixture) status(ctx toolsContext) toolsOutcome {
	if ctx.entry == nil {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Action: actionNotInstalled, Reason: "no metadata"}}
	}
	dest := c.destAbs(ctx.repoRoot)
	data, err := os.ReadFile(dest)
	if err != nil {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Destination: c.destRel, Action: actionNotInstalled, Reason: "destination missing"}}
	}
	res := toolsResult{
		Component: c.componentID, ReleaseVersion: ctx.entry.ReleaseVersion,
		Destination: c.destRel, Managed: true,
	}
	if sha256hex(data) == ctx.entry.Checksum {
		res.Action = actionInstalled
	} else {
		res.Action = actionSkipped
		res.Reason = "destination modified outside git-kura"
	}
	return toolsOutcome{result: res}
}

func (c *fileFixture) uninstall(ctx toolsContext) toolsOutcome {
	if ctx.entry == nil {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Action: actionNotInstalled, Reason: "no metadata"}}
	}
	dest := c.destAbs(ctx.repoRoot)
	data, err := os.ReadFile(dest)
	if err != nil {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Destination: c.destRel, Action: actionNotInstalled, Reason: "destination missing"}, deleteEntry: true}
	}
	if sha256hex(data) != ctx.entry.Checksum {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Destination: c.destRel, Action: actionSkipped, Managed: true, Reason: "destination modified outside git-kura; leaving it untouched"}}
	}
	if err := os.Remove(dest); err != nil {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Action: actionFailed, Reason: err.Error()}}
	}
	return toolsOutcome{
		result:      toolsResult{Component: c.componentID, Destination: c.destRel, Action: actionRemoved, Managed: true},
		deleteEntry: true,
	}
}

// failingFixture always fails, to exercise multi-component continuation.
type failingFixture struct{ componentID string }

func (c *failingFixture) id() string { return c.componentID }
func (c *failingFixture) status(ctx toolsContext) toolsOutcome {
	return toolsOutcome{result: toolsResult{Component: c.componentID, Action: actionFailed, Reason: "boom"}}
}
func (c *failingFixture) install(ctx toolsInstallContext) toolsOutcome {
	return toolsOutcome{result: toolsResult{Component: c.componentID, Action: actionFailed, Reason: "boom"}}
}
func (c *failingFixture) uninstall(ctx toolsContext) toolsOutcome {
	return toolsOutcome{result: toolsResult{Component: c.componentID, Action: actionFailed, Reason: "boom"}}
}

// configFixture is a config-managed component used to exercise the config
// managed-mode uninstall semantics: it only reverts a value it still owns.
type configFixture struct {
	componentID string
	configKey   string
	value       string
}

func (c *configFixture) id() string { return c.componentID }

func (c *configFixture) currentValue(repoRoot string) (string, bool) {
	cmd := exec.Command("git", "config", "--get", c.configKey)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(out), "\n"), true
}

func (c *configFixture) install(ctx toolsInstallContext) toolsOutcome {
	cmd := exec.Command("git", "config", c.configKey, c.value)
	cmd.Dir = ctx.repoRoot
	if err := cmd.Run(); err != nil {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Action: actionFailed, Reason: err.Error()}}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	created := now
	action := actionCreated
	if ctx.entry != nil {
		created = ctx.entry.CreatedAt
		action = actionUpdated
	}
	entry := &toolsMetadataEntry{
		Component: c.componentID, ReleaseVersion: ctx.releaseVersion,
		DestinationPath: c.configKey, Checksum: c.value,
		ManagedMode: managedModeConfig, CreatedAt: created, UpdatedAt: now,
	}
	return toolsOutcome{
		result:   toolsResult{Component: c.componentID, ReleaseVersion: ctx.releaseVersion, Destination: c.configKey, Action: action, Managed: true},
		setEntry: entry,
	}
}

func (c *configFixture) status(ctx toolsContext) toolsOutcome {
	if ctx.entry == nil {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Action: actionNotInstalled, Reason: "no metadata"}}
	}
	cur, ok := c.currentValue(ctx.repoRoot)
	if !ok {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Destination: c.configKey, Action: actionNotInstalled, Reason: "config unset"}}
	}
	if cur == ctx.entry.Checksum {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Destination: c.configKey, Action: actionInstalled, Managed: true}}
	}
	return toolsOutcome{result: toolsResult{Component: c.componentID, Destination: c.configKey, Action: actionSkipped, Managed: true, Reason: "config value changed outside git-kura"}}
}

func (c *configFixture) uninstall(ctx toolsContext) toolsOutcome {
	if ctx.entry == nil {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Action: actionNotInstalled, Reason: "no metadata"}}
	}
	cur, ok := c.currentValue(ctx.repoRoot)
	if !ok {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Destination: c.configKey, Action: actionNotInstalled, Reason: "config already unset"}, deleteEntry: true}
	}
	if cur != ctx.entry.Checksum {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Destination: c.configKey, Action: actionSkipped, Managed: true, Reason: "config value changed outside git-kura; leaving it untouched"}}
	}
	cmd := exec.Command("git", "config", "--unset", c.configKey)
	cmd.Dir = ctx.repoRoot
	if err := cmd.Run(); err != nil {
		return toolsOutcome{result: toolsResult{Component: c.componentID, Action: actionFailed, Reason: err.Error()}}
	}
	return toolsOutcome{result: toolsResult{Component: c.componentID, Destination: c.configKey, Action: actionRemoved, Managed: true}, deleteEntry: true}
}

// componentFunc adapts function fields to the toolsComponent interface for
// lightweight one-off fixtures.
type componentFunc struct {
	idVal       string
	statusFn    func(toolsContext) toolsOutcome
	installFn   func(toolsInstallContext) toolsOutcome
	uninstallFn func(toolsContext) toolsOutcome
}

func (c componentFunc) id() string { return c.idVal }
func (c componentFunc) status(ctx toolsContext) toolsOutcome {
	if c.statusFn != nil {
		return c.statusFn(ctx)
	}
	return toolsOutcome{result: toolsResult{Component: c.idVal, Action: actionNotInstalled}}
}
func (c componentFunc) install(ctx toolsInstallContext) toolsOutcome {
	if c.installFn != nil {
		return c.installFn(ctx)
	}
	return toolsOutcome{result: toolsResult{Component: c.idVal, Action: actionFailed}}
}
func (c componentFunc) uninstall(ctx toolsContext) toolsOutcome {
	if c.uninstallFn != nil {
		return c.uninstallFn(ctx)
	}
	return toolsOutcome{result: toolsResult{Component: c.idVal, Action: actionNotInstalled}}
}

// --- standard fixture wiring -----------------------------------------------

const fixtureVersion = "1.2.3"
const fixtureArchiveName = "git-kura-tools_1.2.3.tar.gz"

// fixtureAssets builds the standard single-file fixture component and the
// release assets that install it for the default fixture version.
func fixtureAssets(t *testing.T, content []byte) (*fakeFetcher, *fileFixture) {
	t.Helper()
	return fixtureAssetsForVersion(t, fixtureVersion, content)
}

// fixtureAssetsForVersion is like fixtureAssets but lets a test pin the release
// version, so cross-version installs (which use a distinct cache directory) can
// be exercised.
func fixtureAssetsForVersion(t *testing.T, version string, content []byte) (*fakeFetcher, *fileFixture) {
	t.Helper()
	comp := &fileFixture{componentID: "alpha", archiveRel: "alpha/tool.txt", destRel: ".kura-tools/alpha.txt"}
	manifest := archiveManifest{
		SchemaVersion: 1,
		Components: map[string]archiveManifestComponent{
			"alpha": {Files: map[string]string{comp.archiveRel: sha256hex(content)}},
		},
	}
	archive := makeToolsArchive(t, manifest, map[string][]byte{comp.archiveRel: content})
	archiveName := "git-kura-tools_" + version + ".tar.gz"
	sidecar := makeSidecar(t, version, archiveName, checksumAlgorithmSHA256, archive)
	return &fakeFetcher{sidecar: sidecar, archive: archive}, comp
}

func installedJSONPath(repo string) string {
	return filepath.Join(repo, ".git", "kura", "tools", "installed.json")
}

// --- tests ------------------------------------------------------------------

func TestToolsProductionRegistryRecognizesComponents(t *testing.T) {
	reg := productionToolsRegistry()
	got := reg.ids()
	want := []string{"pre-commit", "claude-skill", "codex-skill"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("production registry IDs = %v, want %v", got, want)
	}
	for _, id := range want {
		if _, ok := reg.get(id); !ok {
			t.Fatalf("component %q not recognized", id)
		}
	}
	// No dummy/test components must leak into the production registry.
	for _, id := range got {
		if strings.Contains(id, "dummy") || strings.Contains(id, "test") || id == "alpha" {
			t.Fatalf("unexpected component %q in production registry", id)
		}
	}
}

func TestToolsUsageErrors(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := toolsDeps{registry: productionToolsRegistry(), version: fixtureVersion, fetcher: &fakeFetcher{}}

	cases := [][]string{
		{},                                   // no subcommand
		{"bogus"},                            // unknown subcommand
		{"install"},                          // install without component
		{"uninstall"},                        // uninstall without component
		{"install", "--all", "pre-commit"},   // --all + explicit
		{"uninstall", "--all", "pre-commit"}, // --all + explicit
		{"status", "--all"},                  // status does not accept --all
		{"install", "no-such-component"},     // unknown component
		{"status", "no-such-component"},      // unknown component
		{"install", "--bogus"},               // unknown flag
	}
	for _, args := range cases {
		_, err := runToolsCLI(t, repo, deps, args...)
		requireToolsExit(t, err, exitUsageError)
	}
}

func TestToolsHelp(t *testing.T) {
	deps := toolsDeps{registry: productionToolsRegistry()}
	for _, args := range [][]string{{"-h"}, {"--help"}, {"status", "-h"}, {"install", "--help"}, {"uninstall", "-h"}} {
		out, err := captureStdout(t, func() error { return runToolsWith(deps, args) })
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(out, "Usage") {
			t.Fatalf("%v: expected usage help, got %q", args, out)
		}
	}
	// The production entrypoint wires the real registry/fetcher; help needs no
	// repository or network.
	if _, err := captureStdout(t, func() error { return runTools([]string{"--help"}) }); err != nil {
		t.Fatalf("runTools --help: %v", err)
	}
}

func TestToolsFailsOutsideGitRepository(t *testing.T) {
	dir := t.TempDir() // not a git repository
	deps := toolsDeps{registry: productionToolsRegistry(), version: fixtureVersion, fetcher: &fakeFetcher{}}
	var err error
	withWorkingDir(t, dir, func() {
		_, err = captureStdout(t, func() error { return runToolsWith(deps, []string{"status"}) })
	})
	if err == nil {
		t.Fatal("status outside a git repository should fail")
	}
}

func TestToolsStatusShowsAllComponentsWhenUnspecified(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := toolsDeps{registry: productionToolsRegistry(), version: fixtureVersion, fetcher: &fakeFetcher{}}

	out, err := runToolsCLI(t, repo, deps, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, id := range []string{"pre-commit", "claude-skill", "codex-skill"} {
		if !strings.Contains(out, id) {
			t.Fatalf("status output missing %q:\n%s", id, out)
		}
	}
	if !strings.Contains(out, "not-installed") {
		t.Fatalf("status output should report not-installed:\n%s", out)
	}
}

func TestToolsStatusSingleComponent(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := toolsDeps{registry: productionToolsRegistry(), version: fixtureVersion, fetcher: &fakeFetcher{}}

	out, err := runToolsCLI(t, repo, deps, "status", "pre-commit")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "pre-commit") {
		t.Fatalf("status output missing pre-commit:\n%s", out)
	}
	if strings.Contains(out, "claude-skill") {
		t.Fatalf("status of one component should not list others:\n%s", out)
	}
}

func TestToolsInstallCreatesThenIsIdempotent(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("hello tool\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}

	out, err := runToolsCLI(t, repo, deps, "install", "alpha")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(out, "created") {
		t.Fatalf("first install should be created:\n%s", out)
	}
	assertPathExists(t, comp.destAbs(repo))
	assertPathExists(t, installedJSONPath(repo))

	// Second install with unchanged asset is idempotent (skipped), and the
	// archive is served from cache (no second download).
	out, err = runToolsCLI(t, repo, deps, "install", "alpha")
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if !strings.Contains(out, "skipped") {
		t.Fatalf("second install should be skipped:\n%s", out)
	}
	if fetcher.archiveCalls != 1 {
		t.Fatalf("archive should be downloaded once and then cached; archiveCalls=%d", fetcher.archiveCalls)
	}
}

func TestToolsInstallUpdatesAcrossVersions(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssetsForVersion(t, "1.2.3", []byte("v1\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: "1.2.3", fetcher: fetcher}

	if _, err := runToolsCLI(t, repo, deps, "install", "alpha"); err != nil {
		t.Fatalf("install 1.2.3: %v", err)
	}

	// A newer binary version ships a different asset. This uses a distinct cache
	// directory, so it is a legitimate update rather than a same-version release
	// inconsistency.
	fetcher2, comp2 := fixtureAssetsForVersion(t, "1.2.4", []byte("v2\n"))
	deps2 := toolsDeps{registry: newToolsRegistry(comp2), version: "1.2.4", fetcher: fetcher2}
	out, err := runToolsCLI(t, repo, deps2, "install", "alpha")
	if err != nil {
		t.Fatalf("install 1.2.4: %v", err)
	}
	if !strings.Contains(out, "updated") {
		t.Fatalf("install of a newer version should be updated:\n%s", out)
	}
	data, _ := os.ReadFile(comp2.destAbs(repo))
	if string(data) != "v2\n" {
		t.Fatalf("destination = %q, want updated content", data)
	}
}

func TestToolsInstallFailsOnSameVersionCacheInconsistency(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("v1\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}
	if _, err := runToolsCLI(t, repo, deps, "install", "alpha"); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// The same version now advertises a different archive checksum: a release
	// asset must never change, so install must fail rather than refresh or use
	// the cache.
	tampered := makeSidecar(t, fixtureVersion, fixtureArchiveName, checksumAlgorithmSHA256, []byte("different release bytes"))
	badFetcher := &fakeFetcher{sidecar: tampered, archive: fetcher.archive}
	deps2 := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: badFetcher}

	out, err := runToolsCLI(t, repo, deps2, "install", "alpha")
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(strings.ToLower(out), "inconsistent") {
		t.Fatalf("same-version checksum change should fail as a release inconsistency:\n%s", out)
	}
	// The cache must not be used or replaced: no re-download happened.
	if badFetcher.archiveCalls != 0 {
		t.Fatalf("inconsistent cache must not be replaced; archiveCalls=%d", badFetcher.archiveCalls)
	}
	// The original install is untouched.
	data, _ := os.ReadFile(comp.destAbs(repo))
	if string(data) != "v1\n" {
		t.Fatalf("destination = %q, want unchanged v1 content", data)
	}
}

func TestToolsUninstallRemovesManagedFile(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("x\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}

	if _, err := runToolsCLI(t, repo, deps, "install", "alpha"); err != nil {
		t.Fatalf("install: %v", err)
	}
	out, err := runToolsCLI(t, repo, deps, "uninstall", "alpha")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !strings.Contains(out, "removed") {
		t.Fatalf("uninstall should remove:\n%s", out)
	}
	assertPathMissing(t, comp.destAbs(repo))

	// Uninstalling again is a not-installed no-op.
	out, err = runToolsCLI(t, repo, deps, "uninstall", "alpha")
	if err != nil {
		t.Fatalf("second uninstall: %v", err)
	}
	if !strings.Contains(out, "not-installed") {
		t.Fatalf("second uninstall should be not-installed:\n%s", out)
	}
}

func TestToolsUninstallSkipsUserModifiedFile(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("original\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}

	if _, err := runToolsCLI(t, repo, deps, "install", "alpha"); err != nil {
		t.Fatalf("install: %v", err)
	}
	// User edits the managed file out from under git-kura.
	if err := os.WriteFile(comp.destAbs(repo), []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runToolsCLI(t, repo, deps, "uninstall", "alpha")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !strings.Contains(out, "skipped") {
		t.Fatalf("uninstall of modified file should skip:\n%s", out)
	}
	assertPathExists(t, comp.destAbs(repo))
	// Skipped is not a failure: metadata is retained so the file stays managed.
	assertPathExists(t, installedJSONPath(repo))
}

func TestToolsConfigManagedUninstall(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, _ := fixtureAssets(t, []byte("ignored\n"))
	cfg := &configFixture{componentID: "cfg", configKey: "kura.fixtureValue", value: "managed"}
	deps := toolsDeps{registry: newToolsRegistry(cfg), version: fixtureVersion, fetcher: fetcher}

	if _, err := runToolsCLI(t, repo, deps, "install", "cfg"); err != nil {
		t.Fatalf("install cfg: %v", err)
	}
	// Metadata records config managed mode.
	store := requireJSONFile(t, installedJSONPath(repo))
	comps := store["components"].(map[string]any)
	entry := comps["cfg"].(map[string]any)
	if entry["managedMode"] != managedModeConfig {
		t.Fatalf("managedMode = %v, want %q", entry["managedMode"], managedModeConfig)
	}

	// A user changes the value: uninstall must not revert it.
	git(t, repo, "config", "kura.fixtureValue", "user-set")
	out, err := runToolsCLI(t, repo, deps, "uninstall", "cfg")
	if err != nil {
		t.Fatalf("uninstall cfg: %v", err)
	}
	if !strings.Contains(out, "skipped") {
		t.Fatalf("uninstall of user-changed config should skip:\n%s", out)
	}
	if v, _ := cfg.currentValue(repo); v != "user-set" {
		t.Fatalf("config value = %q, want it left untouched", v)
	}
}

func TestToolsMultipleComponentsContinueAfterFailure(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("ok\n"))
	fail := &failingFixture{componentID: "boom"}
	deps := toolsDeps{registry: newToolsRegistry(fail, comp), version: fixtureVersion, fetcher: fetcher}

	out, err := runToolsCLI(t, repo, deps, "install", "boom", "alpha")
	// At least one failure means non-zero exit.
	requireToolsExit(t, err, exitGeneralError)
	// But the non-failing component is still processed.
	if !strings.Contains(out, "created") {
		t.Fatalf("alpha should still be installed despite boom failing:\n%s", out)
	}
	assertPathExists(t, comp.destAbs(repo))
}

func TestToolsInstallFailsOnNonReleaseBinary(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("x\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: "dev", fetcher: fetcher}

	out, err := runToolsCLI(t, repo, deps, "install", "alpha")
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(out, "failed") {
		t.Fatalf("install on dev build should fail:\n%s", out)
	}
	if fetcher.sidecarCalls != 0 {
		t.Fatalf("non-release build must not fetch any asset; sidecarCalls=%d", fetcher.sidecarCalls)
	}
	assertPathMissing(t, installedJSONPath(repo))
}

func TestToolsInstallFailsOnUnsupportedChecksumAlgorithm(t *testing.T) {
	repo := toolsTestRepo(t)
	comp := &fileFixture{componentID: "alpha", archiveRel: "alpha/tool.txt", destRel: ".kura-tools/alpha.txt"}
	content := []byte("x\n")
	manifest := archiveManifest{SchemaVersion: 1, Components: map[string]archiveManifestComponent{"alpha": {Files: map[string]string{comp.archiveRel: sha256hex(content)}}}}
	archive := makeToolsArchive(t, manifest, map[string][]byte{comp.archiveRel: content})
	sidecar := makeSidecar(t, fixtureVersion, fixtureArchiveName, "sha512", archive)
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: &fakeFetcher{sidecar: sidecar, archive: archive}}

	out, err := runToolsCLI(t, repo, deps, "install", "alpha")
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(out, "failed") || !strings.Contains(strings.ToLower(out), "checksum algorithm") {
		t.Fatalf("install should fail with unsupported checksum algorithm:\n%s", out)
	}
	assertPathMissing(t, installedJSONPath(repo))
}

func TestToolsInstallFailsOnSidecarVersionMismatch(t *testing.T) {
	repo := toolsTestRepo(t)
	comp := &fileFixture{componentID: "alpha", archiveRel: "alpha/tool.txt", destRel: ".kura-tools/alpha.txt"}
	content := []byte("x\n")
	manifest := archiveManifest{SchemaVersion: 1, Components: map[string]archiveManifestComponent{"alpha": {Files: map[string]string{comp.archiveRel: sha256hex(content)}}}}
	archive := makeToolsArchive(t, manifest, map[string][]byte{comp.archiveRel: content})
	// The sidecar is fetched for 1.2.3 but its internal version says 9.9.9.
	sidecar := makeSidecar(t, "9.9.9", fixtureArchiveName, checksumAlgorithmSHA256, archive)
	fetcher := &fakeFetcher{sidecar: sidecar, archive: archive}
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}

	out, err := runToolsCLI(t, repo, deps, "install", "alpha")
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(strings.ToLower(out), "does not match the binary release version") {
		t.Fatalf("sidecar version mismatch should fail the install:\n%s", out)
	}
	// The mismatch is caught before downloading/extracting the archive.
	if fetcher.archiveCalls != 0 {
		t.Fatalf("version mismatch must fail before extraction; archiveCalls=%d", fetcher.archiveCalls)
	}
	assertPathMissing(t, installedJSONPath(repo))
}

func TestToolsInstallFailsOnChecksumMismatch(t *testing.T) {
	repo := toolsTestRepo(t)
	comp := &fileFixture{componentID: "alpha", archiveRel: "alpha/tool.txt", destRel: ".kura-tools/alpha.txt"}
	content := []byte("x\n")
	manifest := archiveManifest{SchemaVersion: 1, Components: map[string]archiveManifestComponent{"alpha": {Files: map[string]string{comp.archiveRel: sha256hex(content)}}}}
	archive := makeToolsArchive(t, manifest, map[string][]byte{comp.archiveRel: content})
	// Sidecar advertises a checksum for a different archive.
	sidecar := makeSidecar(t, fixtureVersion, fixtureArchiveName, checksumAlgorithmSHA256, []byte("different bytes"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: &fakeFetcher{sidecar: sidecar, archive: archive}}

	out, err := runToolsCLI(t, repo, deps, "install", "alpha")
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(strings.ToLower(out), "checksum mismatch") {
		t.Fatalf("install should fail with checksum mismatch:\n%s", out)
	}
	assertPathMissing(t, installedJSONPath(repo))
}

func TestToolsCorruptMetadataRefusesDestructiveOps(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("x\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}

	// Write a corrupt metadata store.
	storeFile := installedJSONPath(repo)
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storeFile, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := runToolsCLI(t, repo, deps, "uninstall", "alpha")
	requireToolsExit(t, err, exitGeneralError)
	// The corrupt store is left as-is; no destructive change happened.
	data, _ := os.ReadFile(storeFile)
	if string(data) != "{ not json" {
		t.Fatalf("corrupt store should be untouched, got %q", data)
	}
}

func TestToolsStatusFailsOnCorruptMetadata(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := toolsDeps{registry: productionToolsRegistry(), version: fixtureVersion, fetcher: &fakeFetcher{}}
	storeFile := installedJSONPath(repo)
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storeFile, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runToolsCLI(t, repo, deps, "status"); err == nil {
		t.Fatal("status should fail when metadata cannot be read")
	}
}

func TestToolsInstallFailsWhenLockHeld(t *testing.T) {
	repo := toolsTestRepo(t)
	// A zero timeout makes a single lock attempt with no retry.
	git(t, repo, "config", sealLockTimeoutConfigKey, "0")
	fetcher, comp := fixtureAssets(t, []byte("x\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}

	_, lockFile, err := toolsMetadataPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(lockFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = runToolsCLI(t, repo, deps, "install", "alpha")
	requireToolsExit(t, err, exitSealLockTimeout)
	assertPathMissing(t, installedJSONPath(repo))
	assertPathMissing(t, comp.destAbs(repo))
}

func TestToolsArchiveManifestPerFileChecksumReadable(t *testing.T) {
	// The framework must expose the archive manifest's component-to-asset
	// mapping and per-file checksums to component implementations.
	repo := toolsTestRepo(t)
	content := []byte("inspect me\n")
	fetcher, _ := fixtureAssets(t, content)

	var seen archiveManifestComponent
	var sawAsset bool
	inspector := componentFunc{
		idVal: "inspect",
		installFn: func(ctx toolsInstallContext) toolsOutcome {
			ac, ok := ctx.asset.componentAssets("alpha")
			seen, sawAsset = ac, ok
			return toolsOutcome{result: toolsResult{Component: "inspect", Action: actionSkipped, Reason: "inspection only"}}
		},
	}
	deps := toolsDeps{registry: newToolsRegistry(inspector), version: fixtureVersion, fetcher: fetcher}
	if _, err := runToolsCLI(t, repo, deps, "install", "inspect"); err != nil {
		t.Fatalf("install inspect: %v", err)
	}
	if !sawAsset {
		t.Fatal("component could not read the archive manifest mapping for alpha")
	}
	if seen.Files["alpha/tool.txt"] != sha256hex(content) {
		t.Fatalf("per-file checksum = %q, want %q", seen.Files["alpha/tool.txt"], sha256hex(content))
	}
}

func TestGithubReleaseFetcher(t *testing.T) {
	body := []byte("payload")
	g := githubReleaseFetcher{client: &http.Client{Transport: stubTransport{status: http.StatusOK, body: body}}}
	if got, err := g.fetchSidecar("v1.2.3", "1.2.3"); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("fetchSidecar = %q, %v", got, err)
	}
	if got, err := g.fetchArchive("v1.2.3", "git-kura-tools_1.2.3.tar.gz"); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("fetchArchive = %q, %v", got, err)
	}
	if url := g.downloadURL("v1.2.3", "asset.tar.gz"); url != "https://github.com/tooppoo/git-kura/releases/download/v1.2.3/asset.tar.gz" {
		t.Fatalf("downloadURL = %q", url)
	}

	bad := githubReleaseFetcher{client: &http.Client{Transport: stubTransport{status: http.StatusNotFound, body: nil}}}
	if _, err := bad.fetchSidecar("v9.9.9", "9.9.9"); err == nil {
		t.Fatal("expected error on 404")
	}
	if newGithubReleaseFetcher().client == nil {
		t.Fatal("newGithubReleaseFetcher should set a client")
	}
}

type stubTransport struct {
	status int
	body   []byte
}

func (s stubTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.status,
		Status:     http.StatusText(s.status),
		Body:       io.NopCloser(bytes.NewReader(s.body)),
		Header:     make(http.Header),
	}, nil
}

func TestToolsPendingComponentInstallAndUninstall(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, _ := fixtureAssets(t, []byte("x\n"))
	deps := toolsDeps{registry: productionToolsRegistry(), version: fixtureVersion, fetcher: fetcher}

	// install of a pending component resolves and verifies the asset, then fails
	// with a tracking-issue reason; the command exits non-zero and writes no
	// metadata. pre-commit is implemented (#55), so a still-pending component is
	// used here.
	out, err := runToolsCLI(t, repo, deps, "install", "claude-skill")
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(out, "failed") || !strings.Contains(out, "not yet implemented") {
		t.Fatalf("pending install should fail with a tracking reason:\n%s", out)
	}
	assertPathMissing(t, installedJSONPath(repo))

	// uninstall of a pending component is a not-installed no-op.
	out, err = runToolsCLI(t, repo, deps, "uninstall", "claude-skill")
	if err != nil {
		t.Fatalf("pending uninstall: %v", err)
	}
	if !strings.Contains(out, "not-installed") {
		t.Fatalf("pending uninstall should be not-installed:\n%s", out)
	}
}

func TestToolsInstallAllTargetsEveryComponent(t *testing.T) {
	repo := toolsTestRepo(t)
	a := &fileFixture{componentID: "alpha", archiveRel: "alpha/tool.txt", destRel: ".kura-tools/alpha.txt"}
	b := &fileFixture{componentID: "beta", archiveRel: "alpha/tool.txt", destRel: ".kura-tools/beta.txt"}
	content := []byte("shared\n")
	manifest := archiveManifest{SchemaVersion: 1, Components: map[string]archiveManifestComponent{
		"alpha": {Files: map[string]string{a.archiveRel: sha256hex(content)}},
		"beta":  {Files: map[string]string{b.archiveRel: sha256hex(content)}},
	}}
	archive := makeToolsArchive(t, manifest, map[string][]byte{a.archiveRel: content})
	sidecar := makeSidecar(t, fixtureVersion, fixtureArchiveName, checksumAlgorithmSHA256, archive)
	deps := toolsDeps{registry: newToolsRegistry(a, b), version: fixtureVersion, fetcher: &fakeFetcher{sidecar: sidecar, archive: archive}}

	out, err := runToolsCLI(t, repo, deps, "install", "--all")
	if err != nil {
		t.Fatalf("install --all: %v", err)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Fatalf("install --all should target every component:\n%s", out)
	}
	assertPathExists(t, a.destAbs(repo))
	assertPathExists(t, b.destAbs(repo))
}

func TestToolsResolveIgnoresCorruptCache(t *testing.T) {
	repo := toolsTestRepo(t)
	fetcher, comp := fixtureAssets(t, []byte("y\n"))
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: fetcher}

	// Seed a corrupt cache metadata file for this version.
	resolver := &toolsAssetResolver{version: fixtureVersion, commonDir: filepath.Join(repo, ".git"), fetcher: fetcher}
	cacheDir := resolver.cacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheMetadataPath(cacheDir), []byte("{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runToolsCLI(t, repo, deps, "install", "alpha")
	if err != nil {
		t.Fatalf("install with corrupt cache: %v", err)
	}
	if !strings.Contains(out, "created") {
		t.Fatalf("corrupt cache should be ignored and asset re-downloaded:\n%s", out)
	}
	if fetcher.archiveCalls != 1 {
		t.Fatalf("archive should be downloaded after corrupt cache; archiveCalls=%d", fetcher.archiveCalls)
	}
}

func TestExtractTarGzExtractsFilesAndDirsAndSkipsSymlinks(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "sub/", Typeflag: tar.TypeDir, Mode: 0o755})
	fdata := []byte("hi")
	_ = tw.WriteHeader(&tar.Header{Name: "sub/file.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(fdata))})
	_, _ = tw.Write(fdata)
	_ = tw.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "sub/file.txt", Mode: 0o777})
	_ = tw.Close()
	_ = gz.Close()

	dest := filepath.Join(t.TempDir(), "root")
	if err := extractTarGz(buf.Bytes(), dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "sub", "file.txt"))
	if err != nil || string(got) != "hi" {
		t.Fatalf("file = %q, err = %v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(dest, "link")); !os.IsNotExist(err) {
		t.Fatalf("symlink entry should be skipped")
	}
}

func TestToolsMetadataRoundTripAndValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "installed.json")

	// Absent store reads as empty.
	s, err := readToolsMetadata(path)
	if err != nil || len(s.Components) != 0 {
		t.Fatalf("absent store: %v, %+v", err, s)
	}

	// Round-trip a valid entry.
	s.Components["x"] = toolsMetadataEntry{Component: "x", ManagedMode: managedModeFile, CreatedAt: "t0", UpdatedAt: "t1"}
	if err := writeToolsMetadata(path, s); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readToolsMetadata(path)
	if err != nil || got.Components["x"].Component != "x" {
		t.Fatalf("round-trip: %v, %+v", err, got)
	}

	// An unsupported schemaVersion is rejected.
	if err := os.WriteFile(path, []byte(`{"schemaVersion":2,"components":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readToolsMetadata(path); err == nil {
		t.Fatal("unsupported schemaVersion should be rejected")
	}

	// Non-JSON is rejected.
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readToolsMetadata(path); err == nil {
		t.Fatal("non-JSON store should be rejected")
	}

	// writeToolsMetadata refuses a store that violates the schema (managedMode
	// is required and constrained to file/config).
	bad := toolsMetadataStore{Components: map[string]toolsMetadataEntry{
		"y": {Component: "y", CreatedAt: "t0", UpdatedAt: "t1"},
	}}
	if err := writeToolsMetadata(filepath.Join(dir, "bad.json"), bad); err == nil {
		t.Fatal("writing an invalid entry should be refused")
	}
}

func TestToolsResolveErrorPaths(t *testing.T) {
	dir := t.TempDir()
	good := makeSidecar(t, "1.2.3", "a.tar.gz", checksumAlgorithmSHA256, []byte("ignored"))

	cases := []struct {
		name    string
		fetcher *fakeFetcher
	}{
		{"sidecar fetch error", &fakeFetcher{sidecarErr: errors.New("net down")}},
		{"sidecar parse error", &fakeFetcher{sidecar: []byte("not json")}},
		{"missing archive fields", &fakeFetcher{sidecar: mustJSON(sidecarManifest{ChecksumAlgorithm: checksumAlgorithmSHA256, Version: "1.2.3"})}},
		{"archive fetch error", &fakeFetcher{sidecar: good, archiveErr: errors.New("net down")}},
	}
	for _, tc := range cases {
		r := &toolsAssetResolver{version: "1.2.3", commonDir: filepath.Join(dir, tc.name), fetcher: tc.fetcher}
		if _, err := r.resolve(); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}

func TestToolsResolveFailsWhenArchiveLacksManifest(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "only.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	_ = gz.Close()

	sidecar := makeSidecar(t, "1.2.3", "a.tar.gz", checksumAlgorithmSHA256, buf.Bytes())
	r := &toolsAssetResolver{version: "1.2.3", commonDir: dir, fetcher: &fakeFetcher{sidecar: sidecar, archive: buf.Bytes()}}
	if _, err := r.resolve(); err == nil {
		t.Fatal("archive without manifest.json should fail to resolve")
	}
}

func TestGithubFetcherTransportError(t *testing.T) {
	g := githubReleaseFetcher{client: &http.Client{Transport: errTransport{}}}
	if _, err := g.get("https://example.invalid/x"); err == nil {
		t.Fatal("transport error should propagate")
	}
	// A zero-value fetcher falls back to http.DefaultClient; a refused
	// connection still surfaces as an error.
	if _, err := (githubReleaseFetcher{}).get("http://127.0.0.1:1/nope"); err == nil {
		t.Fatal("connection error should propagate")
	}
}

func TestNewToolsRegistryRejectsDuplicateID(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate component ID should panic")
		}
	}()
	newToolsRegistry(&failingFixture{componentID: "dup"}, &failingFixture{componentID: "dup"})
}

func TestToolsHelpersFailWhenParentIsAFile(t *testing.T) {
	// A regular file cannot have children, so directory creation and reads under
	// it fail deterministically — exercising the I/O error branches.
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	under := filepath.Join(f, "sub", "x.json")

	if _, err := readToolsMetadata(under); err == nil {
		t.Fatal("readToolsMetadata under a file should fail")
	}
	if err := writeToolsMetadata(under, toolsMetadataStore{}); err == nil {
		t.Fatal("writeToolsMetadata under a file should fail")
	}
	if _, err := acquireToolsLock(filepath.Join(f, "sub", "l.lock"), 0); err == nil {
		t.Fatal("acquireToolsLock under a file should fail")
	}

	var empty bytes.Buffer
	gz := gzip.NewWriter(&empty)
	_ = gz.Close()
	if err := extractTarGz(empty.Bytes(), filepath.Join(f, "root")); err == nil {
		t.Fatal("extractTarGz under a file should fail")
	}
}

func TestExtractTarGzFailsWhenFileTargetIsADir(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "x", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1})
	_, _ = tw.Write([]byte("y"))
	_ = tw.Close()
	_ = gz.Close()

	dest := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(filepath.Join(dest, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// "x" already exists as a directory, so writing the regular file fails.
	if err := extractTarGz(buf.Bytes(), dest); err == nil {
		t.Fatal("writing a file over an existing directory should fail")
	}
}

func TestExtractTarGzRejectsBackslashTraversal(t *testing.T) {
	// On Windows a backslash entry name would become a path separator via
	// filepath and could escape destRoot, so it must be rejected on every
	// platform.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: `..\escape.txt`, Mode: 0o644, Size: 3, Typeflag: tar.TypeReg}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte("bad"))
	_ = tw.Close()
	_ = gz.Close()

	if err := extractTarGz(buf.Bytes(), filepath.Join(t.TempDir(), "root")); err == nil {
		t.Fatal("backslash entry should be rejected")
	}
}

func TestExtractTarGzRejectsInvalidGzip(t *testing.T) {
	if err := extractTarGz([]byte("not a gzip stream"), t.TempDir()); err == nil {
		t.Fatal("invalid gzip should be rejected")
	}
}

func TestToolsResolveFailsOnInvalidArchiveManifest(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	bad := []byte("{ not json")
	_ = tw.WriteHeader(&tar.Header{Name: toolsArchiveManifestName, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(bad))})
	_, _ = tw.Write(bad)
	_ = tw.Close()
	_ = gz.Close()

	sidecar := makeSidecar(t, "1.2.3", "a.tar.gz", checksumAlgorithmSHA256, buf.Bytes())
	r := &toolsAssetResolver{version: "1.2.3", commonDir: dir, fetcher: &fakeFetcher{sidecar: sidecar, archive: buf.Bytes()}}
	if _, err := r.resolve(); err == nil {
		t.Fatal("invalid archive manifest should fail to resolve")
	}
}

func TestToolsInvalidActionFromComponentBecomesFailure(t *testing.T) {
	repo := toolsTestRepo(t)
	// status may not emit "created"; a component that does is treated as a bug
	// and reported failed.
	comp := componentFunc{idVal: "weird", statusFn: func(toolsContext) toolsOutcome {
		return toolsOutcome{result: toolsResult{Component: "weird", Action: actionCreated}}
	}}
	deps := toolsDeps{registry: newToolsRegistry(comp), version: fixtureVersion, fetcher: &fakeFetcher{}}
	out, err := runToolsCLI(t, repo, deps, "status", "weird")
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(out, "failed") || !strings.Contains(strings.ToLower(out), "internal") {
		t.Fatalf("invalid action should become an internal failure:\n%s", out)
	}
}

type errTransport struct{}

func (errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport boom")
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestExtractTarGzRejectsTraversal(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "../escape.txt", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte("bad"))
	_ = tw.Close()
	_ = gz.Close()

	dest := t.TempDir()
	if err := extractTarGz(buf.Bytes(), filepath.Join(dest, "root")); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
}
