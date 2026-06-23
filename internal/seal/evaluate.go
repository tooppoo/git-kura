package seal

import (
	"fmt"
	pathpkg "path"
	"path/filepath"
)

// KeyNone is the sentinel current key used when a seal decision runs in a
// context that does not map to any managed worktree key — for example the
// pre-commit hook running in an unmanaged worktree. With this key, a path
// claimed by any key is a conflict, while unclaimed paths are allowed.
const KeyNone = ""

// PathConflict records one path that conflicts with the seal store for the
// current key.
type PathConflict struct {
	Path     string
	SealedBy string
}

// EvaluatePaths reads the seal store and checks relPaths for conflicts.
// If currentKey is KeyNone, every claimed path is a conflict.
// relPaths should be repository-relative paths (e.g. staged file paths).
func EvaluatePaths(repoRoot, currentKey string, relPaths []string) ([]PathConflict, error) {
	storeFile, _, err := StorePaths(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve seal store path: %w", err)
	}
	store, err := ReadStore(storeFile)
	if err != nil {
		return nil, err
	}
	return EvaluateStorePaths(store, currentKey, relPaths), nil
}

// EvaluateStorePaths decides which of relPaths conflict with the seal store for
// currentKey, without reading the store from disk. This is the shared
// path-level decision reused by both seal test and the pre-commit hook.
func EvaluateStorePaths(store PathStore, currentKey string, relPaths []string) []PathConflict {
	var conflicts []PathConflict
	for _, rawPath := range relPaths {
		storeKey := filepath.ToSlash(pathpkg.Clean(rawPath))
		entry, sealed := store.Paths[storeKey]
		if !sealed {
			continue
		}
		if currentKey == KeyNone || entry.Key != currentKey {
			conflicts = append(conflicts, PathConflict{Path: rawPath, SealedBy: entry.Key})
		}
	}
	return conflicts
}
