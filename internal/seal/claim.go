package seal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ClaimInput is the input for the Claim usecase.
type ClaimInput struct {
	RepoRoot   string
	CurrentKey string
	RawPaths   []string
}

// Claim claims one or more paths for the current key in the seal store.
// Returns ConflictErr when paths fail preflight, StoreErr for store failures,
// LockTimeoutErr when the lock cannot be acquired. On success, warnings holds
// any non-fatal lock-release message the caller should surface to the user.
func Claim(input ClaimInput) (result ClaimResult, warnings []string, err error) {
	storeFile, lockFile, pathErr := StorePaths(input.RepoRoot)
	if pathErr != nil {
		return ClaimResult{}, nil, fmt.Errorf("resolve seal store path: %w", pathErr)
	}

	timeout, timeoutErr := ResolveLockTimeout(input.RepoRoot)
	if timeoutErr != nil {
		return ClaimResult{}, nil, timeoutErr
	}
	release, lockErr := AcquireLock(lockFile, timeout)
	if lockErr != nil {
		return ClaimResult{}, nil, lockErr
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
		storeKey    string
		claimStatus string
		item        MutationPathItem
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

		info, statErr := os.Stat(filepath.Join(input.RepoRoot, relPath))
		if statErr != nil {
			var humanErr string
			if os.IsNotExist(statErr) {
				humanErr = fmt.Sprintf("path %q does not exist", rawPath)
			} else {
				humanErr = fmt.Sprintf("check path: %v", statErr)
			}
			results = append(results, pathResult{
				storeKey: storeKey,
				item: MutationPathItem{
					Path:       storeKey,
					Status:     "invalid-path",
					Blocking:   true,
					HumanError: humanErr,
				},
			})
			hasBlocking = true
			continue
		}
		if info.IsDir() {
			results = append(results, pathResult{
				storeKey: storeKey,
				item: MutationPathItem{
					Path:       storeKey,
					Status:     "invalid-path",
					Blocking:   true,
					HumanError: fmt.Sprintf("path %q is a directory; only files can be claimed", rawPath),
				},
			})
			hasBlocking = true
			continue
		}

		if entry, sealed := store.Paths[storeKey]; sealed {
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
			} else {
				results = append(results, pathResult{
					storeKey:    storeKey,
					claimStatus: "already-owned",
					item:        MutationPathItem{Path: storeKey, Status: "already-owned"},
				})
			}
			continue
		}
		results = append(results, pathResult{
			storeKey:    storeKey,
			claimStatus: "claimed",
			item:        MutationPathItem{Path: storeKey, Status: "would-claim"},
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
		if r.claimStatus == "claimed" {
			store.Paths[r.storeKey] = Entry{Key: input.CurrentKey}
		}
	}
	if writeErr := WriteStore(storeFile, store); writeErr != nil {
		err = StoreErr{Phase: "write-store", StorePath: storeFile, Cause: writeErr}
		return
	}

	pathItems := make([]ClaimPathItem, len(results))
	for i, r := range results {
		pathItems[i] = ClaimPathItem{Path: r.storeKey, Status: r.claimStatus}
	}
	result = ClaimResult{CurrentKey: input.CurrentKey, Paths: pathItems}
	return
}
