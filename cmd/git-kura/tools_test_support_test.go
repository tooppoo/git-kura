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

	"github.com/tooppoo/git-kura/internal/gitutil"
)

// --- shared CLI helpers ----------------------------------------------------

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

// --- HTTP transport stubs --------------------------------------------------

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

type errTransport struct{}

func (errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport boom")
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// --- skill test helpers ----------------------------------------------------

func skillFetcher(t *testing.T, claudeContent, codexContent []byte) *fakeFetcher {
	t.Helper()
	return skillFetcherForVersion(t, fixtureVersion, claudeContent, codexContent)
}

func skillFetcherForVersion(t *testing.T, version string, claudeContent, codexContent []byte) *fakeFetcher {
	t.Helper()
	manifest := archiveManifest{
		SchemaVersion: 1,
		Components: map[string]archiveManifestComponent{
			claudeSkillComponentID: {Files: map[string]string{
				claudeSkillArchivePath: sha256hex(claudeContent),
			}},
			codexSkillComponentID: {Files: map[string]string{
				codexSkillArchivePath: sha256hex(codexContent),
			}},
		},
	}
	files := map[string][]byte{
		claudeSkillArchivePath: claudeContent,
		codexSkillArchivePath:  codexContent,
	}
	archive := makeToolsArchive(t, manifest, files)
	archiveName := "git-kura-tools_" + version + ".tar.gz"
	sidecar := makeSidecar(t, version, archiveName, checksumAlgorithmSHA256, archive)
	return &fakeFetcher{sidecar: sidecar, archive: archive}
}

func skillDeps(t *testing.T, claudeContent, codexContent []byte) toolsDeps {
	t.Helper()
	return toolsDeps{
		registry: newToolsRegistry(newClaudeSkillComponent(), newCodexSkillComponent()),
		version:  fixtureVersion,
		fetcher:  skillFetcher(t, claudeContent, codexContent),
	}
}

func claudeSkillDest(repo string) string {
	return filepath.Join(repo, ".claude", "skills", "git-kura", "SKILL.md")
}

func codexSkillDest(repo string) string {
	return filepath.Join(repo, ".agents", "skills", "git-kura", "SKILL.md")
}

// --- pre-commit test helpers -----------------------------------------------

// preCommitFetcher builds a fake release fetcher serving a minimal valid tools
// asset. The pre-commit component generates its wrapper itself and reads no
// archive files, so an empty component manifest is enough to pass the framework
// asset-resolution gate.
func preCommitFetcher(t *testing.T) *fakeFetcher {
	t.Helper()
	manifest := archiveManifest{SchemaVersion: 1, Components: map[string]archiveManifestComponent{}}
	archive := makeToolsArchive(t, manifest, nil)
	archiveName := "git-kura-tools_" + fixtureVersion + ".tar.gz"
	sidecar := makeSidecar(t, fixtureVersion, archiveName, checksumAlgorithmSHA256, archive)
	return &fakeFetcher{sidecar: sidecar, archive: archive}
}

func preCommitDeps(t *testing.T) toolsDeps {
	t.Helper()
	return toolsDeps{registry: newToolsRegistry(preCommitComponent{}), version: fixtureVersion, fetcher: preCommitFetcher(t)}
}

func gitConfigLocal(t *testing.T, repo, key string) (string, bool) {
	t.Helper()
	v, ok, err := gitutil.ConfigGetLocal(repo, key)
	if err != nil {
		t.Fatalf("read local config %s: %v", key, err)
	}
	return strings.TrimRight(v, "\n"), ok
}

func readPreCommitMeta(t *testing.T, repo string) (preCommitMeta, bool) {
	t.Helper()
	store, err := readToolsMetadata(installedJSONPath(repo))
	if err != nil {
		t.Fatalf("read tools metadata: %v", err)
	}
	entry, ok := store.Components[preCommitComponentID]
	if !ok {
		return preCommitMeta{}, false
	}
	return preCommitMetaFromEntry(&entry)
}

func managedHooksDir(repo string) string {
	return filepath.Join(repo, ".kura", "tools", "hooks", "_")
}

// stage writes content to a file relative to dir and stages it with git add.
func stage(t *testing.T, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", rel)
}
