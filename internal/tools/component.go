package tools

import "fmt"

// Context carries the per-component inputs shared by status and uninstall.
type Context struct {
	RepoRoot  string
	CommonDir string
	Entry     *MetadataEntry
}

// InstallContext extends Context with the verified release asset and resolved
// release version available during install.
type InstallContext struct {
	Context
	ReleaseVersion string
	Asset          *Asset
}

// Outcome is what a component returns from each operation: the result to report
// plus the metadata mutation to apply. SetEntry upserts the component's metadata
// entry; DeleteEntry removes it.
type Outcome struct {
	Result      Result
	SetEntry    *MetadataEntry
	DeleteEntry bool
}

// Component is the contract every tool component implements.
type Component interface {
	ID() string
	Status(ctx Context) Outcome
	Install(ctx InstallContext) Outcome
	Uninstall(ctx Context) Outcome
}

// Registry maps component IDs to implementations and preserves registration
// order so status over all components is deterministic.
type Registry struct {
	order      []string
	components map[string]Component
}

// NewRegistry builds a registry from the given components in order.
// It returns an error on a duplicate component ID.
func NewRegistry(components ...Component) (*Registry, error) {
	r := &Registry{components: make(map[string]Component, len(components))}
	for _, c := range components {
		if _, exists := r.components[c.ID()]; exists {
			return nil, fmt.Errorf("duplicate tools component ID %q", c.ID())
		}
		r.components[c.ID()] = c
		r.order = append(r.order, c.ID())
	}
	return r, nil
}

// Get returns the component for id.
func (r *Registry) Get(id string) (Component, bool) {
	c, ok := r.components[id]
	return c, ok
}

// IDs returns all registered component IDs in registration order.
func (r *Registry) IDs() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// PendingComponent is a component whose concrete install/uninstall logic is
// deferred to a follow-up issue. It recognises its ID and participates in the
// framework, but reports not-installed for status/uninstall and failed for
// install, pointing at the tracking issue.
type PendingComponent struct {
	ComponentID string
	TrackingURL string
}

func (c PendingComponent) ID() string { return c.ComponentID }

func (c PendingComponent) Status(_ Context) Outcome {
	return Outcome{Result: Result{
		Component: c.ComponentID,
		Action:    ActionNotInstalled,
		Managed:   false,
		Reason:    fmt.Sprintf("component implementation is not yet available (tracking: %s)", c.TrackingURL),
	}}
}

func (c PendingComponent) Install(ctx InstallContext) Outcome {
	return Outcome{Result: Result{
		Component:      c.ComponentID,
		ReleaseVersion: ctx.ReleaseVersion,
		Action:         ActionFailed,
		Managed:        false,
		Reason:         fmt.Sprintf("component installation is not yet implemented (tracking: %s)", c.TrackingURL),
	}}
}

func (c PendingComponent) Uninstall(_ Context) Outcome {
	return Outcome{Result: Result{
		Component: c.ComponentID,
		Action:    ActionNotInstalled,
		Managed:   false,
		Reason:    fmt.Sprintf("component implementation is not yet available (tracking: %s)", c.TrackingURL),
	}}
}
