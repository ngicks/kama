package partial

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// leafKind tells what a matched type holds behind its pointers.
type leafKind int

const (
	// leafOther is a type the partial form holds whole.
	leafOther leafKind = iota
	// leafPartial is a type with a partial variant the package can spell: a
	// target of the run or an implementor.
	leafPartial
	// leafOption is opt.Option[T].
	leafOption
	// leafUnd is und.Und[T].
	leafUnd
	// leafElastic is elastic.Elastic[T].
	leafElastic
)

// match is what the matcher reads off the type of a struct field, a slice
// element or a map value.
//
// The type is the single source of truth. Nothing keeps a match: the pass
// building the partial type and every method writer match the type again.
type match struct {
	// source is the matched type.
	source types.Type
	// route are the pointer steps from source to the type behind them.
	route []typegraph.EdgeRouteNode
	// leaf is the source type the partial form holds: the target, the type
	// argument of an option or of an und.Und, or the type behind the
	// pointers.
	leaf types.Type
	kind leafKind
	// named is the target or the implementor leaf refers to, or nil.
	named *types.Named
	// implementor tells that named is an implementor: a type outside the
	// run, which the graph of the run does not hold.
	implementor bool
	// ref tells a pointer field closing a cycle, whose partial form holds
	// *MPartial.
	ref bool
	// embedded tells an embedded struct target, which stays embedded as
	// its partial variant.
	embedded bool
	// blank tells a blank field, which the partial struct copies and the
	// methods do not touch.
	blank bool
}

// matchField matches the field v of the struct target n, for a declaration
// of the package of n.
//
// An embedded struct target, or pointer to one, stays embedded. Any other
// embedded type matches as a field named after it does.
func (s *selection) matchField(n *typegraph.Node, v *types.Var) match {
	pkgPath := n.Id.PackagePath

	if v.Name() == "_" {
		return match{source: v.Type(), leaf: v.Type(), blank: true}
	}

	named, ok := s.partialOf(deref(v.Type()), pkgPath)
	if ok && v.Embedded() && isStruct(named) {
		leaf, route := typegraph.PointerRoute(v.Type())

		return match{
			source: v.Type(), route: route, leaf: leaf,
			kind: leafPartial, named: named, implementor: s.node(named) == nil, embedded: true,
		}
	}

	return s.matchType(v.Type(), s.closesCycle(n, v), pkgPath)
}

// matchValue matches t, the type of a slice element or a map value, for a
// declaration of the package at pkgPath.
func (s *selection) matchValue(t types.Type, pkgPath string) match {
	return s.matchType(t, false, pkgPath)
}

// matchType matches t. ref tells a pointer closing a cycle.
//
// The option types are matched only without a pointer in front: a pointer
// to one is a pointer to a type held whole.
func (s *selection) matchType(t types.Type, ref bool, pkgPath string) match {
	leaf, route := typegraph.PointerRoute(t)
	m := match{source: t, route: route, leaf: leaf, kind: leafOther}

	if len(route) == 0 {
		switch {
		case isNamed(t, optPath, "Option"):
			m.kind, m.leaf = leafOption, typeArg(t)
			return m
		case isNamed(t, undPath, "Und"):
			m.kind, m.leaf = leafUnd, typeArg(t)
			return m
		case isNamed(t, elasticPath, "Elastic"):
			m.kind = leafElastic
			return m
		}
	}

	if named, ok := s.partialOf(leaf, pkgPath); ok {
		m.kind, m.named, m.implementor = leafPartial, named, s.node(named) == nil
		m.ref = ref && len(route) == 1
	}

	return m
}

// typeArg returns the first type argument of the instantiated type t.
func typeArg(t types.Type) types.Type {
	return types.Unalias(t).(*types.Named).TypeArgs().At(0)
}

// pointer reports whether the source is a pointer.
func (m match) pointer() bool { return len(m.route) > 0 }

// collection reports whether leaf is a slice or a map, which the append
// merge strategy applies to.
func (m match) collection() bool {
	switch m.leaf.Underlying().(type) {
	case *types.Slice, *types.Map:
		return true
	}

	return false
}

// partialField returns the name of the partial field of the field v
// matched as m.
func partialField(v *types.Var, m match) string {
	if m.embedded {
		return partialName(m.named.Obj().Name())
	}

	return v.Name()
}
