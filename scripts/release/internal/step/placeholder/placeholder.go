// Package placeholder provides no-op release step handlers used only for
// framework testing in issue #107.
//
// IMPORTANT: Every handler in this package is a temporary placeholder.
// When the real handler for a step is implemented (tag → #108, etc.), that
// implementation must replace the corresponding placeholder, and the
// placeholder must be deleted at that time.
// Do NOT treat these handlers as permanent release operation handlers.
package placeholder

import (
	"encoding/json"

	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
	"github.com/tooppoo/git-kura/scripts/release/internal/step"
)

// Handler is a no-op implementation of step.Handler.
// It has no external side effects and is safe to run in any environment.
type Handler struct {
	s step.Step
}

// New returns a placeholder Handler for the given step.
func New(s step.Step) *Handler {
	return &Handler{s: s}
}

// BuildPayload returns a minimal JSON payload with no step-specific fields.
func (h *Handler) BuildPayload(_ string) (json.RawMessage, error) {
	return json.Marshal(map[string]string{"note": "placeholder — replace with real handler"})
}

// Validate performs no checks and always succeeds.
func (h *Handler) Validate(_ *schema.ReleasePlanEnvelope) ([]string, []string, error) {
	return nil, nil, nil
}

// Preflight performs no pre-exec checks and always succeeds.
func (h *Handler) Preflight(_ *schema.ReleasePlanEnvelope) error {
	return nil
}

// Exec is a no-op. It has no external side effects.
func (h *Handler) Exec(_ *schema.ReleasePlanEnvelope) error {
	return nil
}

// NewDefaultRegistry returns a Registry pre-populated with placeholder
// handlers for every known step. Used to wire up the release command framework.
func NewDefaultRegistry() *step.Registry {
	r := step.NewRegistry()
	r.Register(step.StepTag, New(step.StepTag))
	r.Register(step.StepReleaseAsset, New(step.StepReleaseAsset))
	return r
}
