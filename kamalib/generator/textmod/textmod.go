package textmod

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"iter"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// GeneratorId identifies the generator.
func GeneratorId() base.Id { return base.Id{Namespace: "kama", Name: "textmod"} }

func init() { base.MustRegister(GeneratorId()) }

// Generator edits the tagged values of the packages of an Input.
func Generator() base.Generator[Input] {
	return base.Generator[Input]{Id: GeneratorId(), Generate: Generate}
}

var (
	// ErrDirective is wrapped by the errors about a directive that does not
	// parse or that stands where no value can be edited.
	ErrDirective = errors.New("textmod: invalid directive")
	// ErrTarget is wrapped by the errors about a tagged spec whose value
	// cannot be edited.
	ErrTarget = errors.New("textmod: invalid target")
	// ErrMod is wrapped by the errors about a template that does not parse
	// or run, or whose result does not parse back.
	ErrMod = errors.New("textmod: invalid mod")
	// ErrDuplicateId is wrapped by the warning about an id tagged on several
	// specs.
	ErrDuplicateId = errors.New("textmod: duplicate id")
	// ErrUnusedMod is wrapped by the warning about a mod whose id no spec
	// carries.
	ErrUnusedMod = errors.New("textmod: unused mod")
	// ErrUnknownKey is wrapped by the warning about a keyword of a directive
	// that names nothing.
	ErrUnknownKey = errors.New("textmod: unknown key")
)

// Input is the input of a run of Generator.
type Input struct {
	// Pkgs are the packages whose files the run reads, as
	// base.SourcePackages picks them.
	Pkgs []*packages.Package
	// Root is the absolute directory of the root of the output. A file
	// outside it is not read.
	Root string
	// Mods maps an id to the source of the text/template that computes its
	// new value.
	Mods map[string]string
	// Vars are the caller values a template reads as .Vars.
	Vars map[string]any
}

// Data is the data a template runs on.
type Data struct {
	// Id is the id of the directive.
	Id string
	// Name is the name the spec declares.
	Name string
	// Package is the import path of the package of the spec, and
	// PackageName its name.
	Package     string
	PackageName string
	// File is the path of the file of the spec, slash-separated and
	// relative to the root of the output.
	File string
	// Kind is the kind of the value: int, float, string or bool.
	Kind Kind
	// Format is the format of a string: ver, date-time or empty.
	Format string
	// Literal is the value as the source spells it, with its sign.
	Literal string
	// Value is the current value: an int64, or a *big.Int when it does not
	// fit, for int; a float64 for float; a string for string, an
	// exver.Version for the format ver and a time.Time for date-time; a
	// bool for bool.
	Value any
	// Vars are the caller values of the run.
	Vars map[string]any
}

// Kind is the kind of an edited value.
type Kind string

const (
	KindInt    Kind = "int"
	KindFloat  Kind = "float"
	KindString Kind = "string"
	KindBool   Kind = "bool"
)

// Generate edits the value of each spec tagged //kama:textmod <id> whose id
// in.Mods holds, and yields each changed file as a base.Whole.
// The package doc tells the rules.
func Generate(_ context.Context, env base.Env, in Input) iter.Seq2[base.Output, error] {
	return func(yield func(base.Output, error) bool) {
		funcs, err := Funcs(Run{Packages: in.Pkgs, Vars: in.Vars})
		if err != nil {
			yield(nil, err)
			return
		}

		r := run{
			env:   env,
			in:    in,
			mods:  map[string]*template.Template{},
			found: map[string][]string{},
		}

		for _, id := range slices.Sorted(maps.Keys(in.Mods)) {
			tmpl, err := ParseMod(id, in.Mods[id], funcs)
			if err != nil {
				if !yield(nil, err) {
					return
				}

				continue
			}

			r.mods[id] = tmpl
		}

		for pkg, f := range sourceFiles(in) {
			for out, err := range r.file(pkg, f) {
				if !yield(out, err) {
					return
				}
			}
		}

		for _, id := range slices.Sorted(maps.Keys(r.found)) {
			if pos := r.found[id]; len(pos) > 1 {
				env.Reporter.Warn(fmt.Errorf(
					"%w: %q is tagged at %s; %s takes the mod",
					ErrDuplicateId, id, strings.Join(pos, ", "), pos[0],
				))
			}
		}

		for _, id := range slices.Sorted(maps.Keys(in.Mods)) {
			if _, ok := r.found[id]; !ok {
				env.Reporter.Warn(fmt.Errorf("%w: no spec is tagged with %q", ErrUnusedMod, id))
			}
		}
	}
}

// ParseMod parses src, the template of id, with funcs. One trailing
// newline of src is dropped: a YAML block scalar ends with one, and the
// value would grow by a newline each run.
func ParseMod(id, src string, funcs template.FuncMap) (*template.Template, error) {
	tmpl, err := template.New(id).
		Option("missingkey=error").
		Funcs(funcs).
		Parse(strings.TrimSuffix(src, "\n"))
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrMod, id, err)
	}

	return tmpl, nil
}

// run is one run of Generate.
type run struct {
	env  base.Env
	in   Input
	mods map[string]*template.Template
	// found holds the positions of the specs tagged with each id, in the
	// order of the run. The first one takes the mod.
	found map[string][]string
}

// sourceFiles yields the syntax trees of the packages of in under its root,
// ordered by package path, then by file name.
func sourceFiles(in Input) iter.Seq2[*packages.Package, *ast.File] {
	pkgs := slices.SortedFunc(slices.Values(in.Pkgs), func(a, b *packages.Package) int {
		return cmp.Compare(a.PkgPath, b.PkgPath)
	})

	return func(yield func(*packages.Package, *ast.File) bool) {
		for _, pkg := range pkgs {
			files := slices.SortedFunc(slices.Values(pkg.Syntax), func(a, b *ast.File) int {
				return cmp.Compare(fileName(pkg, a), fileName(pkg, b))
			})

			for _, f := range files {
				if _, ok := relPath(in.Root, fileName(pkg, f)); ok && !yield(pkg, f) {
					return
				}
			}
		}
	}
}

func fileName(pkg *packages.Package, f *ast.File) string {
	return pkg.Fset.Position(f.FileStart).Filename
}

// relPath returns abs as a path of the output under root.
func relPath(root, abs string) (string, bool) {
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}

	return filepath.ToSlash(rel), true
}

// edit replaces src[start:end] with text.
type edit struct {
	start, end int
	text       string
}

// file edits the tagged specs of loaded, a file of pkg, in its content in
// the output, and yields the file when it changed.
func (r run) file(pkg *packages.Package, loaded *ast.File) iter.Seq2[base.Output, error] {
	return func(yield func(base.Output, error) bool) {
		rel, _ := relPath(r.in.Root, fileName(pkg, loaded))

		src, err := fs.ReadFile(r.env.Out, rel)
		if err != nil {
			yield(nil, fmt.Errorf("textmod: read %q: %w", rel, err))
			return
		}

		// The content may differ from what the packages were loaded from, so
		// positions come from this parse.
		fset := token.NewFileSet()

		f, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			yield(nil, fmt.Errorf("textmod: parse %q: %w", rel, err))
			return
		}

		target := fileTarget{pkg: pkg, loaded: loaded, rel: rel, fset: fset, file: f}
		tagged := map[*ast.ValueSpec]string{}

		var edits []edit

		i := 0

		for t := range tags(f) {
			ed, ok, err := r.tag(target, i, t, tagged)

			i++

			if err != nil {
				if !yield(nil, err) {
					return
				}

				continue
			}

			if ok {
				edits = append(edits, ed)
			}
		}

		slices.SortFunc(edits, func(a, b edit) int { return cmp.Compare(a.start, b.start) })

		out := slices.Clone(src)

		for _, ed := range slices.Backward(edits) {
			out = slices.Concat(out[:ed.start], []byte(ed.text), out[ed.end:])
		}

		if !bytes.Equal(out, src) {
			yield(base.Whole{Path: rel, Content: out}, nil)
		}
	}
}

// fileTarget is the file a run edits: its parse from the output and its
// syntax tree as pkg loaded it, which holds the types.
type fileTarget struct {
	pkg    *packages.Package
	loaded *ast.File
	rel    string
	fset   *token.FileSet
	file   *ast.File
}

func (ft fileTarget) position(p token.Pos) string {
	pos := ft.fset.Position(p)
	return fmt.Sprintf("%s:%d:%d", ft.rel, pos.Line, pos.Column)
}

// tag checks t, the i-th tag of the file, and returns the edit of its
// value when its id has a mod and is found the first time. tagged holds
// the specs of the file tagged before, with their positions.
func (r run) tag(
	ft fileTarget,
	i int,
	t tag,
	tagged map[*ast.ValueSpec]string,
) (edit, bool, error) {
	pos := ft.position(t.comment.Pos())

	d, err := parseDirective(t.args)
	if err != nil {
		return edit{}, false, fmt.Errorf("%w: %s: %w", ErrDirective, pos, err)
	}

	for _, k := range d.unknown {
		r.env.Reporter.Warn(fmt.Errorf("%w: %s: %s", ErrUnknownKey, pos, k))
	}

	switch {
	case t.spec == nil:
		return edit{}, false, fmt.Errorf("%w: %s: %s", ErrDirective, pos, t.misplaced)
	case generatedFile(ft.file):
		return edit{}, false, fmt.Errorf(
			"%w: %s: the file is the output of a generator", ErrDirective, pos,
		)
	}

	if prev, ok := tagged[t.spec]; ok {
		return edit{}, false, fmt.Errorf(
			"%w: %s: the spec is tagged at %s too",
			ErrDirective,
			pos,
			prev,
		)
	}

	tagged[t.spec] = pos

	lit, err := literalOf(t.spec)
	if err != nil {
		return edit{}, false, fmt.Errorf(
			"%w: %s: %s: %w",
			ErrTarget,
			pos,
			t.spec.Names[0].Name,
			err,
		)
	}

	typ := loadedType(ft.pkg, ft.loaded, i, t.spec)
	kind := kindOf(lit, typ)

	if d.format != "" && kind != KindString {
		return edit{}, false, fmt.Errorf(
			"%w: %s: format %q on a value of kind %s; a format applies to a string",
			ErrTarget, pos, d.format, kind,
		)
	}

	r.found[d.id] = append(r.found[d.id], pos)

	tmpl, ok := r.mods[d.id]
	if !ok || len(r.found[d.id]) > 1 {
		return edit{}, false, nil
	}

	text, err := modify(tmpl, lit, kind, typ, d.format, Data{
		Id:          d.id,
		Name:        t.spec.Names[0].Name,
		Package:     ft.pkg.PkgPath,
		PackageName: ft.pkg.Name,
		File:        ft.rel,
		Kind:        kind,
		Format:      d.format,
		Literal:     lit.text,
		Vars:        r.in.Vars,
	})
	if err != nil {
		return edit{}, false, fmt.Errorf("%w: %q at %s: %w", ErrMod, d.id, pos, err)
	}

	return edit{
		start: ft.fset.Position(lit.expr.Pos()).Offset,
		end:   ft.fset.Position(lit.expr.End()).Offset,
		text:  text,
	}, true, nil
}

// generatedFile reports whether f is the output of a generator: it carries
// the header of generated code or a generated mark of kama.
func generatedFile(f *ast.File) bool {
	if ast.IsGenerated(f) {
		return true
	}

	return opiter.SlicesValues(f.Comments).
		FlatMap(func(cg *ast.CommentGroup) iter.Seq[dircmnt.Comment] {
			return slices.Values(dircmnt.Ast(cg))
		}).
		Any(func(c dircmnt.Comment) bool {
			return c.ToolName == base.ToolName && c.Directive == "generated"
		})
}

// loadedType returns the type of the spec of the i-th tag of loaded, the
// file as pkg loaded it, when that spec declares the name spec declares.
// The content of the output may differ from what pkg loaded, so the two
// are matched by the order of their tags. Without the types, a predeclared
// type spelled on spec is used.
func loadedType(pkg *packages.Package, loaded *ast.File, i int, spec *ast.ValueSpec) types.Type {
	name := spec.Names[0].Name

	n := 0

	for t := range tags(loaded) {
		if n != i {
			n++
			continue
		}

		if t.spec == nil || len(t.spec.Names) != 1 || t.spec.Names[0].Name != name ||
			pkg.TypesInfo == nil {
			break
		}

		if obj := pkg.TypesInfo.Defs[t.spec.Names[0]]; obj != nil {
			return obj.Type()
		}

		break
	}

	if id, ok := spec.Type.(*ast.Ident); ok {
		if tn, ok := types.Universe.Lookup(id.Name).(*types.TypeName); ok {
			return tn.Type()
		}
	}

	return nil
}
