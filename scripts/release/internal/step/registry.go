package step

import "fmt"

// Registry maps each known Step to its Handler.
type Registry struct {
	handlers map[Step]Handler
}

// NewRegistry returns an empty Registry. Use Register to populate it.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[Step]Handler)}
}

// Register associates handler with s.
func (r *Registry) Register(s Step, h Handler) {
	r.handlers[s] = h
}

// Get returns the Handler for s, or an error if no handler is registered.
func (r *Registry) Get(s Step) (Handler, error) {
	h, ok := r.handlers[s]
	if !ok {
		return nil, fmt.Errorf("no handler registered for step %q", s)
	}
	return h, nil
}
