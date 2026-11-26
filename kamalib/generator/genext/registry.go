package genext

import (
	"fmt"
	"iter"
	"slices"
)

// Registry holds type supports in the order they are asked.
type Registry struct {
	supports []TypeSupport
}

// NewRegistry returns a registry holding ss in order. A later support of a
// name replaces an earlier one in its place.
func NewRegistry(ss ...TypeSupport) *Registry {
	r := &Registry{}
	for _, s := range ss {
		r.RegisterTypeSupport(s)
	}

	return r
}

// defaults holds what extension packages register in their init
// functions. Callers get clones of it, so that a change to one registry
// never leaks into another run.
var defaults = NewRegistry()

// RegisterTypeSupport adds ss to the default registry. It panics when a
// support of the same name is registered already, since two extension
// packages then claim one name. An extension package calls it in its init
// function.
func RegisterTypeSupport(ss ...TypeSupport) {
	for _, s := range ss {
		if defaults.index(s.Name()) >= 0 {
			panic(fmt.Errorf("genext: type support %s is registered twice", s.Name()))
		}

		defaults.RegisterTypeSupport(s)
	}
}

// DefaultRegistry returns a new registry holding the type supports
// registered through RegisterTypeSupport.
func DefaultRegistry() *Registry { return defaults.Clone() }

// RegisterTypeSupport adds s after the supports of r, replacing the
// support of the same name in its place.
func (r *Registry) RegisterTypeSupport(s TypeSupport) {
	if i := r.index(s.Name()); i >= 0 {
		r.supports[i] = s
		return
	}

	r.supports = append(r.supports, s)
}

// TypeSupports returns the type supports of r in the order they were
// registered. It implements Supports.
func (r *Registry) TypeSupports() iter.Seq[TypeSupport] { return slices.Values(r.supports) }

// Clone returns a registry holding the type supports of r, which changes
// to either do not affect.
func (r *Registry) Clone() *Registry { return &Registry{supports: slices.Clone(r.supports)} }

func (r *Registry) index(name string) int {
	return slices.IndexFunc(r.supports, func(s TypeSupport) bool { return s.Name() == name })
}
