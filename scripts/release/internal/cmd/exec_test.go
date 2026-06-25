package cmd_test

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tooppoo/git-kura/scripts/release/internal/cmd"
	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
	"github.com/tooppoo/git-kura/scripts/release/internal/step"
	"github.com/tooppoo/git-kura/scripts/release/internal/step/placeholder"
)

// failPreflightHandler is a Handler whose Preflight always returns an error.
// It tracks whether Exec was called so tests can assert it was not invoked.
type failPreflightHandler struct {
	execCalled bool
}

func (h *failPreflightHandler) BuildPayload(_ string) (json.RawMessage, error) {
	return json.Marshal(map[string]string{})
}
func (h *failPreflightHandler) Validate(_ *schema.ReleasePlanEnvelope) ([]string, []string, error) {
	return nil, nil, nil
}
func (h *failPreflightHandler) Preflight(_ *schema.ReleasePlanEnvelope) error {
	return errors.New("preflight: simulated failure")
}
func (h *failPreflightHandler) Exec(_ *schema.ReleasePlanEnvelope) error {
	h.execCalled = true
	return nil
}

func TestPreflightFailure_BlocksExec(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	version, stepName := "v0.0.1", "tag"
	handler := &failPreflightHandler{}
	registry := setupExecFixture(t, version, stepName, handler)

	execErr := cmd.ExecRelease(registry, version, stepName)

	if execErr == nil {
		t.Fatal("expected ExecRelease to fail when preflight returns an error, but it succeeded")
	}
	if handler.execCalled {
		t.Fatal("Exec was called despite preflight failure — safety gate must prevent execution")
	}
}

// setupExecFixture writes a valid plan.json and validate-result.json in the
// current working directory and returns a Registry with handler registered for
// stepName.
func setupExecFixture(t *testing.T, version, stepName string, handler step.Handler) *step.Registry {
	t.Helper()

	payload := schema.ReleasePlanPayload{
		TargetVersion: version,
		StepName:      stepName,
	}
	hash := testPayloadHash(t, payload)

	plan := schema.ReleasePlanEnvelope{
		SchemaVersion: schema.PlanSchemaVersion,
		Kind:          schema.PlanKind,
		PlanID:        "test-plan-id",
		PayloadHash:   hash,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Payload:       payload,
	}
	result := schema.ValidateResult{
		SchemaVersion: schema.ValidateSchemaVersion,
		Kind:          schema.ValidateKind,
		TargetVersion: version,
		StepName:      stepName,
		Status:        schema.ValidateStatusSuccess,
		Errors:        []string{},
		Warnings:      []string{},
		PlanID:        "test-plan-id",
		PayloadHash:   hash,
	}

	outDir := filepath.Join(".git-kura", "release", version, stepName)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	testWriteJSON(t, filepath.Join(outDir, "plan.json"), plan)
	testWriteJSON(t, filepath.Join(outDir, "validate-result.json"), result)

	r := placeholder.NewDefaultRegistry()
	s, err := step.Parse(stepName)
	if err != nil {
		t.Fatalf("parse step %q: %v", stepName, err)
	}
	r.Register(s, handler)
	return r
}

func testPayloadHash(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal payload for hash: %v", err)
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("sha256:%x", sum)
}

func testWriteJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
