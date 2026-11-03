package partial

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

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
	targets map[typegraph.QualifiedType]*typegraph.Node
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

// sorted returns the targets ordered by package path and type name.
func (s *selection) sorted() []*typegraph.Node {
	return slices.SortedFunc(maps.Values(s.targets), func(a, b *typegraph.Node) int {
		return cmp.Or(
			cmp.Compare(a.Id.PackagePath, b.Id.PackagePath),
			cmp.Compare(a.Id.TypeName, b.Id.TypeName),
		)
	})
}

// marks are the //kama:partial directives of the source packages.
type marks struct {
	// explicit are the type-scoped directives without args, in source
	// order.
	explicit []kamaopt.Raw
	ignored  map[typegraph.QualifiedType]bool
	// files are the file names as packages reports them.
	files map[string]bool
	pkgs  map[string]bool
}

func collectMarks(pkgs []*packages.Package) (marks, error) {
	m := marks{
		ignored: map[typegraph.QualifiedType]bool{},
		files:   map[string]bool{},
		pkgs:    map[string]bool{},
	}

	raws, err := kamaopt.Collect(pkgs, toolName)
	if err != nil {
		return marks{}, err
	}

	var errs []error
	for _, r := range raws {
		// Sub-directives such as //kama:partial:merge are left to the
		// option decoding of the methods.
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
				m.explicit = append(m.explicit, r)
			case "ignore":
				m.ignored[id] = true
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

// generatedTypes returns the type names declared by declarations carrying
// the marker of a registered generator.
func generatedTypes(pkgs []*packages.Package) map[types.Object]bool {
	out := map[types.Object]bool{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Syntax {
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}
				_, declGenerated := genmark.DefaultRegistry.Generated(gd.Doc)
				for _, spec := range gd.Specs {
					ts := spec.(*ast.TypeSpec)
					_, generated := genmark.DefaultRegistry.Generated(ts.Doc)
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

func selectTargets(pkgs []*packages.Package) (*selection, error) {
	generated := generatedTypes(pkgs)
	g, err := typegraph.New(pkgs, nil, func(_ *ast.TypeSpec, obj types.Object) (bool, error) {
		return !generated[obj], nil
	})
	if err != nil {
		return nil, fmt.Errorf("partial: %w", err)
	}

	m, err := collectMarks(pkgs)
	if err != nil {
		return nil, err
	}

	sel := &selection{targets: map[typegraph.QualifiedType]*typegraph.Node{}}

	var errs []error
	for _, r := range m.explicit {
		id := typegraph.NewQualifiedType(r.Key.PkgPath, r.Key.Type)
		name := id.PackagePath + "." + id.TypeName
		pos := r.Origin.Pos
		if m.ignored[id] {
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
		if m.ignored[id] || !partialKind(n) {
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
			if sel.targets[id] != nil || m.ignored[id] || !partialKind(e.Child) ||
				!slices.ContainsFunc(e.Routes, followed) {
				continue
			}
			sel.targets[id] = e.Child
			queue = append(queue, e.Child)
		}
	}

	if err := sel.checkNames(generated); err != nil {
		return nil, err
	}
	if err := sel.checkCycles(); err != nil {
		return nil, err
	}
	return sel, nil
}

func lookupType(pkgs []*packages.Package, id typegraph.QualifiedType) types.Object {
	for _, pkg := range pkgs {
		if pkg.PkgPath == id.PackagePath {
			return pkg.Types.Scope().Lookup(id.TypeName)
		}
	}
	return nil
}

// checkNames reports a partial name declared by something other than a
// generator, which the generated declaration would collide with.
func (s *selection) checkNames(generated map[types.Object]bool) error {
	var errs []error
	for _, n := range s.sorted() {
		name := partialName(n.Id.TypeName)
		obj := n.Detail.Pkg.Types.Scope().Lookup(name)
		if obj == nil || generated[obj] {
			continue
		}
		errs = append(errs, fmt.Errorf(
			"%w: %s: %s.%s is already declared for the partial variant of %s",
			ErrConflict,
			n.Detail.Pkg.Fset.Position(obj.Pos()),
			n.Id.PackagePath,
			name,
			n.Id.TypeName,
		))
	}
	return errors.Join(errs...)
}

// checkCycles reports targets that would hold their own partial variant by
// value. und.Und holds its value directly, so a pointer cycle in the source,
// such as a linked list node, becomes an invalid recursive type.
func (s *selection) checkCycles() error {
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
				"%w: %s (%s): partial variants would be recursive: %s",
				ErrUnsupported, nodeName(n),
				n.Detail.Pkg.Fset.Position(n.Detail.Ts.Pos()), strings.Join(names, " -> "),
			)
		case done:
			return nil
		}

		state[n.Id] = visiting
		path = append(path, n.Id)
		for _, child := range s.valueChildren(n) {
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

// valueChildren returns the targets whose partial variants the partial
// variant of n holds by value: fields of a target type or a pointer to
// one.
func (s *selection) valueChildren(n *typegraph.Node) []*typegraph.Node {
	st, ok := n.Detail.Type.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	var out []*typegraph.Node
	for v := range st.Fields() {
		named, ok := s.target(deref(v.Type()))
		if !ok {
			continue
		}
		out = append(out, s.targets[typegraph.QualifiedTypeFromObject(named.Obj())])
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
