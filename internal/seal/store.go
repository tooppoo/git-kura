package seal

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/tooppoo/git-kura/internal/gitutil"
)

const SchemaVersion = 1

//go:embed schema/seal_store.schema.json
var storeSchemaJSON []byte

var storeSchema = mustCompileStoreSchema()

func mustCompileStoreSchema() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(storeSchemaJSON))
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

// StoreValidationErr wraps a schema-validation failure from ReadStore.
// Callers use errors.As to distinguish validate-store failures from file-read failures.
type StoreValidationErr struct{ Cause error }

func (e StoreValidationErr) Error() string { return e.Cause.Error() }
func (e StoreValidationErr) Unwrap() error { return e.Cause }

// Entry records how a path is sealed.
type Entry struct {
	Key string `json:"key"`
}

// PathStore is the on-disk record at <git-common-dir>/kura/seals/paths.json.
type PathStore struct {
	SchemaVersion int              `json:"schemaVersion"`
	Paths         map[string]Entry `json:"paths"`
}

// StorePaths returns the store file and lock file locations for the given repo root.
func StorePaths(repoRoot string) (storePath, lockPath string, err error) {
	commonDir, err := gitutil.CommonDir(repoRoot)
	if err != nil {
		return "", "", fmt.Errorf("get git common dir: %w", err)
	}
	dir := filepath.Join(commonDir, "kura", "seals")
	return filepath.Join(dir, "paths.json"), filepath.Join(dir, "paths.lock"), nil
}

// ReadStore reads and validates the seal store at path.
// A missing store is treated as an empty store.
func ReadStore(path string) (PathStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return PathStore{Paths: make(map[string]Entry)}, nil
		}
		return PathStore{}, fmt.Errorf("read seal store: %w", err)
	}
	if err := validateStoreJSON(data); err != nil {
		return PathStore{}, StoreValidationErr{Cause: fmt.Errorf("validate seal store %s: %w", path, err)}
	}
	var store PathStore
	if err := json.Unmarshal(data, &store); err != nil {
		return PathStore{}, fmt.Errorf("parse seal store: %w", err)
	}
	if store.Paths == nil {
		store.Paths = make(map[string]Entry)
	}
	return store, nil
}

// WriteStore serializes store to path atomically.
func WriteStore(path string, store PathStore) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create seal store dir: %w", err)
	}
	store.SchemaVersion = SchemaVersion
	if store.Paths == nil {
		store.Paths = make(map[string]Entry)
	}
	data, _ := json.Marshal(store)
	if err := validateStoreJSON(data); err != nil {
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

// validateStoreJSON checks that raw store JSON conforms to schema/seal_store.schema.json.
func validateStoreJSON(data []byte) error {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parse seal store: %w", err)
	}
	if err := storeSchema.Validate(inst); err != nil {
		return fmt.Errorf("seal store does not conform to schema: %w", err)
	}
	return nil
}
