package main

import "fmt"

// toolsContext carries the per-component inputs shared by status and uninstall.
// entry is the component's current metadata, or nil when it has none recorded.
type toolsContext struct {
	repoRoot  string
	commonDir string
	entry     *toolsMetadataEntry
}

// toolsInstallContext extends toolsContext with the verified release asset and
// the resolved release version available during install.
type toolsInstallContext struct {
	toolsContext
	releaseVersion string
	asset          *toolsAsset
}

// toolsOutcome is what a component returns from each operation: the result to
// report plus the metadata mutation to apply. setEntry upserts the component's
// metadata entry; deleteEntry removes it. A component reporting failed,
// skipped, or not-installed normally leaves metadata untouched (both fields
// zero), so the framework never records a change that did not happen.
type toolsOutcome struct {
	result      toolsResult
	setEntry    *toolsMetadataEntry
	deleteEntry bool
}

// toolsComponent is the contract every tool component implements. The framework
// owns argument parsing, metadata locking/persistence, asset resolution, and
// output; a component only decides what to do for its own files or config and
// returns a result plus an optional metadata mutation.
type toolsComponent interface {
	id() string
	status(ctx toolsContext) toolsOutcome
	install(ctx toolsInstallContext) toolsOutcome
	uninstall(ctx toolsContext) toolsOutcome
}

// toolsRegistry maps component IDs to implementations and preserves
// registration order so status over all components is deterministic.
type toolsRegistry struct {
	order      []string
	components map[string]toolsComponent
}

// newToolsRegistry builds a registry from the given components in order. It
// panics on a duplicate ID, which can only be a programming error in the
// registry wiring.
func newToolsRegistry(components ...toolsComponent) *toolsRegistry {
	r := &toolsRegistry{components: make(map[string]toolsComponent, len(components))}
	for _, c := range components {
		if _, exists := r.components[c.id()]; exists {
			panic(fmt.Sprintf("duplicate tools component ID %q", c.id()))
		}
		r.components[c.id()] = c
		r.order = append(r.order, c.id())
	}
	return r
}

// get returns the component for id.
func (r *toolsRegistry) get(id string) (toolsComponent, bool) {
	c, ok := r.components[id]
	return c, ok
}

// ids returns all registered component IDs in registration order.
func (r *toolsRegistry) ids() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// productionToolsRegistry is the registry exposed to users. It recognizes the
// three component IDs the framework ships with.
func productionToolsRegistry() *toolsRegistry {
	return newToolsRegistry(
		preCommitComponent{},
		newClaudeSkillComponent(),
		newCodexSkillComponent(),
	)
}

// pendingComponent is a production component whose concrete install/uninstall
// logic is deferred to a follow-up issue. It recognizes its ID and participates
// in the framework, but reports not-installed for status/uninstall and failed
// for install, pointing at the tracking issue.
type pendingComponent struct {
	componentID string
	trackingURL string
}

func newPendingComponent(id, trackingURL string) *pendingComponent {
	return &pendingComponent{componentID: id, trackingURL: trackingURL}
}

func (c *pendingComponent) id() string { return c.componentID }

func (c *pendingComponent) status(ctx toolsContext) toolsOutcome {
	return toolsOutcome{result: toolsResult{
		Component: c.componentID,
		Action:    actionNotInstalled,
		Managed:   false,
		Reason:    fmt.Sprintf("component implementation is not yet available (tracking: %s)", c.trackingURL),
	}}
}

func (c *pendingComponent) install(ctx toolsInstallContext) toolsOutcome {
	return toolsOutcome{result: toolsResult{
		Component:      c.componentID,
		ReleaseVersion: ctx.releaseVersion,
		Action:         actionFailed,
		Managed:        false,
		Reason:         fmt.Sprintf("component installation is not yet implemented (tracking: %s)", c.trackingURL),
	}}
}

func (c *pendingComponent) uninstall(ctx toolsContext) toolsOutcome {
	return toolsOutcome{result: toolsResult{
		Component: c.componentID,
		Action:    actionNotInstalled,
		Managed:   false,
		Reason:    fmt.Sprintf("component implementation is not yet available (tracking: %s)", c.trackingURL),
	}}
}
