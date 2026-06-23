package tools

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

// SHA256Hex returns the lowercase hex sha256 digest of data.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ChecksumAlgorithmSHA256 is the only checksum algorithm supported in v0.
const ChecksumAlgorithmSHA256 = "sha256"

// ArchiveManifestName is the manifest embedded inside the tools archive.
const ArchiveManifestName = "manifest.json"

const releaseRepoSlug = "tooppoo/git-kura"

var officialReleaseRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func officialReleaseTag(version string) (tag string, ok bool) {
	if !officialReleaseRe.MatchString(version) {
		return "", false
	}
	return "v" + version, true
}

// SidecarManifest is the git-kura-tools_<version>.json document attached
// alongside the tools archive.
type SidecarManifest struct {
	ArchiveName       string `json:"archiveName"`
	ArchiveChecksum   string `json:"archiveChecksum"`
	ChecksumAlgorithm string `json:"checksumAlgorithm"`
	Version           string `json:"version"`
}

// ArchiveManifest is manifest.json inside the tools archive.
type ArchiveManifest struct {
	SchemaVersion int                                 `json:"schemaVersion"`
	Components    map[string]ArchiveManifestComponent `json:"components"`
}

// ArchiveManifestComponent maps archive-relative paths to their sha256 checksums.
type ArchiveManifestComponent struct {
	Files map[string]string `json:"files"`
}

// Asset is a verified, extracted tools asset.
type Asset struct {
	root     string
	version  string
	manifest ArchiveManifest
}

// ComponentAssets returns the archive manifest entry for a component.
func (a *Asset) ComponentAssets(id string) (ArchiveManifestComponent, bool) {
	c, ok := a.manifest.Components[id]
	return c, ok
}

// Path resolves a forward-slash, archive-relative path against the asset root.
func (a *Asset) Path(rel string) string {
	return filepath.Join(a.root, filepath.FromSlash(rel))
}

// Version returns the release version of this asset.
func (a *Asset) Version() string { return a.version }

// Fetcher abstracts the network so tests can inject in-memory release assets.
type Fetcher interface {
	FetchSidecar(tag, version string) ([]byte, error)
	FetchArchive(tag, archiveName string) ([]byte, error)
}

// GithubReleaseFetcher downloads tools assets from the project's GitHub releases.
type GithubReleaseFetcher struct {
	Client *http.Client
}

// NewGithubReleaseFetcher creates a GithubReleaseFetcher with a 60-second timeout.
func NewGithubReleaseFetcher() GithubReleaseFetcher {
	return GithubReleaseFetcher{Client: &http.Client{Timeout: 60 * time.Second}}
}

func (g GithubReleaseFetcher) DownloadURL(tag, name string) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", releaseRepoSlug, tag, name)
}

func (g GithubReleaseFetcher) get(url string) ([]byte, error) {
	client := g.Client
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

func (g GithubReleaseFetcher) FetchSidecar(tag, version string) ([]byte, error) {
	return g.get(g.DownloadURL(tag, fmt.Sprintf("git-kura-tools_%s.json", version)))
}

func (g GithubReleaseFetcher) FetchArchive(tag, archiveName string) ([]byte, error) {
	return g.get(g.DownloadURL(tag, archiveName))
}

// assetResolver resolves a verified tools asset for the running binary's
// release version, caching the extracted asset under the git common dir.
type assetResolver struct {
	version   string
	commonDir string
	fetcher   Fetcher
}

type cacheMetadata struct {
	ReleaseVersion    string `json:"releaseVersion"`
	ArchiveName       string `json:"archiveName"`
	ArchiveChecksum   string `json:"archiveChecksum"`
	ChecksumAlgorithm string `json:"checksumAlgorithm"`
}

func (r *assetResolver) cacheDir() string {
	return filepath.Join(r.commonDir, "kura", "tools", "cache", r.version)
}

func cacheMetadataPath(cacheDir string) string { return filepath.Join(cacheDir, "cache.json") }
func cacheAssetRoot(cacheDir string) string    { return filepath.Join(cacheDir, "asset") }

func (r *assetResolver) resolve() (*Asset, error) {
	tag, ok := officialReleaseTag(r.version)
	if !ok {
		return nil, fmt.Errorf("git kura tools install requires an official release binary, but the current version %q is not a release version; go install and source builds are not supported", r.version)
	}

	sidecarData, err := r.fetcher.FetchSidecar(tag, r.version)
	if err != nil {
		return nil, fmt.Errorf("fetch tools sidecar manifest: %w", err)
	}
	var sidecar SidecarManifest
	if err := json.Unmarshal(sidecarData, &sidecar); err != nil {
		return nil, fmt.Errorf("parse tools sidecar manifest: %w", err)
	}
	if sidecar.Version != r.version {
		return nil, fmt.Errorf("tools sidecar manifest version %q does not match the binary release version %q", sidecar.Version, r.version)
	}
	if sidecar.ChecksumAlgorithm != ChecksumAlgorithmSHA256 {
		return nil, fmt.Errorf("unsupported checksum algorithm %q in tools sidecar manifest: only %q is supported", sidecar.ChecksumAlgorithm, ChecksumAlgorithmSHA256)
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

	archiveData, err := r.fetcher.FetchArchive(tag, sidecar.ArchiveName)
	if err != nil {
		return nil, fmt.Errorf("fetch tools archive: %w", err)
	}
	sum := sha256.Sum256(archiveData)
	got := hex.EncodeToString(sum[:])
	if got != sidecar.ArchiveChecksum {
		return nil, fmt.Errorf("tools archive checksum mismatch: sidecar manifest expects %s but archive is %s", sidecar.ArchiveChecksum, got)
	}

	return r.extractToCache(cacheDir, sidecar, archiveData)
}

func (r *assetResolver) tryCache(cacheDir string, sidecar SidecarManifest) (*Asset, bool, error) {
	data, err := os.ReadFile(cacheMetadataPath(cacheDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read tools cache metadata: %w", err)
	}
	var meta cacheMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, false, nil
	}
	if meta.ArchiveChecksum != sidecar.ArchiveChecksum {
		return nil, false, fmt.Errorf("tools cache for version %s is inconsistent with the release: cached archive checksum %s does not match sidecar manifest checksum %s; the release asset may have changed", sidecar.Version, meta.ArchiveChecksum, sidecar.ArchiveChecksum)
	}
	if meta.ReleaseVersion != sidecar.Version ||
		meta.ArchiveName != sidecar.ArchiveName ||
		meta.ChecksumAlgorithm != sidecar.ChecksumAlgorithm {
		return nil, false, nil
	}
	manifest, err := readArchiveManifest(cacheAssetRoot(cacheDir))
	if err != nil {
		return nil, false, nil
	}
	return &Asset{root: cacheAssetRoot(cacheDir), version: r.version, manifest: manifest}, true, nil
}

func (r *assetResolver) extractToCache(cacheDir string, sidecar SidecarManifest, archiveData []byte) (*Asset, error) {
	if err := os.MkdirAll(filepath.Dir(cacheDir), 0o755); err != nil {
		return nil, fmt.Errorf("create tools cache parent: %w", err)
	}
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

	meta := cacheMetadata{
		ReleaseVersion:    sidecar.Version,
		ArchiveName:       sidecar.ArchiveName,
		ArchiveChecksum:   sidecar.ArchiveChecksum,
		ChecksumAlgorithm: sidecar.ChecksumAlgorithm,
	}
	metaData, _ := json.Marshal(meta)
	if err := os.WriteFile(cacheMetadataPath(cacheDir), metaData, 0o644); err != nil {
		return nil, fmt.Errorf("write tools cache metadata: %w", err)
	}

	return &Asset{root: assetRoot, version: r.version, manifest: manifest}, nil
}

func readArchiveManifest(assetRoot string) (ArchiveManifest, error) {
	data, err := os.ReadFile(filepath.Join(assetRoot, ArchiveManifestName))
	if err != nil {
		return ArchiveManifest{}, fmt.Errorf("read %s: %w", ArchiveManifestName, err)
	}
	var manifest ArchiveManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ArchiveManifest{}, fmt.Errorf("parse %s: %w", ArchiveManifestName, err)
	}
	if manifest.Components == nil {
		manifest.Components = make(map[string]ArchiveManifestComponent)
	}
	return manifest, nil
}

// ExtractTarGz extracts a gzip-compressed tar archive into destRoot.
// Entries that would escape destRoot are rejected.
func ExtractTarGz(data []byte, destRoot string) error {
	return extractTarGz(data, destRoot)
}

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

		if strings.Contains(hdr.Name, `\`) {
			return fmt.Errorf("archive entry %q contains a non-/ path separator", hdr.Name)
		}
		clean := path.Clean(hdr.Name)
		if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("archive entry %q escapes the asset root", hdr.Name)
		}
		target := filepath.Join(destRoot, filepath.FromSlash(clean))
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
