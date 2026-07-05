package agent

import (
	"fmt"
	"strings"
)

// Registry maps runtime kind names to Runtime implementations.
// It is populated at composition root and is read-only during execution.
type Registry struct {
	runtimes    map[string]Runtime
	defaultKind string
}

// NewRegistry creates a new Registry.
func NewRegistry() *Registry {
	return &Registry{
		runtimes: make(map[string]Runtime),
	}
}

// Register adds a Runtime implementation to the registry.
// name is normalized to lowercase before storage.
func (r *Registry) Register(name string, rt Runtime) {
	if r == nil || rt == nil {
		return
	}
	name = normalizeKind(name)
	if name == "" {
		return
	}
	r.runtimes[name] = rt
}

// SetDefault sets the default runtime kind returned when Resolve receives an empty kind.
func (r *Registry) SetDefault(kind string) {
	if r == nil {
		return
	}
	r.defaultKind = normalizeKind(kind)
}

// Resolve returns the Runtime for the given kind.
// If kind is empty, the default is returned.
func (r *Registry) Resolve(kind string) (Runtime, error) {
	rt, _, err := r.ResolveWithKind(kind)
	return rt, err
}

// ResolveWithKind returns the Runtime and the canonical kind used for lookup.
// If kind is empty, the configured default kind is returned.
func (r *Registry) ResolveWithKind(kind string) (Runtime, string, error) {
	if r == nil {
		return nil, "", fmt.Errorf("registry is nil")
	}
	kind = normalizeKind(kind)
	if kind == "" {
		kind = r.defaultKind
	}
	rt, ok := r.runtimes[kind]
	if !ok {
		return nil, "", fmt.Errorf("runtime %q is not available", kind)
	}
	return rt, kind, nil
}

// Names returns the registered runtime kind names.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.runtimes))
	for n := range r.runtimes {
		names = append(names, n)
	}
	return names
}

func normalizeKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}
