package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/tooppoo/git-kura/internal/gitutil"
	"github.com/tooppoo/git-kura/internal/worktree"
)

const sealPathSchemaVersion = 1

//go:embed schema/seal_store.schema.json
var sealStoreSchemaJSON []byte

var sealStoreSchema = mustCompileSealStoreSchema()

func mustCompileSealStoreSchema() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(sealStoreSchemaJSON))
	if err != nil {
		panic(fmt.Sprintf("parse seal store schema: %v", err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("seal_store.schema.json", doc); err != nil {
		panic(fmt.Sprintf("add seal store schema resource: %v", err))
	}
	sch, err := c.Compile("seal_store.schema.json")
	if err != nil {
		panic(fmt.Sprintf("compile seal store schema: %v", err))
	}
	return sch
}

// validateSealStoreJSON checks that raw store JSON conforms to
// schema/seal_store.schema.json.
func validateSealStoreJSON(data []byte) error {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parse seal store: %w", err)
	}
	if err := sealStoreSchema.Validate(inst); err != nil {
		return fmt.Errorf("seal store does not conform to schema: %w", err)
	}
	return nil
}

// defaultSealLockTimeout is the seal store lock timeout used when
// kura.sealLockTimeoutMs is unset. See resolveSealLockTimeout.
const defaultSealLockTimeout = 5 * time.Second

// maxSealLockTimeout caps the configurable lock timeout. No seal operation
// should ever block for more than an hour, and the cap also keeps the millisecond
// value well within time.Duration's range, avoiding overflow when a very large
// kura.sealLockTimeoutMs is multiplied by time.Millisecond.
const maxSealLockTimeout = time.Hour

// maxSealLockTimeoutMs is maxSealLockTimeout expressed in milliseconds, the unit
// of kura.sealLockTimeoutMs.
const maxSealLockTimeoutMs = int64(maxSealLockTimeout / time.Millisecond)

const sealStoreLockInterval = 100 * time.Millisecond

// sealLockTimeoutConfigKey is the Git config key that overrides the seal store
// lock timeout. The value is a non-negative integer in milliseconds, resolved
// through Git's standard config scopes (local / global / system).
const sealLockTimeoutConfigKey = "kura.sealLockTimeoutMs"

// resolveSealLockTimeout determines the seal store lock timeout from the
// kura.sealLockTimeoutMs Git config value, falling back to
// defaultSealLockTimeout when the key is unset.
//
// The configured value is interpreted as non-negative integer milliseconds.
// After stripping a trailing newline, it must consist solely of decimal digits;
// values such as "+5", " 5", "5 ", "5s", "abc", "-1", "" (empty), or decimals
// are rejected as errors. "0" is valid and yields a zero timeout (a single lock
// acquisition attempt with no retry). The timeout is capped at
// maxSealLockTimeout: values above the cap (including integers too large to fit
// in int64) are rejected as errors rather than clamped, which also keeps the
// value within time.Duration's range.
func resolveSealLockTimeout(repoRoot string) (time.Duration, error) {
	raw, configured, err := gitutil.ConfigValue(repoRoot, sealLockTimeoutConfigKey)
	if err != nil {
		return 0, err
	}
	if !configured {
		return defaultSealLockTimeout, nil
	}
	// git config appends a trailing newline; strip it before validating so the
	// remaining string is exactly the configured value.
	value := strings.TrimRight(raw, "\n")
	if !isDecimalDigits(value) {
		return 0, fmt.Errorf("invalid %s %q: expected a non-negative integer number of milliseconds", sealLockTimeoutConfigKey, value)
	}
	ms, err := strconv.ParseInt(value, 10, 64)
	if err != nil || ms > maxSealLockTimeoutMs {
		// value is all decimal digits, so the only possible parse failure is
		// range overflow; either way the number exceeds the cap.
		return 0, fmt.Errorf("invalid %s %q: must not exceed %d milliseconds (%s)", sealLockTimeoutConfigKey, value, maxSealLockTimeoutMs, maxSealLockTimeout)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// isDecimalDigits reports whether s is a non-empty string of ASCII decimal
// digits only. It deliberately rejects signs, whitespace, and decimal points so
// that values like "+5", " 5", "5 ", and "1.5" are treated as invalid.
func isDecimalDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// sealEntry records how a path is sealed. It is a struct rather than a bare
// key string so future fields (e.g. sealedAt, agent) can be added without a
// breaking schema change. Schema: schema/seal_store.schema.json.
type sealEntry struct {
	Key string `json:"key"`
}

// sealPathStore is the on-disk record at <git-common-dir>/kura/seals/paths.json.
// Paths maps each repository-relative path (forward-slash) to its seal entry.
type sealPathStore struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Paths         map[string]sealEntry `json:"paths"`
}

// pathsSealStore returns the store file and lock file locations for the given
// repo root.
func pathsSealStore(repoRoot string) (storePath, lockPath string, err error) {
	commonDir, err := gitutil.CommonDir(repoRoot)
	if err != nil {
		return "", "", fmt.Errorf("get git common dir: %w", err)
	}
	dir := filepath.Join(commonDir, "kura", "seals")
	return filepath.Join(dir, "paths.json"), filepath.Join(dir, "paths.lock"), nil
}

func readSealStore(path string) (sealPathStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return sealPathStore{Paths: make(map[string]sealEntry)}, nil
		}
		return sealPathStore{}, fmt.Errorf("read seal store: %w", err)
	}
	// Validate before unmarshalling so a hand-edited or corrupted store is
	// rejected instead of being silently coerced into the Go struct.
	if err := validateSealStoreJSON(data); err != nil {
		return sealPathStore{}, fmt.Errorf("read seal store %s: %w", path, err)
	}
	var store sealPathStore
	if err := json.Unmarshal(data, &store); err != nil {
		return sealPathStore{}, fmt.Errorf("parse seal store: %w", err)
	}
	if store.Paths == nil {
		store.Paths = make(map[string]sealEntry)
	}
	return store, nil
}

// sealStoreInspection holds the structured result of a successful store inspection.
type sealStoreInspection struct {
	checkedClaims int
	findings      []sealDoctorFinding
}

// inspectSealStore reads the seal store at storePath and returns structured
// findings. An error is returned only when the store cannot be read or parsed
// (malformed store); integrity violations are returned as findings.
func inspectSealStore(storePath string) (sealStoreInspection, error) {
	store, err := readSealStore(storePath)
	if err != nil {
		return sealStoreInspection{}, err
	}

	rawPaths := make([]string, 0, len(store.Paths))
	for rawPath := range store.Paths {
		rawPaths = append(rawPaths, rawPath)
	}
	sort.Strings(rawPaths)

	// Validate three integrity properties of every stored entry: each path is
	// a well-formed, repository-relative location that stays inside the repo;
	// no two entries collapse to the same canonical path; and every path is
	// already stored in its canonical (normalized) form. Every violation found
	// in a single pass is collected and reported together.
	var findings []sealDoctorFinding
	seen := make(map[string]string, len(rawPaths))
	for _, rawPath := range rawPaths {
		p := rawPath // local copy for pointer safety
		entry := store.Paths[rawPath]

		canonical, canonErr := canonicalStoredSealPath(rawPath)
		if canonErr != nil {
			findings = append(findings, sealDoctorFinding{
				Severity: "error",
				Code:     "invalid-stored-path",
				Path:     &p,
				Message:  canonErr.Error(),
			})
			continue
		}
		if firstRawPath, ok := seen[canonical]; ok {
			firstKey := store.Paths[firstRawPath].Key
			var msg string
			if firstKey != entry.Key {
				msg = fmt.Sprintf("store entries %q (key %q) and %q (key %q) refer to the same canonical path %q",
					firstRawPath, firstKey, rawPath, entry.Key, canonical)
			} else {
				msg = fmt.Sprintf("store entries %q and %q duplicate canonical path %q", firstRawPath, rawPath, canonical)
			}
			findings = append(findings, sealDoctorFinding{
				Severity: "error",
				Code:     "duplicate-canonical-path",
				Path:     &p,
				Message:  msg,
			})
			continue
		}
		seen[canonical] = rawPath
		if canonical != rawPath {
			findings = append(findings, sealDoctorFinding{
				Severity: "error",
				Code:     "non-normalized-path",
				Path:     &p,
				Message:  fmt.Sprintf("store entry %q is not normalized; canonical path is %q", rawPath, canonical),
			})
		}
	}

	return sealStoreInspection{
		checkedClaims: len(rawPaths),
		findings:      findings,
	}, nil
}

// doctorSealStore is a backward-compatible wrapper over inspectSealStore that
// returns an error listing all integrity violations, or nil when the store is
// healthy. A store-read failure is returned directly.
func doctorSealStore(storePath string) error {
	inspection, err := inspectSealStore(storePath)
	if err != nil {
		return err
	}
	errs := make([]error, 0, len(inspection.findings))
	for _, f := range inspection.findings {
		errs = append(errs, fmt.Errorf("%s", f.Message))
	}
	return errors.Join(errs...)
}

func canonicalStoredSealPath(rawPath string) (string, error) {
	if rawPath == "" {
		return "", fmt.Errorf("store entry has empty path")
	}
	if strings.Contains(rawPath, `\`) {
		return "", fmt.Errorf("store entry %q contains a non-/ path separator", rawPath)
	}
	if pathpkg.IsAbs(rawPath) || filepath.IsAbs(rawPath) {
		return "", fmt.Errorf("store entry %q must be repository-relative", rawPath)
	}
	clean := pathpkg.Clean(rawPath)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("store entry %q escapes the repository root", rawPath)
	}
	return clean, nil
}

func writeSealStore(path string, store sealPathStore) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create seal store dir: %w", err)
	}
	store.SchemaVersion = sealPathSchemaVersion
	if store.Paths == nil {
		store.Paths = make(map[string]sealEntry)
	}
	data, _ := json.Marshal(store)
	// Validate before writing so a bug can never persist a store that other
	// readers (or the future commit hook) would reject.
	if err := validateSealStoreJSON(data); err != nil {
		return fmt.Errorf("refusing to write seal store: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write seal store: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return errors.Join(fmt.Errorf("commit seal store: %w", err), os.Remove(tmp))
	}
	return nil
}

// acquireSealLock creates the lock file using atomic O_CREATE|O_EXCL, retrying
// until the supplied timeout elapses. The caller is responsible for resolving
// the timeout (see resolveSealLockTimeout); this function never reads Git config
// or environment variables, keeping lock acquisition independent of config
// resolution. A zero timeout makes exactly one acquisition attempt with no
// retry, failing immediately with seal-lock-timeout if the lock is held.
// Returns a release function that removes the lock file. If removal fails the
// lock would block all future seal commands, so the failure is reported on
// stderr with the lock path so the user can remove it manually.
func acquireSealLock(lockPath string, timeout time.Duration) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create seal store dir: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return func() {
				if removeErr := os.Remove(lockPath); removeErr != nil && !os.IsNotExist(removeErr) {
					fmt.Fprintf(os.Stderr,
						"warning: failed to release seal store lock %s: %v\nremove the file manually or subsequent seal commands will time out\n",
						lockPath, removeErr)
				}
			}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("acquire seal store lock: %w", err)
		}
		if time.Now().After(deadline) {
			return nil, exitCodeError(exitSealLockTimeout,
				fmt.Errorf("seal-lock-timeout: failed to acquire seal store lock after %s", timeout))
		}
		time.Sleep(sealStoreLockInterval)
	}
}

// normalizeSealPath converts rawPath to a clean repository-relative path.
// rawPath must be relative and is interpreted relative to the repository
// root — never the caller's working directory — so the same argument always
// resolves to the same file. Returns an error for absolute paths and paths
// that escape the repository.
func normalizeSealPath(repoRoot, rawPath string) (string, error) {
	if filepath.IsAbs(rawPath) {
		return "", fmt.Errorf("path %q must be relative to the repository root", rawPath)
	}
	abs := filepath.Clean(filepath.Join(repoRoot, filepath.FromSlash(rawPath)))

	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil {
		return "", fmt.Errorf("resolve path relative to repo root: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the repository root", rawPath)
	}
	return rel, nil
}

// readSealContext resolves the current seal key from the active git-kura managed worktree.
func readSealContext() (string, error) {
	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return worktree.CurrentKey(repoRoot)
}

// sealConflict records one path that could not be claimed/unclaimed because it
// is claimed by a key other than the current one.
type sealConflict struct {
	path     string // path as given by the user
	sealedBy string // key that currently seals the path
}

// sealConflictError builds the seal-conflict error listing every conflicting
// path and the key that seals it, so the user can see all blockers at once.
func sealConflictError(conflicts []sealConflict) error {
	parts := make([]string, 0, len(conflicts))
	for _, c := range conflicts {
		parts = append(parts, fmt.Sprintf("path %q is already claimed by key %q", c.path, c.sealedBy))
	}
	return exitCodeError(exitSealConflict,
		fmt.Errorf("seal-conflict: %s", strings.Join(parts, "; ")))
}

func cmdSealClaim(rawPaths []string) error {
	key, err := readSealContext()
	if err != nil {
		return err
	}

	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository")
	}

	storeFile, lockFile, err := pathsSealStore(repoRoot)
	if err != nil {
		return err
	}

	timeout, err := resolveSealLockTimeout(repoRoot)
	if err != nil {
		return err
	}
	release, err := acquireSealLock(lockFile, timeout)
	if err != nil {
		return err
	}
	defer release()

	store, err := readSealStore(storeFile)
	if err != nil {
		return err
	}

	// Validate all paths before modifying the store; partial success is not
	// allowed. Cross-key conflicts are collected so the error reports every
	// conflicting path with the key that seals it.
	var conflicts []sealConflict
	toAdd := make([]string, 0, len(rawPaths))
	for _, rawPath := range rawPaths {
		relPath, err := normalizeSealPath(repoRoot, rawPath)
		if err != nil {
			return err
		}
		storeKey := filepath.ToSlash(relPath)

		info, err := os.Stat(filepath.Join(repoRoot, relPath))
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("path %q does not exist", rawPath)
			}
			return fmt.Errorf("check path: %w", err)
		}
		// Only files can be sealed; directory seals are out of scope (see
		// docs/adr/20260611T114624Z_limit-seal-targets-to-repository-relative-files.md).
		if info.IsDir() {
			return fmt.Errorf("path %q is a directory; only files can be claimed", rawPath)
		}

		if entry, sealed := store.Paths[storeKey]; sealed {
			if entry.Key != key {
				conflicts = append(conflicts, sealConflict{path: rawPath, sealedBy: entry.Key})
			}
			// Already claimed under the current key: idempotent, nothing to write.
			continue
		}
		toAdd = append(toAdd, storeKey)
	}

	if len(conflicts) > 0 {
		return sealConflictError(conflicts)
	}
	if len(toAdd) == 0 {
		return nil
	}
	for _, storeKey := range toAdd {
		store.Paths[storeKey] = sealEntry{Key: key}
	}
	return writeSealStore(storeFile, store)
}

func cmdSealUnclaim(rawPaths []string) error {
	key, err := readSealContext()
	if err != nil {
		return err
	}

	repoRoot, err := gitutil.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository")
	}

	storeFile, lockFile, err := pathsSealStore(repoRoot)
	if err != nil {
		return err
	}

	timeout, err := resolveSealLockTimeout(repoRoot)
	if err != nil {
		return err
	}
	release, err := acquireSealLock(lockFile, timeout)
	if err != nil {
		return err
	}
	defer release()

	store, err := readSealStore(storeFile)
	if err != nil {
		return err
	}

	// Validate all paths before modifying the store; partial success is not
	// allowed. Cross-key conflicts are collected so the error reports every
	// conflicting path with the key that seals it.
	var conflicts []sealConflict
	toRemove := make([]string, 0, len(rawPaths))
	for _, rawPath := range rawPaths {
		relPath, err := normalizeSealPath(repoRoot, rawPath)
		if err != nil {
			return err
		}
		storeKey := filepath.ToSlash(relPath)

		entry, sealed := store.Paths[storeKey]
		if !sealed {
			// Unclaiming a path that was never claimed: idempotent no-op.
			continue
		}
		if entry.Key != key {
			conflicts = append(conflicts, sealConflict{path: rawPath, sealedBy: entry.Key})
			continue
		}
		toRemove = append(toRemove, storeKey)
	}

	if len(conflicts) > 0 {
		return sealConflictError(conflicts)
	}
	if len(toRemove) == 0 {
		return nil
	}
	for _, storeKey := range toRemove {
		delete(store.Paths, storeKey)
	}
	return writeSealStore(storeFile, store)
}

// cmdSealTest checks whether every path in rawPaths may be handled in the
// current seal context without modifying the store. It is read-only and does
// not take paths.lock, so a held lock never blocks it (mirroring cmdSealLs).
//
// In JSON mode, a conflict is a business result: ok is true with
// data.passed false, and exit code 6 is still used. Execution failures
// (current key unresolved, invalid path) produce ok:false envelopes.
func cmdSealTest(opts sealTestOptions, rawPaths []string) error {
	repoTop, err := gitutil.RepoRoot()
	if err != nil {
		return sealTestCurrentKeyFail(opts, fmt.Errorf("not inside a git repository"), "")
	}

	key, keyErr := worktree.CurrentKey(repoTop)
	if keyErr != nil {
		return sealTestCurrentKeyFail(opts, keyErr, repoTop)
	}

	storeFile, _, err := pathsSealStore(repoTop)
	if err != nil {
		return sealTestFail(opts, err)
	}
	store, err := readSealStore(storeFile)
	if err != nil {
		return sealTestFail(opts, err)
	}

	results := make([]sealTestResultItem, 0, len(rawPaths))
	for _, rawPath := range rawPaths {
		relPath, normErr := normalizeSealPath(repoTop, rawPath)
		if normErr != nil {
			// Absolute / repo-external / non-normalizable path: input contract violation.
			return sealTestFail(opts, normErr)
		}
		storeKey := filepath.ToSlash(relPath)

		_, statErr := os.Stat(filepath.Join(repoTop, relPath))
		fileExists := statErr == nil

		entry, sealed := store.Paths[storeKey]

		var item sealTestResultItem
		item.Path = storeKey
		switch {
		case sealed && entry.Key != key:
			// Claimed by a different key: conflict regardless of filesystem existence.
			k := entry.Key
			item.Status = "claimed-by-other-key"
			item.Safe = false
			item.ClaimedBy = &k
		case sealed && entry.Key == key:
			k := key
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

	data := sealTestData{
		CurrentKey: key,
		Passed:     passed,
		Results:    results,
	}

	if opts.JSON {
		if err := validateData(sealTestDataSchema, data); err != nil {
			return err
		}
		if err := emitResult(renderJSON, Result{Command: commandSealTest, Data: data}); err != nil {
			return err
		}
		if !passed {
			// Conflict is a business result in JSON mode: ok:true but exit 6.
			return &renderedError{code: exitSealConflict}
		}
		return nil
	}

	// Human mode: conflict → existing exit-6 error path.
	if !passed {
		var conflicts []sealConflict
		for _, r := range results {
			if r.Status == "claimed-by-other-key" {
				conflicts = append(conflicts, sealConflict{path: r.Path, sealedBy: *r.ClaimedBy})
			}
		}
		return sealConflictError(conflicts)
	}
	return nil
}

// sealTestFail routes a seal test execution failure to the right output.
// JSON requests render an ok:false envelope on stdout; plain requests keep the
// existing error behavior.
func sealTestFail(opts sealTestOptions, err error) error {
	if !opts.JSON {
		return err
	}
	return emitError(renderJSON, toCommandError(commandSealTest, err))
}

// sealTestCurrentKeyFail handles the current-key-unresolved failure for seal
// test. In JSON mode it produces a structured ok:false envelope with reason
// details; in human mode it returns a plain error with the current-key-unresolved
// token in the message.
func sealTestCurrentKeyFail(opts sealTestOptions, keyErr error, repoTop string) error {
	reason, metaPath := classifyCurrentKeyError(keyErr, repoTop)
	msg := fmt.Sprintf("current-key-unresolved: %s", keyErr.Error())

	if !opts.JSON {
		return fmt.Errorf("%s", msg)
	}

	var repoTopPtr *string
	if repoTop != "" {
		repoTopCopy := repoTop
		repoTopPtr = &repoTopCopy
	}
	cerr := &CommandError{
		Command:  commandSealTest,
		Code:     "current-key-unresolved",
		Message:  msg,
		ExitCode: exitGeneralError,
		Details: currentKeyUnresolvedDetails{
			Reason:         reason,
			RepositoryRoot: repoTopPtr,
			MetadataPath:   metaPath,
		},
	}
	return emitError(renderJSON, cerr)
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

// sealKeyNone is the sentinel "current key" used when a seal decision runs in a
// context that does not map to any managed worktree key — for example the
// pre-commit hook running in an unmanaged worktree. With this key, a path
// claimed by any key is a conflict, while unclaimed paths are allowed.
const sealKeyNone = ""

// evaluateSealedPaths decides which of relPaths conflict with the seal store for
// currentKey, without touching the store. Each path in relPaths is a
// repository-relative path (for example a staged path from
// "git diff --cached --name-only"); it is canonicalized to a store key the same
// way the store records claims.
//
// A path is safe when it is unclaimed or, for a concrete current key, already
// claimed by that key. When currentKey is sealKeyNone, every claimed path is a
// conflict. This is the shared path-level decision the pre-commit hook reuses so
// its judgment matches "git kura seal test".
func evaluateSealedPaths(store sealPathStore, currentKey string, relPaths []string) []sealConflict {
	var conflicts []sealConflict
	for _, rawPath := range relPaths {
		storeKey := filepath.ToSlash(pathpkg.Clean(rawPath))
		entry, sealed := store.Paths[storeKey]
		if !sealed {
			continue
		}
		if currentKey == sealKeyNone || entry.Key != currentKey {
			conflicts = append(conflicts, sealConflict{path: rawPath, sealedBy: entry.Key})
		}
	}
	return conflicts
}
