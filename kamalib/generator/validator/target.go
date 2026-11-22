package validator

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/types"
	"iter"
	"maps"
	"slices"
	"strings"

	"github.com/ngicks/opiter"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

const (
	toolName  = "kama"
	directive = "validate"
)

// selection is the set of types to generate Validate for, with the options
// of the run.
//
// targets is derived: it is the marked types and the types holding them,
// trimmed from the type graph. It is kept as the one decision of the run
// that everything else asks about. Every other fact is read again from the
// types and from the options, which were decoded once from directive
// comments and option files.
type selection struct {
	opts    Options
	targets map[typegraph.QualifiedType]*typegraph.Node
	// pkgs are the loaded packages the run generates into.
	pkgs []*packages.Package
}

func selectTargets(pkgs []*packages.Package, opts Options) (*selection, error) {
	sel := &selection{opts: opts, pkgs: pkgs}

	g, err := typegraph.New(pkgs, nil, func(_ *ast.TypeSpec, obj types.Object) (bool, error) {
		return !sel.generated(obj), nil
	})
	if err != nil {
		return nil, fmt.Errorf("validator: %w", err)
	}

	if err := sel.checkMarks(g); err != nil {
		return nil, err
	}

	// A parent that gets no Validate cannot pass validation on, so the trim
	// stops at it.
	dependsThrough := func(e *typegraph.Edge[typegraph.Node]) bool {
		return sel.generates(e.Parent) && slices.ContainsFunc(e.Routes, followed)
	}

	sel.targets = opiter.WrapSeq2(g.Trim(sel.matches, dependsThrough, nil).All()).
		Filter(func(_ typegraph.QualifiedType, n *typegraph.MatchedNode) bool {
			return sel.generates(n.Node)
		}).
		Map(func(id typegraph.QualifiedType, n *typegraph.MatchedNode) (
			typegraph.QualifiedType, *typegraph.Node,
		) {
			return id, n.Node
		}).
		Collect(maps.Collect)

	if err := sel.checkFieldOptions(); err != nil {
		return nil, err
	}

	if err := sel.checkConflicts(); err != nil {
		return nil, err
	}

	return sel, nil
}

// matches reports whether n starts the trim: a marked type, a type holding
// an implementor of another package, which the graph has no node for, or an
// implementor, whose holders need a Validate to reach it.
func (s *selection) matches(n *typegraph.Node) bool {
	if s.implementor(n.Detail.Type) {
		return true
	}

	return s.generates(n) && (s.marked(n) || s.holdsForeignImplementor(n))
}

// generates reports whether n gets a Validate when the trim reaches it: a
// struct, slice or map type that is not ignored and has no Validate of
// its own.
func (s *selection) generates(n *typegraph.Node) bool {
	return validatorKind(n) && !s.ignored(n.Id) && !s.implementor(n.Detail.Type)
}

// validatorKind reports whether the generator can give n a Validate: a
// struct, slice or map underlying type.
func validatorKind(n *typegraph.Node) bool {
	switch n.Detail.Type.Underlying().(type) {
	case *types.Struct, *types.Slice, *types.Map:
		return true
	}

	return false
}

// followed reports whether r reaches its child through struct fields,
// slice and array elements, map values, pointers and aliases.
func followed(r *typegraph.EdgeRoute) bool {
	return followedRoute(r.Nodes)
}

func followedRoute(route []typegraph.EdgeRouteNode) bool {
	const allowed = typegraph.EdgeKindStruct |
		typegraph.EdgeKindSlice |
		typegraph.EdgeKindArray |
		typegraph.EdgeKindMapValue |
		typegraph.EdgeKindPointer |
		typegraph.EdgeKindAlias

	return opiter.WrapSeq(slices.Values(route)).
		Every(func(n typegraph.EdgeRouteNode) bool { return n.Kind&allowed != 0 })
}

// holdsForeignImplementor reports whether n holds an implementor of a
// package outside the run along a followed route.
func (s *selection) holdsForeignImplementor(n *typegraph.Node) bool {
	var found bool

	_ = typegraph.WalkTypesToNamed(
		nil,
		n.Detail.Type.Underlying(),
		nil,
		func(named *types.Named, route []typegraph.EdgeRouteNode) error {
			found = found || followedRoute(route) && !s.loaded(named.Obj().Pkg()) &&
				s.implementor(named)

			return nil
		},
	)

	return found
}

// loaded reports whether pkg is one of the packages of the run.
func (s *selection) loaded(pkg *types.Package) bool {
	return slices.ContainsFunc(s.pkgs, func(p *packages.Package) bool { return p.Types == pkg })
}

// isTarget reports whether named refers to a target, through aliases and
// instantiation.
func (s *selection) isTarget(named *types.Named) bool {
	_, ok := s.targets[typegraph.QualifiedTypeFromObject(named.Origin().Obj())]
	return ok
}

func (s *selection) filename(n *typegraph.Node) string {
	return n.Detail.Pkg.Fset.Position(n.Detail.File.FileStart).Filename
}

// targetsIn returns the targets the source file at filename declares, in
// the order of their declarations.
func (s *selection) targetsIn(filename string) []*typegraph.Node {
	byPosition := func(seq iter.Seq[*typegraph.Node]) []*typegraph.Node {
		return slices.SortedFunc(seq, func(a, b *typegraph.Node) int {
			return cmp.Compare(a.Detail.Ts.Pos(), b.Detail.Ts.Pos())
		})
	}

	return opiter.WrapSeq(maps.Values(s.targets)).
		Filter(func(n *typegraph.Node) bool { return s.filename(n) == filename }).
		Collect(byPosition)
}

// sorted returns the targets ordered by package path and type name.
func (s *selection) sorted() []*typegraph.Node {
	return slices.SortedFunc(maps.Values(s.targets), func(a, b *typegraph.Node) int {
		return a.Id.Compare(b.Id)
	})
}

// checkConflicts reports names the generated method would collide with: a
// method Validate the generator did not write that is no implementor, a
// field named Validate, and a type parameter shadowing a predeclared
// identifier the generated code spells.
func (s *selection) checkConflicts() error {
	var errs []error

	for _, n := range s.sorted() {
		named := n.Detail.Type
		pos := n.Detail.Pkg.Fset.Position

		for m := range named.Methods() {
			if m.Name() != "Validate" || s.generated(m) {
				continue
			}

			recv := "value"
			if _, ok := m.Signature().Recv().Type().(*types.Pointer); ok {
				recv = "pointer"
			}

			errs = append(errs, fmt.Errorf(
				"%w: %s: %s declares Validate on its %s receiver with signature %s;"+
					" want %s on the value receiver, or ignore the type with //kama:validate ignore",
				ErrConflict, pos(m.Pos()), n.Id, recv, m.Type(), wantValidate(named),
			))
		}

		if st, ok := named.Underlying().(*types.Struct); ok {
			for v := range st.Fields() {
				if v.Name() == "Validate" {
					errs = append(errs, fmt.Errorf(
						"%w: %s: field Validate of %s collides with the generated method",
						ErrConflict, pos(v.Pos()), n.Id,
					))
				}
			}
		}

		for tp := range named.TypeParams().TypeParams() {
			if types.Universe.Lookup(tp.Obj().Name()) != nil {
				errs = append(errs, fmt.Errorf(
					"%w: %s: type parameter %s of %s shadows the predeclared %s"+
						" the generated code spells",
					ErrConflict, pos(tp.Obj().Pos()), tp.Obj().Name(), n.Id, tp.Obj().Name(),
				))
			}
		}
	}

	return errors.Join(errs...)
}

// wantValidate spells the signature of a Validate method of named.
func wantValidate(named *types.Named) string {
	var b strings.Builder

	b.WriteString("func(bool")

	for tp := range named.TypeParams().TypeParams() {
		b.WriteString(", func(" + tp.Obj().Name() + ", bool) error")
	}

	b.WriteString(") error")

	return b.String()
}
