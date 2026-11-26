package genext

import (
	"go/types"
	"iter"
)

// TypeSupport tells generators about named types whose values hold other
// values, such as the option types of github.com/ngicks/option or a
// container type of another package. A support answers through the
// interfaces it implements besides: Holder, Container and Composite, and
// the optional interfaces a generator defines for its own questions. Each
// of their methods reports false for a type the support does not claim, so
// that the next support of the registry answers. Pointers, slices, arrays
// and maps need no support.
//
// A generator asks the supports of the registry in use at the point it
// needs an answer and keeps none.
type TypeSupport interface {
	// Name names the support. A registry holds one support per name.
	Name() string
}

// Holder is a TypeSupport for types whose values hold at most one value,
// as opt.Option does.
type Holder interface {
	TypeSupport
	// Held returns the one value the value v of type t may hold: the role
	// naming it, its type, the condition of v holding it and the expression
	// of it. ok is false for a type the support does not claim.
	Held(t types.Type, v Gen) (role string, held types.Type, guard, value Gen, ok bool)
}

// Segment is the kind of the path segment by which a held value is
// located.
type Segment int

const (
	// SegmentNone adds no segment, as for a value that encodes alone.
	SegmentNone Segment = iota
	// SegmentIndex locates a value by an int index.
	SegmentIndex
	// SegmentKey locates a value by a map key.
	SegmentKey
	// SegmentField locates a value by the name of a JSON member.
	SegmentField
)

// Composite is a TypeSupport for types whose values hold several values
// side by side, each of its own type and under its own guard, as
// either.Either holds a left or a right value and tuple.Tuple2 holds two.
type Composite interface {
	TypeSupport
	// Roles returns the roles of the values a value of type t holds, in the
	// order a generator walks them, or nil for a type the support does not
	// claim.
	Roles(t types.Type) []string
	// Part returns the value of role that the value v of type t holds: its
	// type, the condition of v holding it, nil when v always holds it, the
	// expression of it, the kind of segment locating it and the label of
	// that segment. ok is false for a type the support does not claim and
	// for a role Roles does not return.
	Part(
		t types.Type,
		v Gen,
		role string,
	) (part types.Type, guard, value Gen, segment Segment, label Gen, ok bool)
}

// Container is a TypeSupport for types whose values hold any number of
// elements of one type, as a slice does.
type Container interface {
	TypeSupport
	// Elements returns how to walk the elements of the value v of type t:
	// the role naming an element, the type of an element, the expression a
	// range statement ranges over yielding the index or key and the
	// element, and the segment locating an element by what the range yields
	// first. ok is false for a type the support does not claim.
	Elements(
		t types.Type,
		v Gen,
	) (role string, elem types.Type, over Gen, segment Segment, ok bool)
}

// Supports is the registry in use as a generator sees it. *Registry is a
// Supports, and so is the resolver of a generator holding one.
type Supports interface {
	// TypeSupports returns the type supports in the order they are asked.
	TypeSupports() iter.Seq[TypeSupport]
}

// SupportsOf returns the supports of r implementing S, in the order r
// gives them. r may be nil, which yields none. A generator asks its own
// optional interfaces through it.
func SupportsOf[S TypeSupport](r Supports) iter.Seq[S] {
	return func(yield func(S) bool) {
		if r == nil {
			return
		}

		for s := range r.TypeSupports() {
			if x, ok := s.(S); ok && !yield(x) {
				return
			}
		}
	}
}

// HeldValue returns the one value the value v of type t may hold: the value
// a pointer points to, else the value the first Holder of r claiming t
// tells. r may be nil, which claims nothing. ok is false for a type of
// another kind.
func HeldValue(r Supports, t types.Type, v Gen) (held types.Type, guard, value Gen, ok bool) {
	if p, isPtr := types.Unalias(t).(*types.Pointer); isPtr {
		return p.Elem(), lang.Expr("%s != nil", v), lang.Expr("(*%s)", v), true
	}

	if h, ok := HolderOf(r, t); ok {
		_, held, guard, value, _ := h.Held(t, v)
		return held, guard, value, true
	}

	return nil, nil, nil, false
}

// HolderOf returns the first Holder of r claiming t. r may be nil.
func HolderOf(r Supports, t types.Type) (Holder, bool) {
	for h := range SupportsOf[Holder](r) {
		if _, _, _, _, ok := h.Held(t, nil); ok {
			return h, true
		}
	}

	return nil, false
}

// ContainerOf returns the first Container of r claiming t. r may be nil.
func ContainerOf(r Supports, t types.Type) (Container, bool) {
	for c := range SupportsOf[Container](r) {
		if _, _, _, _, ok := c.Elements(t, nil); ok {
			return c, true
		}
	}

	return nil, false
}

// CompositeOf returns the first Composite of r claiming t. r may be nil.
func CompositeOf(r Supports, t types.Type) (Composite, bool) {
	for c := range SupportsOf[Composite](r) {
		if len(c.Roles(t)) > 0 {
			return c, true
		}
	}

	return nil, false
}

// Dependants returns the types of the values a value of type t holds as the
// supports of r tell: the type of the value a Holder tells, the type of
// the elements a Container tells and the type of each value a Composite
// tells. It yields nothing for a type no support of r claims.
func Dependants(r Supports, t types.Type) iter.Seq[types.Type] {
	return func(yield func(types.Type) bool) {
		if h, ok := HolderOf(r, t); ok {
			_, held, _, _, _ := h.Held(t, nil)
			if !yield(held) {
				return
			}
		}

		if c, ok := ContainerOf(r, t); ok {
			_, elem, _, _, _ := c.Elements(t, nil)
			if !yield(elem) {
				return
			}
		}

		if c, ok := CompositeOf(r, t); ok {
			for _, role := range c.Roles(t) {
				part, _, _, _, _, _ := c.Part(t, nil, role)
				if !yield(part) {
					return
				}
			}
		}
	}
}
