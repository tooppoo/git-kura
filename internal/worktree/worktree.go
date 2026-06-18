package worktree

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/tooppoo/git-kura/internal/gitutil"
)

//go:embed schema/metadata.schema.json
var metadataSchemaJSON []byte

var metadataSchema = mustCompileMetadataSchema()

func mustCompileMetadataSchema() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(metadataSchemaJSON))
	if err != nil {
		panic(fmt.Sprintf("parse worktree metadata schema: %v", err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("metadata.schema.json", doc); err != nil {
		panic(fmt.Sprintf("add worktree metadata schema resource: %v", err))
	}
	sch, err := c.Compile("metadata.schema.json")
	if err != nil {
		panic(fmt.Sprintf("compile worktree metadata schema: %v", err))
	}
	return sch
}

// validateMetadataJSON checks that raw metadata JSON conforms to
// schema/metadata.schema.json.
func validateMetadataJSON(data []byte) error {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parse metadata: %w", err)
	}
	if err := metadataSchema.Validate(inst); err != nil {
		return fmt.Errorf("metadata does not conform to schema: %w", err)
	}
	return nil
}

type MetadataFile struct {
	RepositoryRoot string `json:"repositoryRoot"`
	BaseBranch     string `json:"baseBranch"`
	WorktreePath   string `json:"worktreePath"`
}

func BranchName(key string) string {
	return key
}

func StateDir(repoRoot string) (string, error) {
	commonDir, err := gitutil.CommonDir(repoRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(commonDir, "kura"), nil
}

func Path(repoRoot, key string) (string, error) {
	dir, err := StateDir(repoRoot)
	if err != nil {
		return "", err
	}
	return PathInStateDir(dir, key), nil
}

func PathInStateDir(stateDir, key string) string {
	return filepath.Join(stateDir, "worktrees", key)
}

func MetadataPath(repoRoot, key string) (string, error) {
	dir, err := StateDir(repoRoot)
	if err != nil {
		return "", err
	}
	return MetadataPathInStateDir(dir, key), nil
}

func MetadataPathInStateDir(stateDir, key string) string {
	return filepath.Join(stateDir, "meta", "worktrees", key+".json")
}

func ReadMetadata(repoRoot, key string) (MetadataFile, error) {
	path, err := MetadataPath(repoRoot, key)
	if err != nil {
		return MetadataFile{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return MetadataFile{}, err
	}
	var meta MetadataFile
	if err := json.Unmarshal(data, &meta); err != nil {
		return MetadataFile{}, err
	}
	return meta, nil
}

// CurrentKey derives the managed-worktree key from the current git worktree.
//
// currentTop is the top-level directory of the current git worktree, i.e. the output of "git rev-parse --show-toplevel".
// A git-kura managed worktree always lives at "<git-common-dir>/kura/worktrees/<key>", so the key is the single path component below that directory.
//
// It fails safely when:
//   - the current directory is not inside a git-kura managed worktree;
//   - the worktree's metadata is missing, unparseable, or does not conform to the metadata schema;
//   - the metadata records a worktree path that does not match currentTop (an inconsistent or relocated worktree).
func CurrentKey(currentTop string) (string, error) {
	commonDir, err := gitutil.CommonDir(currentTop)
	if err != nil {
		return "", fmt.Errorf("resolve git common dir: %w", err)
	}
	stateDir := filepath.Join(commonDir, "kura")
	worktreesDir := filepath.Join(stateDir, "worktrees")

	rel, err := filepath.Rel(worktreesDir, currentTop)
	if err != nil || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
		strings.ContainsRune(rel, filepath.Separator) {
		return "", fmt.Errorf("current directory is not inside a git-kura managed worktree (expected a worktree under %s)", worktreesDir)
	}
	key := rel

	metaPath := MetadataPathInStateDir(stateDir, key)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("worktree at %s has no git-kura metadata; it is not a managed worktree", currentTop)
		}
		return "", fmt.Errorf("read metadata for key %q: %w", key, err)
	}
	// Validate before unmarshalling so a hand-edited or corrupted metadata file
	// is rejected instead of being silently coerced into the Go struct.
	if err := validateMetadataJSON(data); err != nil {
		return "", fmt.Errorf("metadata for key %q is invalid: %w", key, err)
	}
	var meta MetadataFile
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", fmt.Errorf("metadata for key %q is invalid: %w", key, err)
	}
	if !samePath(meta.WorktreePath, currentTop) {
		return "", fmt.Errorf("metadata for key %q records worktree %s, but the current worktree is %s", key, meta.WorktreePath, currentTop)
	}
	return key, nil
}

// ResolveKeyForWorktreeRoot scans the git-kura managed worktree metadata under
// stateDir and returns the keys whose recorded worktree path matches
// worktreeRoot. stateDir is "<git-common-dir>/kura".
//
// It returns every matching key (normally zero or one). The pre-commit hook
// uses this to map the worktree Git is running the hook for back to its key:
//   - zero matches means the hook is running in an unmanaged worktree (current
//     key is "none");
//   - exactly one match is the current key;
//   - more than one match is an inconsistent metadata state the caller must
//     treat as fail-closed.
//
// An absent metadata directory yields zero matches and a nil error. A metadata
// file that cannot be read or parsed is skipped rather than failing the whole
// scan, so one corrupt entry does not hide a valid match.
func ResolveKeyForWorktreeRoot(stateDir, worktreeRoot string) ([]string, error) {
	metaDir := filepath.Join(stateDir, "meta", "worktrees")
	entries, err := os.ReadDir(metaDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read worktree metadata dir: %w", err)
	}
	var keys []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(metaDir, entry.Name()))
		if err != nil {
			continue
		}
		if err := validateMetadataJSON(data); err != nil {
			continue
		}
		var meta MetadataFile
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		if samePath(meta.WorktreePath, worktreeRoot) {
			keys = append(keys, strings.TrimSuffix(entry.Name(), ".json"))
		}
	}
	return keys, nil
}

// samePath reports whether a and b refer to the same filesystem location,
// tolerating symlinked path prefixes (e.g. /tmp vs /private/tmp).
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

func ReadStructuredMetadata(repoRoot, key, worktreePath string, worktreeExists bool) (MetadataFile, error) {
	metaPath, err := MetadataPath(repoRoot, key)
	if err != nil {
		return MetadataFile{}, fmt.Errorf("resolve metadata path: %w", err)
	}

	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			if worktreeExists {
				return MetadataFile{}, fmt.Errorf("metadata for key %q is missing; worktree exists at %s, but Kura cannot reconstruct creation-time metadata such as baseBranch", key, worktreePath)
			}
			return MetadataFile{}, fmt.Errorf("worktree for key %q is not open; run \"git kura open %s\" first", key, key)
		}
		return MetadataFile{}, fmt.Errorf("read metadata for key %q: %w", key, err)
	}

	var meta MetadataFile
	if err := json.Unmarshal(data, &meta); err != nil {
		return MetadataFile{}, fmt.Errorf("metadata for key %q is invalid: %w", key, err)
	}
	if !worktreeExists {
		return MetadataFile{}, fmt.Errorf("worktree for key %q is missing; metadata exists at %s, but expected worktree at %s", key, metaPath, worktreePath)
	}

	return meta, nil
}
