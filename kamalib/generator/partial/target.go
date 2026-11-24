package partial

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"iter"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ngicks/opiter"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

const directive = "partial"

// selection is the run: the loaded packages and the options, which were
// decoded once from directive comments and option files. Every fact, the
// set of targets included, is read again from the types and from the
// options where it is needed.
type selection struct {
	opts Options
	// pkgs are the loaded packages the run generates into.
	pkgs []*packages.Package
}

// fieldKey names a struct field of a type.
type fieldKey struct {
	pkgPath, typeName, field string
}

func fieldKeyOf(id typegraph.QualifiedType, field string) fieldKey {
	return fieldKey{pkgPath: id.PackagePath, typeName: id.TypeName, field: field}
}

func (k fieldKey) String() string { return k.pkgPath + "." + k.typeName + "." + k.field }

// target reports the target named type t refers to, through aliases.
//
// Callers ask only about types a target reaches where followed tells. The
// targets hold every such type generates reports true for, so generates
// alone tells a target there without building the type graph.
func (s *selection) target(t types.Type) (*types.Named, bool) {
	named, ok := types.Unalias(t).(*types.Named)
	return named, ok && s.generates(named)
}

// generates reports whether named gets a partial variant once a target
// reaches it where followed tells, or once it is marked: a struct, slice or
// map type declared at the top level of a loaded package outside the files
// a generator wrote, and not ignored.
func (s *selection) generates(named *types.Named) bool {
	// Obj of an instantiated type is the TypeName of its origin.
	obj := named.Obj()

	pkg := s.loaded(obj)
	if pkg == nil || pkg.Types.Scope().Lookup(obj.Name()) != obj {
		return false
	}

	return typequery.IsStructSliceOrMap(named) && !s.generated(obj) &&
		!s.ignored(typegraph.QualifiedTypeFromObject(obj))
}

// loaded returns the loaded package declaring obj, or nil.
func (s *selection) loaded(obj types.Object) *packages.Package {
	return opiter.WrapSeq(slices.Values(s.pkgs)).
		Filter(func(pkg *packages.Package) bool { return pkg.Types == obj.Pkg() }).
		First().
		UnwrapOrDefault()
}

// partialOf reports the target or the implementor t refers to when a
// declaration of the package at pkgPath can refer to its partial
// variant. The partial
// variant of an unexported type of another package, reached through an
// exported alias, cannot be spelled there.
func (s *selection) partialOf(t types.Type, pkgPath string) (*types.Named, bool) {
	named, ok := s.target(t)
	if !ok {
		named, ok = s.implementor(t)
	}

	if !ok {
		return nil, false
	}

	return named, named.Obj().Exported() || named.Obj().Pkg().Path() == pkgPath
}

// targets returns the targets keyed by id, read off a type graph built
// again from the packages.
func (s *selection) targets() map[typegraph.QualifiedType]*typegraph.Node {
	g, err := base.TypeGraph(s.pkgs, s.opts.Registry)
	if err != nil {
		// selectTargets built the same graph from the same packages and
		// stopped the run on its error.
		panic(fmt.Errorf("partial: %w", err))
	}

	return s.targetsOf(g)
}

// followed reports whether r reaches its child through a struct field, a
// slice or array element, a map value or a pointer. Map keys, channels and
// type arguments never hold a partial value, so they do not make their
// types reachable.
func followed(r *typegraph.EdgeRoute) bool {
	const allowed = typegraph.EdgeKindStruct |
		typegraph.EdgeKindSlice |
		typegraph.EdgeKindArray |
		typegraph.EdgeKindMapValue |
		typegraph.EdgeKindPointer |
		typegraph.EdgeKindAlias

	return opiter.WrapSeq(slices.Values(r.Nodes)).
		Every(func(n typegraph.EdgeRouteNode) bool { return n.Kind&allowed != 0 })
}

func selectTargets(pkgs []*packages.Package, opts Options) (*selection, error) {
	sel := &selection{opts: opts, pkgs: pkgs}

	g, err := base.TypeGraph(pkgs, opts.Registry)
	if err != nil {
		return nil, fmt.Errorf("partial: %w", err)
	}

	if err := sel.checkOptions(); err != nil {
		return nil, err
	}

	if err := sel.checkMarks(g); err != nil {
		return nil, err
	}

	targets := sel.targetsOf(g)
	sorted := typegraph.SortedNodes(maps.Values(targets))

	if err := sel.checkMerges(targets); err != nil {
		return nil, err
	}

	if err := sel.checkFlagTargets(targets); err != nil {
		return nil, err
	}

	if err := sel.checkFieldMaskTargets(targets); err != nil {
		return nil, err
	}

	if err := sel.checkNames(sorted); err != nil {
		return nil, err
	}

	if err := sel.checkEmbeddedCycles(sorted); err != nil {
		return nil, err
	}
	// Segments flatten embedded partial values, which terminates only once
	// checkEmbeddedCycles has ruled out cycles through embedded fields.
	if err := sel.checkFieldMaskSegments(sorted); err != nil {
		return nil, err
	}

	return sel, nil
}

// targetsOf returns the targets of the type graph g keyed by id: the
// marked types generates reports true for and the types they reach where
// followed tells that generates reports true for.
func (s *selection) targetsOf(g *typegraph.Graph) map[typegraph.QualifiedType]*typegraph.Node {
	targets := map[typegraph.QualifiedType]*typegraph.Node{}

	for id, n := range g.All() {
		if s.generates(n.Detail.Type) && s.marked(n) {
			targets[id] = n
		}
	}

	queue := slices.Collect(maps.Values(targets))
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]

		for id, e := range n.Children {
			if targets[id] != nil || !s.generates(e.Child.Detail.Type) ||
				!slices.ContainsFunc(e.Routes, followed) {
				continue
			}

			targets[id] = e.Child
			queue = append(queue, e.Child)
		}
	}

	return targets
}

// checkMarks reports types marked explicitly that cannot have partial
// variants: aliases and types of other kinds than struct, slice and map.
func (s *selection) checkMarks(g *typegraph.Graph) error {
	var errs []error

	for id, to := range s.opts.Loaded.Option.Types() {
		if mark, ok := explicitMark(to); ok {
			errs = append(errs, base.MarkedTypeError(
				s.pkgs, s.opts.Registry, g, id, mark.origin(), ErrUnsupported,
			))
		}
	}

	// errors.Join drops the nil errors of types the generator can mark.
	return errors.Join(errs...)
}

// checkMerges reports //kama:partial:merge on fields of types without
// partial variants, where it would have no effect. targets are the targets
// keyed by id.
func (s *selection) checkMerges(targets map[typegraph.QualifiedType]*typegraph.Node) error {
	return errors.Join(
		opiter.WrapSeq2(s.allFieldOptions()).
			Filter(
				func(k fieldKey, o FieldOptions) bool { return o.Merge != "" && !ofTarget(targets, k) },
			).
			Assemble(func(k fieldKey, o FieldOptions) error { return noPartialError(k, o.mergeSrc) }).
			CollectSlice()...,
	)
}

// checkFlagTargets reports //kama:partial:flag on fields of types without
// partial variants. targets are the targets keyed by id.
func (s *selection) checkFlagTargets(targets map[typegraph.QualifiedType]*typegraph.Node) error {
	return errors.Join(
		opiter.WrapSeq2(s.allFieldOptions()).
			Filter(
				func(k fieldKey, o FieldOptions) bool { return o.Flag != nil && !ofTarget(targets, k) },
			).
			Assemble(func(k fieldKey, o FieldOptions) error { return noPartialError(k, o.flagSrc) }).
			CollectSlice()...,
	)
}

// ofTarget reports whether the field k is a field of one of targets.
func ofTarget(targets map[typegraph.QualifiedType]*typegraph.Node, k fieldKey) bool {
	_, ok := targets[typegraph.NewQualifiedType(k.pkgPath, k.typeName)]
	return ok
}

// noPartialError reports the field option at src on the field k of a type
// without a partial variant.
func noPartialError(k fieldKey, src optionSource) error {
	return fmt.Errorf(
		"%w: %s: %s on field %s; %s has no partial variant",
		ErrDirective, src.origin(), src.spelling(), k, k.typeName,
	)
}

// checkNames reports names the generated declarations would collide with:
// a partial type name, a method of a partial type or the flag set
// interface declared by something other than a generator, and a flag set
// file or helpers file holding a declaration the generator did not write. A field of a
// partial type named like one of its methods is reported while mapping the
// fields, and a helper named like another declaration once the helpers are
// known. sorted are the targets ordered by id.
func (s *selection) checkNames(sorted []*typegraph.Node) error {
	var errs []error

	conflict := func(fset *token.FileSet, pos token.Pos, format string, args ...any) {
		errs = append(errs, fmt.Errorf(
			"%w: %s: "+format,
			append([]any{ErrConflict, fset.Position(pos)}, args...)...,
		))
	}

	targetPkgs := map[string]*packages.Package{}

	for _, n := range sorted {
		pkg := n.Detail.Pkg
		targetPkgs[pkg.PkgPath] = pkg
		name := partialName(n.Id.TypeName)

		obj := pkg.Types.Scope().Lookup(name)
		if obj == nil {
			continue
		}

		if !s.generated(obj) {
			conflict(pkg.Fset, obj.Pos(),
				"%s.%s is already declared for the partial variant of %s",
				n.Id.PackagePath, name, n.Id.TypeName,
			)

			continue
		}

		named, ok := obj.Type().(*types.Named)
		if !ok {
			continue
		}

		for m := range named.Methods() {
			if slices.Contains(s.partialMethodNames(n), m.Name()) && !s.generated(m) {
				conflict(pkg.Fset, m.Pos(),
					"method %s of %s.%s is declared by something other than the generator",
					m.Name(), n.Id.PackagePath, name,
				)
			}
		}
	}

	for _, pkgPath := range slices.Sorted(maps.Keys(targetPkgs)) {
		pkg := targetPkgs[pkgPath]
		if obj := pkg.Types.Scope().Lookup(flagSetName); obj != nil && !s.generated(obj) {
			conflict(pkg.Fset, obj.Pos(),
				"%s.%s is already declared for the flag set of the generated methods",
				pkgPath, flagSetName,
			)
		}

		for _, f := range pkg.Syntax {
			base := filepath.Base(pkg.Fset.Position(f.FileStart).Filename)
			if base != FlagSetFile && !isHelpersFile(base) {
				continue
			}

			for _, decl := range f.Decls {
				if s.ownDecl(base, decl) {
					continue
				}

				conflict(pkg.Fset, decl.Pos(),
					"%s holds a declaration the generator did not write", base,
				)
			}
		}
	}

	return errors.Join(errs...)
}

// ownDecl reports whether decl of the file named base is one the generator
// writes there: imports, and marked types in FlagSetFile or marked
// functions in a helpers file.
func (s *selection) ownDecl(base string, decl ast.Decl) bool {
	switch d := decl.(type) {
	case *ast.GenDecl:
		if d.Tok == token.IMPORT {
			return true
		}

		_, ok := s.opts.Registry.Generated(d.Doc)

		return ok && d.Tok == token.TYPE && base == FlagSetFile
	case *ast.FuncDecl:
		_, ok := s.opts.Registry.Generated(d.Doc)
		return ok && d.Recv == nil && isHelpersFile(base)
	}

	return false
}

// closesCycle reports whether the pointer field v of the struct target n
// closes a cycle of partial variants held by value, and so maps to
// und.Und[*MPartial].
//
// It does when the target v points to reaches n again through the fields
// holding partial variants by value. That is the same as n and that target
// lying in one strongly connected component, so every pointer field on a
// cycle closes it and the answer does not depend on the order types are
// visited in. An embedded pointer closes no cycle: its partial variant
// stays embedded by value.
func (s *selection) closesCycle(n *types.Named, v *types.Var) bool {
	elem := deref(v.Type())
	if elem == v.Type() || v.Embedded() {
		return false
	}

	// An implementor lies outside the run and closes no cycle.
	child, ok := s.valueChild(n, elem)
	if !ok {
		return false
	}

	id := typegraph.QualifiedTypeFromObject(n.Obj())
	seen := map[typegraph.QualifiedType]bool{}

	var reaches func(from *types.Named) bool

	reaches = func(from *types.Named) bool {
		fromId := typegraph.QualifiedTypeFromObject(from.Obj())
		if fromId == id {
			return true
		}

		if seen[fromId] {
			return false
		}

		seen[fromId] = true

		for _, next := range s.valueChildren(from) {
			if reaches(next) {
				return true
			}
		}

		return false
	}

	return reaches(child)
}

// checkEmbeddedCycles reports a cycle of partial variants held by value
// that no pointer field closes. Such a cycle goes through embedded
// pointers, whose partial variants stay embedded by value, and would make
// the partial variants recursive types. sorted are the targets ordered by
// id.
func (s *selection) checkEmbeddedCycles(sorted []*typegraph.Node) error {
	const (
		visiting = iota + 1
		done
	)

	state := map[typegraph.QualifiedType]int{}

	var path []typegraph.QualifiedType

	var visit func(n *types.Named) error

	visit = func(n *types.Named) error {
		id := typegraph.QualifiedTypeFromObject(n.Obj())

		switch state[id] {
		case visiting:
			i := slices.Index(path, id)

			names := opiter.WrapSeq(slices.Values(append(path[i:], id))).
				Map(func(id typegraph.QualifiedType) string { return id.TypeName }).
				CollectSlice()

			return fmt.Errorf(
				"%w: %s (%s): partial variants would be recursive through embedded fields: %s",
				ErrUnsupported,
				id.String(),
				s.position(n, n.Obj().Pos()),
				strings.Join(names, " -> "),
			)
		case done:
			return nil
		}

		state[id] = visiting

		path = append(path, id)

		for v, child := range s.valueChildren(n) {
			if s.closesCycle(n, v) {
				continue
			}

			if err := visit(child); err != nil {
				return err
			}
		}

		path = path[:len(path)-1]
		state[id] = done

		return nil
	}

	for _, n := range sorted {
		if err := visit(n.Detail.Type); err != nil {
			return err
		}
	}

	return nil
}

// valueChildren yields the fields of the struct target n whose partial
// values the partial variant of n holds without indirection, each with the
// origin of the target it holds: fields of a target type or of a pointer
// to one.
func (s *selection) valueChildren(n *types.Named) iter.Seq2[*types.Var, *types.Named] {
	return func(yield func(*types.Var, *types.Named) bool) {
		st, ok := n.Origin().Underlying().(*types.Struct)
		if !ok {
			return
		}

		for v := range st.Fields() {
			if child, ok := s.valueChild(n, deref(v.Type())); ok && !yield(v, child) {
				return
			}
		}
	}
}

// valueChild returns the origin of the target t refers to when the
// partial variant of the struct target n can hold its partial variant. An
// implementor lies outside the run and is no such target.
func (s *selection) valueChild(n *types.Named, t types.Type) (*types.Named, bool) {
	named, ok := s.partialOf(t, n.Obj().Pkg().Path())
	if !ok {
		return nil, false
	}

	if _, ok := s.target(named); !ok {
		return nil, false
	}

	return named.Origin(), true
}

// position returns pos in the file set of the package of the target n. pos
// may lie in a package outside the run, as the fields of type A sub.S do.
func (s *selection) position(n *types.Named, pos token.Pos) token.Position {
	return s.loaded(n.Obj()).Fset.Position(pos)
}

// deref strips one pointer from t, through aliases.
func deref(t types.Type) types.Type {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		return p.Elem()
	}

	return t
}
