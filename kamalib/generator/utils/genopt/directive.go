package genopt

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/ngicks/option/opt"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// targetLines are the directive lines of one target, grouped by directive
// name in the order the names first appear.
type targetLines struct {
	target Target
	names  []nameLines
}

// nameLines are the lines of one directive on one target, in source order.
type nameLines struct {
	name  string
	lines []line
}

// fromDirectives builds the option tree of the directives of ws.
func (l Loader[P, T, F]) fromDirectives(ws []walked) (Option[P, T, F], error) {
	o := Option[P, T, F]{Packages: map[string]PackageOption[P, T, F]{}}

	var errs []error

	for _, w := range ws {
		po, pkgErrs := l.packageDirectives(w)
		errs = append(errs, pkgErrs...)

		if len(po.Files) > 0 || len(po.Types) > 0 {
			o.Packages[w.pkg.PkgPath] = po
		}
	}

	return o, errors.Join(errs...)
}

func (l Loader[P, T, F]) packageDirectives(w walked) (PackageOption[P, T, F], []error) {
	po := PackageOption[P, T, F]{}

	var errs []error

	typeOption := func(name string, pos token.Position) TypeOption[T, F] {
		to, ok := po.Types[name]
		if !ok {
			file, ts := typeSpecOf(w.pkg, name)
			to.Source = Source{
				Kind: SourceDirective,
				Name: file,
				Pos:  pos,
				Target: Target{
					Kind:    TargetType,
					PkgPath: w.pkg.PkgPath,
					File:    file,
					Type:    name,
					Node:    ts,
				},
			}

			if isStructSpec(w.pkg, ts) {
				to.Fields = opt.Some(map[string]F{})
			}
		}

		if pos.Offset < to.Source.Pos.Offset {
			to.Source.Pos = pos
		}

		return to
	}

	for _, tl := range l.targetLines(w.lines) {
		t := tl.target
		pos := tl.names[0].lines[0].pos
		src := Source{Kind: SourceDirective, Name: t.File, Pos: pos, Target: t}

		switch t.Kind {
		case TargetFreeFloating:
			var p P

			errs = append(errs, l.unmarshal(&p, src, tl.names)...)

			if po.Files == nil {
				po.Files = map[string]P{}
			}

			po.Files[t.File] = p
		case TargetTypeGroup:
			var g T

			errs = append(errs, l.unmarshal(&g, src, tl.names)...)

			for _, spec := range t.Node.(*ast.GenDecl).Specs {
				name := spec.(*ast.TypeSpec).Name.Name
				to := typeOption(name, pos)
				to.Group = g
				setType(&po, name, to)
			}
		case TargetType:
			to := typeOption(t.Type, pos)

			errs = append(errs, l.unmarshal(&to.Type, src, tl.names)...)

			setType(&po, t.Type, to)
		case TargetField:
			to := typeOption(t.Type, pos)

			var f F

			errs = append(errs, l.unmarshal(&f, src, tl.names)...)

			// The walk sees fields of struct type literals only, which
			// isStructSpec reports as struct types.
			if fields, ok := to.Fields.Unpack(); ok {
				fields[t.Field] = f
			}

			setType(&po, t.Type, to)
		}
	}

	return po, errs
}

func setType[P, T, F any](po *PackageOption[P, T, F], name string, to TypeOption[T, F]) {
	if po.Types == nil {
		po.Types = map[string]TypeOption[T, F]{}
	}

	po.Types[name] = to
}

// targetLines groups the lines of the generator by target, skipping the
// targets Filter rejects.
func (l Loader[P, T, F]) targetLines(lines []line) []*targetLines {
	return groupLines(lines, func(ln line) (string, bool) {
		name, ok := l.directiveName(ln.directive)
		return name, ok && (l.Filter == nil || l.Filter(ln.target))
	})
}

// groupLines groups the lines keep accepts by target, in the order the
// targets first appear, and then by the name keep gives them.
func groupLines(lines []line, keep func(line) (string, bool)) []*targetLines {
	var out []*targetLines

	byTarget := map[Target]*targetLines{}

	for _, ln := range lines {
		name, ok := keep(ln)
		if !ok {
			continue
		}

		tl := byTarget[ln.target]
		if tl == nil {
			tl = &targetLines{target: ln.target}
			byTarget[ln.target] = tl
			out = append(out, tl)
		}

		i := slices.IndexFunc(tl.names, func(n nameLines) bool { return n.name == name })
		if i < 0 {
			tl.names = append(tl.names, nameLines{name: name})
			i = len(tl.names) - 1
		}

		tl.names[i].lines = append(tl.names[i].lines, ln)
	}

	return out
}

// directiveName returns the name of directive after the generator, or
// false when directive is not one of the generator.
func (l Loader[P, T, F]) directiveName(directive string) (string, bool) {
	if directive == l.Generator {
		return "", true
	}

	return strings.CutPrefix(directive, l.Generator+":")
}

// unmarshal hands the lines of each directive name to v, a pointer to a
// leaf, as one line.
func (l Loader[P, T, F]) unmarshal(v any, src Source, names []nameLines) []error {
	u, ok := v.(KeyValueUnmarshaler)

	var errs []error

	for _, n := range names {
		first := n.lines[0]
		spelled := "//" + l.ToolName + ":" + first.directive

		if !ok {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s on %s; %T takes no directives",
				ErrDirective, first.pos, spelled, describe(src.Target), v,
			))

			continue
		}

		kv, err := dircmnt.ParseArgsKeyValue(joinArgs(n.lines))
		if err != nil {
			errs = append(errs, fmt.Errorf("%w: %s: %s: %w", ErrDirective, first.pos, spelled, err))
			continue
		}

		s := src
		s.Pos = first.pos

		if err := u.UnmarshalKeyValue(s, n.name, kv); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// describe names a target in error messages.
func describe(t Target) string {
	switch t.Kind {
	case TargetFreeFloating:
		return "a free-floating comment"
	case TargetTypeGroup:
		return "a type group"
	case TargetType:
		return "type " + t.PkgPath + "." + t.Type
	case TargetField:
		return "field " + t.PkgPath + "." + t.Type + "." + t.Field
	case TargetPackage:
		return "package " + t.PkgPath
	}

	return "an unknown target"
}

// typeSpecOf returns the top-level type spec of pkg named name and the file
// declaring it, or nil.
func typeSpecOf(pkg *packages.Package, name string) (file string, ts *ast.TypeSpec) {
	for _, f := range pkg.Syntax {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}

			for _, spec := range gd.Specs {
				if ts := spec.(*ast.TypeSpec); ts.Name.Name == name {
					return pkg.Fset.Position(f.FileStart).Filename, ts
				}
			}
		}
	}

	return "", nil
}

// isStructSpec reports whether the type ts declares is a struct type: by
// its type when pkg holds types, or else by its declaration.
func isStructSpec(pkg *packages.Package, ts *ast.TypeSpec) bool {
	if pkg.Types != nil {
		_, ok := structOf(pkg.Types.Scope().Lookup(ts.Name.Name))
		return ok
	}

	_, ok := unparen(ts.Type).(*ast.StructType)

	return ok
}

// structOf returns the underlying struct type of the type name obj.
func structOf(obj types.Object) (*types.Struct, bool) {
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, false
	}

	st, ok := types.Unalias(tn.Type()).Underlying().(*types.Struct)

	return st, ok
}
