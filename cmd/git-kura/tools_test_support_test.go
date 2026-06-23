package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tooppoo/git-kura/internal/gitutil"
	"github.com/tooppoo/git-kura/internal/tools"
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

// --- registry helpers ------------------------------------------------------

func mustToolsRegistry(t *testing.T, comps ...tools.Component) *tools.Registry {
	t.Helper()
	reg, err := tools.NewRegistry(comps...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return reg
}

func mustProductionRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	reg, err := tools.ProductionRegistry()
	if err != nil {
		t.Fatalf("ProductionRegistry: %v", err)
	}
	return reg
}

// --- in-memory release asset construction ----------------------------------

func makeToolsArchive(t *testing.T, manifest tools.ArchiveManifest, files map[string][]byte) []byte {
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
	writeArchiveEntry(tools.ArchiveManifestName, mj)
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
	s := tools.SidecarManifest{
		ArchiveName:       archiveName,
		ArchiveChecksum:   tools.SHA256Hex(archive),
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

func (f *fakeFetcher) FetchSidecar(tag, version string) ([]byte, error) {
	f.sidecarCalls++
	if f.sidecarErr != nil {
		return nil, f.sidecarErr
	}
	return f.sidecar, nil
}

func (f *fakeFetcher) FetchArchive(tag, name string) ([]byte, error) {
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

func (c *fileFixture) ID() string { return c.componentID }

func (c *fileFixture) destAbs(repoRoot string) string {
	return filepath.Join(repoRoot, filepath.FromSlash(c.destRel))
}

func (c *fileFixture) Install(ctx tools.InstallContext) tools.Outcome {
	data, err := os.ReadFile(ctx.Asset.Path(c.archiveRel))
	if err != nil {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Action: tools.ActionFailed, Reason: "read asset: " + err.Error()}}
	}
	sum := tools.SHA256Hex(data)
	action := tools.ActionCreated
	if ctx.Entry != nil {
		if ctx.Entry.Checksum == sum {
			return tools.Outcome{Result: tools.Result{
				Component: c.componentID, ReleaseVersion: ctx.ReleaseVersion,
				SourceAsset: c.archiveRel, Destination: c.destRel,
				Action: tools.ActionSkipped, Managed: true, Reason: "already installed; checksum matches",
			}}
		}
		action = tools.ActionUpdated
	}
	dest := c.destAbs(ctx.RepoRoot)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Action: tools.ActionFailed, Reason: err.Error()}}
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Action: tools.ActionFailed, Reason: err.Error()}}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	created := now
	if ctx.Entry != nil {
		created = ctx.Entry.CreatedAt
	}
	entry := &tools.MetadataEntry{
		Component: c.componentID, SourceAssetID: c.archiveRel,
		ReleaseVersion: ctx.ReleaseVersion, DestinationPath: c.destRel,
		InstalledVersion: ctx.ReleaseVersion, Checksum: sum,
		ManagedMode: tools.ManagedModeFile, CreatedAt: created, UpdatedAt: now,
	}
	return tools.Outcome{
		Result: tools.Result{
			Component: c.componentID, ReleaseVersion: ctx.ReleaseVersion,
			SourceAsset: c.archiveRel, Destination: c.destRel,
			Action: action, Managed: true, Reason: "",
		},
		SetEntry: entry,
	}
}

func (c *fileFixture) Status(ctx tools.Context) tools.Outcome {
	if ctx.Entry == nil {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Action: tools.ActionNotInstalled, Reason: "no metadata"}}
	}
	dest := c.destAbs(ctx.RepoRoot)
	data, err := os.ReadFile(dest)
	if err != nil {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Destination: c.destRel, Action: tools.ActionNotInstalled, Reason: "destination missing"}}
	}
	res := tools.Result{
		Component: c.componentID, ReleaseVersion: ctx.Entry.ReleaseVersion,
		Destination: c.destRel, Managed: true,
	}
	if tools.SHA256Hex(data) == ctx.Entry.Checksum {
		res.Action = tools.ActionInstalled
	} else {
		res.Action = tools.ActionSkipped
		res.Reason = "destination modified outside git-kura"
	}
	return tools.Outcome{Result: res}
}

func (c *fileFixture) Uninstall(ctx tools.Context) tools.Outcome {
	if ctx.Entry == nil {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Action: tools.ActionNotInstalled, Reason: "no metadata"}}
	}
	dest := c.destAbs(ctx.RepoRoot)
	data, err := os.ReadFile(dest)
	if err != nil {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Destination: c.destRel, Action: tools.ActionNotInstalled, Reason: "destination missing"}, DeleteEntry: true}
	}
	if tools.SHA256Hex(data) != ctx.Entry.Checksum {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Destination: c.destRel, Action: tools.ActionSkipped, Managed: true, Reason: "destination modified outside git-kura; leaving it untouched"}}
	}
	if err := os.Remove(dest); err != nil {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Action: tools.ActionFailed, Reason: err.Error()}}
	}
	return tools.Outcome{
		Result:      tools.Result{Component: c.componentID, Destination: c.destRel, Action: tools.ActionRemoved, Managed: true},
		DeleteEntry: true,
	}
}

// failingFixture always fails, to exercise multi-component continuation.
type failingFixture struct{ componentID string }

func (c *failingFixture) ID() string { return c.componentID }
func (c *failingFixture) Status(_ tools.Context) tools.Outcome {
	return tools.Outcome{Result: tools.Result{Component: c.componentID, Action: tools.ActionFailed, Reason: "boom"}}
}
func (c *failingFixture) Install(_ tools.InstallContext) tools.Outcome {
	return tools.Outcome{Result: tools.Result{Component: c.componentID, Action: tools.ActionFailed, Reason: "boom"}}
}
func (c *failingFixture) Uninstall(_ tools.Context) tools.Outcome {
	return tools.Outcome{Result: tools.Result{Component: c.componentID, Action: tools.ActionFailed, Reason: "boom"}}
}

// configFixture is a config-managed component used to exercise the config
// managed-mode uninstall semantics: it only reverts a value it still owns.
type configFixture struct {
	componentID string
	configKey   string
	value       string
}

func (c *configFixture) ID() string { return c.componentID }

func (c *configFixture) currentValue(repoRoot string) (string, bool) {
	cmd := exec.Command("git", "config", "--get", c.configKey)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(out), "\n"), true
}

func (c *configFixture) Install(ctx tools.InstallContext) tools.Outcome {
	cmd := exec.Command("git", "config", c.configKey, c.value)
	cmd.Dir = ctx.RepoRoot
	if err := cmd.Run(); err != nil {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Action: tools.ActionFailed, Reason: err.Error()}}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	created := now
	action := tools.ActionCreated
	if ctx.Entry != nil {
		created = ctx.Entry.CreatedAt
		action = tools.ActionUpdated
	}
	entry := &tools.MetadataEntry{
		Component: c.componentID, ReleaseVersion: ctx.ReleaseVersion,
		DestinationPath: c.configKey, Checksum: c.value,
		ManagedMode: tools.ManagedModeConfig, CreatedAt: created, UpdatedAt: now,
	}
	return tools.Outcome{
		Result:   tools.Result{Component: c.componentID, ReleaseVersion: ctx.ReleaseVersion, Destination: c.configKey, Action: action, Managed: true},
		SetEntry: entry,
	}
}

func (c *configFixture) Status(ctx tools.Context) tools.Outcome {
	if ctx.Entry == nil {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Action: tools.ActionNotInstalled, Reason: "no metadata"}}
	}
	cur, ok := c.currentValue(ctx.RepoRoot)
	if !ok {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Destination: c.configKey, Action: tools.ActionNotInstalled, Reason: "config unset"}}
	}
	if cur == ctx.Entry.Checksum {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Destination: c.configKey, Action: tools.ActionInstalled, Managed: true}}
	}
	return tools.Outcome{Result: tools.Result{Component: c.componentID, Destination: c.configKey, Action: tools.ActionSkipped, Managed: true, Reason: "config value changed outside git-kura"}}
}

func (c *configFixture) Uninstall(ctx tools.Context) tools.Outcome {
	if ctx.Entry == nil {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Action: tools.ActionNotInstalled, Reason: "no metadata"}}
	}
	cur, ok := c.currentValue(ctx.RepoRoot)
	if !ok {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Destination: c.configKey, Action: tools.ActionNotInstalled, Reason: "config already unset"}, DeleteEntry: true}
	}
	if cur != ctx.Entry.Checksum {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Destination: c.configKey, Action: tools.ActionSkipped, Managed: true, Reason: "config value changed outside git-kura; leaving it untouched"}}
	}
	cmd := exec.Command("git", "config", "--unset", c.configKey)
	cmd.Dir = ctx.RepoRoot
	if err := cmd.Run(); err != nil {
		return tools.Outcome{Result: tools.Result{Component: c.componentID, Action: tools.ActionFailed, Reason: err.Error()}}
	}
	return tools.Outcome{Result: tools.Result{Component: c.componentID, Destination: c.configKey, Action: tools.ActionRemoved, Managed: true}, DeleteEntry: true}
}

// componentFunc adapts function fields to the tools.Component interface for
// lightweight one-off fixtures.
type componentFunc struct {
	idVal       string
	statusFn    func(tools.Context) tools.Outcome
	installFn   func(tools.InstallContext) tools.Outcome
	uninstallFn func(tools.Context) tools.Outcome
}

func (c componentFunc) ID() string { return c.idVal }
func (c componentFunc) Status(ctx tools.Context) tools.Outcome {
	if c.statusFn != nil {
		return c.statusFn(ctx)
	}
	return tools.Outcome{Result: tools.Result{Component: c.idVal, Action: tools.ActionNotInstalled}}
}
func (c componentFunc) Install(ctx tools.InstallContext) tools.Outcome {
	if c.installFn != nil {
		return c.installFn(ctx)
	}
	return tools.Outcome{Result: tools.Result{Component: c.idVal, Action: tools.ActionFailed}}
}
func (c componentFunc) Uninstall(ctx tools.Context) tools.Outcome {
	if c.uninstallFn != nil {
		return c.uninstallFn(ctx)
	}
	return tools.Outcome{Result: tools.Result{Component: c.idVal, Action: tools.ActionNotInstalled}}
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
	manifest := tools.ArchiveManifest{
		SchemaVersion: 1,
		Components: map[string]tools.ArchiveManifestComponent{
			"alpha": {Files: map[string]string{comp.archiveRel: tools.SHA256Hex(content)}},
		},
	}
	archive := makeToolsArchive(t, manifest, map[string][]byte{comp.archiveRel: content})
	archiveName := "git-kura-tools_" + version + ".tar.gz"
	sidecar := makeSidecar(t, version, archiveName, tools.ChecksumAlgorithmSHA256, archive)
	return &fakeFetcher{sidecar: sidecar, archive: archive}, comp
}

func installedJSONPath(repo string) string {
	return filepath.Join(repo, ".git", "kura", "tools", "installed.json")
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
	manifest := tools.ArchiveManifest{
		SchemaVersion: 1,
		Components: map[string]tools.ArchiveManifestComponent{
			tools.ClaudeSkillComponentID: {Files: map[string]string{
				tools.ClaudeSkillArchivePath: tools.SHA256Hex(claudeContent),
			}},
			tools.CodexSkillComponentID: {Files: map[string]string{
				tools.CodexSkillArchivePath: tools.SHA256Hex(codexContent),
			}},
		},
	}
	files := map[string][]byte{
		tools.ClaudeSkillArchivePath: claudeContent,
		tools.CodexSkillArchivePath:  codexContent,
	}
	archive := makeToolsArchive(t, manifest, files)
	archiveName := "git-kura-tools_" + version + ".tar.gz"
	sidecar := makeSidecar(t, version, archiveName, tools.ChecksumAlgorithmSHA256, archive)
	return &fakeFetcher{sidecar: sidecar, archive: archive}
}

func skillDeps(t *testing.T, claudeContent, codexContent []byte) toolsDeps {
	t.Helper()
	return toolsDeps{
		registry: mustToolsRegistry(t, tools.NewClaudeSkillComponent(), tools.NewCodexSkillComponent()),
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
// asset.
func preCommitFetcher(t *testing.T) *fakeFetcher {
	t.Helper()
	manifest := tools.ArchiveManifest{SchemaVersion: 1, Components: map[string]tools.ArchiveManifestComponent{}}
	archive := makeToolsArchive(t, manifest, nil)
	archiveName := "git-kura-tools_" + fixtureVersion + ".tar.gz"
	sidecar := makeSidecar(t, fixtureVersion, archiveName, tools.ChecksumAlgorithmSHA256, archive)
	return &fakeFetcher{sidecar: sidecar, archive: archive}
}

func preCommitDeps(t *testing.T) toolsDeps {
	t.Helper()
	return toolsDeps{registry: mustToolsRegistry(t, tools.PreCommitComponent{}), version: fixtureVersion, fetcher: preCommitFetcher(t)}
}

func gitConfigLocal(t *testing.T, repo, key string) (string, bool) {
	t.Helper()
	v, ok, err := gitutil.ConfigGetLocal(repo, key)
	if err != nil {
		t.Fatalf("read local config %s: %v", key, err)
	}
	return strings.TrimRight(v, "\n"), ok
}

func readPreCommitMeta(t *testing.T, repo string) (tools.PreCommitMeta, bool) {
	t.Helper()
	store, err := tools.ReadMetadata(installedJSONPath(repo))
	if err != nil {
		t.Fatalf("read tools metadata: %v", err)
	}
	entry, ok := store.Components[tools.PreCommitComponentID]
	if !ok {
		return tools.PreCommitMeta{}, false
	}
	return tools.PreCommitMetaFromEntry(&entry)
}

func managedHooksDir(repo string) string {
	return tools.PreCommitHooksDir(filepath.Join(repo, ".git"))
}

// persistToolsEntry writes a MetadataEntry to the tools metadata store.
// Used by tests that set up specific metadata state before running CLI commands.
func persistToolsEntry(t *testing.T, repo string, entry tools.MetadataEntry) {
	t.Helper()
	storeFile, _, err := tools.MetadataPaths(repo)
	if err != nil {
		t.Fatalf("metadata paths: %v", err)
	}
	store, err := tools.ReadMetadata(storeFile)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	store.Components[entry.Component] = entry
	if err := tools.WriteMetadata(storeFile, store); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
}

// writeWrapperForTest writes the managed pre-commit wrapper script to path,
// creating parent directories as needed.
func writeWrapperForTest(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(tools.PreCommitWrapperScript), 0o755); err != nil {
		t.Fatal(err)
	}
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
