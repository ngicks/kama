package partial

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/dstreplace"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/kamaopt"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

const (
	toolName  = "kama"
	directive = "partial"
)

// selection is the set of types to generate partial variants of.
type selection struct {
	opts    Options
	targets map[typegraph.QualifiedType]*typegraph.Node
	// merges are the //kama:partial:merge directives keyed by field.
	merges map[kamaopt.Key]kamaopt.Raw
	// flags are the //kama:partial:flag directives keyed by field.
	flags map[kamaopt.Key]flagOption
	// flagged are the targets with a field holding a flag directive.
	flagged map[typegraph.QualifiedType]bool
	// masked are the //kama:partial:fieldmask directives keyed by type, and
	// maskNames those keyed by field.
	masked    map[typegraph.QualifiedType]kamaopt.Raw
	maskNames map[kamaopt.Key]maskName
	// refs are the pointer fields mapped to und.Und[*MPartial] since they
	// close a cycle of partial variants.
	refs map[*types.Var]bool
	// generated are the objects declared by declarations carrying the
	// marker of a registered generator.
	generated map[types.Object]bool
	// sources holds the decorated source files declaring targets. Every
	// partial file is rendered twice, so the files are decorated once per
	// run rather than once per rendering.
	sources dstreplace.Cache
}

func (s *selection) filename(n *typegraph.Node) string {
	return n.Detail.Pkg.Fset.Position(n.Detail.File.FileStart).Filename
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

// partialOf reports the target t refers to when a declaration of the
// package at pkgPath can refer to its partial variant. The partial
// variant of an unexported type of another package, reached through an
// exported alias, cannot be spelled there.
func (s *selection) partialOf(t types.Type, pkgPath string) (*types.Named, bool) {
	named, ok := s.target(t)
	if !ok {
		return nil, false
	}
	return named, named.Obj().Exported() || named.Obj().Pkg().Path() == pkgPath
}

func (s *selection) node(named *types.Named) *typegraph.Node {
	return s.targets[typegraph.QualifiedTypeFromObject(named.Obj())]
}

// sorted returns the targets ordered by package path and type name.
func (s *selection) sorted() []*typegraph.Node {
	return slices.SortedFunc(maps.Values(s.targets), func(a, b *typegraph.Node) int {
		return cmp.Or(
			cmp.Compare(a.Id.PackagePath, b.Id.PackagePath),
			cmp.Compare(a.Id.TypeName, b.Id.TypeName),
		)
	})
}

// marks are the //kama:partial directives of the source packages and the
// option file entries standing for them.
type marks struct {
	merges    map[kamaopt.Key]kamaopt.Raw
	flags     map[kamaopt.Key]flagOption
	masked    map[typegraph.QualifiedType]kamaopt.Raw
	maskNames map[kamaopt.Key]maskName
	// explicit are the type marks, the first one of each type, in source
	// order and then in option file order.
	explicit []kamaopt.Raw
	// marked and ignored are the first type mark and ignore of each type.
	marked  map[typegraph.QualifiedType]kamaopt.Raw
	ignored map[typegraph.QualifiedType]kamaopt.Raw
	// files are the file names as packages reports them.
	files map[string]bool
	pkgs  map[string]bool
}

func (m *marks) isIgnored(id typegraph.QualifiedType) bool {
	_, ok := m.ignored[id]
	return ok
}

// collectMarks collects the directives of pkgs and the entries of file,
// which may be nil. The entries follow the directives, so a conflict
// between the two names the directive first.
func collectMarks(pkgs []*packages.Package, file *OptionFile) (marks, error) {
	m := marks{
		merges:    map[kamaopt.Key]kamaopt.Raw{},
		flags:     map[kamaopt.Key]flagOption{},
		masked:    map[typegraph.QualifiedType]kamaopt.Raw{},
		maskNames: map[kamaopt.Key]maskName{},
		marked:    map[typegraph.QualifiedType]kamaopt.Raw{},
		ignored:   map[typegraph.QualifiedType]kamaopt.Raw{},
		files:     map[string]bool{},
		pkgs:      map[string]bool{},
	}

	raws, err := kamaopt.Collect(pkgs, toolName)
	if err != nil {
		return marks{}, err
	}
	fileRaws, err := file.raws(pkgs)
	if err != nil {
		return marks{}, err
	}
	raws = append(raws, fileRaws...)

	var errs []error
	for _, r := range raws {
		if sub, ok := strings.CutPrefix(r.Directive, directive+":"); ok {
			if err := m.subDirective(sub, r); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if r.Directive != directive {
			continue
		}

		args := strings.TrimSpace(r.Args)
		pos := r.Origin.Pos
		switch {
		case r.Key.Field != "":
			errs = append(errs, fmt.Errorf(
				"%w: %s: //kama:partial on field %s.%s.%s; mark the type instead",
				ErrDirective, pos, r.Key.PkgPath, r.Key.Type, r.Key.Field,
			))
		case r.Key.Type != "":
			id := typegraph.NewQualifiedType(r.Key.PkgPath, r.Key.Type)
			switch args {
			case "":
				if _, ok := m.marked[id]; !ok {
					m.marked[id] = r
					m.explicit = append(m.explicit, r)
				}
			case "ignore":
				if !m.isIgnored(id) {
					m.ignored[id] = r
				}
			default:
				errs = append(errs, fmt.Errorf(
					"%w: %s: unknown args %q on type %s.%s; want none or ignore",
					ErrDirective, pos, args, r.Key.PkgPath, r.Key.Type,
				))
			}
		default:
			switch args {
			case "file":
				m.files[r.Key.File] = true
			case "package":
				m.pkgs[r.Key.PkgPath] = true
			default:
				errs = append(errs, fmt.Errorf(
					"%w: %s: unknown args %q on a free-floating comment; want file or package",
					ErrDirective, pos, args,
				))
			}
		}
	}
	return m, errors.Join(errs...)
}

// subDirective records //kama:partial:<sub>.
func (m *marks) subDirective(sub string, r kamaopt.Raw) error {
	pos := r.Origin.Pos
	switch sub {
	case "flag":
		return m.flag(r)
	case "fieldmask":
		return m.fieldMask(r)
	case "merge":
	default:
		return fmt.Errorf("%w: %s: unknown directive //kama:%s", ErrDirective, pos, r.Directive)
	}

	if r.Key.Field == "" {
		return fmt.Errorf(
			"%w: %s: //kama:partial:merge applies to struct fields only", ErrDirective, pos,
		)
	}
	field := r.Key.PkgPath + "." + r.Key.Type + "." + r.Key.Field
	strategy := strings.TrimSpace(r.Args)
	switch strategy {
	case string(MergeOverwrite), string(MergeAppend), string(MergeNestedRecursive):
	default:
		return fmt.Errorf(
			"%w: %s: %s: unknown strategy %q on field %s; want overwrite, append or recursive",
			ErrDirective, origin(r), spelling(r), strategy, field,
		)
	}
	if prev, ok := m.merges[r.Key]; ok {
		if prev.Origin.Source == r.Origin.Source {
			return fmt.Errorf(
				"%w: %s: second //kama:partial:merge on field %s; the first is at %s",
				ErrDirective, pos, field, prev.Origin.Pos,
			)
		}
		prevStrategy := strings.TrimSpace(prev.Args)
		if prevStrategy == strategy {
			return nil
		}
		return sourceConflict(
			"the merge strategy of field "+field,
			prev, strconv.Quote(prevStrategy), r, strconv.Quote(strategy),
		)
	}
	m.merges[r.Key] = r
	return nil
}

// flag records //kama:partial:flag.
func (m *marks) flag(r kamaopt.Raw) error {
	pos := r.Origin.Pos
	if r.Key.Field == "" {
		return fmt.Errorf(
			"%w: %s: //kama:partial:flag applies to struct fields only", ErrDirective, pos,
		)
	}
	field := r.Key.PkgPath + "." + r.Key.Type + "." + r.Key.Field
	prev, hasPrev := m.flags[r.Key]
	if hasPrev && prev.raw.Origin.Source == r.Origin.Source {
		return fmt.Errorf(
			"%w: %s: second //kama:partial:flag on field %s; the first is at %s",
			ErrDirective, pos, field, prev.raw.Origin.Pos,
		)
	}
	var (
		o   flagOption
		err error
	)
	if v, ok := r.Value.(FlagOptions); ok {
		o, err = flagFromOptions(v)
	} else {
		o, err = parseFlag(r.Args)
	}
	if err != nil {
		return fmt.Errorf(
			"%w: %s: %s on field %s: %w",
			ErrDirective,
			origin(r),
			spelling(r),
			field,
			err,
		)
	}
	o.raw = r
	if hasPrev {
		if prev.same(o) {
			return nil
		}
		return sourceConflict("the flag of field "+field, prev.raw, prev.String(), r, o.String())
	}
	m.flags[r.Key] = o
	return nil
}

// generatedObjects returns the types, functions and methods declared by
// declarations carrying the marker of a generator registered in reg.
func generatedObjects(pkgs []*packages.Package, reg *genmark.Registry) map[types.Object]bool {
	out := map[types.Object]bool{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Syntax {
			for _, decl := range f.Decls {
				if fd, ok := decl.(*ast.FuncDecl); ok {
					if _, generated := reg.Generated(fd.Doc); generated {
						if obj := pkg.TypesInfo.Defs[fd.Name]; obj != nil {
							out[obj] = true
						}
					}
					continue
				}
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}
				_, declGenerated := reg.Generated(gd.Doc)
				for _, spec := range gd.Specs {
					ts := spec.(*ast.TypeSpec)
					_, generated := reg.Generated(ts.Doc)
					if !declGenerated && !generated {
						continue
					}
					if obj := pkg.TypesInfo.Defs[ts.Name]; obj != nil {
						out[obj] = true
					}
				}
			}
		}
	}
	return out
}

// partialKind reports whether the generator gives n a partial variant:
// a struct, slice or map underlying type.
func partialKind(n *typegraph.Node) bool {
	switch n.Detail.Type.Underlying().(type) {
	case *types.Struct, *types.Slice, *types.Map:
		return true
	}
	return false
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
	for _, n := range r.Nodes {
		if n.Kind&allowed == 0 {
			return false
		}
	}
	return true
}

func selectTargets(pkgs []*packages.Package, opts Options) (*selection, error) {
	generated := generatedObjects(pkgs, opts.Registry)
	g, err := typegraph.New(pkgs, nil, func(_ *ast.TypeSpec, obj types.Object) (bool, error) {
		return !generated[obj], nil
	})
	if err != nil {
		return nil, fmt.Errorf("partial: %w", err)
	}

	m, err := collectMarks(pkgs, opts.OptionFile)
	if err != nil {
		return nil, err
	}

	sel := &selection{
		opts:      opts,
		targets:   map[typegraph.QualifiedType]*typegraph.Node{},
		merges:    m.merges,
		flags:     m.flags,
		flagged:   map[typegraph.QualifiedType]bool{},
		masked:    m.masked,
		maskNames: m.maskNames,
		refs:      map[*types.Var]bool{},
		generated: generated,
	}

	var errs []error
	for _, r := range m.explicit {
		id := typegraph.NewQualifiedType(r.Key.PkgPath, r.Key.Type)
		name := id.PackagePath + "." + id.TypeName
		pos := origin(r)
		if ig, ok := m.ignored[id]; ok {
			if ig.Origin.Source != r.Origin.Source {
				errs = append(errs, sourceConflict("type "+name, r, "marked", ig, "ignored"))
				continue
			}
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s is both marked and ignored", ErrDirective, pos, name,
			))
			continue
		}

		n, ok := g.Get(id)
		if !ok {
			if obj := lookupType(pkgs, id); obj != nil && generated[obj] {
				continue
			}
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s is an alias; mark the type it denotes", ErrUnsupported, pos, name,
			))
			continue
		}
		if !partialKind(n) {
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
		if m.isIgnored(id) || !partialKind(n) {
			continue
		}
		if m.pkgs[id.PackagePath] || m.files[sel.filename(n)] {
			sel.targets[id] = n
		}
	}

	queue := slices.Collect(maps.Values(sel.targets))
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for id, e := range n.Children {
			if sel.targets[id] != nil || m.isIgnored(id) || !partialKind(e.Child) ||
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
	if err := sel.checkNames(generated); err != nil {
		return nil, err
	}
	if err := sel.mapCycles(); err != nil {
		return nil, err
	}
	// Segments flatten embedded partial values, which terminates only once
	// mapCycles has ruled out cycles through embedded fields.
	if err := sel.checkFieldMaskSegments(); err != nil {
		return nil, err
	}
	return sel, nil
}

// checkMerges reports //kama:partial:merge on fields of types without
// partial variants, where it would have no effect.
func (s *selection) checkMerges() error {
	var errs []error
	for _, k := range slices.SortedFunc(maps.Keys(s.merges), compareKey) {
		if s.targets[typegraph.NewQualifiedType(k.PkgPath, k.Type)] != nil {
			continue
		}
		r := s.merges[k]
		errs = append(errs, fmt.Errorf(
			"%w: %s: %s on field %s.%s.%s; %s has no partial variant",
			ErrDirective, origin(r), spelling(r), k.PkgPath, k.Type, k.Field, k.Type,
		))
	}
	return errors.Join(errs...)
}

// checkFlagTargets reports //kama:partial:flag on fields of types without
// partial variants and records the targets with flags in s.flagged.
func (s *selection) checkFlagTargets() error {
	var errs []error
	for _, k := range slices.SortedFunc(maps.Keys(s.flags), compareKey) {
		id := typegraph.NewQualifiedType(k.PkgPath, k.Type)
		if s.targets[id] != nil {
			s.flagged[id] = true
			continue
		}
		r := s.flags[k].raw
		errs = append(errs, fmt.Errorf(
			"%w: %s: %s on field %s.%s.%s; %s has no partial variant",
			ErrDirective, origin(r), spelling(r), k.PkgPath, k.Type, k.Field, k.Type,
		))
	}
	return errors.Join(errs...)
}

func compareKey(a, b kamaopt.Key) int {
	return cmp.Or(
		cmp.Compare(a.PkgPath, b.PkgPath),
		cmp.Compare(a.File, b.File),
		cmp.Compare(a.Type, b.Type),
		cmp.Compare(a.Field, b.Field),
	)
}

func lookupType(pkgs []*packages.Package, id typegraph.QualifiedType) types.Object {
	for _, pkg := range pkgs {
		if pkg.PkgPath == id.PackagePath {
			return pkg.Types.Scope().Lookup(id.TypeName)
		}
	}
	return nil
}

// checkNames reports names the generated declarations would collide with:
// a partial type name, a method of a partial type or the flag set
// interface declared by something other than a generator, and a flag set
// file holding a declaration the generator did not write. A field of a
// partial type named like one of its methods is reported while mapping the
// fields, and a helper named like another declaration once the helpers are
// known.
func (s *selection) checkNames(generated map[types.Object]bool) error {
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
		if !generated[obj] {
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
			if slices.Contains(s.partialMethodNames(n), m.Name()) && !generated[m] {
				conflict(pkg.Fset, m.Pos(),
					"method %s of %s.%s is declared by something other than the generator",
					m.Name(), n.Id.PackagePath, name,
				)
			}
		}
	}

	for _, pkgPath := range slices.Sorted(maps.Keys(targetPkgs)) {
		pkg := targetPkgs[pkgPath]
		if obj := pkg.Types.Scope().Lookup(flagSetName); obj != nil && !generated[obj] {
			conflict(pkg.Fset, obj.Pos(),
				"%s.%s is already declared for the flag set of the generated methods",
				pkgPath, flagSetName,
			)
		}
		for _, f := range pkg.Syntax {
			base := filepath.Base(pkg.Fset.Position(f.FileStart).Filename)
			if base != FlagSetFile {
				continue
			}
			for _, decl := range f.Decls {
				if s.ownDecl(decl) {
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

// ownDecl reports whether decl of FlagSetFile is one the generator writes
// there: imports and marked types.
func (s *selection) ownDecl(decl ast.Decl) bool {
	gd, ok := decl.(*ast.GenDecl)
	if !ok {
		return false
	}
	if gd.Tok == token.IMPORT {
		return true
	}
	_, ok = s.opts.Registry.Generated(gd.Doc)
	return ok && gd.Tok == token.TYPE
}

// mapCycles finds the pointer fields that close a cycle of partial
// variants held by value and records them in s.refs.
//
// Every pointer field whose owner and target lie in one strongly connected
// component of the graph of struct fields holding partial variants by value
// is recorded, so the choice does not depend on the order types are
// visited in. A cycle left without such a field goes through embedded
// pointers, whose partial variants stay embedded by value, and is an error.
func (s *selection) mapCycles() error {
	comp := s.components()
	for _, n := range s.sorted() {
		for _, e := range s.valueChildren(n) {
			if comp[e.child.Id] == comp[n.Id] && e.pointer && !e.field.Embedded() {
				s.refs[e.field] = true
			}
		}
	}

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
			var names []string
			for _, id := range append(path[i:], n.Id) {
				names = append(names, id.TypeName)
			}
			return fmt.Errorf(
				"%w: %s (%s): partial variants would be recursive through embedded fields: %s",
				ErrUnsupported, nodeName(n),
				n.Detail.Pkg.Fset.Position(n.Detail.Ts.Pos()), strings.Join(names, " -> "),
			)
		case done:
			return nil
		}

		state[n.Id] = visiting
		path = append(path, n.Id)
		for _, e := range s.valueChildren(n) {
			if s.refs[e.field] {
				continue
			}
			if err := visit(e.child); err != nil {
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

// components returns the strongly connected components of the targets
// under valueChildren, numbered by Tarjan's algorithm.
func (s *selection) components() map[typegraph.QualifiedType]int {
	var (
		index   = map[typegraph.QualifiedType]int{}
		low     = map[typegraph.QualifiedType]int{}
		onStack = map[typegraph.QualifiedType]bool{}
		stack   []typegraph.QualifiedType
		comp    = map[typegraph.QualifiedType]int{}
		next    int
	)
	var visit func(n *typegraph.Node)
	visit = func(n *typegraph.Node) {
		index[n.Id], low[n.Id] = next, next
		next++
		stack = append(stack, n.Id)
		onStack[n.Id] = true
		for _, e := range s.valueChildren(n) {
			c := e.child.Id
			if _, seen := index[c]; !seen {
				visit(e.child)
				low[n.Id] = min(low[n.Id], low[c])
			} else if onStack[c] {
				low[n.Id] = min(low[n.Id], index[c])
			}
		}
		if low[n.Id] != index[n.Id] {
			return
		}
		for {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[id] = false
			comp[id] = index[n.Id]
			if id == n.Id {
				return
			}
		}
	}
	for _, n := range s.sorted() {
		if _, seen := index[n.Id]; !seen {
			visit(n)
		}
	}
	return comp
}

// valueEdge is a struct field of a target holding the partial variant of
// child.
type valueEdge struct {
	field   *types.Var
	child   *typegraph.Node
	pointer bool
}

// valueChildren returns the fields whose partial values the partial
// variant of n holds without indirection: fields of a target type or a
// pointer to one.
func (s *selection) valueChildren(n *typegraph.Node) []valueEdge {
	st, ok := n.Detail.Type.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	var out []valueEdge
	for v := range st.Fields() {
		elem := deref(v.Type())
		named, ok := s.partialOf(elem, n.Id.PackagePath)
		if !ok {
			continue
		}
		out = append(out, valueEdge{field: v, child: s.node(named), pointer: elem != v.Type()})
	}
	return out
}

// deref strips one pointer from t, through aliases.
func deref(t types.Type) types.Type {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}
