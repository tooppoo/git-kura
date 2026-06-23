package seal

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// UnclaimInput is the input for the Unclaim usecase.
type UnclaimInput struct {
	RepoRoot   string
	CurrentKey string
	RawPaths   []string
}

// Unclaim releases the current key's claim on one or more paths in the seal store.
// Returns ConflictErr when paths fail preflight, StoreErr for store failures,
// LockTimeoutErr when the lock cannot be acquired. On success, warnings holds
// any non-fatal lock-release message the caller should surface to the user.
func Unclaim(input UnclaimInput) (result UnclaimResult, warnings []string, err error) {
	storeFile, lockFile, pathErr := StorePaths(input.RepoRoot)
	if pathErr != nil {
		return UnclaimResult{}, nil, fmt.Errorf("resolve seal store path: %w", pathErr)
	}

	timeout, timeoutErr := ResolveLockTimeout(input.RepoRoot)
	if timeoutErr != nil {
		return UnclaimResult{}, nil, timeoutErr
	}
	release, lockErr := AcquireLock(lockFile, timeout)
	if lockErr != nil {
		return UnclaimResult{}, nil, lockErr
	}
	defer func() {
		if releaseErr := release(); releaseErr != nil && err == nil {
			warnings = append(warnings, releaseErr.Error())
		}
	}()

	store, storeErr := ReadStore(storeFile)
	if storeErr != nil {
		phase := "read-store"
		if errors.As(storeErr, new(StoreValidationErr)) {
			phase = "validate-store"
		}
		err = StoreErr{Phase: phase, StorePath: storeFile, Cause: storeErr}
		return
	}

	type pathResult struct {
		storeKey      string
		unclaimStatus string
		item          MutationPathItem
	}
	results := make([]pathResult, 0, len(input.RawPaths))
	seen := make(map[string]int, len(input.RawPaths))
	hasBlocking := false

	for i, rawPath := range input.RawPaths {
		relPath, normErr := NormalizePath(input.RepoRoot, rawPath)
		if normErr != nil {
			status := "invalid-path"
			if strings.Contains(normErr.Error(), "outside the repository root") {
				status = "outside-repository"
			}
			results = append(results, pathResult{
				item: MutationPathItem{
					Path:       rawPath,
					Status:     status,
					Blocking:   true,
					HumanError: normErr.Error(),
				},
			})
			hasBlocking = true
			continue
		}
		storeKey := filepath.ToSlash(relPath)

		if firstIdx, dup := seen[storeKey]; dup {
			idx := firstIdx
			results = append(results, pathResult{
				storeKey: storeKey,
				item: MutationPathItem{
					Path:        rawPath,
					Status:      "duplicate",
					DuplicateOf: &idx,
					Blocking:    true,
					HumanError:  fmt.Sprintf("path %q is a duplicate of argument at index %d", rawPath, firstIdx),
				},
			})
			hasBlocking = true
			continue
		}
		seen[storeKey] = i

		entry, sealed := store.Paths[storeKey]
		if !sealed {
			results = append(results, pathResult{
				storeKey:      storeKey,
				unclaimStatus: "not-claimed",
				item:          MutationPathItem{Path: storeKey, Status: "not-claimed"},
			})
			continue
		}
		if entry.Key != input.CurrentKey {
			results = append(results, pathResult{
				storeKey: storeKey,
				item: MutationPathItem{
					Path:     storeKey,
					Status:   "owned-by-other",
					OwnerKey: entry.Key,
					Blocking: true,
				},
			})
			hasBlocking = true
			continue
		}
		results = append(results, pathResult{
			storeKey:      storeKey,
			unclaimStatus: "released",
			item:          MutationPathItem{Path: storeKey, Status: "would-release"},
		})
	}

	if hasBlocking {
		items := make([]MutationPathItem, len(results))
		var conflictItems []ConflictItem
		var duplicateItems []DuplicateItem
		for i, r := range results {
			items[i] = r.item
			if r.item.Status == "owned-by-other" {
				conflictItems = append(conflictItems, ConflictItem{
					Path:         r.item.Path,
					OwnerKey:     r.item.OwnerKey,
					RequestedKey: input.CurrentKey,
				})
			}
			if r.item.Status == "duplicate" && r.item.DuplicateOf != nil {
				duplicateItems = append(duplicateItems, DuplicateItem{
					Path:           r.item.Path,
					FirstIndex:     *r.item.DuplicateOf,
					DuplicateIndex: i,
				})
			}
		}
		err = ConflictErr{
			Phase:      "preflight",
			CurrentKey: input.CurrentKey,
			Paths:      items,
			Conflicts:  conflictItems,
			Duplicates: duplicateItems,
		}
		return
	}

	for _, r := range results {
		if r.unclaimStatus == "released" {
			delete(store.Paths, r.storeKey)
		}
	}
	if writeErr := WriteStore(storeFile, store); writeErr != nil {
		err = StoreErr{Phase: "write-store", StorePath: storeFile, Cause: writeErr}
		return
	}

	pathItems := make([]UnclaimPathItem, len(results))
	for i, r := range results {
		pathItems[i] = UnclaimPathItem{Path: r.storeKey, Status: r.unclaimStatus}
	}
	result = UnclaimResult{CurrentKey: input.CurrentKey, Paths: pathItems}
	return
}
