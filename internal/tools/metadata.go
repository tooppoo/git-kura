package tools

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/tooppoo/git-kura/internal/gitutil"
)

const metadataSchemaVersion = 1

// ManagedModeFile and ManagedModeConfig are the managed modes recorded in
// metadata.
const (
	ManagedModeFile   = "file"
	ManagedModeConfig = "config"
)

//go:embed schema/tools_metadata.schema.json
var toolsMetadataSchemaJSON []byte

var (
	metadataSchemaOnce sync.Once
	metadataSchema     *jsonschema.Schema
	metadataSchemaErr  error
)

func getMetadataSchema() (*jsonschema.Schema, error) {
	metadataSchemaOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(toolsMetadataSchemaJSON))
		if err != nil {
			metadataSchemaErr = fmt.Errorf("parse tools metadata schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource("tools_metadata.schema.json", doc); err != nil {
			metadataSchemaErr = fmt.Errorf("add tools metadata schema resource: %w", err)
			return
		}
		sch, err := c.Compile("tools_metadata.schema.json")
		if err != nil {
			metadataSchemaErr = fmt.Errorf("compile tools metadata schema: %w", err)
			return
		}
		metadataSchema = sch
	})
	return metadataSchema, metadataSchemaErr
}

func validateMetadataJSON(data []byte) error {
	sch, err := getMetadataSchema()
	if err != nil {
		return err
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parse tools metadata: %w", err)
	}
	if err := sch.Validate(inst); err != nil {
		return fmt.Errorf("tools metadata does not conform to schema: %w", err)
	}
	return nil
}

// MetadataEntry is the per-component record persisted across install/uninstall/status.
type MetadataEntry struct {
	Component         string         `json:"component"`
	SourceAssetID     string         `json:"sourceAssetId,omitempty"`
	ReleaseVersion    string         `json:"releaseVersion,omitempty"`
	ReleaseAssetName  string         `json:"releaseAssetName,omitempty"`
	DestinationPath   string         `json:"destinationPath,omitempty"`
	InstalledVersion  string         `json:"installedVersion,omitempty"`
	Checksum          string         `json:"checksum,omitempty"`
	ManagedMode       string         `json:"managedMode"`
	ComponentMetadata map[string]any `json:"componentMetadata,omitempty"`
	CreatedAt         string         `json:"createdAt"`
	UpdatedAt         string         `json:"updatedAt"`
}

// MetadataStore is the on-disk record at <git-common-dir>/kura/tools/installed.json.
type MetadataStore struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Components    map[string]MetadataEntry `json:"components"`
}

// MetadataPaths returns the metadata store file and lock file locations for repoRoot.
func MetadataPaths(repoRoot string) (storePath, lockPath string, err error) {
	commonDir, err := gitutil.CommonDir(repoRoot)
	if err != nil {
		return "", "", fmt.Errorf("get git common dir: %w", err)
	}
	dir := filepath.Join(commonDir, "kura", "tools")
	return filepath.Join(dir, "installed.json"), filepath.Join(dir, "installed.lock"), nil
}

// ReadMetadata loads the metadata store. An absent store is treated as empty.
func ReadMetadata(path string) (MetadataStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return MetadataStore{SchemaVersion: metadataSchemaVersion, Components: make(map[string]MetadataEntry)}, nil
		}
		return MetadataStore{}, fmt.Errorf("read tools metadata: %w", err)
	}
	if err := validateMetadataJSON(data); err != nil {
		return MetadataStore{}, fmt.Errorf("read tools metadata %s: %w", path, err)
	}
	var store MetadataStore
	if err := json.Unmarshal(data, &store); err != nil {
		return MetadataStore{}, fmt.Errorf("parse tools metadata: %w", err)
	}
	if store.Components == nil {
		store.Components = make(map[string]MetadataEntry)
	}
	return store, nil
}

// WriteMetadata atomically writes the metadata store.
func WriteMetadata(path string, store MetadataStore) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create tools metadata dir: %w", err)
	}
	store.SchemaVersion = metadataSchemaVersion
	if store.Components == nil {
		store.Components = make(map[string]MetadataEntry)
	}
	data, _ := json.Marshal(store)
	if err := validateMetadataJSON(data); err != nil {
		return fmt.Errorf("refusing to write tools metadata: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tools metadata: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return errors.Join(fmt.Errorf("commit tools metadata: %w", err), os.Remove(tmp))
	}
	return nil
}

// ErrLockTimeout is returned when the tools metadata lock cannot be acquired
// within the deadline.
var ErrLockTimeout = errors.New("tools-metadata-lock-timeout")

const lockInterval = 100 * time.Millisecond

// acquireLock takes the tools metadata lock with O_CREATE|O_EXCL strategy.
// The returned release function returns an error if the lock file cannot be removed.
func acquireLock(lockPath string, timeout time.Duration) (release func() error, err error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create tools metadata dir: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return func() error {
				if removeErr := os.Remove(lockPath); removeErr != nil && !os.IsNotExist(removeErr) {
					return fmt.Errorf("failed to release tools metadata lock %s: %w\nremove the file manually or subsequent tools commands will time out", lockPath, removeErr)
				}
				return nil
			}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("acquire tools metadata lock: %w", err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: failed to acquire tools metadata lock after %s", ErrLockTimeout, timeout)
		}
		time.Sleep(lockInterval)
	}
}
