package schema

import "encoding/json"

const PlanSchemaVersion = "1"

const PlanKind = "ReleasePlan"

// ReleasePlanEnvelope is the top-level plan file written by the plan command.
// It wraps a step-specific Payload with metadata for integrity checking.
type ReleasePlanEnvelope struct {
	SchemaVersion string             `json:"schemaVersion"`
	Kind          string             `json:"kind"`
	PlanID        string             `json:"planId"`
	PayloadHash   string             `json:"payloadHash"`
	GeneratedAt   string             `json:"generatedAt"`
	Payload       ReleasePlanPayload `json:"payload"`
}

// ReleasePlanPayload is the content hashed for payloadHash and inspected by
// validate / exec. StepData carries step-specific fields added by each handler.
type ReleasePlanPayload struct {
	TargetVersion string          `json:"targetVersion"`
	StepName      string          `json:"stepName"`
	StepData      json.RawMessage `json:"stepData,omitempty"`
}
