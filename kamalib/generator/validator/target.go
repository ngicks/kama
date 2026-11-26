package validator

import (
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

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// selection is the run: the loaded packages and the options, which were
// decoded once from directive comments and option files. Every fact,
// the set of targets included, is read again from the types and from the
// options where it is needed.
type selection struct {
	opts Options
	// markers tells the declarations emitted by generators.
	markers *genmark.Registry
	// loadedOptions holds the options of the directives and the option
	// files.
	loadedOptions *LoadedOptions
	// pkgs are the loaded packages the run generates into.
	pkgs []*packages.Package
	// warn reports a warning of a directive. It is nil when the run takes
	// no warnings.
	warn func(w error)
}

func selectTargets(
	pkgs []*packages.Package,
	opts Options,
	markers *genmark.Registry,
	loaded *LoadedOptions,
) (*selection, error) {
	sel := &selection{opts: opts, markers: markers, loadedOptions: loaded, pkgs: pkgs}

	g, err := sel.typeGraph()
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

// typeGraph returns the graph of the types of the loaded packages. It
// leaves out a declaration carrying the marker of a registered generator,
// which the generator writes again, unless a directive or an option file
// entry marks it explicitly. A generator emitting data models writes
// //kama:validate on the types it hands on to this generator.
func (s *selection) typeGraph() (*typegraph.Graph, error) {
	return typegraph.New(s.pkgs, nil, func(_ *ast.TypeSpec, obj types.Object) (bool, error) {
		if !base.Generated(s.pkgs, s.markers, obj) {
			return true, nil
		}

		to, _ := s.loadedOptions.Option.Type(typegraph.QualifiedTypeFromObject(obj))
		_, marked := explicitMark(to)

		return marked, nil
	})
}

// trim returns the marked types and the types holding them, trimmed from a
// type graph built again from the packages.
func (s *selection) trim() *typegraph.MatchedGraph {
	g, err := s.typeGraph()
	if err != nil {
		// selectTargets built the same graph from the same packages and
		// stopped the run on its error.
		panic(fmt.Errorf("validator: %w", err))
	}

	// A parent that gets no Validate cannot pass validation on, so the trim
	// stops at it. The routes of the edge cannot tell the type argument of a
	// type a type support claims from that of any other generic type, so the
	// type of the parent is walked again.
	dependsThrough := func(e *typegraph.Edge[typegraph.Node]) bool {
		isChild := func(named *types.Named) bool {
			return typegraph.QualifiedTypeFromObject(named.Origin().Obj()) == e.Child.Id
		}

		return s.generates(e.Parent) && s.reaches(e.Parent.Detail.Type.Underlying(), isChild)
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
	return s.targetIn(s.trim(), id)
}

// targetIn reports whether the type id gets a Validate. trimmed is the
// graph trim returns.
func (s *selection) targetIn(trimmed *typegraph.MatchedGraph, id typegraph.QualifiedType) bool {
	n, ok := trimmed.Get(id)
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
// struct, slice or map type, or a type validatesValue reports, that is not
// ignored and has no Validate of its own.
func (s *selection) generates(n *typegraph.Node) bool {
	return (typequery.IsStructSliceOrMap(n.Detail.Type) || s.validatesValue(n)) &&
		!s.ignored(n.Id) && !s.implementor(n.Detail.Type)
}

// validatesValue reports whether the directive of n names validators, pre
// or post for the value of n and n can declare Validate: its underlying
// type is no pointer and no interface. Without them, a type of another kind
// than struct, slice and map has nothing to validate.
func (s *selection) validatesValue(n *typegraph.Node) bool {
	switch n.Detail.Type.Underlying().(type) {
	case *types.Pointer, *types.Interface:
		return false
	}

	o := s.typeOptions(n.Id)

	return len(s.typeValidators(n.Id)) > 0 || o.Pre != "" || o.Post != ""
}

// reaches reports whether a value of type t holds a value of a named type
// that pred reports true for, where Validate walks: through pointers,
// slices, arrays, map values, aliases, the fields of struct type literals
// and the types ext.Dependants tells for a type a type support claims. A
// named
// pointer or array type of a package outside the run is walked through
// when pred reports false for it, as throughForeign tells. Any other named
// type ends the walk, its type arguments included.
func (s *selection) reaches(t types.Type, pred func(named *types.Named) bool) bool {
	return s.reachesLeaf(t, func(leaf types.Type) bool {
		named, ok := leaf.(*types.Named)
		return ok && pred(named)
	})
}

// reachesLeaf is reaches with pred over the types that end the walk: the
// named types and the type parameters. pred also sees each named type the
// walk goes through.
func (s *selection) reachesLeaf(t types.Type, pred func(leaf types.Type) bool) bool {
	// memo keeps the answers for the types of other packages walked
	// through in this call. Without it the walk visits such a type once per
	// path to it, which is exponential in the depth of a chain such as type
	// A00 [1]struct{ X A01; Y A01 } down to A18 declared in a package
	// outside the run: generation took 1.7 s at depth 16 and 7.2 s at depth
	// 18, twice as long per level. With it, generation takes about 6 ms at
	// depth 22. A false answer that a cut of a cycle gave depends on the
	// path and is not kept.
	var (
		memo = map[*types.Named]bool{}
		// path holds the types of other packages the walk is going through.
		// It stops a cycle such as type A [2]B; type B *A.
		path []*types.Named
		cuts int
		walk func(t types.Type) bool
	)

	walk = func(t types.Type) bool {
		switch u := types.Unalias(t).(type) {
		case *types.Named:
			deps := slices.Collect(ext.Dependants(s.opts.Registry, u))

			switch {
			case len(deps) > 0:
				return slices.ContainsFunc(deps, walk)
			case pred(u):
				return true
			case !s.throughForeign(u):
				return false
			}

			if v, ok := memo[u]; ok {
				return v
			}

			if onPath(path, u) {
				cuts++
				return false
			}

			path = append(path, u)
			before := cuts
			v := walk(u.Underlying())
			path = path[:len(path)-1]

			if v || cuts == before {
				memo[u] = v
			}

			return v
		case *types.TypeParam:
			return pred(u)
		case *types.Pointer:
			return walk(u.Elem())
		case *types.Slice:
			return walk(u.Elem())
		case *types.Array:
			return walk(u.Elem())
		case *types.Map:
			return walk(u.Elem())
		case *types.Struct:
			return opiter.WrapSeq(u.Fields()).
				Any(func(v *types.Var) bool { return walk(v.Type()) })
		}

		return false
	}

	return walk(t)
}

// throughForeign reports whether Validate walks through named as through
// its underlying type: named is a pointer or array type of a package outside
// the run without a Validate of its own. Generated code spells *x and
// ranges over x for such a type as for an unnamed one. A pointer or array
// type of the run is an error instead, as unsupported tells. The user can
// change those.
func (s *selection) throughForeign(named *types.Named) bool {
	switch named.Underlying().(type) {
	case *types.Pointer, *types.Array:
	default:
		return false
	}

	return !s.loaded(named.Obj().Pkg()) && !s.implementor(named)
}

// onPath reports whether path holds a type identical to named. Instances
// of one generic type are told apart by their type arguments.
func onPath(path []*types.Named, named *types.Named) bool {
	return slices.ContainsFunc(path, func(p *types.Named) bool { return types.Identical(p, named) })
}

// holdsForeignImplementor reports whether n holds an implementor of a
// package outside the run where Validate walks.
func (s *selection) holdsForeignImplementor(n *typegraph.Node) bool {
	return s.reaches(n.Detail.Type.Underlying(), func(named *types.Named) bool {
		return !s.loaded(named.Obj().Pkg()) && s.implementor(named)
	})
}

// loaded reports whether pkg is one of the packages of the run. A package
// of the run may import a variant of pkg other than the one the run holds,
// so packages are told by path.
func (s *selection) loaded(pkg *types.Package) bool {
	return pkg != nil &&
		slices.ContainsFunc(
			s.pkgs,
			func(p *packages.Package) bool { return p.PkgPath == pkg.Path() },
		)
}

// targetsIn returns the targets the source file at filename declares, in
// the order of their declarations.
func (s *selection) targetsIn(filename string) []*typegraph.Node {
	return typegraph.NodesIn(s.targets(), filename)
}

// sorted returns the targets ordered by package path and type name.
func (s *selection) sorted() []*typegraph.Node {
	return typegraph.SortedNodes(s.targets())
}

// checkConflicts reports names the generated methods would collide with: a
// method Validate the generator did not write that is no implementor, a
// method ValidateShallow or ValidateEvaluated the generator did not write
// on a target getting one, a field named like a generated method, a type parameter shadowing a
// predeclared
// identifier the generated code spells, and two fields whose hooks are on
// and share a name. A type parameter named like a local of Validate is no
// conflict. The local takes a fresh name instead.
func (s *selection) checkConflicts() error {
	var errs []error

	for _, n := range s.sorted() {
		named := n.Detail.Type
		pos := n.Detail.Pkg.Fset.Position

		shallow := s.hasShallow(named)
		written := map[string]bool{shallowName: shallow, evaluatedName: hasEvaluated(named)}

		for m := range named.Methods() {
			if s.generated(m) {
				continue
			}

			docs := base.DeclDocs(n.Detail.Pkg, m)

			if written[m.Name()] {
				errs = append(errs, base.ExplainConflict(
					fmt.Errorf(
						"%w: %s: %s declares %s, which the generator writes;"+
							" remove it, or ignore the type with //kama:validate ignore",
						ErrConflict, pos(m.Pos()), n.Id, m.Name(),
					),
					s.markers,
					docs...,
				))
			}

			if m.Name() != "Validate" {
				continue
			}

			recv := "value"
			if _, ok := m.Signature().Recv().Type().(*types.Pointer); ok {
				recv = "pointer"
			}

			errs = append(errs, base.ExplainConflict(
				fmt.Errorf(
					"%w: %s: %s declares Validate on its %s receiver with signature %s;"+
						" want %s on the value receiver, or ignore the type with //kama:validate ignore",
					ErrConflict, pos(m.Pos()), n.Id, recv, m.Type(), wantValidate(named),
				),
				s.markers,
				docs...,
			))
		}

		if st, ok := named.Underlying().(*types.Struct); ok {
			for v := range st.Fields() {
				if v.Name() == "Validate" || v.Name() == shallowName && shallow {
					errs = append(errs, fmt.Errorf(
						"%w: %s: field %s of %s collides with the generated method",
						ErrConflict, pos(v.Pos()), v.Name(), n.Id,
					))
				}
			}
		}

		for tp := range named.TypeParams().TypeParams() {
			name := tp.Obj().Name()
			if types.Universe.Lookup(name) != nil {
				errs = append(errs, fmt.Errorf(
					"%w: %s: type parameter %s of %s shadows the predeclared %s"+
						" the generated code spells",
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
				" rename one or turn its hook off with //kama:validate hook=False",
			ErrConflict, n.Detail.Pkg.Fset.Position(v.Pos()), prev.Name(), v.Name(), n.Id, name,
		))
	}

	return errs
}

// checkUnsupportedHolders reports types holding a value of a type that gets
// no Validate although the value holds something to validate, as
// unsupported tells. It checks the fields of struct types and the slice and
// map types that get a Validate when the trim reaches them. The values such
// a type holds would go unchecked. The check covers types the trim does not
// reach. They would be dependants if the type had a Validate. A type
// parameter of the holder counts as something to validate only in a
// target. A type parameter makes no dependant, so a holder the trim does
// not reach validates nothing behind it.
func (s *selection) checkUnsupportedHolders(g *typegraph.Graph) error {
	nodes := slices.SortedFunc(
		opiter.WrapSeq2(g.All()).
			OmitF().
			Filter(s.generates).
			Iter(),
		func(a, b *typegraph.Node) int { return a.Id.Compare(b.Id) },
	)

	trimmed := s.trim()
	unsupported := s.unsupportedFunc(trimmed)

	var errs []error

	for _, n := range nodes {
		pos := n.Detail.Pkg.Fset.Position
		u := n.Detail.Type.Underlying()
		target := s.targetIn(trimmed, n.Id)

		st, ok := u.(*types.Struct)
		if !ok {
			if named := s.unsupportedIn(u, target, unsupported); named != nil {
				errs = append(errs, unsupportedHolderError(pos(n.Detail.Type.Obj().Pos()),
					"type "+n.Id.String(), named))
			}

			continue
		}

		for v := range st.Fields() {
			if !accessible(v, n.Detail.Pkg.Types) {
				continue
			}

			// The walk follows the description of a field, not its type.
			if s.described(n.Id, v.Name()) {
				continue
			}

			if named := s.unsupportedIn(v.Type(), target, unsupported); named != nil {
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
	if isArrayOrPointer(named) {
		return fmt.Errorf(
			"%w: %s: %s holds %s, a named %s type holding values to validate; %s",
			ErrUnsupported, pos, holder, named, kindName(named.Underlying()),
			unsupportedNamedHint,
		)
	}

	return fmt.Errorf(
		"%w: %s: %s holds %s, which holds values to validate through its type arguments;"+
			" mark %s with //kama:validate to pass validation on",
		ErrUnsupported, pos, holder, named, originId(named),
	)
}

// unsupportedError reports named, which unsupported reports true for, where
// a Validate would walk into it.
func unsupportedError(named *types.Named) error {
	if isArrayOrPointer(named) {
		return fmt.Errorf(
			"%w: %s is a named %s type holding values to validate; %s",
			ErrUnsupported, named, kindName(named.Underlying()), unsupportedNamedHint,
		)
	}

	return fmt.Errorf(
		"%w: %s holds values to validate through its type arguments;"+
			" mark %s with //kama:validate to pass validation on",
		ErrUnsupported, named, originId(named),
	)
}

func isArrayOrPointer(named *types.Named) bool {
	switch named.Underlying().(type) {
	case *types.Array, *types.Pointer:
		return true
	}

	return false
}

func kindName(u types.Type) string {
	if _, ok := u.(*types.Array); ok {
		return "array"
	}

	return "pointer"
}

// originId returns the id of the generic type named instantiates.
func originId(named *types.Named) typegraph.QualifiedType {
	return typegraph.QualifiedTypeFromObject(named.Origin().Obj())
}

// unsupportedIn returns the first type unsupported reports true for that a
// value of type t holds where Validate walks, or nil. typeParams tells
// unsupported whether a type parameter counts.
func (s *selection) unsupportedIn(
	t types.Type,
	typeParams bool,
	unsupported func(named *types.Named, typeParams bool) bool,
) *types.Named {
	var found *types.Named

	s.reaches(t, func(named *types.Named) bool {
		if unsupported(named, typeParams) {
			found = named
		}

		return found != nil
	})

	return found
}

// unsupported reports whether named gets no Validate although a value of it
// holds something to validate, as the function unsupportedFunc returns
// tells.
func (s *selection) unsupported(named *types.Named, typeParams bool) bool {
	return s.unsupportedFunc(s.trim())(named, typeParams)
}

// unsupportedFunc returns the function reporting whether named gets no
// Validate although a value of it holds something to validate. trimmed is
// the graph trim returns, which tells the targets. Such a type is one of
// the run of these:
//
//   - a pointer type without a Validate of its own, or an array type that
//     is no target and has no Validate of its own.
//   - an instance of a generic struct, slice or map type that is neither a
//     target nor ignored. Its type arguments hold what it holds. A type
//     holding something to validate otherwise is a target itself.
//
// Something to validate is a value of a target, of an implementor or of
// another such type. With typeParams, a value of a type parameter counts
// too, as in PT[T] of type PT[T any] *T. The type arguments of named are
// those of the type holding it, so the type parameter is one of that
// holder.
func (s *selection) unsupportedFunc(
	trimmed *typegraph.MatchedGraph,
) func(named *types.Named, typeParams bool) bool {
	// memo keeps the answers of one check, and the caller passes one trimmed
	// graph for all of them. Without both, the walk visits a type once per
	// path to it and trims the graph again at each type, which is
	// exponential in the depth of a chain such as
	// type A00 [1]struct{ X A01; Y A01 } down to A22 held by a target: a
	// test generating from it ran 0.6 s at depth 10, 13 s at depth 16 and
	// longer than 6.5 minutes at depth 22. With both, Generate takes 7 ms
	// at depth 22. The key holds every input of the answer. A false answer
	// that a cut of a cycle gave depends on the path to named and is not
	// kept.
	type key struct {
		named      *types.Named
		typeParams bool
	}

	var (
		memo = map[key]bool{}
		// path holds the types on the way to named. It stops a cycle such
		// as type A [2]B; type B *A.
		path        []*types.Named
		cuts        int
		unsupported func(named *types.Named, typeParams bool) bool
	)

	unsupported = func(named *types.Named, typeParams bool) bool {
		k := key{named, typeParams}
		if v, ok := memo[k]; ok {
			return v
		}

		if !s.getsNoValidate(trimmed, named) {
			return false
		}

		if onPath(path, named) {
			cuts++
			return false
		}

		path = append(path, named)
		before := cuts

		holds := s.reachesLeaf(named.Underlying(), s.holdsLeaf(trimmed, typeParams, unsupported))

		path = path[:len(path)-1]

		if holds || cuts == before {
			memo[k] = holds
		}

		return holds
	}

	return unsupported
}

// holdsLeaf returns the predicate over the types ending a walk that tells
// something to validate: a target of trimmed, an implementor, a type
// unsupported reports true for, and with typeParams a type parameter.
func (s *selection) holdsLeaf(
	trimmed *typegraph.MatchedGraph,
	typeParams bool,
	unsupported func(named *types.Named, typeParams bool) bool,
) func(leaf types.Type) bool {
	return func(leaf types.Type) bool {
		switch leaf := leaf.(type) {
		case *types.TypeParam:
			return typeParams
		case *types.Named:
			return s.callableIn(trimmed, leaf) || unsupported(leaf, typeParams)
		}

		return false
	}
}

// getsNoValidate reports whether named is a type of the run that gets no
// Validate and that Validate cannot walk through: a pointer type without a
// Validate of its own, an array type that is neither a target nor has a
// Validate of its own, or an instance of a generic struct, slice or map
// type that is neither a target nor ignored. An array type is a target
// only with validators of its own. An ignored type is left unchecked on
// purpose.
func (s *selection) getsNoValidate(trimmed *typegraph.MatchedGraph, named *types.Named) bool {
	if !s.loaded(named.Obj().Pkg()) || s.implementor(named) {
		return false
	}

	switch named.Underlying().(type) {
	case *types.Pointer:
		return true
	case *types.Array:
		return !s.targetIn(trimmed, originId(named))
	case *types.Struct, *types.Slice, *types.Map:
		id := originId(named)
		return named.TypeArgs().Len() > 0 && !s.targetIn(trimmed, id) && !s.ignored(id)
	}

	return false
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
