package job

import (
	"fmt"
	"sync"
)

// Registry maps job type names to their handlers.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewRegistry creates an empty job registry.
func NewRegistry() *Registry {
	return &Registry{
		handlers: make(map[string]Handler),
	}
}

// Register adds a handler for a job type.
func (r *Registry) Register(typeName string, handler Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[typeName] = handler
}

// RegisterFunc adds a function handler for a job type.
func (r *Registry) RegisterFunc(typeName string, fn HandlerFunc) {
	r.Register(typeName, fn)
}

// Get returns the handler for a job type.
func (r *Registry) Get(typeName string) (Handler, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	h, ok := r.handlers[typeName]
	if !ok {
		return nil, fmt.Errorf("no handler registered for job type %q", typeName)
	}
	return h, nil
}

// Types returns all registered job type names.
func (r *Registry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.handlers))
	for t := range r.handlers {
		types = append(types, t)
	}
	return types
}
