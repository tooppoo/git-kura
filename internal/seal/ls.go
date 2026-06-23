package seal

import (
	"fmt"
	"sort"
)

// LsInput is the input for the Ls usecase.
type LsInput struct {
	RepoRoot  string
	FilterKey string
}

// Ls lists claimed paths from the seal store. An empty FilterKey lists all keys.
// It is repository-wide and read-only; it does not acquire the store lock.
func Ls(input LsInput) (LsResult, error) {
	storeFile, _, err := StorePaths(input.RepoRoot)
	if err != nil {
		return LsResult{}, fmt.Errorf("resolve seal store path: %w", err)
	}
	store, err := ReadStore(storeFile)
	if err != nil {
		return LsResult{}, err
	}

	rawPaths := make([]string, 0, len(store.Paths))
	for p, entry := range store.Paths {
		if input.FilterKey != "" && entry.Key != input.FilterKey {
			continue
		}
		rawPaths = append(rawPaths, p)
	}
	sort.Slice(rawPaths, func(i, j int) bool {
		ki, kj := store.Paths[rawPaths[i]].Key, store.Paths[rawPaths[j]].Key
		if ki != kj {
			return ki < kj
		}
		return rawPaths[i] < rawPaths[j]
	})

	claims := make([]LsClaim, len(rawPaths))
	for i, p := range rawPaths {
		claims[i] = LsClaim{Key: store.Paths[p].Key, Path: p}
	}

	var filterKey *string
	if input.FilterKey != "" {
		k := input.FilterKey
		filterKey = &k
	}
	return LsResult{FilterKey: filterKey, Claims: claims}, nil
}
