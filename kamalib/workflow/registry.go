package workflow

import (
	"errors"
	"fmt"
	"sync"
)

var ErrDuplicateKind = errors.New("workflow: duplicate kind")

// DefaultRegistry holds the procs registered through Register. Proc packages
// register into it from their init functions, so importing a proc package
// makes its kinds available.
var DefaultRegistry = NewRegistry()

// Register adds newProc under kind to DefaultRegistry. It panics on error,
// since it is meant for init functions, where a duplicate kind is a
// programming error.
func Register(kind string, newProc func() Proc) {
	if err := DefaultRegistry.Register(kind, newProc); err != nil {
		panic(err)
	}
}

// Registry maps kinds to proc constructors. It is safe for concurrent use.
//
// Build calls a constructor once per kind a graph uses, so every graph gets
// procs of its own and cleaning up one graph leaves the others alone.
type Registry struct {
	mu    sync.RWMutex
	procs map[string]func() Proc
}

func NewRegistry() *Registry {
	return &Registry{procs: map[string]func() Proc{}}
}

// Register adds newProc under kind.
func (r *Registry) Register(kind string, newProc func() Proc) error {
	if newProc == nil {
		return fmt.Errorf("%w: %q is nil", ErrInvalidProc, kind)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, dup := r.procs[kind]; dup {
		return fmt.Errorf("%w: %q", ErrDuplicateKind, kind)
	}

	r.procs[kind] = newProc
	return nil
}

// Lookup returns the proc constructor registered under kind.
func (r *Registry) Lookup(kind string) (func() Proc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	newProc, ok := r.procs[kind]
	return newProc, ok
}
