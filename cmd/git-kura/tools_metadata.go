package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/tooppoo/git-kura/internal/gitutil"
)

const toolsMetadataSchemaVersion = 1

// Managed modes recorded in metadata. A file-managed component owns a file on
// disk; a config-managed component owns a git config value.
const (
	managedModeFile   = "file"
	managedModeConfig = "config"
)

//go:embed schema/tools_metadata.schema.json
var toolsMetadataSchemaJSON []byte

var toolsMetadataSchema = mustCompileToolsMetadataSchema()

func mustCompileToolsMetadataSchema() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(toolsMetadataSchemaJSON))
	if err != nil {
		panic(fmt.Sprintf("parse tools metadata schema: %v", err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("tools_metadata.schema.json", doc); err != nil {
		panic(fmt.Sprintf("add tools metadata schema resource: %v", err))
	}
	sch, err := c.Compile("tools_metadata.schema.json")
	if err != nil {
		panic(fmt.Sprintf("compile tools metadata schema: %v", err))
	}
	return sch
}

func validateToolsMetadataJSON(data []byte) error {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parse tools metadata: %w", err)
	}
	if err := toolsMetadataSchema.Validate(inst); err != nil {
		return fmt.Errorf("tools metadata does not conform to schema: %w", err)
	}
	return nil
}

// toolsMetadataEntry is the per-component record persisted across install /
// uninstall / status. componentMetadata is an opaque blob a component may use
// for its own bookkeeping; the framework never interprets it.
type toolsMetadataEntry struct {
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

// toolsMetadataStore is the on-disk record at
// <git-common-dir>/kura/tools/installed.json. Components maps each installed
// component ID to its entry.
type toolsMetadataStore struct {
	SchemaVersion int                           `json:"schemaVersion"`
	Components    map[string]toolsMetadataEntry `json:"components"`
}

// toolsMetadataPaths returns the metadata store file and lock file locations
// for repoRoot.
func toolsMetadataPaths(repoRoot string) (storePath, lockPath string, err error) {
	commonDir, err := gitutil.CommonDir(repoRoot)
	if err != nil {
		return "", "", fmt.Errorf("get git common dir: %w", err)
	}
	dir := filepath.Join(commonDir, "kura", "tools")
	return filepath.Join(dir, "installed.json"), filepath.Join(dir, "installed.lock"), nil
}

// readToolsMetadata loads the metadata store. An absent store is treated as
// empty. A store that cannot be read, parsed, or validated against the schema
// (including an unsupported schemaVersion) is returned as an error so callers
// can refuse destructive operations rather than act on corrupt metadata.
func readToolsMetadata(path string) (toolsMetadataStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return toolsMetadataStore{SchemaVersion: toolsMetadataSchemaVersion, Components: make(map[string]toolsMetadataEntry)}, nil
		}
		return toolsMetadataStore{}, fmt.Errorf("read tools metadata: %w", err)
	}
	// Validate before unmarshalling so a hand-edited or corrupted store is
	// rejected (the schema pins schemaVersion to the supported value) instead
	// of being silently coerced into the Go struct.
	if err := validateToolsMetadataJSON(data); err != nil {
		return toolsMetadataStore{}, fmt.Errorf("read tools metadata %s: %w", path, err)
	}
	var store toolsMetadataStore
	if err := json.Unmarshal(data, &store); err != nil {
		return toolsMetadataStore{}, fmt.Errorf("parse tools metadata: %w", err)
	}
	if store.Components == nil {
		store.Components = make(map[string]toolsMetadataEntry)
	}
	return store, nil
}

// writeToolsMetadata atomically writes the metadata store: it validates against
// the schema, writes to a temp file, then renames into place so a reader never
// observes a partial store.
func writeToolsMetadata(path string, store toolsMetadataStore) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create tools metadata dir: %w", err)
	}
	store.SchemaVersion = toolsMetadataSchemaVersion
	if store.Components == nil {
		store.Components = make(map[string]toolsMetadataEntry)
	}
	data, _ := json.Marshal(store)
	if err := validateToolsMetadataJSON(data); err != nil {
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

const toolsLockInterval = 100 * time.Millisecond

// acquireToolsLock takes the tools metadata lock with the same O_CREATE|O_EXCL
// strategy as the seal store lock. install/uninstall hold this lock across the
// whole metadata read-modify-write so concurrent invocations cannot corrupt the
// store. A lock that cannot be acquired before timeout fails with
// seal-lock-timeout (exit code 5) and the caller must make no metadata, file, or
// config changes. A zero timeout makes a single attempt with no retry.
func acquireToolsLock(lockPath string, timeout time.Duration) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create tools metadata dir: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return func() {
				if removeErr := os.Remove(lockPath); removeErr != nil && !os.IsNotExist(removeErr) {
					fmt.Fprintf(os.Stderr,
						"warning: failed to release tools metadata lock %s: %v\nremove the file manually or subsequent tools commands will time out\n",
						lockPath, removeErr)
				}
			}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("acquire tools metadata lock: %w", err)
		}
		if time.Now().After(deadline) {
			return nil, exitCodeError(exitSealLockTimeout,
				fmt.Errorf("tools-metadata-lock-timeout: failed to acquire tools metadata lock after %s", timeout))
		}
		time.Sleep(toolsLockInterval)
	}
}
