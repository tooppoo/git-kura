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

	results := make([]TestResultItem, 0, len(input.RawPaths))
	for _, rawPath := range input.RawPaths {
		relPath, normErr := NormalizePath(input.RepoRoot, rawPath)
		if normErr != nil {
			return TestResult{}, normErr
		}
		storeKey := filepath.ToSlash(relPath)

		_, statErr := os.Stat(filepath.Join(input.RepoRoot, relPath))
		fileExists := statErr == nil

		entry, sealed := store.Paths[storeKey]

		var item TestResultItem
		item.Path = storeKey
		switch {
		case sealed && entry.Key != input.CurrentKey:
			k := entry.Key
			item.Status = "claimed-by-other-key"
			item.Safe = false
			item.ClaimedBy = &k
		case sealed && entry.Key == input.CurrentKey:
			k := input.CurrentKey
			item.Status = "claimed-by-current-key"
			item.Safe = true
			item.ClaimedBy = &k
		case !fileExists:
			item.Status = "missing-path"
			item.Safe = true
			item.ClaimedBy = nil
		default:
			item.Status = "unclaimed"
			item.Safe = true
			item.ClaimedBy = nil
		}
		results = append(results, item)
	}

	passed := true
	for _, r := range results {
		if r.Status == "claimed-by-other-key" {
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
