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
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
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

// unmarshal hands the items of the lines of each directive name to v, a
// pointer to a leaf.
func (l Loader[P, T, F]) unmarshal(v any, src Source, names []nameLines) []error {
	u, ok := v.(ExprUnmarshaler)

	var errs []error

	for _, n := range names {
		first := n.lines[0]
		spelled := "//" + l.ToolName + ":" + first.directive

		if !ok {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s on %s; %T takes no directives",
				ErrDirective, first.pos, spelled, src.Target.Describe(), v,
			))

			continue
		}

		args, err := exprArgs(n.lines, spelled)
		if err == nil {
			s := src
			s.Pos = first.pos
			err = u.UnmarshalExpr(s, n.name, args)
		}

		if err != nil {
			errs = append(errs, err)
		}
	}

	return errs
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

// exprArgs parses each of lines, the lines of one directive on one target,
// with expr.Parse and returns their items in source order. The positions
// in the items and in a syntax error count lines from the first line, and
// columns from the start of the args of their line.
func exprArgs(lines []line, spelled string) ([]expr.Arg, error) {
	var out []expr.Arg

	for i, ln := range lines {
		// The newlines put the positions of this line on line i+1 without
		// a walk over the tree. Each line is parsed on its own, so a line
		// need not end with a comma.
		args, err := expr.Parse(strings.Repeat("\n", i) + ln.args)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: %s: %s: %w%s",
				ErrDirective,
				lines[0].pos,
				spelled,
				err,
				oldSpellingHint(ln.args),
			)
		}

		out = append(out, args...)
	}

	return out, nil
}

// oldSpellingHint spells args, a line that does not parse, in the
// expression grammar when args reads as space-separated words and
// name=value pairs. It returns "" otherwise.
func oldSpellingHint(args string) string {
	kvs, err := dircmnt.ParseArgsKeyValue(args)
	if err != nil || len(kvs) == 0 {
		return ""
	}

	items := make([]expr.Arg, 0, len(kvs))

	for _, kv := range kvs {
		switch {
		case !expr.IsName(kv.Key):
			return ""
		case kv.HasValue:
			items = append(items, expr.Arg{Name: kv.Key, Value: hintValue(kv)})
		default:
			items = append(items, expr.Arg{Value: expr.Call{Name: kv.Key}})
		}
	}

	return "; separate items with commas and quote strings, as in " + expr.Print(items)
}

// hintValue spells the value of kv for [oldSpellingHint]. A literal, as
// True, 1 or [1, 2], stays as written. A quoted value and any other bare
// value become a string.
func hintValue(kv dircmnt.KeyValue) expr.Node {
	if kv.Quoted {
		return expr.String{Value: kv.Value}
	}

	// A bare word parses as a call without parentheses, and a=b or 1,2
	// parse as other items, so only a single positional literal is kept.
	args, err := expr.Parse(kv.Value)
	if err != nil || len(args) != 1 || args[0].Name != "" {
		return expr.String{Value: kv.Value}
	}

	switch args[0].Value.(type) {
	case expr.Bool, expr.Null, expr.Number, expr.List, expr.Dict:
		return args[0].Value
	}

	return expr.String{Value: kv.Value}
}

// Word returns the name of a when a is a bare word, as ignore or file: a
// positional name without parentheses.
func Word(a expr.Arg) (string, bool) {
	c, ok := a.Value.(expr.Call)
	if !ok || a.Name != "" || c.Parens || len(c.Args) > 0 {
		return "", false
	}

	return c.Name, true
}

// KeywordString returns the value of a, a keyword item holding a string
// literal, as name="addr". For any other value the error spells a with a
// string, as name=addr spells as name="addr".
func KeywordString(a expr.Arg) (string, error) {
	switch v := a.Value.(type) {
	case expr.String:
		return v.Value, nil
	case expr.Call:
		if !v.Parens && len(v.Args) == 0 {
			return "", stringWanted(a, v.Name)
		}
	case expr.Number:
		return "", stringWanted(a, v.Text)
	}

	return "", fmt.Errorf("%s; want %s=\"<text>\"", a, a.Name)
}

func stringWanted(a expr.Arg, text string) error {
	return fmt.Errorf(
		"%s; want a string, as in %s",
		a, expr.Arg{Name: a.Name, Value: expr.String{Value: text}},
	)
}
