// Package dashboard implements the read-only TUI overview of seal ownership
// shown by "git kura dashboard". Snapshot collection, aggregation, filtering,
// and the TUI model are separated from terminal I/O so they can be tested
// without a terminal.
package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tooppoo/git-kura/internal/seal"
	"github.com/tooppoo/git-kura/internal/worktree"
)

// Group is one managed worktree key together with the repository-relative
// paths it currently claims in the seal store.
type Group struct {
	Key string
	// Paths holds the claimed repository-relative slash paths, sorted.
	Paths []string
	// Orphaned marks a key that holds claims but has no open managed worktree.
	Orphaned bool
}

// Violation is one store integrity finding that must not be presented as a
// normal claim (invalid-stored-path, non-normalized-path,
// duplicate-canonical-path).
type Violation struct {
	Code    string
	Path    string
	Message string
}

// Snapshot is one lock-free read of the seal store and the open managed
// worktree keys, aggregated for display.
type Snapshot struct {
	// Groups is the union of open keys and claim-holding keys, sorted by key.
	Groups []Group
	// Violations lists store integrity findings, sorted by path.
	Violations []Violation
	// OpenKeys counts open managed worktrees.
	OpenKeys int
	// ClaimedPaths counts claims shown under Groups (violations excluded).
	ClaimedPaths int
}

// BuildSnapshot aggregates open keys, claims, and integrity findings into a
// Snapshot. Claims whose path has an integrity finding are excluded from
// Groups so an anomalous entry is never shown as a normal claim.
func BuildSnapshot(openKeys []string, claims []seal.LsClaim, findings []seal.DoctorFinding) Snapshot {
	violations := make([]Violation, 0, len(findings))
	violatedPaths := make(map[string]bool, len(findings))
	// contestedCanonical collects the canonical paths behind
	// duplicate-canonical-path findings. The finding is attached only to the
	// second and later raw entries of a duplicate pair, so the first entry
	// must be excluded via its canonical form or it would render as a normal,
	// uncontested claim.
	contestedCanonical := make(map[string]bool)
	for _, f := range findings {
		v := Violation{Code: f.Code, Message: f.Message}
		if f.Path != nil {
			v.Path = *f.Path
			violatedPaths[*f.Path] = true
			if f.Code == "duplicate-canonical-path" {
				if canonical, err := seal.CanonicalStoredPath(*f.Path); err == nil {
					contestedCanonical[canonical] = true
				}
			}
		}
		violations = append(violations, v)
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Path != violations[j].Path {
			return violations[i].Path < violations[j].Path
		}
		return violations[i].Code < violations[j].Code
	})

	open := make(map[string]bool, len(openKeys))
	for _, k := range openKeys {
		open[k] = true
	}

	pathsByKey := make(map[string][]string)
	claimedPaths := 0
	for _, c := range claims {
		if violatedPaths[c.Path] {
			continue
		}
		if len(contestedCanonical) > 0 {
			if canonical, err := seal.CanonicalStoredPath(c.Path); err == nil && contestedCanonical[canonical] {
				continue
			}
		}
		pathsByKey[c.Key] = append(pathsByKey[c.Key], c.Path)
		claimedPaths++
	}

	keys := make([]string, 0, len(open)+len(pathsByKey))
	for k := range open {
		keys = append(keys, k)
	}
	for k := range pathsByKey {
		if !open[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	groups := make([]Group, 0, len(keys))
	for _, k := range keys {
		paths := pathsByKey[k]
		sort.Strings(paths)
		groups = append(groups, Group{
			Key:      k,
			Paths:    paths,
			Orphaned: !open[k],
		})
	}

	return Snapshot{
		Groups:       groups,
		Violations:   violations,
		OpenKeys:     len(openKeys),
		ClaimedPaths: claimedPaths,
	}
}

// Sources holds the resolved filesystem locations one snapshot read needs.
// Resolving them once up front keeps periodic reloads free of git
// subprocess invocations.
type Sources struct {
	// MetaDir is <git-common-dir>/kura/meta/worktrees.
	MetaDir string
	// StoreFile is the seal store at <git-common-dir>/kura/seals/paths.json.
	StoreFile string
}

// ResolveSources resolves the state locations for repoRoot.
func ResolveSources(repoRoot string) (Sources, error) {
	dir, err := worktree.StateDir(repoRoot)
	if err != nil {
		return Sources{}, fmt.Errorf("resolve state dir: %w", err)
	}
	storeFile, _, err := seal.StorePaths(repoRoot)
	if err != nil {
		return Sources{}, fmt.Errorf("resolve seal store path: %w", err)
	}
	return Sources{MetaDir: filepath.Join(dir, "meta", "worktrees"), StoreFile: storeFile}, nil
}

// Collect reads the open managed worktree keys and the seal store for
// repoRoot and aggregates them into a Snapshot. It is read-only and never
// acquires the seal store writer lock (paths.lock).
func Collect(repoRoot string) (Snapshot, error) {
	src, err := ResolveSources(repoRoot)
	if err != nil {
		return Snapshot{}, err
	}
	return CollectFrom(src)
}

// CollectFrom is Collect for already-resolved state locations.
func CollectFrom(src Sources) (Snapshot, error) {
	openKeys, err := openWorktreeKeys(src.MetaDir)
	if err != nil {
		return Snapshot{}, err
	}

	store, err := seal.ReadStore(src.StoreFile)
	if err != nil {
		return Snapshot{}, err
	}

	inspection := seal.InspectPathStore(store)

	claims := make([]seal.LsClaim, 0, len(store.Paths))
	for p, entry := range store.Paths {
		claims = append(claims, seal.LsClaim{Key: entry.Key, Path: p})
	}

	return BuildSnapshot(openKeys, claims, inspection.Findings), nil
}

// openWorktreeKeys enumerates open managed worktree keys the same way
// "git kura ls" does: one key per metadata file under metaDir.
func openWorktreeKeys(metaDir string) ([]string, error) {
	entries, err := os.ReadDir(metaDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read metadata dir: %w", err)
	}
	var keys []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		keys = append(keys, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(keys)
	return keys, nil
}
