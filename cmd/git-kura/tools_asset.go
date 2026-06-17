package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// checksumAlgorithmSHA256 is the only checksum algorithm supported in v0. A
// sidecar manifest naming any other algorithm makes install fail rather than
// silently trust an unverifiable archive.
const checksumAlgorithmSHA256 = "sha256"

// toolsArchiveManifestName is the manifest embedded inside the tools archive.
const toolsArchiveManifestName = "manifest.json"

// releaseRepoSlug is the GitHub owner/repo that hosts tools release assets.
const releaseRepoSlug = "tooppoo/git-kura"

// officialReleaseRe matches a clean release version such as "1.2.3". GoReleaser
// injects main.version as the tag without the leading "v" (see .goreleaser.yaml
// and main.version cookbook), so a release binary reports e.g. "1.2.3". Anything
// else — the "dev" default of a source build, a "1.2.3-snapshot" snapshot build,
// or a go-install pseudo build that never had the version injected — is not an
// official release.
var officialReleaseRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// officialReleaseTag reports whether version identifies an official release and,
// if so, returns the matching git tag (the version prefixed with "v"). Only
// official release binaries may download release assets; go install and source
// builds are intentionally unsupported.
func officialReleaseTag(version string) (tag string, ok bool) {
	if !officialReleaseRe.MatchString(version) {
		return "", false
	}
	return "v" + version, true
}

// sidecarManifest is the git-kura-tools_<version>.json document attached
// alongside the tools archive. It is used to verify the archive before
// extraction.
type sidecarManifest struct {
	ArchiveName       string `json:"archiveName"`
	ArchiveChecksum   string `json:"archiveChecksum"`
	ChecksumAlgorithm string `json:"checksumAlgorithm"`
	Version           string `json:"version"`
}

// archiveManifest is manifest.json inside the tools archive. It maps each
// component to the assets it owns and the expected per-file checksum.
type archiveManifest struct {
	SchemaVersion int                                 `json:"schemaVersion"`
	Components    map[string]archiveManifestComponent `json:"components"`
}

type archiveManifestComponent struct {
	// Files maps a path inside the archive (forward-slash, relative) to its
	// sha256 checksum.
	Files map[string]string `json:"files"`
}

// toolsAsset is a verified, extracted tools asset. Component implementations
// receive it during install and reference files under root, consulting the
// archive manifest for the component-to-asset mapping and per-file checksums.
type toolsAsset struct {
	root     string
	version  string
	manifest archiveManifest
}

// componentAssets returns the archive manifest entry for a component.
func (a *toolsAsset) componentAssets(id string) (archiveManifestComponent, bool) {
	c, ok := a.manifest.Components[id]
	return c, ok
}

// path resolves a forward-slash, archive-relative path against the asset root.
func (a *toolsAsset) path(rel string) string {
	return filepath.Join(a.root, filepath.FromSlash(rel))
}

// releaseFetcher abstracts the network so tests can inject in-memory release
// assets. fetchSidecar and fetchArchive download by release tag; implementations
// must not consult "latest".
type releaseFetcher interface {
	fetchSidecar(tag, version string) ([]byte, error)
	fetchArchive(tag, archiveName string) ([]byte, error)
}

// githubReleaseFetcher downloads tools assets from the project's GitHub
// releases, keyed by the exact release tag (never "latest").
type githubReleaseFetcher struct {
	client *http.Client
}

func newGithubReleaseFetcher() githubReleaseFetcher {
	return githubReleaseFetcher{client: &http.Client{Timeout: 60 * time.Second}}
}

func (g githubReleaseFetcher) downloadURL(tag, name string) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", releaseRepoSlug, tag, name)
}

func (g githubReleaseFetcher) get(url string) ([]byte, error) {
	client := g.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: unexpected status %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	return data, nil
}

func (g githubReleaseFetcher) fetchSidecar(tag, version string) ([]byte, error) {
	return g.get(g.downloadURL(tag, fmt.Sprintf("git-kura-tools_%s.json", version)))
}

func (g githubReleaseFetcher) fetchArchive(tag, archiveName string) ([]byte, error) {
	return g.get(g.downloadURL(tag, archiveName))
}

// toolsAssetResolver resolves a verified tools asset for the running binary's
// release version, caching the extracted asset under the git common dir.
type toolsAssetResolver struct {
	version   string
	commonDir string
	fetcher   releaseFetcher
}

// toolsCacheMetadata records what an extracted cache directory contains, so a
// later resolve can confirm the cache still matches the current sidecar manifest
// before reusing it.
type toolsCacheMetadata struct {
	ReleaseVersion    string `json:"releaseVersion"`
	ArchiveName       string `json:"archiveName"`
	ArchiveChecksum   string `json:"archiveChecksum"`
	ChecksumAlgorithm string `json:"checksumAlgorithm"`
}

func (r *toolsAssetResolver) cacheDir() string {
	return filepath.Join(r.commonDir, "kura", "tools", "cache", r.version)
}

func cacheMetadataPath(cacheDir string) string { return filepath.Join(cacheDir, "cache.json") }
func cacheAssetRoot(cacheDir string) string    { return filepath.Join(cacheDir, "asset") }

// resolve returns a verified tools asset. It fails when the binary is not an
// official release, when the sidecar manifest names an unsupported checksum
// algorithm, or when the downloaded archive's checksum does not match the
// sidecar manifest. A matching cache is reused without any download.
func (r *toolsAssetResolver) resolve() (*toolsAsset, error) {
	tag, ok := officialReleaseTag(r.version)
	if !ok {
		return nil, fmt.Errorf("git kura tools install requires an official release binary, but the current version %q is not a release version; go install and source builds are not supported", r.version)
	}

	sidecarData, err := r.fetcher.fetchSidecar(tag, r.version)
	if err != nil {
		return nil, fmt.Errorf("fetch tools sidecar manifest: %w", err)
	}
	var sidecar sidecarManifest
	if err := json.Unmarshal(sidecarData, &sidecar); err != nil {
		return nil, fmt.Errorf("parse tools sidecar manifest: %w", err)
	}
	// The tools asset is bound to the binary's exact release: the sidecar
	// manifest is fetched for this version's tag, so its own corresponding
	// version must agree. A mismatch means the wrong asset was published under
	// the tag; fail before any cache lookup or extraction.
	if sidecar.Version != r.version {
		return nil, fmt.Errorf("tools sidecar manifest version %q does not match the binary release version %q", sidecar.Version, r.version)
	}
	if sidecar.ChecksumAlgorithm != checksumAlgorithmSHA256 {
		return nil, fmt.Errorf("unsupported checksum algorithm %q in tools sidecar manifest: only %q is supported", sidecar.ChecksumAlgorithm, checksumAlgorithmSHA256)
	}
	if sidecar.ArchiveName == "" || sidecar.ArchiveChecksum == "" {
		return nil, fmt.Errorf("tools sidecar manifest is missing archive name or checksum")
	}

	cacheDir := r.cacheDir()
	if asset, ok, err := r.tryCache(cacheDir, sidecar); err != nil {
		return nil, err
	} else if ok {
		return asset, nil
	}

	archiveData, err := r.fetcher.fetchArchive(tag, sidecar.ArchiveName)
	if err != nil {
		return nil, fmt.Errorf("fetch tools archive: %w", err)
	}
	sum := sha256.Sum256(archiveData)
	got := hex.EncodeToString(sum[:])
	if got != sidecar.ArchiveChecksum {
		// A mismatch means the release assets are inconsistent (the archive does
		// not match its own sidecar). Abort rather than extract an unverified
		// archive.
		return nil, fmt.Errorf("tools archive checksum mismatch: sidecar manifest expects %s but archive is %s", sidecar.ArchiveChecksum, got)
	}

	return r.extractToCache(cacheDir, sidecar, archiveData)
}

// tryCache returns the cached asset when cache metadata exists and matches every
// identifying field of the sidecar manifest.
//
// Releases are immutable, so a cache recorded for this version must never
// disagree with the sidecar manifest on the archive checksum. When it does, the
// release asset has changed underneath us: tryCache returns an error so the
// install fails rather than using the stale cache or silently accepting the new
// bytes. An absent or corrupt cache metadata file is not such an inconsistency —
// it is treated as a miss so resolve re-downloads and re-verifies.
func (r *toolsAssetResolver) tryCache(cacheDir string, sidecar sidecarManifest) (*toolsAsset, bool, error) {
	data, err := os.ReadFile(cacheMetadataPath(cacheDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read tools cache metadata: %w", err)
	}
	var meta toolsCacheMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		// A corrupt cache metadata file is not fatal: ignore the cache and
		// re-download.
		return nil, false, nil
	}
	if meta.ArchiveChecksum != sidecar.ArchiveChecksum {
		return nil, false, fmt.Errorf("tools cache for version %s is inconsistent with the release: cached archive checksum %s does not match sidecar manifest checksum %s; the release asset may have changed", sidecar.Version, meta.ArchiveChecksum, sidecar.ArchiveChecksum)
	}
	// The archive checksum agrees; a clean hit also requires the other
	// identifying fields to match. Any other divergence is treated as a miss and
	// re-resolved.
	if meta.ReleaseVersion != sidecar.Version ||
		meta.ArchiveName != sidecar.ArchiveName ||
		meta.ChecksumAlgorithm != sidecar.ChecksumAlgorithm {
		return nil, false, nil
	}
	manifest, err := readArchiveManifest(cacheAssetRoot(cacheDir))
	if err != nil {
		return nil, false, nil
	}
	return &toolsAsset{root: cacheAssetRoot(cacheDir), version: r.version, manifest: manifest}, true, nil
}

// extractToCache extracts a verified archive into a fresh cache directory and
// records cache metadata so a later resolve can reuse it. The extraction is
// staged in a temp directory and renamed into place so a partially extracted
// asset is never observed as a cache hit.
func (r *toolsAssetResolver) extractToCache(cacheDir string, sidecar sidecarManifest, archiveData []byte) (*toolsAsset, error) {
	if err := os.MkdirAll(filepath.Dir(cacheDir), 0o755); err != nil {
		return nil, fmt.Errorf("create tools cache parent: %w", err)
	}
	// Remove any stale cache for this version before re-extracting.
	if err := os.RemoveAll(cacheDir); err != nil {
		return nil, fmt.Errorf("clear stale tools cache: %w", err)
	}
	assetRoot := cacheAssetRoot(cacheDir)
	if err := extractTarGz(archiveData, assetRoot); err != nil {
		return nil, fmt.Errorf("extract tools archive: %w", err)
	}
	manifest, err := readArchiveManifest(assetRoot)
	if err != nil {
		return nil, fmt.Errorf("read tools archive manifest: %w", err)
	}

	meta := toolsCacheMetadata{
		ReleaseVersion:    sidecar.Version,
		ArchiveName:       sidecar.ArchiveName,
		ArchiveChecksum:   sidecar.ArchiveChecksum,
		ChecksumAlgorithm: sidecar.ChecksumAlgorithm,
	}
	metaData, _ := json.Marshal(meta)
	if err := os.WriteFile(cacheMetadataPath(cacheDir), metaData, 0o644); err != nil {
		return nil, fmt.Errorf("write tools cache metadata: %w", err)
	}

	return &toolsAsset{root: assetRoot, version: r.version, manifest: manifest}, nil
}

func readArchiveManifest(assetRoot string) (archiveManifest, error) {
	data, err := os.ReadFile(filepath.Join(assetRoot, toolsArchiveManifestName))
	if err != nil {
		return archiveManifest{}, fmt.Errorf("read %s: %w", toolsArchiveManifestName, err)
	}
	var manifest archiveManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return archiveManifest{}, fmt.Errorf("parse %s: %w", toolsArchiveManifestName, err)
	}
	if manifest.Components == nil {
		manifest.Components = make(map[string]archiveManifestComponent)
	}
	return manifest, nil
}

// extractTarGz extracts a gzip-compressed tar archive into destRoot. Entries
// that would escape destRoot (absolute paths or "../" traversal) are rejected so
// a malicious archive cannot write outside the cache.
func extractTarGz(data []byte, destRoot string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return fmt.Errorf("create asset root: %w", err)
	}

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		// Tar entry names use "/" separators. A backslash is never a valid
		// separator in the archive, but filepath would treat it as one on
		// Windows, so reject it outright rather than let it become a separator
		// that escapes destRoot.
		if strings.Contains(hdr.Name, `\`) {
			return fmt.Errorf("archive entry %q contains a non-/ path separator", hdr.Name)
		}
		clean := path.Clean(hdr.Name)
		if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("archive entry %q escapes the asset root", hdr.Name)
		}
		target := filepath.Join(destRoot, filepath.FromSlash(clean))
		// Defense in depth: confirm the resolved path stays under destRoot on the
		// host filesystem regardless of platform separator handling.
		if rel, err := filepath.Rel(destRoot, target); err != nil ||
			rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("archive entry %q escapes the asset root", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create dir %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create parent of %s: %w", target, err)
			}
			if err := writeTarFile(tr, target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		default:
			// Skip symlinks, devices, and other entry types: tools assets are
			// plain files and directories, and honoring symlinks would reopen the
			// path-escape hole the cleaning above closes.
			continue
		}
	}
	return nil
}

func writeTarFile(tr *tar.Reader, target string, mode os.FileMode) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm()|0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(f, tr); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return nil
}
