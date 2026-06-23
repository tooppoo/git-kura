package seal

import (
	"fmt"
	"os"
	"path/filepath"
)

// TestInput is the input for the Test usecase.
type TestInput struct {
	RepoRoot   string
	CurrentKey string
	RawPaths   []string
}

// Test checks whether every path in RawPaths may be handled in the current seal
// context without modifying the store. It is read-only and does not acquire the
// store lock.
func Test(input TestInput) (TestResult, error) {
	storeFile, _, err := StorePaths(input.RepoRoot)
	if err != nil {
		return TestResult{}, fmt.Errorf("resolve seal store path: %w", err)
	}
	store, err := ReadStore(storeFile)
	if err != nil {
		return TestResult{}, err
	}

	// Normalize all paths upfront so conflict detection and per-path decoration
	// both use the same storeKey.
	type normed struct {
		raw, storeKey string
	}
	paths := make([]normed, 0, len(input.RawPaths))
	for _, rawPath := range input.RawPaths {
		relPath, normErr := NormalizePath(input.RepoRoot, rawPath)
		if normErr != nil {
			return TestResult{}, normErr
		}
		paths = append(paths, normed{raw: rawPath, storeKey: filepath.ToSlash(relPath)})
	}

	// Use the shared evaluator for the conflict decision (same logic as pre-commit).
	storeKeys := make([]string, len(paths))
	for i, p := range paths {
		storeKeys[i] = p.storeKey
	}
	conflicts := EvaluateStorePaths(store, input.CurrentKey, storeKeys)
	conflictMap := make(map[string]string, len(conflicts)) // storeKey → ownerKey
	for _, c := range conflicts {
		conflictMap[c.Path] = c.SealedBy
	}

	results := make([]TestResultItem, 0, len(paths))
	for _, p := range paths {
		var item TestResultItem
		item.Path = p.storeKey
		if ownerKey, isConflict := conflictMap[p.storeKey]; isConflict {
			item.Status = "claimed-by-other-key"
			item.Safe = false
			item.ClaimedBy = &ownerKey
		} else if entry, sealed := store.Paths[p.storeKey]; sealed {
			k := entry.Key
			item.Status = "claimed-by-current-key"
			item.Safe = true
			item.ClaimedBy = &k
		} else {
			_, statErr := os.Stat(filepath.Join(input.RepoRoot, p.storeKey))
			if os.IsNotExist(statErr) {
				item.Status = "missing-path"
			} else {
				item.Status = "unclaimed"
			}
			item.Safe = true
		}
		results = append(results, item)
	}

	passed := true
	for _, r := range results {
		if !r.Safe {
			passed = false
			break
		}
	}

	return TestResult{
		CurrentKey: input.CurrentKey,
		Passed:     passed,
		Results:    results,
	}, nil
}
