package workflow

import (
	"errors"
	"fmt"
)

var ErrDuplicateKind = errors.New("workflow: duplicate kind")

// Registry maps kinds to procs.
type Registry struct {
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

	if _, dup := r.procs[kind]; dup {
		return fmt.Errorf("%w: %q", ErrDuplicateKind, kind)
	}

	r.procs[kind] = p
	return nil
}

// Lookup returns the proc registered under kind.
func (r *Registry) Lookup(kind string) (Proc, bool) {
	p, ok := r.procs[kind]
	return p, ok
}
