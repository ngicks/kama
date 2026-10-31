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

// Register adds p under kind to DefaultRegistry. It panics on error, since
// it is meant for init functions, where a bad or duplicate proc is a
// programming error.
func Register(kind string, p Proc) {
	if err := DefaultRegistry.Register(kind, p); err != nil {
		panic(err)
	}
}

// Registry maps kinds to procs. It is safe for concurrent use.
type Registry struct {
	mu    sync.RWMutex
	procs map[string]Proc
}

func NewRegistry() *Registry {
	return &Registry{procs: map[string]Proc{}}
}

// Register adds p under kind.
func (r *Registry) Register(kind string, p Proc) error {
	if p == nil {
		return fmt.Errorf("%w: %q is nil", ErrInvalidProc, kind)
	}

	for i, t := range p.InTypes() {
		if t == nil {
			return fmt.Errorf("%w: %q: in %d is nil", ErrInvalidProc, kind, i)
		}
	}

	for i, t := range p.OutTypes() {
		if t == nil {
			return fmt.Errorf("%w: %q: out %d is nil", ErrInvalidProc, kind, i)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, dup := r.procs[kind]; dup {
		return fmt.Errorf("%w: %q", ErrDuplicateKind, kind)
	}

	r.procs[kind] = p
	return nil
}

// Lookup returns the proc registered under kind.
func (r *Registry) Lookup(kind string) (Proc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.procs[kind]
	return p, ok
}
