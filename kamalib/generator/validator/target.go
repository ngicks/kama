package validator

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"iter"
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

// selection is the run: the loaded packages and the options, which were
// decoded once from directive comments and option files. Every fact,
// the set of targets included, is read again from the types and from the
// options where it is needed.
type selection struct {
	opts Options
	// pkgs are the loaded packages the run generates into.
	pkgs []*packages.Package
}

func selectTargets(pkgs []*packages.Package, opts Options) (*selection, error) {
	sel := &selection{opts: opts, pkgs: pkgs}

	g, err := sel.graph()
	if err != nil {
		return nil, fmt.Errorf("validator: %w", err)
	}

	if err := sel.checkMarks(g); err != nil {
		return nil, err
	}

	if err := sel.checkFieldOptions(); err != nil {
		return nil, err
	}

	if err := sel.checkConflicts(); err != nil {
		return nil, err
	}

	if err := sel.checkUnsupportedHolders(g); err != nil {
		return nil, err
	}

	return sel, nil
}

// graph builds the type graph of the packages of the run, leaving out the
// types of generated files.
func (s *selection) graph() (*typegraph.Graph, error) {
	return typegraph.New(s.pkgs, nil, func(_ *ast.TypeSpec, obj types.Object) (bool, error) {
		return !s.generated(obj), nil
	})
}

// trim returns the marked types and the types holding them, trimmed from a
// type graph built again from the packages.
func (s *selection) trim() *typegraph.MatchedGraph {
	g, err := s.graph()
	if err != nil {
		// selectTargets built the same graph from the same packages and
		// stopped the run on its error.
		panic(fmt.Errorf("validator: %w", err))
	}

	// A parent that gets no Validate cannot pass validation on, so the trim
	// stops at it. The routes of the edge cannot tell the type argument of an
	// option type from that of any other generic type, so the type of the
	// parent is walked again.
	dependsThrough := func(e *typegraph.Edge[typegraph.Node]) bool {
		isChild := func(named *types.Named) bool {
			return typegraph.QualifiedTypeFromObject(named.Origin().Obj()) == e.Child.Id
		}

		return s.generates(e.Parent) && reaches(e.Parent.Detail.Type.Underlying(), isChild)
	}

	return g.Trim(s.matches, dependsThrough, nil)
}

// targets returns the types that get a Validate: the trimmed types the
// generator can give one.
func (s *selection) targets() iter.Seq[*typegraph.Node] {
	return opiter.WrapSeq2(s.trim().All()).
		OmitF().
		Map(func(n *typegraph.MatchedNode) *typegraph.Node { return n.Node }).
		Filter(s.generates).
		Iter()
}

// isTargetId reports whether the type id gets a Validate.
func (s *selection) isTargetId(id typegraph.QualifiedType) bool {
	n, ok := s.trim().Get(id)
	return ok && s.generates(n.Node)
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

// reaches reports whether a value of type t holds a value of a named type
// that pred reports true for, where Validate walks: through pointers,
// slices, arrays, map values, aliases, the fields of struct type literals
// and the type argument of opt.Option, und.Und and elastic.Elastic. Any
// other named type ends the walk, its type arguments included.
func reaches(t types.Type, pred func(named *types.Named) bool) bool {
	switch u := types.Unalias(t).(type) {
	case *types.Named:
		if isOptionType(u) {
			return reaches(typeArg(u), pred)
		}

		return pred(u)
	case *types.Pointer:
		return reaches(u.Elem(), pred)
	case *types.Slice:
		return reaches(u.Elem(), pred)
	case *types.Array:
		return reaches(u.Elem(), pred)
	case *types.Map:
		return reaches(u.Elem(), pred)
	case *types.Struct:
		return opiter.WrapSeq(u.Fields()).
			Any(func(v *types.Var) bool { return reaches(v.Type(), pred) })
	}

	return false
}

// isOptionType reports whether t is opt.Option, und.Und or elastic.Elastic.
func isOptionType(t types.Type) bool {
	return isNamed(t, optPath, "Option") || isNamed(t, undPath, "Und") ||
		isNamed(t, elasticPath, "Elastic")
}

// holdsForeignImplementor reports whether n holds an implementor of a
// package outside the run where Validate walks.
func (s *selection) holdsForeignImplementor(n *typegraph.Node) bool {
	return reaches(n.Detail.Type.Underlying(), func(named *types.Named) bool {
		return !s.loaded(named.Obj().Pkg()) && s.implementor(named)
	})
}

// loaded reports whether pkg is one of the packages of the run.
func (s *selection) loaded(pkg *types.Package) bool {
	return slices.ContainsFunc(s.pkgs, func(p *packages.Package) bool { return p.Types == pkg })
}

// isTarget reports whether named refers to a target, through aliases and
// instantiation.
func (s *selection) isTarget(named *types.Named) bool {
	return s.isTargetId(typegraph.QualifiedTypeFromObject(named.Origin().Obj()))
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

	return opiter.WrapSeq(s.targets()).
		Filter(func(n *typegraph.Node) bool { return s.filename(n) == filename }).
		Collect(byPosition)
}

// sorted returns the targets ordered by package path and type name.
func (s *selection) sorted() []*typegraph.Node {
	return slices.SortedFunc(s.targets(), func(a, b *typegraph.Node) int {
		return a.Id.Compare(b.Id)
	})
}

// checkConflicts reports names the generated method would collide with: a
// method Validate the generator did not write that is no implementor, a
// field named Validate, a type parameter shadowing a predeclared
// identifier the generated code spells or named like one of fixedNames,
// and two fields whose hooks are on and share a name.
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
			name := tp.Obj().Name()

			switch {
			case types.Universe.Lookup(name) != nil:
				errs = append(errs, fmt.Errorf(
					"%w: %s: type parameter %s of %s shadows the predeclared %s"+
						" the generated code spells",
					ErrConflict, pos(tp.Obj().Pos()), name, n.Id, name,
				))
			case slices.Contains(fixedNames, name):
				errs = append(errs, fmt.Errorf(
					"%w: %s: type parameter %s of %s collides with the identifier %s"+
						" the generated Validate declares; rename the type parameter",
					ErrConflict, pos(tp.Obj().Pos()), name, n.Id, name,
				))
			}
		}

		errs = append(errs, s.hookConflicts(n)...)
	}

	return errors.Join(errs...)
}

// hookConflicts reports fields of the struct target n whose hooks are on
// and share a name. Fields name and Name share validateName, for example.
// The hook cannot tell which field it validates.
func (s *selection) hookConflicts(n *typegraph.Node) []error {
	st, ok := n.Detail.Type.Underlying().(*types.Struct)
	if !ok {
		return nil
	}

	var (
		errs  []error
		first = map[string]*types.Var{}
	)

	for v := range st.Fields() {
		if !accessible(v, n.Detail.Pkg.Types) || !s.hookOn(n, v) {
			continue
		}

		name := hookName(v)

		prev, ok := first[name]
		if !ok {
			first[name] = v
			continue
		}

		errs = append(errs, fmt.Errorf(
			"%w: %s: fields %s and %s of %s share the hook %s;"+
				" rename one or turn its hook off with //kama:validate:hook off",
			ErrConflict, n.Detail.Pkg.Fset.Position(v.Pos()), prev.Name(), v.Name(), n.Id, name,
		))
	}

	return errs
}

// checkUnsupportedHolders reports types holding a named array or pointer
// type of the run that holds something to validate. It checks the fields of
// struct types and the slice and map types that get a Validate when the
// trim reaches them. Such a named type gets no Validate. The values it holds
// would go unchecked. The check covers types the trim does not reach. They
// would be dependants if the named type had a Validate.
func (s *selection) checkUnsupportedHolders(g *typegraph.Graph) error {
	nodes := slices.SortedFunc(
		opiter.WrapSeq2(g.All()).
			OmitF().
			Filter(s.generates).
			Iter(),
		func(a, b *typegraph.Node) int { return a.Id.Compare(b.Id) },
	)

	var errs []error

	for _, n := range nodes {
		pos := n.Detail.Pkg.Fset.Position
		u := n.Detail.Type.Underlying()

		st, ok := u.(*types.Struct)
		if !ok {
			if named := s.unsupportedIn(u); named != nil {
				errs = append(errs, unsupportedHolderError(pos(n.Detail.Type.Obj().Pos()),
					"type "+n.Id.String(), named))
			}

			continue
		}

		for v := range st.Fields() {
			if !accessible(v, n.Detail.Pkg.Types) {
				continue
			}

			if named := s.unsupportedIn(v.Type()); named != nil {
				errs = append(errs, unsupportedHolderError(pos(v.Pos()),
					"field "+v.Name()+" of "+n.Id.String(), named))
			}
		}
	}

	return errors.Join(errs...)
}

// unsupportedNamedHint tells what to use in place of a named array or
// pointer type holding values to validate.
const unsupportedNamedHint = "only struct, slice and map named types get a Validate;" +
	" use an unnamed array or pointer type, or a wrapper struct"

func unsupportedHolderError(pos token.Position, holder string, named *types.Named) error {
	return fmt.Errorf(
		"%w: %s: %s holds %s, a named %s type holding values to validate; %s",
		ErrUnsupported, pos, holder, named, kindName(named.Underlying()), unsupportedNamedHint,
	)
}

func kindName(u types.Type) string {
	if _, ok := u.(*types.Array); ok {
		return "array"
	}

	return "pointer"
}

// unsupportedIn returns the first named array or pointer type of the run
// holding something to validate that a value of type t holds where
// Validate walks, or nil.
func (s *selection) unsupportedIn(t types.Type) *types.Named {
	var found *types.Named

	seen := map[*types.Named]bool{}

	reaches(t, func(named *types.Named) bool {
		if s.unsupported(named, seen) {
			found = named
		}

		return found != nil
	})

	return found
}

// unsupported reports whether named is an array or pointer type of the run
// without a Validate of its own holding a value of a target, of an
// implementor or of another such type. seen holds the types visited. It
// stops a cycle such as type A [2]B; type B *A.
func (s *selection) unsupported(named *types.Named, seen map[*types.Named]bool) bool {
	origin := named.Origin()

	switch named.Underlying().(type) {
	case *types.Array, *types.Pointer:
	default:
		return false
	}

	if seen[origin] || !s.loaded(origin.Obj().Pkg()) || s.implementor(named) {
		return false
	}

	seen[origin] = true

	return reaches(named.Underlying(), func(m *types.Named) bool {
		return s.callable(m) || s.unsupported(m, seen)
	})
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
