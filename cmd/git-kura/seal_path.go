package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tooppoo/git-kura/internal/gitutil"
	"github.com/tooppoo/git-kura/internal/output"
	"github.com/tooppoo/git-kura/internal/seal"
	"github.com/tooppoo/git-kura/internal/worktree"
)

// sealMutationErrorDetails is the error.details payload for seal claim/unclaim
// failures. Preflight failures populate Phase, CurrentKey, Paths, Conflicts,
// and Duplicates. Store-level failures populate Phase and StoreError only.
type sealMutationErrorDetails struct {
	Phase      string                  `json:"phase,omitempty"`
	CurrentKey string                  `json:"currentKey,omitempty"`
	Paths      []seal.MutationPathItem `json:"paths,omitempty"`
	Conflicts  []seal.ConflictItem     `json:"conflicts,omitempty"`
	Duplicates []seal.DuplicateItem    `json:"duplicates,omitempty"`
	StoreError *sealStoreError         `json:"storeError,omitempty"`
}

// sealStoreError describes a store-wide failure in error.details.storeError.
type sealStoreError struct {
	Status string `json:"status"`
	Path   string `json:"path,omitempty"`
}

// currentKeyUnresolvedDetails is the error.details payload when current key
// resolution fails for seal test --json.
type currentKeyUnresolvedDetails struct {
	Reason         string  `json:"reason"`
	RepositoryRoot *string `json:"repositoryRoot"`
	MetadataPath   *string `json:"metadataPath"`
}

// readSealContext resolves the current seal key from the active git-kura managed worktree.
func readSealContext() (string, error) {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return worktree.CurrentKey(repoRoot)
}

// sealConflictError builds the seal-conflict error listing every conflicting path
// and the key that seals it.
func sealConflictError(conflicts []seal.PathConflict) error {
	parts := make([]string, 0, len(conflicts))
	for _, c := range conflicts {
		parts = append(parts, fmt.Sprintf("path %q is already claimed by key %q", c.Path, c.SealedBy))
	}
	return exitCodeError(exitSealConflict,
		fmt.Errorf("seal-conflict: %s", strings.Join(parts, "; ")))
}

func cmdSealClaim(opts sealClaimOptions, rawPaths []string) error {
	key, err := readSealContext()
	if err != nil {
		return sealClaimFail(opts, "preflight", err)
	}

	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return sealClaimFail(opts, "preflight", fmt.Errorf("not inside a git repository"))
	}

	result, claimErr := seal.Claim(seal.ClaimInput{
		RepoRoot:   repoRoot,
		CurrentKey: key,
		RawPaths:   rawPaths,
	})
	if claimErr != nil {
		var lockErr seal.LockTimeoutErr
		if errors.As(claimErr, &lockErr) {
			return sealClaimFail(opts, "preflight", exitCodeError(exitSealLockTimeout, lockErr))
		}
		var storeErr seal.StoreErr
		if errors.As(claimErr, &storeErr) {
			return sealClaimStoreFail(opts, storeErr.Phase, storeErr.StorePath, storeErr.Cause)
		}
		var conflictErr seal.ConflictErr
		if errors.As(claimErr, &conflictErr) {
			details := sealMutationErrorDetails{
				Phase:      conflictErr.Phase,
				CurrentKey: conflictErr.CurrentKey,
				Paths:      conflictErr.Paths,
				Conflicts:  conflictErr.Conflicts,
				Duplicates: conflictErr.Duplicates,
			}
			cerr := &output.CommandError{
				Command:  output.CommandSealClaim,
				Code:     "seal-conflict",
				Message:  "seal-conflict: one or more paths could not be claimed",
				ExitCode: exitSealConflict,
				Details:  details,
			}
			if opts.renderMode() == output.RenderHuman {
				var pConflicts []seal.PathConflict
				for _, item := range conflictErr.Paths {
					if !item.Blocking {
						continue
					}
					if item.Status == "owned-by-other" {
						pConflicts = append(pConflicts, seal.PathConflict{Path: item.Path, SealedBy: item.OwnerKey})
					} else {
						return fmt.Errorf("%s", item.HumanError)
					}
				}
				if len(pConflicts) > 0 {
					return sealConflictError(pConflicts)
				}
			}
			return emitError(opts.renderMode(), cerr)
		}
		return sealClaimFail(opts, "preflight", claimErr)
	}

	mode := opts.renderMode()
	if mode != output.RenderHuman {
		if err := validateData(sealClaimDataSchema, result); err != nil {
			return err
		}
	}
	return emitResult(mode, output.Result{Command: output.CommandSealClaim, Data: result})
}

// sealClaimFail routes a seal claim preflight failure to the right output.
func sealClaimFail(opts sealClaimOptions, phase string, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	details := sealMutationErrorDetails{Phase: phase}
	cerr := toCommandError(output.CommandSealClaim, err)
	cerr.Details = details
	return emitError(mode, cerr)
}

// sealClaimStoreFail routes a seal claim store-level failure to the right output.
func sealClaimStoreFail(opts sealClaimOptions, phase string, storeFile string, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	statusMap := map[string]string{
		"read-store":     "store-read-error",
		"validate-store": "store-validation-error",
		"write-store":    "store-write-error",
	}
	details := sealMutationErrorDetails{
		Phase:      phase,
		StoreError: &sealStoreError{Status: statusMap[phase], Path: storeFile},
	}
	cerr := toCommandError(output.CommandSealClaim, err)
	cerr.Details = details
	return emitError(mode, cerr)
}

func cmdSealUnclaim(opts sealUnclaimOptions, rawPaths []string) error {
	key, err := readSealContext()
	if err != nil {
		return sealUnclaimFail(opts, "preflight", err)
	}

	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return sealUnclaimFail(opts, "preflight", fmt.Errorf("not inside a git repository"))
	}

	result, unclaimErr := seal.Unclaim(seal.UnclaimInput{
		RepoRoot:   repoRoot,
		CurrentKey: key,
		RawPaths:   rawPaths,
	})
	if unclaimErr != nil {
		var lockErr seal.LockTimeoutErr
		if errors.As(unclaimErr, &lockErr) {
			return sealUnclaimFail(opts, "preflight", exitCodeError(exitSealLockTimeout, lockErr))
		}
		var storeErr seal.StoreErr
		if errors.As(unclaimErr, &storeErr) {
			return sealUnclaimStoreFail(opts, storeErr.Phase, storeErr.StorePath, storeErr.Cause)
		}
		var conflictErr seal.ConflictErr
		if errors.As(unclaimErr, &conflictErr) {
			details := sealMutationErrorDetails{
				Phase:      conflictErr.Phase,
				CurrentKey: conflictErr.CurrentKey,
				Paths:      conflictErr.Paths,
				Conflicts:  conflictErr.Conflicts,
				Duplicates: conflictErr.Duplicates,
			}
			cerr := &output.CommandError{
				Command:  output.CommandSealUnclaim,
				Code:     "seal-conflict",
				Message:  "seal-conflict: one or more paths could not be released",
				ExitCode: exitSealConflict,
				Details:  details,
			}
			if opts.renderMode() == output.RenderHuman {
				var pConflicts []seal.PathConflict
				for _, item := range conflictErr.Paths {
					if !item.Blocking {
						continue
					}
					if item.Status == "owned-by-other" {
						pConflicts = append(pConflicts, seal.PathConflict{Path: item.Path, SealedBy: item.OwnerKey})
					} else {
						return fmt.Errorf("%s", item.HumanError)
					}
				}
				if len(pConflicts) > 0 {
					return sealConflictError(pConflicts)
				}
			}
			return emitError(opts.renderMode(), cerr)
		}
		return sealUnclaimFail(opts, "preflight", unclaimErr)
	}

	mode := opts.renderMode()
	if mode != output.RenderHuman {
		if err := validateData(sealUnclaimDataSchema, result); err != nil {
			return err
		}
	}
	return emitResult(mode, output.Result{Command: output.CommandSealUnclaim, Data: result})
}

// sealUnclaimFail routes a seal unclaim preflight failure to the right output.
func sealUnclaimFail(opts sealUnclaimOptions, phase string, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	details := sealMutationErrorDetails{Phase: phase}
	cerr := toCommandError(output.CommandSealUnclaim, err)
	cerr.Details = details
	return emitError(mode, cerr)
}

// sealUnclaimStoreFail routes a seal unclaim store-level failure to the right output.
func sealUnclaimStoreFail(opts sealUnclaimOptions, phase string, storeFile string, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	statusMap := map[string]string{
		"read-store":     "store-read-error",
		"validate-store": "store-validation-error",
		"write-store":    "store-write-error",
	}
	details := sealMutationErrorDetails{
		Phase:      phase,
		StoreError: &sealStoreError{Status: statusMap[phase], Path: storeFile},
	}
	cerr := toCommandError(output.CommandSealUnclaim, err)
	cerr.Details = details
	return emitError(mode, cerr)
}

// cmdSealTest checks whether every path in rawPaths may be handled in the
// current seal context without modifying the store. Read-only.
func cmdSealTest(opts sealTestOptions, rawPaths []string) error {
	repoTop, err := gitutil.RepoRoot()
	if err != nil {
		return sealTestCurrentKeyFail(opts, fmt.Errorf("not inside a git repository"), "")
	}

	key, keyErr := worktree.CurrentKey(repoTop)
	if keyErr != nil {
		return sealTestCurrentKeyFail(opts, keyErr, repoTop)
	}

	data, testErr := seal.Test(seal.TestInput{
		RepoRoot:   repoTop,
		CurrentKey: key,
		RawPaths:   rawPaths,
	})
	if testErr != nil {
		return sealTestFail(opts, testErr)
	}

	mode := opts.renderMode()
	if mode != output.RenderHuman {
		if err := validateData(sealTestDataSchema, data); err != nil {
			return err
		}
		if err := emitResult(mode, output.Result{Command: output.CommandSealTest, Data: data}); err != nil {
			return err
		}
		if !data.Passed {
			return &renderedError{code: exitSealConflict}
		}
		return nil
	}

	if err := emitResult(output.RenderHuman, output.Result{Command: output.CommandSealTest, Data: data}); err != nil {
		return err
	}
	if !data.Passed {
		return &renderedError{code: exitSealConflict}
	}
	return nil
}

// sealTestFail routes a seal test execution failure to the right output.
func sealTestFail(opts sealTestOptions, err error) error {
	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return err
	}
	return emitError(mode, toCommandError(output.CommandSealTest, err))
}

// sealTestCurrentKeyFail handles the current-key-unresolved failure for seal test.
func sealTestCurrentKeyFail(opts sealTestOptions, keyErr error, repoTop string) error {
	reason, metaPath := classifyCurrentKeyError(keyErr, repoTop)
	msg := fmt.Sprintf("current-key-unresolved: %s", keyErr.Error())

	mode := opts.renderMode()
	if mode == output.RenderHuman {
		return fmt.Errorf("%s", msg)
	}

	var repoTopPtr *string
	if repoTop != "" {
		repoTopCopy := repoTop
		repoTopPtr = &repoTopCopy
	}
	cerr := &output.CommandError{
		Command:  output.CommandSealTest,
		Code:     "current-key-unresolved",
		Message:  msg,
		ExitCode: exitGeneralError,
		Details: currentKeyUnresolvedDetails{
			Reason:         reason,
			RepositoryRoot: repoTopPtr,
			MetadataPath:   metaPath,
		},
	}
	return emitError(mode, cerr)
}

// classifyCurrentKeyError derives the structured reason token from a
// worktree.CurrentKey failure by pattern-matching the error message.
func classifyCurrentKeyError(err error, repoTop string) (reason string, metaPath *string) {
	msg := err.Error()
	if strings.Contains(msg, "not inside a git repository") {
		return "not-inside-git-repository", nil
	}
	if strings.Contains(msg, "not inside a git-kura managed worktree") {
		return "not-in-managed-worktree", nil
	}
	if strings.Contains(msg, "has no git-kura metadata") {
		if mp := tryResolveWorktreeMetadataPath(repoTop); mp != "" {
			return "metadata-missing", &mp
		}
		return "metadata-missing", nil
	}
	return "metadata-inconsistent", nil
}

// tryResolveWorktreeMetadataPath attempts to compute the expected metadata
// file path for the managed worktree rooted at repoTop.
func tryResolveWorktreeMetadataPath(repoTop string) string {
	commonDir, err := gitutil.CommonDir(repoTop)
	if err != nil {
		return ""
	}
	stateDir := filepath.Join(commonDir, "kura")
	worktreesDir := filepath.Join(stateDir, "worktrees")
	rel, err := filepath.Rel(worktreesDir, repoTop)
	if err != nil || strings.ContainsRune(rel, filepath.Separator) || rel == "." || rel == ".." {
		return ""
	}
	return worktree.MetadataPathInStateDir(stateDir, rel)
}
