// Package contextkey generates context keys and typed accessor functions.
//
// Each Entry yields one file holding a key variable and the functions
// WithName, ValueName, ValueNameFallback, ValueNameFallbackFn and
// ValueNameDefault for a value of the entry's type. Every generated
// declaration carries the genmark marker naming GeneratorId in its doc
// comment.
//
// A file is named by inserting .kama_contextkey into Entry.FileName before
// its build constraint suffixes, as in sig.kama_contextkey_linux.go for
// sig_linux. It starts with the header of package base: the
// generated-code line, the params template and key_type_func, and the time
// of the run.
//
// A key of the template file that names no field of Template or Entry is
// no error. Generate warns, wrapping ErrUnknownKey, about each of them with
// the path of the file and the key path, as in entries.0.default.
//
// # Proc
//
// The proc, the workflow step kind contextkey.generate, takes the packages
// a packages.load step loaded as its in and generates from the template
// files of each package into the directory of the package, in the package
// of the package. A package names its template files with the directive
// //kama:option-file contextkey=<path> in a free-floating comment, the path
// being relative to the package directory. A package naming none has the
// template file kama_contextkey.toml, .yaml, .yml or .json in its
// directory, the first one present in that order, or none. Several
// directives, or several paths in one, give a package several template
// files. Several packages may name one file. A package without a template
// file generates nothing.
//
// The proc reads the name, the directory and the syntax trees of a
// package, not its types. A spec loads the packages for it with the
// packages.load param allow_type_errors true, so that Go files referring
// to the files the proc has not generated yet, or a generated file that no
// longer compiles, do not stop the run:
//
//	version: 1
//	steps:
//	  root:
//	    proc: overlayfs.open
//	  load:
//	    proc: packages.load
//	    in: root
//	    param:
//	      allow_type_errors: true
//	  generate:
//	    proc: contextkey.generate
//	    in: load
//	    out: root
//	  final:
//	    proc: overlayfs.copy_down
//	    in: root
//	    after: generate
//
// The final copy_down still checks that the packages compile with the
// generated files.
//
// The param of the proc has one key, key_type_func, which is the
// key_type_func of a template file leaving it out.
//
// The proc keeps a file that would change in the generated: line only, and
// warns, wrapping base.ErrStale, about each Go file of the directory of a
// package it loaded that carries the marker of the generator and that the
// run did not produce. Every generated file of a directory must therefore
// come from one step.
//
// Lifted from github.com/ngicks/go-common/contextkey/generate.
package contextkey

import (
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"iter"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
)

// GeneratorId identifies the generator.
func GeneratorId() base.Id { return base.Id{Namespace: "kama", Name: "contextkey"} }

func init() { base.MustRegister(GeneratorId()) }

// Generator generates the files of the entries of an Input.
func Generator() base.Generator[Input] {
	return base.Generator[Input]{Id: GeneratorId(), Generate: Generate}
}

var (
	ErrInvalidEntry = errors.New("contextkey: invalid entry")
	// ErrUnknownKey is wrapped by the warnings of Generate about the keys of
	// a template file that name no field of Template or Entry.
	ErrUnknownKey = errors.New("contextkey: unknown key")
	ErrNoEntries  = errors.New("contextkey: no entries")
)

// naming names the files the generator writes.
var naming = GeneratorId().Naming()

// FileName returns the name of the file generated for an Entry whose
// FileName is name.
func FileName(name string) string { return naming.SourceFile(name + ".go") }

// Input is the input of a run of Generator.
type Input struct {
	// Path is the path of the template file, which messages and the header
	// of the generated files name.
	Path     string
	Template Template
	// Dir is the directory the files land in, slash-separated and relative
	// to the root of the output.
	Dir     string
	Options Options
}

// Template is the decoded template file. The json tags of Template and
// Entry give its keys. The keys are lowercase snake case.
type Template struct {
	// KeyTypeFunc overrides Options.KeyTypeFunc for the entries of the
	// template unless it is empty.
	KeyTypeFunc string  `json:"key_type_func"`
	Entries     []Entry `json:"entries"`
	// Unknown captures the keys of the file that name no field.
	Unknown map[string]any `json:",embed"`
}

// UnknownKeys returns the key paths of the unknown keys of t: those of t
// first, then those of each entry in order.
func (t Template) UnknownKeys() [][]string {
	keys := unknownkey.Paths(t.Unknown)
	for i, e := range t.Entries {
		keys = append(keys, unknownkey.Paths(e.Unknown, "entries", strconv.Itoa(i))...)
	}

	return keys
}

// Entry describes one context key.
type Entry struct {
	// FileName names the generated file, as FileName tells.
	FileName string `json:"file_name"`
	// Name is used in the key variable and function names.
	Name string `json:"name"`
	// Type is the type of the value associated with the key.
	Type string `json:"type"`
	// DefaultExpr is evaluated by ValueNameDefault when no value is
	// associated.
	DefaultExpr string `json:"default_expr"`
	// Imports are package paths the generated file needs besides "context".
	Imports []string `json:"imports"`
	// Unknown captures the keys of the entry that name no field. Generate
	// ignores it.
	Unknown map[string]any `json:",embed"`
}

func (e Entry) validate() error {
	if e.FileName == "" || strings.ContainsAny(e.FileName, `/\`) {
		return fmt.Errorf("%w: file name %q", ErrInvalidEntry, e.FileName)
	}

	if !token.IsIdentifier(e.Name) {
		return fmt.Errorf("%w: name %q", ErrInvalidEntry, e.Name)
	}

	if _, err := parser.ParseExpr(e.Type); err != nil {
		return fmt.Errorf("%w: type %q: %w", ErrInvalidEntry, e.Type, err)
	}

	if _, err := parser.ParseExpr(e.DefaultExpr); err != nil {
		return fmt.Errorf("%w: default expr %q: %w", ErrInvalidEntry, e.DefaultExpr, err)
	}

	return nil
}

// Options configures Generate.
type Options struct {
	// Package is the package clause of generated files.
	Package string
	// KeyTypeFunc names a function in Package that takes a string and
	// returns a key. It must return a distinct key on every call, since
	// entries of the same Type pass it the same string. Template.KeyTypeFunc
	// overrides it.
	KeyTypeFunc string
	// ExternalTest tells that Package is an external test package, which
	// shares its directory with the package it tests. Its generated files
	// are test files, as in a.kama_contextkey_test.go.
	ExternalTest bool
}

// file returns name, the name of a generated file without a directory, as
// the package of o names it: an external test package takes test files.
func (o Options) file(name string) string {
	if !o.ExternalTest {
		return name
	}

	return strings.TrimSuffix(name, ".go") + "_test.go"
}

func (o Options) validate() error {
	if !token.IsIdentifier(o.Package) {
		return fmt.Errorf("contextkey: package %q is not an identifier", o.Package)
	}

	if !token.IsIdentifier(o.KeyTypeFunc) {
		return fmt.Errorf("contextkey: key type func %q is not an identifier", o.KeyTypeFunc)
	}

	return nil
}

// Generate yields one file per entry of in, in the order of the entries,
// and warns about the unknown keys of its template. Invalid options, an
// invalid entry and a file name two entries share are errors naming the
// template file.
func Generate(_ context.Context, env base.Env, in Input) iter.Seq2[base.Output, error] {
	return func(yield func(base.Output, error) bool) {
		for _, k := range in.Template.UnknownKeys() {
			env.Reporter.Warn(
				fmt.Errorf("%w: %s: %s", ErrUnknownKey, in.Path, strings.Join(k, ".")),
			)
		}

		if in.Template.KeyTypeFunc != "" {
			in.Options.KeyTypeFunc = in.Template.KeyTypeFunc
		}

		if err := in.Options.validate(); err != nil {
			yield(nil, fmt.Errorf("%s: %w", in.Path, err))
			return
		}

		if len(in.Template.Entries) == 0 {
			yield(nil, fmt.Errorf("%w: %s", ErrNoEntries, in.Path))
			return
		}

		seen := map[string]bool{}

		for _, e := range in.Template.Entries {
			out, err := entryFile(in, e, seen[e.FileName])
			if err != nil {
				err = fmt.Errorf("%s: %w", in.Path, err)
			}

			seen[e.FileName] = true

			if !yield(out, err) {
				return
			}
		}
	}
}

// entryFile returns the file of e. dup tells that an earlier entry has its
// file name.
func entryFile(in Input, e Entry, dup bool) (base.Output, error) {
	if err := e.validate(); err != nil {
		return nil, err
	}

	if dup {
		return nil, fmt.Errorf("%w: duplicate file name %q", ErrInvalidEntry, e.FileName)
	}

	imports, err := entryImports(in.Options.Package, e)
	if err != nil {
		return nil, fmt.Errorf("contextkey: %q: %w", e.FileName, err)
	}

	c := genhelper.NewBaseContext(genhelper.NewScope(), &genhelper.Package{Imports: imports})

	parts, err := opiter.TryCollect(
		opiter.WrapSeq(slices.Values(entrySteps(in.Options, e))).
			Disassemble(func(step genStep) (base.Part, error) { return renderDecl(c, step) }),
	)
	if err != nil {
		return nil, fmt.Errorf("contextkey: render %q: %w", e.FileName, err)
	}

	return base.Composed{
		Path: path.Join(in.Dir, in.Options.file(FileName(e.FileName))),
		Params: map[string]any{
			"template":      in.Path,
			"key_type_func": in.Options.KeyTypeFunc,
		},
		Package: in.Options.Package,
		Imports: imports,
		Parts:   parts,
	}, nil
}

// renderDecl renders the declaration of step. A type or a default
// expression of an entry can still break the declaration it goes into,
// which is reported here rather than when the file is staged.
func renderDecl(c genhelper.BaseContext, step genStep) (base.Part, error) {
	text, err := lang.Render(c, step)
	if err != nil {
		return nil, err
	}

	_, err = parser.ParseFile(
		token.NewFileSet(), "", "package p\n\n"+text, parser.SkipObjectResolution,
	)
	if err != nil {
		return nil, err
	}

	return base.Decl(text), nil
}

// entryImports returns the imports of the file of e: context and the
// imports of e. The packages are not loaded, so an import keeps the name
// its path suggests and the file names no import, leaving the package
// name to the compiler, as the type and the default expression of e
// spell it.
func entryImports(pkgName string, e Entry) (*importdecl.Map, error) {
	parser := importdecl.NewParser(types.NewPackage(pkgName, pkgName))

	for _, p := range slices.Concat([]string{"context"}, e.Imports) {
		imp := importdecl.Import{Package: importdecl.Package{Path: p}}
		imp.Package.Name = imp.Ident()

		if err := parser.Require(imp); err != nil {
			return nil, err
		}
	}

	return parser.ParseAst(nil)
}

type genStep = genhelper.Step[genhelper.BaseContext]

// lang is the vocabulary of steps of the generated declarations.
var lang genhelper.Lang[genhelper.BaseContext]

// entrySteps returns the declarations of e, one step each. The bodies
// spell their locals by fixed names: the only other identifiers in scope
// are the package-level ones of the entry, which differ from them.
func entrySteps(opts Options, e Entry) []genStep {
	key := "Key" + e.Name
	value := "Value" + e.Name

	doc := func(text string) genhelper.Gen[genhelper.BaseContext] {
		return lang.Comment(lang.Expr("%s", text))
	}
	ifValue := lang.If(lang.Expr("v, ok := %s(ctx); ok", value), lang.Stmt("return v"), nil)

	return []genStep{
		lang.Steps(
			lang.Line(doc(fmt.Sprintf("%s is the context key for a %s value.", key, e.Type))),
			lang.Stmt("var %s = %s(%s)", key, opts.KeyTypeFunc, strconv.Quote(e.Type)),
		),
		lang.DocFunc(
			doc(fmt.Sprintf(
				"With%s returns a copy of ctx in which %s is associated with v.", e.Name, key,
			)),
			lang.Expr("func With%s(ctx context.Context, v %s) context.Context", e.Name, e.Type),
			lang.Stmt("return context.WithValue(ctx, %s, v)", key),
		),
		lang.DocFunc(
			doc(fmt.Sprintf(
				"%s returns the %s associated with %s in ctx.\n"+
					"ok is false if no value is associated or the value is not a %s.",
				value, e.Type, key, e.Type,
			)),
			lang.Expr("func %s(ctx context.Context) (v %s, ok bool)", value, e.Type),
			lang.Stmt("v, ok = ctx.Value(%s).(%s)", key, e.Type),
			lang.Stmt("return v, ok"),
		),
		lang.DocFunc(
			doc(fmt.Sprintf(
				"%sFallback returns the %s associated with %s in ctx,\n"+
					"or fallback if %s reports none.",
				value, e.Type, key, value,
			)),
			lang.Expr(
				"func %sFallback(ctx context.Context, fallback %s) %s", value, e.Type, e.Type,
			),
			ifValue,
			lang.Stmt("return fallback"),
		),
		lang.DocFunc(
			doc(fmt.Sprintf(
				"%sFallbackFn returns the %s associated with %s in ctx,\n"+
					"or the result of fallbackFn if %s reports none.",
				value, e.Type, key, value,
			)),
			lang.Expr(
				"func %sFallbackFn(ctx context.Context, fallbackFn func() %s) %s",
				value, e.Type, e.Type,
			),
			ifValue,
			lang.Stmt("return fallbackFn()"),
		),
		lang.DocFunc(
			doc(fmt.Sprintf(
				"%sDefault returns the %s associated with %s in ctx,\n"+
					"or %s if %s reports none.",
				value, e.Type, key, e.DefaultExpr, value,
			)),
			lang.Expr("func %sDefault(ctx context.Context) %s", value, e.Type),
			lang.Stmt(
				"return %sFallbackFn(ctx, func() %s { return %s })", value, e.Type, e.DefaultExpr,
			),
		),
	}
}
