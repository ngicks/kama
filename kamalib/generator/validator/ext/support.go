package ext

import (
	"go/types"
	"iter"
)

// TypeSupport tells the generator and the validators about named types
// whose values hold other values, such as the option types of
// github.com/ngicks/option or a container type of another package. A
// support answers through the interfaces it implements besides: Holder,
// Container, PresenceTester, ZeroTester and NullTester. Each of their
// methods reports false for a type the support does not claim, so that the
// next support of the registry answers. Pointers, slices, arrays and maps
// need no support.
//
// The generator asks the supports of the registry in use at the point it
// needs an answer and keeps none. A value a Holder or a Container tells is
// walked as any other value: Validate of its type validates it, and the
// walk goes on into the values it holds. Target selection walks the same
// types, so a type argument of a claimed type holding something to
// validate makes its holder a target.
type TypeSupport interface {
	// Name names the support. A registry holds one support per name.
	Name() string
}

// Holder is a TypeSupport for types whose values hold at most one value,
// as opt.Option does. A rule rejecting a claimed type applies to the value
// held, under the guard, as it does to the value a pointer points to.
type Holder interface {
	TypeSupport
	// Held returns the one value the value v of type t may hold: the role
	// naming it, its type, the condition of v holding it and the expression
	// of it. ok is false for a type the support does not claim.
	Held(t types.Type, v Gen) (role string, held types.Type, guard, value Gen, ok bool)
}

// Segment is the path segment by which an error of an element is located.
type Segment int

const (
	// SegmentIndex locates an element by an int index, as validator.Index
	// does.
	SegmentIndex Segment = iota
	// SegmentKey locates an element by its key, as validator.Key does.
	SegmentKey
)

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

// PresenceTester is a TypeSupport for types whose values may hold no
// value, as the required validator tests it.
type PresenceTester interface {
	TypeSupport
	// Presence returns the condition of the value v of type t holding a
	// value and the words telling what v is when it holds none, as
	// "undefined". ok is false for a type the support does not claim.
	Presence(t types.Type, v Gen) (cond Gen, absent string, ok bool)
}

// ZeroTester is a TypeSupport for types telling their zero value, as the
// zero validator tests it.
type ZeroTester interface {
	TypeSupport
	// Zero returns the condition of the value v of type t being zero and the
	// words telling what v is otherwise, as "a non-zero value". ok is false
	// for a type the support does not claim.
	Zero(t types.Type, v Gen) (cond Gen, nonZero string, ok bool)
}

// NullTester is a TypeSupport for types telling a null value, as the null
// validator tests it.
type NullTester interface {
	TypeSupport
	// Null returns the condition of the value v of type t being null. ok is
	// false for a type the support does not claim.
	Null(t types.Type, v Gen) (cond Gen, ok bool)
}

// HeldValue returns the one value the value v of type t may hold: the value
// a pointer points to, else the value the first Holder of r claiming t
// tells. r may be nil, which claims nothing. ok is false for a type of
// another kind.
func HeldValue(r Resolver, t types.Type, v Gen) (held types.Type, guard, value Gen, ok bool) {
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
func HolderOf(r Resolver, t types.Type) (Holder, bool) {
	for h := range supportsOf[Holder](r) {
		if _, _, _, _, ok := h.Held(t, nil); ok {
			return h, true
		}
	}

	return nil, false
}

// ContainerOf returns the first Container of r claiming t. r may be nil.
func ContainerOf(r Resolver, t types.Type) (Container, bool) {
	for c := range supportsOf[Container](r) {
		if _, _, _, _, ok := c.Elements(t, nil); ok {
			return c, true
		}
	}

	return nil, false
}

// Presence returns what the first PresenceTester of r claiming t tells
// about the value v of type t. r may be nil.
func Presence(r Resolver, t types.Type, v Gen) (cond Gen, absent string, ok bool) {
	for p := range supportsOf[PresenceTester](r) {
		if cond, absent, ok := p.Presence(t, v); ok {
			return cond, absent, true
		}
	}

	return nil, "", false
}

// Zero returns what the first ZeroTester of r claiming t tells about the
// value v of type t. r may be nil.
func Zero(r Resolver, t types.Type, v Gen) (cond Gen, nonZero string, ok bool) {
	for z := range supportsOf[ZeroTester](r) {
		if cond, nonZero, ok := z.Zero(t, v); ok {
			return cond, nonZero, true
		}
	}

	return nil, "", false
}

// Null returns what the first NullTester of r claiming t tells about the
// value v of type t. r may be nil.
func Null(r Resolver, t types.Type, v Gen) (cond Gen, ok bool) {
	for n := range supportsOf[NullTester](r) {
		if cond, ok := n.Null(t, v); ok {
			return cond, true
		}
	}

	return nil, false
}

// Dependants returns the types of the values a value of type t holds as the
// supports of r tell: the type of the value a Holder tells and the type of
// the elements a Container tells. Target selection walks into them. It
// yields nothing for a type no Holder or Container of r claims.
func Dependants(r Resolver, t types.Type) iter.Seq[types.Type] {
	return func(yield func(types.Type) bool) {
		if h, ok := HolderOf(r, t); ok {
			_, held, _, _, _ := h.Held(t, nil)
			if !yield(held) {
				return
			}
		}

		if c, ok := ContainerOf(r, t); ok {
			_, elem, _, _, _ := c.Elements(t, nil)
			yield(elem)
		}
	}
}

func supportsOf[S TypeSupport](r Resolver) iter.Seq[S] {
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
