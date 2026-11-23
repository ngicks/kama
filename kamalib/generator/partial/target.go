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

	"github.com/ngicks/kama/kamalib/pkg/typegraph"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

const (
	toolName  = "kama"
	directive = "partial"
)

// selection is the run: the loaded packages, the options, which were
// decoded once from directive comments and option files, and the targets.
// Every other fact is read again from the types and from the options where
// it is needed.
//
// targets is derived from the type graph and kept because mapping asks
// whether a type is a target once per field, and the cycle checks once per
// step of their walks. Building the graph again at each question took
// Generate from 170 ms to 6.6 s on a package of 25 struct types with 9
// fields each.
type selection struct {
	opts    Options
	targets map[typegraph.QualifiedType]*typegraph.Node
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
func (s *selection) target(t types.Type) (*types.Named, bool) {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return nil, false
	}
	// Obj of an instantiated type is the TypeName of its origin.
	return named, s.isTargetId(typegraph.QualifiedTypeFromObject(named.Obj()))
}

// isTargetId reports whether the type id has a partial variant.
func (s *selection) isTargetId(id typegraph.QualifiedType) bool {
	_, ok := s.targets[id]
	return ok
}

// ofTarget reports whether the field k is a field of a target.
func (s *selection) ofTarget(k fieldKey) bool {
	return s.isTargetId(typegraph.NewQualifiedType(k.pkgPath, k.typeName))
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

// node returns the node of the target named, or nil for an implementor,
// which the graph of the run does not hold.
func (s *selection) node(named *types.Named) *typegraph.Node {
	return s.targets[typegraph.QualifiedTypeFromObject(named.Obj())]
}

// sorted returns the targets ordered by package path and type name.
func (s *selection) sorted() []*typegraph.Node {
	return typegraph.SortedNodes(maps.Values(s.targets))
}

// targetsIn returns the targets the source file at filename declares, in
// the order of their declarations.
func (s *selection) targetsIn(filename string) []*typegraph.Node {
	return typegraph.NodesIn(maps.Values(s.targets), filename)
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

	g, err := sel.graph()
	if err != nil {
		return nil, fmt.Errorf("partial: %w", err)
	}

	if err := sel.checkOptions(); err != nil {
		return nil, err
	}

	if err := sel.checkMarks(g); err != nil {
		return nil, err
	}

	sel.targets = sel.targetsOf(g)

	if err := sel.checkMerges(); err != nil {
		return nil, err
	}

	if err := sel.checkFlagTargets(); err != nil {
		return nil, err
	}

	if err := sel.checkFieldMaskTargets(); err != nil {
		return nil, err
	}

	if err := sel.checkNames(); err != nil {
		return nil, err
	}

	if err := sel.checkEmbeddedCycles(); err != nil {
		return nil, err
	}
	// Segments flatten embedded partial values, which terminates only once
	// checkEmbeddedCycles has ruled out cycles through embedded fields.
	if err := sel.checkFieldMaskSegments(); err != nil {
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

// targetsOf returns the targets of the type graph g keyed by id: the
// marked struct, slice and map types and the types they reach where
// followed tells, ignored types left out.
func (s *selection) targetsOf(g *typegraph.Graph) map[typegraph.QualifiedType]*typegraph.Node {
	targets := map[typegraph.QualifiedType]*typegraph.Node{}

	for id, n := range g.All() {
		if typequery.IsStructSliceOrMap(n.Detail.Type) && s.marked(n) {
			targets[id] = n
		}
	}

	queue := slices.Collect(maps.Values(targets))
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]

		for id, e := range n.Children {
			if targets[id] != nil || s.ignored(id) ||
				!typequery.IsStructSliceOrMap(e.Child.Detail.Type) ||
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

	for id, to := range s.allTypeOptions() {
		mark, ok := explicitMark(to)
		if !ok {
			continue
		}

		n, ok := g.Get(id)
		if !ok {
			if obj := typegraph.LookupType(s.pkgs, id); obj == nil || !s.generated(obj) {
				errs = append(errs, fmt.Errorf(
					"%w: %s: %s is an alias; mark the type it denotes",
					ErrUnsupported, mark.origin(), id.String(),
				))
			}

			continue
		}

		if !typequery.IsStructSliceOrMap(n.Detail.Type) {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s has underlying type %s; want a struct, slice or map",
				ErrUnsupported, mark.origin(), id.String(), n.Detail.Type.Underlying(),
			))
		}
	}

	return errors.Join(errs...)
}

// checkMerges reports //kama:partial:merge on fields of types without
// partial variants, where it would have no effect.
func (s *selection) checkMerges() error {
	return errors.Join(
		opiter.WrapSeq2(s.allFieldOptions()).
			Filter(func(k fieldKey, o FieldOptions) bool { return o.Merge != "" && !s.ofTarget(k) }).
			Assemble(func(k fieldKey, o FieldOptions) error { return noPartialError(k, o.mergeSrc) }).
			CollectSlice()...,
	)
}

// checkFlagTargets reports //kama:partial:flag on fields of types without
// partial variants.
func (s *selection) checkFlagTargets() error {
	return errors.Join(
		opiter.WrapSeq2(s.allFieldOptions()).
			Filter(func(k fieldKey, o FieldOptions) bool { return o.Flag != nil && !s.ofTarget(k) }).
			Assemble(func(k fieldKey, o FieldOptions) error { return noPartialError(k, o.flagSrc) }).
			CollectSlice()...,
	)
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
// known.
func (s *selection) checkNames() error {
	var errs []error

	conflict := func(fset *token.FileSet, pos token.Pos, format string, args ...any) {
		errs = append(errs, fmt.Errorf(
			"%w: %s: "+format,
			append([]any{ErrConflict, fset.Position(pos)}, args...)...,
		))
	}

	targetPkgs := map[string]*packages.Package{}

	for _, n := range s.sorted() {
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
func (s *selection) closesCycle(n *typegraph.Node, v *types.Var) bool {
	elem := deref(v.Type())
	if elem == v.Type() || v.Embedded() {
		return false
	}

	named, ok := s.partialOf(elem, n.Id.PackagePath)
	if !ok {
		return false
	}
	// An implementor lies outside the run and closes no cycle.
	child := s.node(named)
	if child == nil {
		return false
	}

	seen := map[typegraph.QualifiedType]bool{}

	var reaches func(from *typegraph.Node) bool

	reaches = func(from *typegraph.Node) bool {
		if from.Id == n.Id {
			return true
		}

		if seen[from.Id] {
			return false
		}

		seen[from.Id] = true

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
// the partial variants recursive types.
func (s *selection) checkEmbeddedCycles() error {
	const (
		visiting = iota + 1
		done
	)

	state := map[typegraph.QualifiedType]int{}

	var path []typegraph.QualifiedType

	var visit func(n *typegraph.Node) error

	visit = func(n *typegraph.Node) error {
		switch state[n.Id] {
		case visiting:
			i := slices.Index(path, n.Id)

			names := opiter.WrapSeq(slices.Values(append(path[i:], n.Id))).
				Map(func(id typegraph.QualifiedType) string { return id.TypeName }).
				CollectSlice()

			return fmt.Errorf(
				"%w: %s (%s): partial variants would be recursive through embedded fields: %s",
				ErrUnsupported, n.Id.String(),
				n.Detail.Pkg.Fset.Position(n.Detail.Ts.Pos()), strings.Join(names, " -> "),
			)
		case done:
			return nil
		}

		state[n.Id] = visiting

		path = append(path, n.Id)
		for v, child := range s.valueChildren(n) {
			if s.closesCycle(n, v) {
				continue
			}

			if err := visit(child); err != nil {
				return err
			}
		}

		path = path[:len(path)-1]
		state[n.Id] = done

		return nil
	}

	for _, n := range s.sorted() {
		if err := visit(n); err != nil {
			return err
		}
	}

	return nil
}

// valueChildren yields the fields of the struct target n whose partial
// values the partial variant of n holds without indirection, each with the
// target it holds: fields of a target type or of a pointer to one.
func (s *selection) valueChildren(n *typegraph.Node) iter.Seq2[*types.Var, *typegraph.Node] {
	return func(yield func(*types.Var, *typegraph.Node) bool) {
		st, ok := n.Detail.Type.Underlying().(*types.Struct)
		if !ok {
			return
		}

		for v := range st.Fields() {
			named, ok := s.partialOf(deref(v.Type()), n.Id.PackagePath)
			if !ok {
				continue
			}
			// An implementor lies outside the run and closes no cycle.
			if child := s.node(named); child != nil && !yield(v, child) {
				return
			}
		}
	}
}

// deref strips one pointer from t, through aliases.
func deref(t types.Type) types.Type {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		return p.Elem()
	}

	return t
}
