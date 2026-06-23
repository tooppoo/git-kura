package tools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- test helpers -------------------------------------------------------

func toolsTestRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "init", "-b", "main")
	testGit(t, repo, "config", "user.email", "kura-test@example.com")
	testGit(t, repo, "config", "user.name", "Kura Test")
	return repo
}

func testGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func commitFile(t *testing.T, repo, name, content string) {
	t.Helper()
	writeFile(t, filepath.Join(repo, name), content)
	testGit(t, repo, "add", name)
	testGit(t, repo, "commit", "-m", "add "+name)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func withWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	fn()
}

// openManagedWorktreeForTest creates the kura directory structure and metadata
// that resolveRepresentativeRoot and worktree.ResolveKeyForWorktreeRoot expect.
// It does not need a real git worktree because resolveRepresentativeRoot only
// checks gitutil.CommonDir on the representative root, not the worktree path.
func openManagedWorktreeForTest(t *testing.T, repo, key string) string {
	t.Helper()
	commonDir := filepath.Join(repo, ".git")

	worktreePath := filepath.Join(commonDir, "kura", "worktrees", key)
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	metaDir := filepath.Join(commonDir, "kura", "meta", "worktrees")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := map[string]any{
		"repositoryRoot": repo,
		"worktreePath":   worktreePath,
		"baseBranch":     "main",
	}
	data, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(metaDir, key+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	return worktreePath
}

// --- representative root tests ------------------------------------------

func TestResolveRepresentativeRootFromMainWorktree(t *testing.T) {
	repo := toolsTestRepo(t)
	commitFile(t, repo, "init.txt", "initial\n")

	commonDir := filepath.Join(repo, ".git")

	var repRoot string
	withWorkingDir(t, repo, func() {
		var err error
		repRoot, err = resolveRepresentativeRoot(repo, commonDir)
		if err != nil {
			t.Fatalf("resolveRepresentativeRoot: %v", err)
		}
	})
	if !SamePathSafe(repRoot, repo) {
		t.Fatalf("representative root = %q, want %q", repRoot, repo)
	}
}

func TestResolveRepresentativeRootFromManagedWorktree(t *testing.T) {
	repo := toolsTestRepo(t)
	commitFile(t, repo, "init.txt", "initial\n")

	worktreePath := openManagedWorktreeForTest(t, repo, "reproot-test")
	commonDir := filepath.Join(repo, ".git")

	var repRoot string
	withWorkingDir(t, worktreePath, func() {
		var err error
		repRoot, err = resolveRepresentativeRoot(worktreePath, commonDir)
		if err != nil {
			t.Fatalf("resolveRepresentativeRoot from managed worktree: %v", err)
		}
	})
	if !SamePathSafe(repRoot, repo) {
		t.Fatalf("representative root = %q, want %q", repRoot, repo)
	}
}

func TestResolveRepresentativeRootMissingMetadata(t *testing.T) {
	repo := toolsTestRepo(t)
	commitFile(t, repo, "init.txt", "initial\n")

	commonDir := filepath.Join(repo, ".git")
	fakeWorktreePath := filepath.Join(commonDir, "kura", "worktrees", "orphan")
	if err := os.MkdirAll(fakeWorktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := resolveRepresentativeRoot(fakeWorktreePath, commonDir)
	if err == nil {
		t.Fatal("expected error for managed worktree with missing metadata")
	}
	if !strings.Contains(err.Error(), "missing-repository-metadata") {
		t.Fatalf("error = %q, want it to contain missing-repository-metadata", err.Error())
	}
}

func TestResolveRepresentativeRootMissingDir(t *testing.T) {
	tmp := t.TempDir()
	missingRoot := filepath.Join(tmp, "nonexistent")
	commonDir := filepath.Join(tmp, ".git")

	_, err := resolveRepresentativeRoot(missingRoot, commonDir)
	if err == nil {
		t.Fatal("expected error for non-existent representative root")
	}
	if !strings.Contains(err.Error(), "representative-root-missing") {
		t.Fatalf("error = %q, want it to contain representative-root-missing", err.Error())
	}
}

func TestResolveRepresentativeRootNotDirectory(t *testing.T) {
	repo := toolsTestRepo(t)
	fileAsRoot := filepath.Join(repo, "notadir")
	writeFile(t, fileAsRoot, "x")

	commonDir := filepath.Join(repo, ".git")
	_, err := resolveRepresentativeRoot(fileAsRoot, commonDir)
	if err == nil {
		t.Fatal("expected error when representative root is a file")
	}
	if !strings.Contains(err.Error(), "representative-root-not-directory") {
		t.Fatalf("error = %q, want it to contain representative-root-not-directory", err.Error())
	}
}

func TestResolveRepresentativeRootCommonDirMismatch(t *testing.T) {
	repo1 := toolsTestRepo(t)
	repo2 := toolsTestRepo(t)

	commonDir2 := filepath.Join(repo2, ".git")

	_, err := resolveRepresentativeRoot(repo1, commonDir2)
	if err == nil {
		t.Fatal("expected error for common dir mismatch")
	}
	if !strings.Contains(err.Error(), "representative-root-common-dir-mismatch") {
		t.Fatalf("error = %q, want it to contain representative-root-common-dir-mismatch", err.Error())
	}
}

func TestResolveRepresentativeRootMalformedMetadata(t *testing.T) {
	repo := toolsTestRepo(t)
	commitFile(t, repo, "init.txt", "initial\n")

	commonDir := filepath.Join(repo, ".git")
	key := "badmeta"
	worktreePath := filepath.Join(commonDir, "kura", "worktrees", key)
	metaPath := filepath.Join(commonDir, "kura", "meta", "worktrees", key+".json")

	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// Valid JSON but missing repositoryRoot field.
	if err := os.WriteFile(metaPath, []byte(`{"key":"badmeta"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := resolveRepresentativeRoot(worktreePath, commonDir)
	if err == nil {
		t.Fatal("expected error for metadata without repositoryRoot")
	}
	if !strings.Contains(err.Error(), "missing-repository-metadata") {
		t.Fatalf("error = %q, want missing-repository-metadata", err.Error())
	}
}

func TestResolveRepresentativeRootCommonDirError(t *testing.T) {
	tmp := t.TempDir()
	commonDir := filepath.Join(tmp, ".git")

	_, err := resolveRepresentativeRoot(tmp, commonDir)
	if err == nil {
		t.Fatal("expected error when CommonDir resolution fails")
	}
	if !strings.Contains(err.Error(), "representative-root-common-dir-mismatch") {
		t.Fatalf("error = %q, want representative-root-common-dir-mismatch", err.Error())
	}
}

func testSkillComponent(repo string) SkillComponent {
	return SkillComponent{
		componentID: "alpha",
		archivePath: "alpha/SKILL.md",
		destPath: func(representativeRoot string) string {
			return filepath.Join(representativeRoot, ".agents", "skills", "alpha", "SKILL.md")
		},
	}
}

func testSkillAsset(t *testing.T, repo, version, content string) *Asset {
	t.Helper()
	fetcher := serviceTestFetcher(t, version, map[string][]byte{"alpha/SKILL.md": []byte(content)})
	resolver := &assetResolver{version: version, commonDir: filepath.Join(repo, ".git"), fetcher: fetcher}
	asset, err := resolver.resolve()
	if err != nil {
		t.Fatalf("resolve asset: %v", err)
	}
	return asset
}

func TestSkillComponentLifecycle(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	comp := testSkillComponent(repo)
	asset := testSkillAsset(t, repo, "1.2.3", "# Alpha\n")

	missing := comp.Status(Context{RepoRoot: repo, CommonDir: commonDir})
	if missing.Result.Action != ActionNotInstalled || missing.Result.Reason != "not-installed" {
		t.Fatalf("missing status = %#v", missing.Result)
	}

	install := comp.Install(InstallContext{
		Context:        Context{RepoRoot: repo, CommonDir: commonDir},
		ReleaseVersion: "1.2.3",
		Asset:          asset,
	})
	if install.Result.Action != ActionCreated || install.SetEntry == nil {
		t.Fatalf("install = %#v entry=%#v", install.Result, install.SetEntry)
	}
	if got := string(mustReadFile(t, install.Result.Destination)); got != "# Alpha\n" {
		t.Fatalf("installed content = %q", got)
	}

	status := comp.Status(Context{RepoRoot: repo, CommonDir: commonDir, Entry: install.SetEntry})
	if status.Result.Action != ActionInstalled || !status.Result.Managed {
		t.Fatalf("installed status = %#v", status.Result)
	}

	skipped := comp.Install(InstallContext{
		Context:        Context{RepoRoot: repo, CommonDir: commonDir, Entry: install.SetEntry},
		ReleaseVersion: "1.2.3",
		Asset:          asset,
	})
	if skipped.Result.Action != ActionSkipped || !strings.Contains(skipped.Result.Reason, "already installed") {
		t.Fatalf("reinstall = %#v", skipped.Result)
	}

	uninstall := comp.Uninstall(Context{RepoRoot: repo, CommonDir: commonDir, Entry: install.SetEntry})
	if uninstall.Result.Action != ActionRemoved || !uninstall.DeleteEntry {
		t.Fatalf("uninstall = %#v delete=%v", uninstall.Result, uninstall.DeleteEntry)
	}
	if _, err := os.Stat(install.Result.Destination); !os.IsNotExist(err) {
		t.Fatalf("destination should be removed: %v", err)
	}
}

func TestSkillComponentStatusVariants(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	comp := testSkillComponent(repo)
	dest := comp.destPath(repo)

	writeFile(t, dest, "user content\n")
	unmanaged := comp.Status(Context{RepoRoot: repo, CommonDir: commonDir})
	if unmanaged.Result.Action != ActionNotInstalled || unmanaged.Result.Managed || unmanaged.Result.Reason != "unmanaged-file-exists" {
		t.Fatalf("unmanaged status = %#v", unmanaged.Result)
	}

	entry := &MetadataEntry{
		Component:         comp.ID(),
		ReleaseVersion:    "1.2.3",
		InstalledVersion:  "1.2.3",
		DestinationPath:   dest,
		Checksum:          SHA256Hex([]byte("expected\n")),
		ManagedMode:       ManagedModeFile,
		SourceAssetID:     "alpha/SKILL.md",
		CreatedAt:         "2026-06-23T00:00:00Z",
		UpdatedAt:         "2026-06-23T00:00:00Z",
		ComponentMetadata: map[string]any{},
	}
	modified := comp.Status(Context{RepoRoot: repo, CommonDir: commonDir, Entry: entry})
	if modified.Result.Action != ActionInstalled || modified.Result.Managed || !strings.Contains(modified.Result.Reason, "modified outside git-kura") {
		t.Fatalf("modified status = %#v", modified.Result)
	}

	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	missing := comp.Status(Context{RepoRoot: repo, CommonDir: commonDir, Entry: entry})
	if missing.Result.Action != ActionNotInstalled || !missing.Result.Managed || !strings.Contains(missing.Result.Reason, "missing") {
		t.Fatalf("managed missing status = %#v", missing.Result)
	}
}

func TestSkillComponentInstallFailures(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	comp := testSkillComponent(repo)
	asset := testSkillAsset(t, repo, "1.2.3", "# Alpha\n")

	otherComp := SkillComponent{
		componentID: "missing",
		archivePath: "missing/SKILL.md",
		destPath: func(root string) string {
			return filepath.Join(root, "missing.md")
		},
	}
	missingComponent := otherComp.Install(InstallContext{
		Context:        Context{RepoRoot: repo, CommonDir: commonDir},
		ReleaseVersion: "1.2.3",
		Asset:          asset,
	})
	if missingComponent.Result.Action != ActionFailed || !strings.Contains(missingComponent.Result.Reason, "not found") {
		t.Fatalf("missing component install = %#v", missingComponent.Result)
	}

	badPathComp := comp
	badPathComp.archivePath = "alpha/MISSING.md"
	missingPath := badPathComp.Install(InstallContext{
		Context:        Context{RepoRoot: repo, CommonDir: commonDir},
		ReleaseVersion: "1.2.3",
		Asset:          asset,
	})
	if missingPath.Result.Action != ActionFailed || !strings.Contains(missingPath.Result.Reason, "not in component manifest") {
		t.Fatalf("missing path install = %#v", missingPath.Result)
	}

	dest := comp.destPath(repo)
	writeFile(t, dest, "user content\n")
	unmanaged := comp.Install(InstallContext{
		Context:        Context{RepoRoot: repo, CommonDir: commonDir},
		ReleaseVersion: "1.2.3",
		Asset:          asset,
	})
	if unmanaged.Result.Action != ActionFailed || !strings.Contains(unmanaged.Result.Reason, "unmanaged-file-exists") {
		t.Fatalf("unmanaged install = %#v", unmanaged.Result)
	}

	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(asset.Path("alpha/SKILL.md"), []byte("corrupt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checksumMismatch := comp.Install(InstallContext{
		Context:        Context{RepoRoot: repo, CommonDir: commonDir},
		ReleaseVersion: "1.2.3",
		Asset:          asset,
	})
	if checksumMismatch.Result.Action != ActionFailed || !strings.Contains(checksumMismatch.Result.Reason, "checksum mismatch") {
		t.Fatalf("checksum mismatch install = %#v", checksumMismatch.Result)
	}

	asset = testSkillAsset(t, repo, "1.2.4", "# Alpha\n")
	writeFile(t, dest, "user content\n")
	entry := &MetadataEntry{
		Component:        comp.ID(),
		ReleaseVersion:   "1.0.0",
		InstalledVersion: "1.0.0",
		DestinationPath:  dest,
		Checksum:         SHA256Hex([]byte("old managed\n")),
		ManagedMode:      ManagedModeFile,
		SourceAssetID:    "alpha/SKILL.md",
		CreatedAt:        "2026-06-23T00:00:00Z",
		UpdatedAt:        "2026-06-23T00:00:00Z",
	}
	modified := comp.Install(InstallContext{
		Context:        Context{RepoRoot: repo, CommonDir: commonDir, Entry: entry},
		ReleaseVersion: "1.2.3",
		Asset:          asset,
	})
	if modified.Result.Action != ActionFailed || !strings.Contains(modified.Result.Reason, "modified outside git-kura") {
		t.Fatalf("modified install = %#v", modified.Result)
	}
}

func TestSkillComponentUpdateAndUninstallVariants(t *testing.T) {
	repo := toolsTestRepo(t)
	commonDir := filepath.Join(repo, ".git")
	comp := testSkillComponent(repo)
	dest := comp.destPath(repo)

	writeFile(t, dest, "old managed\n")
	entry := &MetadataEntry{
		Component:        comp.ID(),
		ReleaseVersion:   "1.0.0",
		InstalledVersion: "1.0.0",
		DestinationPath:  dest,
		Checksum:         SHA256Hex([]byte("old managed\n")),
		ManagedMode:      ManagedModeFile,
		SourceAssetID:    "alpha/SKILL.md",
		CreatedAt:        "2026-06-22T00:00:00Z",
		UpdatedAt:        "2026-06-22T00:00:00Z",
	}
	asset := testSkillAsset(t, repo, "1.2.3", "new managed\n")
	updated := comp.Install(InstallContext{
		Context:        Context{RepoRoot: repo, CommonDir: commonDir, Entry: entry},
		ReleaseVersion: "1.2.3",
		Asset:          asset,
	})
	if updated.Result.Action != ActionUpdated || updated.SetEntry == nil || updated.SetEntry.CreatedAt != entry.CreatedAt {
		t.Fatalf("updated install = %#v entry=%#v", updated.Result, updated.SetEntry)
	}

	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	alreadyAbsent := comp.Uninstall(Context{RepoRoot: repo, CommonDir: commonDir, Entry: updated.SetEntry})
	if alreadyAbsent.Result.Action != ActionRemoved || !strings.Contains(alreadyAbsent.Result.Reason, "already absent") {
		t.Fatalf("uninstall absent = %#v", alreadyAbsent.Result)
	}

	notInstalled := comp.Uninstall(Context{RepoRoot: repo, CommonDir: commonDir})
	if notInstalled.Result.Action != ActionNotInstalled {
		t.Fatalf("uninstall without metadata = %#v", notInstalled.Result)
	}

	writeFile(t, dest, "user modified\n")
	skipped := comp.Uninstall(Context{RepoRoot: repo, CommonDir: commonDir, Entry: updated.SetEntry})
	if skipped.Result.Action != ActionSkipped || skipped.Result.Managed || !strings.Contains(skipped.Result.Reason, "modified outside git-kura") {
		t.Fatalf("uninstall modified = %#v", skipped.Result)
	}

	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	readFailure := comp.Uninstall(Context{RepoRoot: repo, CommonDir: commonDir, Entry: updated.SetEntry})
	if readFailure.Result.Action != ActionFailed || !strings.Contains(readFailure.Result.Reason, "read destination") {
		t.Fatalf("uninstall read failure = %#v", readFailure.Result)
	}
}

func TestBuiltInSkillConstructors(t *testing.T) {
	repo := toolsTestRepo(t)
	claude := NewClaudeSkillComponent()
	if claude.ID() != ClaudeSkillComponentID || !strings.HasSuffix(claude.destPath(repo), filepath.Join(".claude", "skills", "git-kura", "SKILL.md")) {
		t.Fatalf("claude component = %#v dest=%q", claude, claude.destPath(repo))
	}
	codex := NewCodexSkillComponent()
	if codex.ID() != CodexSkillComponentID || !strings.HasSuffix(codex.destPath(repo), filepath.Join(".agents", "skills", "git-kura", "SKILL.md")) {
		t.Fatalf("codex component = %#v dest=%q", codex, codex.destPath(repo))
	}
}

func TestSkillComponentRepresentativeRootErrors(t *testing.T) {
	dir := t.TempDir()
	comp := testSkillComponent(dir)
	asset := &Asset{root: dir, version: "1.2.3", manifest: ArchiveManifest{Components: map[string]ArchiveManifestComponent{}}}

	status := comp.Status(Context{RepoRoot: filepath.Join(dir, "missing"), CommonDir: filepath.Join(dir, ".git")})
	if status.Result.Action != ActionFailed || !strings.Contains(status.Result.Reason, "representative-root-missing") {
		t.Fatalf("status = %#v", status.Result)
	}

	install := comp.Install(InstallContext{
		Context:        Context{RepoRoot: filepath.Join(dir, "missing"), CommonDir: filepath.Join(dir, ".git")},
		ReleaseVersion: "1.2.3",
		Asset:          asset,
	})
	if install.Result.Action != ActionFailed || !strings.Contains(install.Result.Reason, "representative-root-missing") {
		t.Fatalf("install = %#v", install.Result)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
