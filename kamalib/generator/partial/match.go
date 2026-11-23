package partial

import (
	"go/types"
	"reflect"

	"github.com/ngicks/kama/kamalib/pkg/structtag"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
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
	// fallback tells a field catching the unknown members of a JSON object,
	// which the partial struct copies and the methods copy whole.
	fallback bool
}

// matchField matches the field v of the struct target n, for a declaration
// of the package of n.
//
// An embedded struct target, or pointer to one, stays embedded. Any other
// embedded type matches as a field named after it does.
func (s *selection) matchField(n *types.Named, v *types.Var) match {
	pkgPath := n.Obj().Pkg().Path()

	if v.Name() == "_" {
		return match{source: v.Type(), leaf: v.Type(), blank: true}
	}

	if isFallback(n, v) {
		return match{source: v.Type(), leaf: v.Type(), fallback: true}
	}

	named, ok := s.partialOf(deref(v.Type()), pkgPath)
	if ok && v.Embedded() && isStruct(named) {
		leaf, route := typegraph.PointerRoute(v.Type())
		_, target := s.target(named)

		return match{
			source: v.Type(), route: route, leaf: leaf,
			kind: leafPartial, named: named, implementor: !target, embedded: true,
		}
	}

	return s.matchType(v.Type(), s.closesCycle(n, v), pkgPath)
}

// isFallback reports whether the field v of the struct target n is an
// unknown member fallback of encoding/json/v2: a field of map type, not
// embedded, whose json tag has the embed option. encoding/json/v2 rejects
// any other option on it, and an und.Und around it, so the partial struct
// keeps its type and tag.
func isFallback(n *types.Named, v *types.Var) bool {
	if v.Embedded() {
		return false
	}

	if _, ok := v.Type().Underlying().(*types.Map); !ok {
		return false
	}

	st, ok := n.Underlying().(*types.Struct)
	if !ok {
		return false
	}

	for i := range st.NumFields() {
		if st.Field(i).Name() != v.Name() {
			continue
		}

		tags, err := structtag.Parse(reflect.StructTag(st.Tag(i)))
		if err != nil {
			return false
		}

		_, _, err = tags.Get("json", "embed")

		return err == nil
	}

	return false
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
		case typequery.IsNamed(t, typequery.OptPath, "Option"):
			m.kind, m.leaf = leafOption, typequery.TypeArg(t)
			return m
		case typequery.IsNamed(t, typequery.UndPath, "Und"):
			m.kind, m.leaf = leafUnd, typequery.TypeArg(t)
			return m
		case typequery.IsNamed(t, typequery.ElasticPath, "Elastic"):
			m.kind = leafElastic
			return m
		}
	}

	if named, ok := s.partialOf(leaf, pkgPath); ok {
		_, target := s.target(named)
		m.kind, m.named, m.implementor = leafPartial, named, !target
		m.ref = ref && len(route) == 1
	}

	return m
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
