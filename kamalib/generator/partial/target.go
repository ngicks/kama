package partial

import (
	"cmp"
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

	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

const (
	toolName  = "kama"
	directive = "partial"
)

// selection is the set of types to generate partial variants of, with
// the options of the run.
//
// targets is derived: it is the marked types and every type reachable
// from them. It is kept as the one decision of the run that everything
// else asks about. The other maps hold options, decoded once from
// directive comments and option files.
type selection struct {
	opts    Options
	targets map[typegraph.QualifiedType]*typegraph.Node
	// merges are the merge strategies keyed by field.
	merges map[fieldKey]mergeOption
	// flags are the flag options keyed by field.
	flags map[fieldKey]flagOption
	// masked are the types with field masks enabled, and maskNames the
	// field mask names keyed by field.
	masked    map[typegraph.QualifiedType]optionSource
	maskNames map[fieldKey]maskName
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

// compare orders k and other by package path, type name and field name.
func (k fieldKey) compare(other fieldKey) int {
	return cmp.Or(
		cmp.Compare(k.pkgPath, other.pkgPath),
		cmp.Compare(k.typeName, other.typeName),
		cmp.Compare(k.field, other.field),
	)
}

func (k fieldKey) String() string { return k.pkgPath + "." + k.typeName + "." + k.field }

// mergeOption is the merge strategy of a field.
type mergeOption struct {
	strategy string
	src      optionSource
}

// target reports the target named type t refers to, through aliases.
func (s *selection) target(t types.Type) (*types.Named, bool) {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return nil, false
	}
	// Obj of an instantiated type is the TypeName of its origin.
	_, ok = s.targets[typegraph.QualifiedTypeFromObject(named.Obj())]

	return named, ok
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

// marks are the options of the source packages, from the directives and
// the option files.
type marks struct {
	merges    map[fieldKey]mergeOption
	flags     map[fieldKey]flagOption
	masked    map[typegraph.QualifiedType]optionSource
	maskNames map[fieldKey]maskName
	// explicit are the marked types ordered by package path and type name.
	explicit []markedType
	ignored  map[typegraph.QualifiedType]optionSource
	// files are the file names as packages reports them.
	files map[string]bool
	pkgs  map[string]bool
}

// markedType is a type marked by //kama:partial or by its option file
// entry.
type markedType struct {
	id  typegraph.QualifiedType
	src optionSource
}

func (m *marks) isIgnored(id typegraph.QualifiedType) bool {
	_, ok := m.ignored[id]
	return ok
}

// collectMarks collects the options of o and checks their values.
func collectMarks(o genopt.Option[PackageOptions, TypeOptions, FieldOptions]) (marks, error) {
	m := marks{
		merges:    map[fieldKey]mergeOption{},
		flags:     map[fieldKey]flagOption{},
		masked:    map[typegraph.QualifiedType]optionSource{},
		maskNames: map[fieldKey]maskName{},
		ignored:   map[typegraph.QualifiedType]optionSource{},
		files:     map[string]bool{},
		pkgs:      map[string]bool{},
	}

	var errs []error

	for _, pkgPath := range slices.Sorted(maps.Keys(o.Packages)) {
		po := o.Packages[pkgPath]
		if po.Package.AllTypes {
			m.pkgs[pkgPath] = true
		}

		for file, p := range po.Files {
			if p.AllTypes {
				m.pkgs[pkgPath] = true
			}

			if p.file {
				m.files[file] = true
			}
		}

		for _, typeName := range slices.Sorted(maps.Keys(po.Types)) {
			to := po.Types[typeName]
			id := typegraph.NewQualifiedType(pkgPath, typeName)

			if err := m.typeMarks(id, to.Group, to.Type); err != nil {
				errs = append(errs, err)
			}

			fields, _ := to.Fields.Unpack()
			for _, fieldName := range slices.Sorted(maps.Keys(fields)) {
				errs = append(errs, m.fieldMarks(fieldKeyOf(id, fieldName), fields[fieldName])...)
			}
		}
	}

	return m, errors.Join(errs...)
}

// typeMarks records the options of the type id, those of its type group
// and its own.
func (m *marks) typeMarks(id typegraph.QualifiedType, group, typ TypeOptions) error {
	var (
		mark, ignore        optionSource
		isMarked, isIgnored bool
	)

	for _, o := range []TypeOptions{group, typ} {
		if o.marked && !isMarked {
			mark, isMarked = o.markSrc, true
		}

		if o.Ignore && !isIgnored {
			ignore, isIgnored = o.markSrc, true
		}

		if _, ok := m.masked[id]; o.FieldMask && !ok {
			m.masked[id] = o.maskSrc
		}
	}

	if isMarked && isIgnored {
		return fmt.Errorf(
			"%w: %s: %s is both marked and ignored (%s)",
			ErrDirective, mark.origin(), id.String(), ignore.origin(),
		)
	}

	if isMarked {
		m.explicit = append(m.explicit, markedType{id: id, src: mark})
	}

	if isIgnored {
		m.ignored[id] = ignore
	}

	return nil
}

// fieldMarks records the options of the field k.
func (m *marks) fieldMarks(k fieldKey, o FieldOptions) []error {
	var errs []error

	if o.Merge != "" {
		switch o.Merge {
		case string(MergeOverwrite), string(MergeAppend), string(MergeNestedRecursive):
			m.merges[k] = mergeOption{strategy: o.Merge, src: o.mergeSrc}
		default:
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s: unknown strategy %q on field %s; want overwrite, append or recursive",
				ErrDirective, o.mergeSrc.origin(), o.mergeSrc.spelling(), o.Merge, k,
			))
		}
	}

	if o.Flag != nil {
		f, err := flagFromOptions(*o.Flag)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s on field %s: %w",
				ErrDirective, o.flagSrc.origin(), o.flagSrc.spelling(), k, err,
			))
		} else {
			f.src = o.flagSrc
			m.flags[k] = f
		}
	}

	if o.FieldMask != nil {
		mn, err := maskNameFromArgs(map[string]string{"name": o.FieldMask.Name})
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s on field %s: %w",
				ErrDirective, o.maskSrc.origin(), o.maskSrc.spelling(), k, err,
			))
		} else {
			mn.src = o.maskSrc
			m.maskNames[k] = mn
		}
	}

	return errs
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

	g, err := typegraph.New(pkgs, nil, func(_ *ast.TypeSpec, obj types.Object) (bool, error) {
		return !sel.generated(obj), nil
	})
	if err != nil {
		return nil, fmt.Errorf("partial: %w", err)
	}

	m, err := collectMarks(opts.Loaded.Option)
	if err != nil {
		return nil, err
	}

	sel.targets = map[typegraph.QualifiedType]*typegraph.Node{}
	sel.merges, sel.flags = m.merges, m.flags
	sel.masked, sel.maskNames = m.masked, m.maskNames

	var errs []error

	for _, r := range m.explicit {
		id := r.id
		name := id.String()
		pos := r.src.origin()

		n, ok := g.Get(id)
		if !ok {
			if obj := typegraph.LookupType(pkgs, id); obj != nil && sel.generated(obj) {
				continue
			}

			errs = append(errs, fmt.Errorf(
				"%w: %s: %s is an alias; mark the type it denotes", ErrUnsupported, pos, name,
			))

			continue
		}

		if !typequery.IsStructSliceOrMap(n.Detail.Type) {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s has underlying type %s; want a struct, slice or map",
				ErrUnsupported, pos, name, n.Detail.Type.Underlying(),
			))

			continue
		}

		sel.targets[id] = n
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	for id, n := range g.All() {
		if m.isIgnored(id) || !typequery.IsStructSliceOrMap(n.Detail.Type) {
			continue
		}

		if m.pkgs[id.PackagePath] || m.files[n.Filename()] {
			sel.targets[id] = n
		}
	}

	queue := slices.Collect(maps.Values(sel.targets))
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]

		for id, e := range n.Children {
			if sel.targets[id] != nil || m.isIgnored(id) ||
				!typequery.IsStructSliceOrMap(e.Child.Detail.Type) ||
				!slices.ContainsFunc(e.Routes, followed) {
				continue
			}

			sel.targets[id] = e.Child
			queue = append(queue, e.Child)
		}
	}

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

// checkMerges reports //kama:partial:merge on fields of types without
// partial variants, where it would have no effect.
func (s *selection) checkMerges() error {
	hasNoTarget := func(k fieldKey) bool {
		return s.targets[typegraph.NewQualifiedType(k.pkgPath, k.typeName)] == nil
	}
	mergeErr := func(k fieldKey) error {
		src := s.merges[k].src

		return fmt.Errorf(
			"%w: %s: %s on field %s; %s has no partial variant",
			ErrDirective, src.origin(), src.spelling(), k, k.typeName,
		)
	}

	return errors.Join(
		opiter.WrapSeq2(opiter.MapsSortedFunc(s.merges, fieldKey.compare)).
			OmitL().
			Filter(hasNoTarget).
			Map(mergeErr).
			CollectSlice()...,
	)
}

// checkFlagTargets reports //kama:partial:flag on fields of types without
// partial variants.
func (s *selection) checkFlagTargets() error {
	var errs []error

	for _, k := range slices.SortedFunc(maps.Keys(s.flags), fieldKey.compare) {
		id := typegraph.NewQualifiedType(k.pkgPath, k.typeName)
		if s.targets[id] != nil {
			continue
		}

		src := s.flags[k].src
		errs = append(errs, fmt.Errorf(
			"%w: %s: %s on field %s; %s has no partial variant",
			ErrDirective, src.origin(), src.spelling(), k, k.typeName,
		))
	}

	return errors.Join(errs...)
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

// targetsIn returns the targets the source file at filename declares, in
// the order of their declarations.
func (s *selection) targetsIn(filename string) []*typegraph.Node {
	return typegraph.NodesIn(maps.Values(s.targets), filename)
}
