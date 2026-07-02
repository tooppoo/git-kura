package placeholder_test

import (
	"testing"

	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
	"github.com/tooppoo/git-kura/scripts/release/internal/step"
	"github.com/tooppoo/git-kura/scripts/release/internal/step/placeholder"
)

func TestBuildPayload_ReturnsValidJSON(t *testing.T) {
	h := placeholder.New(step.StepTag)
	data, err := h.BuildPayload("v1.0.0")
	if err != nil {
		t.Fatalf("BuildPayload returned error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("BuildPayload returned empty payload")
	}
}

func TestValidate_AlwaysSucceeds(t *testing.T) {
	h := placeholder.New(step.StepTag)
	errs, warnings, err := h.Validate(&schema.ReleasePlanEnvelope{})
	if err != nil {
		t.Fatalf("Validate returned unexpected internal error: %v", err)
	}
	if len(errs) > 0 {
		t.Fatalf("Validate returned unexpected errors: %v", errs)
	}
	if len(warnings) > 0 {
		t.Fatalf("Validate returned unexpected warnings: %v", warnings)
	}
}

func TestPreflight_AlwaysSucceeds(t *testing.T) {
	h := placeholder.New(step.StepTag)
	if err := h.Preflight(&schema.ReleasePlanEnvelope{}); err != nil {
		t.Fatalf("Preflight returned unexpected error: %v", err)
	}
}

func TestExec_NoSideEffects(t *testing.T) {
	h := placeholder.New(step.StepTag)
	if err := h.Exec(&schema.ReleasePlanEnvelope{}); err != nil {
		t.Fatalf("Exec returned unexpected error: %v", err)
	}
}

func TestNewDefaultRegistry_ContainsAllKnownSteps(t *testing.T) {
	r := placeholder.NewDefaultRegistry()
	for _, s := range []step.Step{step.StepTag, step.StepReleaseAsset} {
		if _, err := r.Get(s); err != nil {
			t.Errorf("registry missing handler for step %q: %v", s, err)
		}
	}
}
