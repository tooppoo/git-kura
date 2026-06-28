package schema

import "encoding/json"

const ValidateSchemaVersion = "1"

const ValidateKind = "ValidateResult"

type ValidateStatus string

const (
	ValidateStatusSuccess ValidateStatus = "success"
	ValidateStatusFailure ValidateStatus = "failure"
)

// ValidateResult is the machine-readable output of the validate command.
// It records which plan was validated (via PlanID and PayloadHash) so exec can
// detect stale or tampered plans before running.
// StepData carries step-specific per-asset results when a handler implements
// step.DetailedValidator; it is omitted for handlers that do not.
type ValidateResult struct {
	SchemaVersion string          `json:"schemaVersion"`
	Kind          string          `json:"kind"`
	TargetVersion string          `json:"targetVersion"`
	StepName      string          `json:"stepName"`
	Status        ValidateStatus  `json:"status"`
	Errors        []string        `json:"errors"`
	Warnings      []string        `json:"warnings"`
	PlanID        string          `json:"planId"`
	PayloadHash   string          `json:"payloadHash"`
	StepData      json.RawMessage `json:"stepData,omitempty"`
}
