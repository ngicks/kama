package genext

import (
	"encoding/json/jsontext"
	"fmt"
	"go/build"
	"go/types"
	"iter"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"
	"unicode/utf8"
)

// Output is the file the code of a type support lands in, as a generator
// sees it while writing: Import spells a package by path and imports it
// into the file once the text renders, and Declare declares a
// package-level helper in the shared helper file of the package. The
// emitter of the validator is an Output.
type Output interface {
	Importer
	// Declare declares the package-level declaration named name. decl
	// returns the step rendering exactly one function, variable or constant
	// declaration of name, importing packages through imp. The first decl
	// given for a name wins.
	Declare(name string, decl func(imp Importer) Step)
}

// Importer spells packages by path in the file generated code lands in.
type Importer interface {
	// Import returns the text of the identifier the file refers to the
	// package at path by. The file imports the package once the text
	// renders.
	Import(path string) Gen
}

// Binder is a TypeSupport whose code needs the Output it lands in, as a
// support from a text extension file imports packages and declares
// helpers. Bind returns the support writing to out and declaring its
// helpers under names starting with helperPrefix.
type Binder interface {
	TypeSupport
	Bind(out Output, helperPrefix string) TypeSupport
}

// Bound returns r with every Binder of it bound to out and helperPrefix. A
// generator asks the supports through it while writing a file, so that
// their code imports into that file. helperPrefix keeps the helpers of one
// generator apart from those of another in the same package, as
// kamaValidateExt does for the validator.
func Bound(r Supports, out Output, helperPrefix string) Supports {
	return bound{r: r, out: out, prefix: helperPrefix}
}

type bound struct {
	r      Supports
	out    Output
	prefix string
}

func (b bound) TypeSupports() iter.Seq[TypeSupport] {
	return func(yield func(TypeSupport) bool) {
		if b.r == nil {
			return
		}

		for s := range b.r.TypeSupports() {
			if x, ok := s.(Binder); ok {
				s = x.Bind(b.out, b.prefix)
			}

			if !yield(s) {
				return
			}
		}
	}
}

// TextScope is the description of one type in a text extension file, as a
// section builder sees it. It answers from the file at each call.
type TextScope struct {
	file   string
	doc    *TextFile
	key    string
	out    Output
	prefix string
}

// File returns the name of the file.
func (s TextScope) File() string { return s.file }

// Key returns the key of the type in the file.
func (s TextScope) Key() string { return s.key }

// Bind returns s writing its code to out and declaring its helpers under
// names starting with helperPrefix.
func (s TextScope) Bind(out Output, helperPrefix string) TextScope {
	s.out, s.prefix = out, helperPrefix
	return s
}

// Section returns the section of the generator name in the description of
// the type, as the file holds it.
func (s TextScope) Section(name string) jsontext.Value {
	return s.doc.Types[s.key].Generator[name]
}

// Claims reports whether t is the type s describes, or an instance of it.
func (s TextScope) Claims(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}

	pkgPath, name, _ := splitTypeKey(s.key)
	obj := named.Obj()

	return obj.Pkg() != nil && obj.Pkg().Path() == pkgPath && obj.Name() == name
}

// ValueType returns the type spec names for the claimed type t: the type
// argument of the type parameter spec names, as "T", or, for ".Name", the
// type of the field Name of t or of the first result of its method Name.
func (s TextScope) ValueType(t types.Type, spec string) (types.Type, error) {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return nil, fmt.Errorf("%s is no named type", t)
	}

	return resolveSpec(named, spec)
}

// Gen returns the code of the template text at the key path of the type
// in the file, executed with the data .V, the code of v, and the members
// of extra. path is below the entry of the type, as holder.value. The
// template executes while the code renders, so that std, pkg and helper
// import and declare into the Output in use.
func (s TextScope) Gen(path []string, text string, v Gen, extra map[string]string) Gen {
	return func(c Context) (string, error) {
		data := map[string]string{}
		maps.Copy(data, extra)

		if v != nil {
			vs, err := v(c)
			if err != nil {
				return "", err
			}

			data["V"] = vs
		}

		return s.execute(c, s.path(path...), text, data, s.out, true)
	}
}

func (s TextScope) path(rest ...string) []string {
	return append([]string{"types", s.key}, rest...)
}

// Check reports the error of the template text at path, below the entry
// of the type, that does not parse.
func (s TextScope) Check(path []string, text string) error {
	if _, err := parseText(text); err != nil {
		return textError(s.file, s.path(path...), "%v", err)
	}

	return nil
}

// Error returns an error wrapping ErrTextFile at path, below the entry of
// the type.
func (s TextScope) Error(path []string, format string, args ...any) error {
	return textError(s.file, s.path(path...), format, args...)
}

func (s TextScope) execute(
	c Context,
	path []string,
	text string,
	data any,
	imp Importer,
	helpers bool,
) (string, error) {
	fail := func(format string, args ...any) (string, error) {
		return "", textError(s.file, path, format, args...)
	}

	importOf := func(p string) (string, error) {
		if imp != nil {
			return imp.Import(p)(c)
		}

		// Unbound, the code reaches only the packages the generated package
		// depends on already, since only those the file knows by path.
		pkg := c.Package()

		var (
			ident string
			ok    bool
		)

		if pkg != nil && pkg.Imports != nil {
			ident, ok = pkg.Imports.Ident(p)
		}

		if !ok {
			return "", fmt.Errorf(
				"package %s is unknown to the file; the generator binds no output",
				p,
			)
		}

		return ident, nil
	}

	var sb strings.Builder

	t, err := template.New(path[len(path)-1]).
		Option("missingkey=error").
		Funcs(template.FuncMap{
			"std": func(p string) (string, error) {
				if !isStd(p) {
					return "", fmt.Errorf("std %q: no standard library package", p)
				}

				return importOf(p)
			},
			"pkg": func(name string) (string, error) {
				p, ok := s.doc.Imports[name]
				if !ok {
					return "", fmt.Errorf("pkg %q: imports declares no such name", name)
				}

				return importOf(p)
			},
			"helper": func(name string) (string, error) {
				if !helpers {
					return "", fmt.Errorf("helper %q: a helper calls no helper", name)
				}

				return s.declare(name)
			},
		}).
		Parse(text)
	if err != nil {
		return fail("%v", err)
	}

	if err := t.Execute(&sb, data); err != nil {
		return fail("%v", err)
	}

	return sb.String(), nil
}

// declare declares the helper name of the file into the Output and returns
// the name of its declaration.
func (s TextScope) declare(name string) (string, error) {
	h, ok := s.doc.Helpers[name]
	if !ok {
		return "", fmt.Errorf("helper %q: helpers declares no such name", name)
	}

	if s.out == nil {
		return "", fmt.Errorf("helper %q: the generator binds no output to declare it in", name)
	}

	decl := s.prefix + upperFirst(name)

	s.out.Declare(decl, func(imp Importer) Step {
		return lang.Line(func(c Context) (string, error) {
			return s.execute(
				c,
				[]string{"helpers", name, "decl"},
				h.Decl,
				map[string]string{"Name": decl},
				imp,
				false,
			)
		})
	})

	return decl, nil
}

func upperFirst(name string) string {
	r, n := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(r)) + name[n:]
}

// isStd reports whether the package at path lies in the standard library:
// the first element of path holds no dot, as the go command tells standard
// packages apart, and the directory of the package lies under the src
// directory of GOROOT. Asking go/build to import path instead would run
// the go command for a path outside GOROOT.
func isStd(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	if path == "" || strings.Contains(first, ".") || build.Default.GOROOT == "" {
		return false
	}

	info, err := os.Stat(filepath.Join(build.Default.GOROOT, "src", filepath.FromSlash(path)))

	return err == nil && info.IsDir()
}
