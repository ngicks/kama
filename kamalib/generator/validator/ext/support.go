package ext

import (
	"go/types"
	"iter"
)

// TypeSupport tells the generator and the validators about named types
// whose values hold other values, such as the option types of
// github.com/ngicks/option or a container type of another package. A
// support answers through the interfaces it implements besides: Holder,
// Container, Composite, PresenceTester, ZeroTester, NullTester,
// LengthTester and Comparer. Each of their
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

// Segment is the kind of the path segment by which an error of a held
// value is located.
type Segment int

const (
	// SegmentNone adds no segment, as for a value that encodes alone.
	SegmentNone Segment = iota
	// SegmentIndex locates a value by an int index, as validator.Index
	// does.
	SegmentIndex
	// SegmentKey locates a value by a map key, as validator.Key does.
	SegmentKey
	// SegmentField locates a value by the name of a JSON member, as
	// validator.Field does.
	SegmentField
)

// Composite is a TypeSupport for types whose values hold several values
// side by side, each of its own type and under its own guard, as
// either.Either holds a left or a right value and tuple.Tuple2 holds two.
// A rule rejecting a claimed type does not apply to the values it holds.
type Composite interface {
	TypeSupport
	// Roles returns the roles of the values a value of type t holds, in the
	// order the generator walks them, or nil for a type the support does
	// not claim.
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

// LengthTester is a TypeSupport for types telling the number of values
// they hold, as the length validator counts them.
type LengthTester interface {
	TypeSupport
	// Length returns the expression of type int of the number of values the
	// value v of type t holds. ok is false for a type the support does not
	// claim.
	Length(t types.Type, v Gen) (n Gen, ok bool)
}

// OpMultiple is the operator by which multipleof asks a Comparer whether a
// value is a multiple of the operand.
const OpMultiple = "%"

// Comparer is a TypeSupport for types whose values compare with an operand
// written in a directive, as cmp(ge="1s") compares a time.Duration. cmp
// asks it with the operators >, >=, <, <=, == and !=, and multipleof with
// OpMultiple. Both ask it before their own tests, so a support claiming a
// type of a basic underlying type replaces them for it.
type Comparer interface {
	TypeSupport
	// Comparable reports whether a value of type t compares with operand by
	// op. operand is the Go literal the directive wrote: a quoted string, a
	// number as written, true or false. ok is false for a type the support
	// does not claim, and for an op or an operand it leaves to the rule, as
	// a number compared with a time.Duration, which compares as any
	// integer. err tells why the support does not take a claimed op or
	// operand. It wraps ErrArgs for an operand the support cannot read for
	// t, as an unparsable duration.
	Comparable(t types.Type, op, operand string) (ok bool, err error)
	// Compare returns the condition of the value v of type t satisfying the
	// comparison by op with operand, which Comparable accepted. It declares
	// through e what the condition needs, such as the operand parsed once at
	// generation into a package-level variable.
	Compare(e Emitter, t types.Type, v Gen, op, operand string) (cond Gen)
}

// Comparable returns what the first Comparer of r claiming t tells about
// the comparison by op with operand. r may be nil.
func Comparable(r Resolver, t types.Type, op, operand string) (ok bool, err error) {
	for c := range supportsOf[Comparer](r) {
		if ok, err := c.Comparable(t, op, operand); ok {
			return true, err
		}
	}

	return false, nil
}

// Compare returns the condition the first Comparer of r claiming t gives
// for the value v. r may be nil.
func Compare(r Resolver, e Emitter, t types.Type, v Gen, op, operand string) (cond Gen, ok bool) {
	for c := range supportsOf[Comparer](r) {
		if ok, _ := c.Comparable(t, op, operand); ok {
			return c.Compare(e, t, v, op, operand), true
		}
	}

	return nil, false
}

// Length returns what the first LengthTester of r claiming t tells about
// the value v of type t. r may be nil.
func Length(r Resolver, t types.Type, v Gen) (n Gen, ok bool) {
	for l := range supportsOf[LengthTester](r) {
		if n, ok := l.Length(t, v); ok {
			return n, true
		}
	}

	return nil, false
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

// CompositeOf returns the first Composite of r claiming t. r may be nil.
func CompositeOf(r Resolver, t types.Type) (Composite, bool) {
	for c := range supportsOf[Composite](r) {
		if len(c.Roles(t)) > 0 {
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
// supports of r tell: the type of the value a Holder tells, the type of
// the elements a Container tells and the type of each value a Composite
// tells. Target selection walks into them. It
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
