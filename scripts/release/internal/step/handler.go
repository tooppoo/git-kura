package step

import (
	"encoding/json"

	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
)

// Handler defines the contract each release step must implement.
// BuildPayload runs during plan; Validate during validate; Preflight and Exec
// during exec (Preflight is called first and must succeed before Exec runs).
type Handler interface {
	// BuildPayload returns the step-specific JSON to embed in the plan payload.
	BuildPayload(version string) (json.RawMessage, error)

	// Validate checks whether the plan is ready to execute.
	// Returns (errors, warnings, internalErr). internalErr indicates an
	// unexpected failure; validation-specific issues go in errors/warnings.
	Validate(plan *schema.ReleasePlanEnvelope) (errs []string, warnings []string, err error)

	// Preflight is the final safety check before execution begins.
	// A non-nil return prevents Exec from running.
	Preflight(plan *schema.ReleasePlanEnvelope) error

	// Exec carries out the release step. It is only called after Preflight
	// succeeds and all safety-gate checks pass.
	Exec(plan *schema.ReleasePlanEnvelope) error
}

// DetailedValidator is an optional extension of Handler for steps that
// produce per-asset machine-readable data in the validate result StepData.
// The validate command checks for this interface via type assertion and, when
// present, uses ValidateWithData instead of Validate so that stepData is
// recorded in the result file.
type DetailedValidator interface {
	ValidateWithData(plan *schema.ReleasePlanEnvelope) (errs []string, warnings []string, stepData json.RawMessage, err error)
}
