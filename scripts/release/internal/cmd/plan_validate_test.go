package cmd_test

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/tooppoo/git-kura/scripts/release/internal/cmd"
	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
	"github.com/tooppoo/git-kura/scripts/release/internal/step"
	"github.com/tooppoo/git-kura/scripts/release/internal/step/placeholder"
)

// chdir sets the working directory to dir and restores the original on cleanup.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestNewRootCommand_Plan_Success(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	root := cmd.NewRootCommand(placeholder.NewDefaultRegistry())
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"plan", "--version", "v0.0.1", "--step", "tag"})
	if err := root.Execute(); err != nil {
		t.Fatalf("plan command failed: %v", err)
	}

	planPath := filepath.Join(".git-kura", "release", "v0.0.1", "tag", "plan.json")
	b, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("plan file not found: %v", err)
	}

	var envelope schema.ReleasePlanEnvelope
	if err := json.Unmarshal(b, &envelope); err != nil {
		t.Fatalf("parse plan file: %v", err)
	}
	if envelope.SchemaVersion != schema.PlanSchemaVersion {
		t.Errorf("schemaVersion = %q, want %q", envelope.SchemaVersion, schema.PlanSchemaVersion)
	}
	if envelope.Kind != schema.PlanKind {
		t.Errorf("kind = %q, want %q", envelope.Kind, schema.PlanKind)
	}
	if envelope.PlanID == "" {
		t.Error("planId must not be empty")
	}
	if envelope.PayloadHash == "" {
		t.Error("payloadHash must not be empty")
	}
	if envelope.Payload.TargetVersion != "v0.0.1" {
		t.Errorf("targetVersion = %q, want v0.0.1", envelope.Payload.TargetVersion)
	}
	if envelope.Payload.StepName != "tag" {
		t.Errorf("stepName = %q, want tag", envelope.Payload.StepName)
	}
}

func TestNewRootCommand_Plan_InvalidVersion(t *testing.T) {
	chdir(t, t.TempDir())
	root := cmd.NewRootCommand(placeholder.NewDefaultRegistry())
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"plan", "--version", "1.0.0", "--step", "tag"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error for invalid version, got nil")
	}
}

func TestNewRootCommand_Plan_UnknownStep(t *testing.T) {
	chdir(t, t.TempDir())
	root := cmd.NewRootCommand(placeholder.NewDefaultRegistry())
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"plan", "--version", "v0.0.1", "--step", "unknown-step"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error for unknown step, got nil")
	}
}

func TestNewRootCommand_Validate_Success(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	r := placeholder.NewDefaultRegistry()

	root := cmd.NewRootCommand(r)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"plan", "--version", "v0.0.1", "--step", "scoop"})
	if err := root.Execute(); err != nil {
		t.Fatalf("plan command failed: %v", err)
	}

	root.SetArgs([]string{"validate", "--version", "v0.0.1", "--step", "scoop"})
	if err := root.Execute(); err != nil {
		t.Fatalf("validate command failed: %v", err)
	}

	resultPath := filepath.Join(".git-kura", "release", "v0.0.1", "scoop", "validate-result.json")
	b, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("validate result file not found: %v", err)
	}

	var result schema.ValidateResult
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("parse validate result: %v", err)
	}
	if result.Status != schema.ValidateStatusSuccess {
		t.Errorf("status = %q, want %q", result.Status, schema.ValidateStatusSuccess)
	}
	if result.TargetVersion != "v0.0.1" {
		t.Errorf("targetVersion = %q, want v0.0.1", result.TargetVersion)
	}
	if result.StepName != "scoop" {
		t.Errorf("stepName = %q, want scoop", result.StepName)
	}
	if result.PlanID == "" {
		t.Error("planId must not be empty in validate result")
	}
	if result.PayloadHash == "" {
		t.Error("payloadHash must not be empty in validate result")
	}
	if result.Errors == nil {
		t.Error("errors field must be an empty slice, not nil")
	}
	if result.Warnings == nil {
		t.Error("warnings field must be an empty slice, not nil")
	}
}

func TestNewRootCommand_Validate_FailureWritesResult(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	r := placeholder.NewDefaultRegistry()
	r.Register(step.StepTag, &alwaysFailHandler{})

	root := cmd.NewRootCommand(r)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	root.SetArgs([]string{"plan", "--version", "v0.0.2", "--step", "tag"})
	if err := root.Execute(); err != nil {
		t.Fatalf("plan command failed: %v", err)
	}

	root.SetArgs([]string{"validate", "--version", "v0.0.2", "--step", "tag"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected validate to fail for step with errors, got nil")
	}

	resultPath := filepath.Join(".git-kura", "release", "v0.0.2", "tag", "validate-result.json")
	b, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("validate result file must exist even on failure: %v", err)
	}

	var result schema.ValidateResult
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("parse validate result: %v", err)
	}
	if result.Status != schema.ValidateStatusFailure {
		t.Errorf("status = %q, want %q", result.Status, schema.ValidateStatusFailure)
	}
	if len(result.Errors) == 0 {
		t.Error("expected at least one error in validate result")
	}
}

func TestNewRootCommand_Validate_InvalidVersion(t *testing.T) {
	chdir(t, t.TempDir())
	root := cmd.NewRootCommand(placeholder.NewDefaultRegistry())
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"validate", "--version", "bad", "--step", "tag"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error for invalid version")
	}
}

func TestNewRootCommand_Validate_UnknownStep(t *testing.T) {
	chdir(t, t.TempDir())
	root := cmd.NewRootCommand(placeholder.NewDefaultRegistry())
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"validate", "--version", "v0.0.1", "--step", "noop"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error for unknown step")
	}
}

func TestExecRelease_SafetyGate_UnsupportedSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	version, stepName := "v0.0.1", "tag"
	tracker := &noopExecTracker{}
	registry := setupExecFixture(t, version, stepName, tracker)

	planPath := filepath.Join(".git-kura", "release", version, stepName, "plan.json")
	b, _ := os.ReadFile(planPath)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	m["schemaVersion"] = "999"
	out, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(planPath, out, 0o644)

	if err := cmd.ExecRelease(registry, version, stepName); err == nil {
		t.Fatal("expected error for unsupported schemaVersion")
	}
	if tracker.execCalled {
		t.Fatal("Exec must not be called when schemaVersion is unsupported")
	}
}

func TestExecRelease_SafetyGate_StatusNotSuccess(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	version, stepName := "v0.0.1", "tag"
	tracker := &noopExecTracker{}
	registry := setupExecFixture(t, version, stepName, tracker)

	resultPath := filepath.Join(".git-kura", "release", version, stepName, "validate-result.json")
	b, _ := os.ReadFile(resultPath)
	var result schema.ValidateResult
	_ = json.Unmarshal(b, &result)
	result.Status = schema.ValidateStatusFailure
	out, _ := json.MarshalIndent(result, "", "  ")
	_ = os.WriteFile(resultPath, out, 0o644)

	if err := cmd.ExecRelease(registry, version, stepName); err == nil {
		t.Fatal("expected error when validate status is failure")
	}
	if tracker.execCalled {
		t.Fatal("Exec must not be called when validate status is failure")
	}
}

func TestExecRelease_SafetyGate_VersionMismatch(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	version, stepName := "v0.0.1", "tag"
	tracker := &noopExecTracker{}
	registry := setupExecFixture(t, version, stepName, tracker)

	resultPath := filepath.Join(".git-kura", "release", version, stepName, "validate-result.json")
	b, _ := os.ReadFile(resultPath)
	var result schema.ValidateResult
	_ = json.Unmarshal(b, &result)
	result.TargetVersion = "v9.9.9"
	out, _ := json.MarshalIndent(result, "", "  ")
	_ = os.WriteFile(resultPath, out, 0o644)

	if err := cmd.ExecRelease(registry, version, stepName); err == nil {
		t.Fatal("expected error on version mismatch")
	}
}

func TestExecRelease_SafetyGate_StepMismatch(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	version, stepName := "v0.0.1", "tag"
	tracker := &noopExecTracker{}
	registry := setupExecFixture(t, version, stepName, tracker)

	resultPath := filepath.Join(".git-kura", "release", version, stepName, "validate-result.json")
	b, _ := os.ReadFile(resultPath)
	var result schema.ValidateResult
	_ = json.Unmarshal(b, &result)
	result.StepName = "scoop"
	out, _ := json.MarshalIndent(result, "", "  ")
	_ = os.WriteFile(resultPath, out, 0o644)

	if err := cmd.ExecRelease(registry, version, stepName); err == nil {
		t.Fatal("expected error on step mismatch")
	}
}

func TestExecRelease_SafetyGate_PayloadHashMismatch(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	version, stepName := "v0.0.1", "tag"
	tracker := &noopExecTracker{}
	registry := setupExecFixture(t, version, stepName, tracker)

	resultPath := filepath.Join(".git-kura", "release", version, stepName, "validate-result.json")
	b, _ := os.ReadFile(resultPath)
	var result schema.ValidateResult
	_ = json.Unmarshal(b, &result)
	result.PayloadHash = "sha256:000000"
	out, _ := json.MarshalIndent(result, "", "  ")
	_ = os.WriteFile(resultPath, out, 0o644)

	if err := cmd.ExecRelease(registry, version, stepName); err == nil {
		t.Fatal("expected error on payloadHash mismatch")
	}
}

func TestExecRelease_NoPlanFile(t *testing.T) {
	chdir(t, t.TempDir())
	r := placeholder.NewDefaultRegistry()
	if err := cmd.ExecRelease(r, "v0.0.1", "tag"); err == nil {
		t.Fatal("expected error when plan file does not exist")
	}
}

func TestNewRootCommand_Exec_InvalidVersion(t *testing.T) {
	chdir(t, t.TempDir())
	root := cmd.NewRootCommand(placeholder.NewDefaultRegistry())
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"exec", "--version", "bad", "--step", "tag"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error for invalid version")
	}
}

func TestNewRootCommand_Exec_UnknownStep(t *testing.T) {
	chdir(t, t.TempDir())
	root := cmd.NewRootCommand(placeholder.NewDefaultRegistry())
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"exec", "--version", "v0.0.1", "--step", "noop"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error for unknown step")
	}
}

func TestExecRelease_SafetyGate_PlanPayloadTampered(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	version, stepName := "v0.0.1", "tag"
	tracker := &noopExecTracker{}
	registry := setupExecFixture(t, version, stepName, tracker)

	// Change the payload in plan.json without updating payloadHash.
	planPath := filepath.Join(".git-kura", "release", version, stepName, "plan.json")
	b, _ := os.ReadFile(planPath)
	var envelope schema.ReleasePlanEnvelope
	_ = json.Unmarshal(b, &envelope)
	envelope.Payload.StepData = json.RawMessage(`{"tampered":true}`)
	// Deliberately do NOT recompute payloadHash — this simulates payload tampering.
	out, _ := json.MarshalIndent(envelope, "", "  ")
	_ = os.WriteFile(planPath, out, 0o644)

	if err := cmd.ExecRelease(registry, version, stepName); err == nil {
		t.Fatal("expected error when plan payload was tampered without updating payloadHash")
	}
	if tracker.execCalled {
		t.Fatal("Exec must not be called when plan payload has been tampered")
	}
}

func TestExecRelease_SafetyGate_ResultUnsupportedSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	version, stepName := "v0.0.1", "tag"
	tracker := &noopExecTracker{}
	registry := setupExecFixture(t, version, stepName, tracker)

	resultPath := filepath.Join(".git-kura", "release", version, stepName, "validate-result.json")
	b, _ := os.ReadFile(resultPath)
	var result schema.ValidateResult
	_ = json.Unmarshal(b, &result)
	result.SchemaVersion = "999"
	out, _ := json.MarshalIndent(result, "", "  ")
	_ = os.WriteFile(resultPath, out, 0o644)

	if err := cmd.ExecRelease(registry, version, stepName); err == nil {
		t.Fatal("expected error for unsupported validate result schemaVersion")
	}
	if tracker.execCalled {
		t.Fatal("Exec must not be called when result schemaVersion is unsupported")
	}
}

func TestExecRelease_SafetyGate_ResultWrongKind(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	version, stepName := "v0.0.1", "tag"
	tracker := &noopExecTracker{}
	registry := setupExecFixture(t, version, stepName, tracker)

	resultPath := filepath.Join(".git-kura", "release", version, stepName, "validate-result.json")
	b, _ := os.ReadFile(resultPath)
	var result schema.ValidateResult
	_ = json.Unmarshal(b, &result)
	result.Kind = "UnexpectedKind"
	out, _ := json.MarshalIndent(result, "", "  ")
	_ = os.WriteFile(resultPath, out, 0o644)

	if err := cmd.ExecRelease(registry, version, stepName); err == nil {
		t.Fatal("expected error for wrong validate result kind")
	}
	if tracker.execCalled {
		t.Fatal("Exec must not be called when result kind is wrong")
	}
}

func TestNewRootCommand_Validate_TamperedPayload(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	r := placeholder.NewDefaultRegistry()
	root := cmd.NewRootCommand(r)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	root.SetArgs([]string{"plan", "--version", "v0.0.1", "--step", "winget"})
	if err := root.Execute(); err != nil {
		t.Fatalf("plan command failed: %v", err)
	}

	// Tamper the payload without updating payloadHash.
	planPath := filepath.Join(".git-kura", "release", "v0.0.1", "winget", "plan.json")
	b, _ := os.ReadFile(planPath)
	var envelope schema.ReleasePlanEnvelope
	_ = json.Unmarshal(b, &envelope)
	envelope.Payload.StepData = json.RawMessage(`{"injected":"data"}`)
	out, _ := json.MarshalIndent(envelope, "", "  ")
	_ = os.WriteFile(planPath, out, 0o644)

	root.SetArgs([]string{"validate", "--version", "v0.0.1", "--step", "winget"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected validate to fail when plan payload has been tampered")
	}
}

// noopExecTracker is a step.Handler that succeeds at everything and records
// whether Exec was called.
type noopExecTracker struct {
	execCalled bool
}

func (h *noopExecTracker) BuildPayload(_ string) (json.RawMessage, error) {
	return json.Marshal(map[string]string{})
}
func (h *noopExecTracker) Validate(_ *schema.ReleasePlanEnvelope) ([]string, []string, error) {
	return nil, nil, nil
}
func (h *noopExecTracker) Preflight(_ *schema.ReleasePlanEnvelope) error { return nil }
func (h *noopExecTracker) Exec(_ *schema.ReleasePlanEnvelope) error {
	h.execCalled = true
	return nil
}

// alwaysFailHandler is a step.Handler whose Validate always returns errors.
type alwaysFailHandler struct{}

func (h *alwaysFailHandler) BuildPayload(_ string) (json.RawMessage, error) {
	return json.Marshal(map[string]string{})
}
func (h *alwaysFailHandler) Validate(_ *schema.ReleasePlanEnvelope) ([]string, []string, error) {
	return []string{"simulated validation error"}, nil, nil
}
func (h *alwaysFailHandler) Preflight(_ *schema.ReleasePlanEnvelope) error { return nil }
func (h *alwaysFailHandler) Exec(_ *schema.ReleasePlanEnvelope) error      { return nil }
