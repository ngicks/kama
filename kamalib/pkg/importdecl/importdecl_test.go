package importdecl

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"
)

const internalPath = "github.com/ngicks/kama/kamalib/pkg/importdecl/internal/"

const pkg22Path = internalPath + "pkg2/pkg2-2"

func load(t *testing.T) (map[string]*packages.Package, []*packages.Package) {
	t.Helper()

	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedImports |
			packages.NeedDeps | packages.NeedSyntax,
	}, "./internal/...")
	assert.NilError(t, err)

	byName := map[string]*packages.Package{}

	for _, p := range pkgs {
		assert.Equal(t, len(p.Errors), 0, "%v", p.Errors)
		byName[p.Name] = p
	}

	return byName, pkgs
}

func parse(t *testing.T, p *Parser, pkg *packages.Package) *Map {
	t.Helper()

	m, err := p.ParseAst(pkg.Syntax[0].Imports)
	assert.NilError(t, err)

	return m
}

func missing(m *Map) []Import { return slices.Collect(m.Missing()) }

func TestIdent(t *testing.T) {
	pkgs, all := load(t)

	t.Run("imported", func(t *testing.T) {
		m := parse(t, NewParserFromPackages(all), pkgs["pkg1"])

		for pkgPath, want := range map[string]string{
			"encoding/json": "json",
			"math/rand/v2":  "rand",
		} {
			got, ok := m.Ident(pkgPath)
			assert.Assert(t, ok)
			assert.Equal(t, got, want)
		}

		assert.Equal(t, len(missing(m)), 0)
	})

	t.Run("explicit name", func(t *testing.T) {
		m := parse(t, NewParserFromPackages(all), pkgs["pkg2"])

		got, ok := m.Ident(pkg22Path)
		assert.Assert(t, ok)
		assert.Equal(t, got, "pkg22222")
		assert.Equal(t, len(missing(m)), 0)
	})

	t.Run("known but not imported", func(t *testing.T) {
		m := parse(t, NewParserFromPackages(all), pkgs["pkg1"])

		got, ok := m.Ident(pkg22Path)
		assert.Assert(t, ok)
		assert.Equal(t, got, "pkg22")

		// Asking again returns the same identifier.
		got, _ = m.Ident(pkg22Path)
		assert.Equal(t, got, "pkg22")

		assert.DeepEqual(t, missing(m), []Import{
			// pkg2-2 drops to pkg22, the package name, so no name is written.
			{Package: Package{Path: pkg22Path, Name: "pkg22"}},
		})
	})

	t.Run("blank and dot imports give no identifier", func(t *testing.T) {
		m := parse(t, NewParserFromPackages(all), pkgs["pkg3"])

		got, ok := m.Ident("strings")
		assert.Assert(t, ok)
		assert.Equal(t, got, "strings")

		assert.DeepEqual(t, missing(m), []Import{
			{Package: Package{Path: "strings", Name: "strings"}},
		})
	})

	t.Run("unknown", func(t *testing.T) {
		m := parse(t, NewParserFromPackages(all), pkgs["pkg1"])

		_, ok := m.Ident("example.com/unknown")
		assert.Assert(t, !ok)
		assert.Assert(t, m.AstExpr(QualifiedType{"example.com/unknown", "T"}) == nil)
		assert.Assert(t, m.DstExpr(QualifiedType{"example.com/unknown", "T"}) == nil)
	})

	t.Run("NewParser knows only the package and its imports", func(t *testing.T) {
		p := NewParser(pkgs["pkg2"].Types)

		m := parse(t, p, pkgs["pkg4"])
		got, ok := m.Ident(pkg22Path)
		assert.Assert(t, ok)
		assert.Equal(t, got, "pkg22")

		// pkg1 imports encoding/json without a name, and pkg2 does not import
		// it.
		_, err := p.ParseAst(pkgs["pkg1"].Syntax[0].Imports)
		assert.ErrorIs(t, err, ErrUnknownImport)
	})
}

func TestRequire(t *testing.T) {
	pkgs, _ := load(t)

	p := NewParser(pkgs["pkg1"].Types)
	assert.NilError(t, p.Require(
		Import{Package: Package{Path: "example.com/foo1", Name: "foo"}},
		Import{Package: Package{Path: "example.com/foo0", Name: "foo"}},
		Import{Package: Package{Path: "example.com/foo0", Name: "foo"}},
		Import{Package: Package{Path: "example.com/foo2", Name: "foo"}},
		// The path does not tell the package name foo, so foo0 is named
		// explicitly too.
		// example.com/json conflicts with the file's own import.
		Import{Package: Package{Path: "example.com/json", Name: "json"}},
		// The file already imports it.
		Import{Package: Package{Path: "encoding/json", Name: "json"}},
		// The name is inferred from the path and made explicit.
		Import{Package: Package{Path: "example.com/x/v3"}},
		Import{Name: "renamed", Package: Package{Path: "example.com/y", Name: "y"}},
		Import{Package: Package{Path: "example.com/z/v2", Name: "z"}},
	))
	m := parse(t, p, pkgs["pkg1"])

	assert.DeepEqual(t, missing(m), []Import{
		{Name: "foo", Package: Package{Path: "example.com/foo0", Name: "foo"}},
		{Name: "foo_2", Package: Package{Path: "example.com/foo1", Name: "foo"}},
		{Name: "foo_3", Package: Package{Path: "example.com/foo2", Name: "foo"}},
		{Name: "json_2", Package: Package{Path: "example.com/json", Name: "json"}},
		{Name: "x", Package: Package{Path: "example.com/x/v3"}},
		{Name: "renamed", Package: Package{Path: "example.com/y", Name: "y"}},
		{Package: Package{Path: "example.com/z/v2", Name: "z"}},
	})

	got, ok := m.Ident("example.com/foo1")
	assert.Assert(t, ok)
	assert.Equal(t, got, "foo_2")

	// The file's own import of a required package wins, and the package
	// name comes from the requirement.
	m, err := p.ParseAst([]*ast.ImportSpec{{
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.com/y"`},
	}})
	assert.NilError(t, err)

	got, _ = m.Ident("example.com/y")
	assert.Equal(t, got, "y")

	yMissing := opiter.WrapSeq(
		m.Missing(),
	).
		ContainsFunc(
			func(imp Import) bool { return imp.Package.Path == "example.com/y" },
		)

	assert.Assert(t, !yMissing)

	// A requirement without a package name does not tell the name of an
	// unnamed import.
	_, err = p.ParseAst([]*ast.ImportSpec{{
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.com/x/v3"`},
	}})
	assert.ErrorIs(t, err, ErrUnknownImport)
}

func TestParseErrors(t *testing.T) {
	p := NewParser(types.NewPackage("example.com/p", "p"))

	_, err := p.ParseAst([]*ast.ImportSpec{{
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.com/unknown"`},
	}})
	assert.ErrorIs(t, err, ErrUnknownImport)

	// A named import needs no package information.
	m, err := p.ParseDst([]*dst.ImportSpec{{
		Name: dst.NewIdent("u"),
		Path: &dst.BasicLit{Kind: token.STRING, Value: `"example.com/unknown"`},
	}})
	assert.NilError(t, err)

	got, _ := m.Ident("example.com/unknown")
	assert.Equal(t, got, "u")

	_, err = p.ParseAst([]*ast.ImportSpec{{
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"unterminated`},
	}})
	assert.ErrorContains(t, err, "import path")
}

func TestMapReserve(t *testing.T) {
	pkg := types.NewPackage("example.com/p", "p")
	pkg.SetImports([]*types.Package{
		types.NewPackage("example.com/bar", "bar"),
		types.NewPackage("example.com/foo", "foo"),
		types.NewPackage("example.com/qux", "qux"),
	})
	p := NewParser(pkg)
	p.Reserve("parsed")
	m, err := p.ParseAst([]*ast.ImportSpec{{
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.com/bar"`},
	}})
	assert.NilError(t, err)

	got, _ := m.Ident("example.com/qux")
	assert.Equal(t, got, "qux")

	m.Reserve("bar", "foo", "qux", "local")

	// Imports the map already holds keep their identifiers.
	got, _ = m.Ident("example.com/bar")
	assert.Equal(t, got, "bar")
	got, _ = m.Ident("example.com/qux")
	assert.Equal(t, got, "qux")

	got, _ = m.Ident("example.com/foo")
	assert.Equal(t, got, "foo_2")

	for ident, want := range map[string]bool{
		"bar":     true,
		"qux":     true,
		"foo_2":   true,
		"foo":     false,
		"local":   false,
		"parsed":  false,
		"unknown": false,
	} {
		assert.Equal(t, m.HasIdent(ident), want, "%s", ident)
	}
}

func TestExpr(t *testing.T) {
	pkgs, _ := load(t)
	m := parse(t, NewParser(pkgs["pkg2"].Types), pkgs["pkg2"])

	a := m.AstExpr(QualifiedType{pkg22Path, "Pkg22"})
	assert.Equal(t, a.X.(*ast.Ident).Name, "pkg22222")
	assert.Equal(t, a.Sel.Name, "Pkg22")

	d := m.DstExpr(QualifiedType{pkg22Path, "Pkg22"})
	assert.Equal(t, d.X.(*dst.Ident).Name, "pkg22222")
	assert.Equal(t, d.Sel.Name, "Pkg22")
}

func TestQualifier(t *testing.T) {
	pkgs, _ := load(t)
	pkg2 := pkgs["pkg2"]
	m := parse(t, NewParser(pkg2.Types), pkg2)

	pkg22 := pkg2.Types.Imports()[0]
	ty := pkg22.Scope().Lookup("Pkg22").Type()
	assert.Equal(t, types.TypeString(ty, m.Qualifier(pkg2.PkgPath)), "pkg22222.Pkg22")
	assert.Equal(t, types.TypeString(ty, m.Qualifier(pkg22Path)), "Pkg22")
}

func TestQualifiedTypeIs(t *testing.T) {
	pkgs, _ := load(t)
	pkg22 := pkgs["pkg2"].Types.Imports()[0]
	ty := pkg22.Scope().Lookup("Pkg22").Type()

	assert.Assert(t, QualifiedType{pkg22Path, "Pkg22"}.Is(ty))
	assert.Assert(t, !QualifiedType{pkg22Path, "Other"}.Is(ty))
	assert.Assert(t, !QualifiedType{"example.com/other", "Pkg22"}.Is(ty))
	assert.Assert(t, QualifiedType{"", "error"}.Is(types.Universe.Lookup("error").Type()))
	assert.Assert(t, !QualifiedType{"", "int"}.Is(types.Typ[types.Int]))
}

func requireExtra(t *testing.T, p *Parser) {
	t.Helper()

	assert.NilError(t, p.Require(
		Import{Package: Package{Path: "example.com/x/v3"}},
		Import{Package: Package{Path: "example.com/json", Name: "json"}},
	))
}

const wantPkg1 = `// Package pkg1 imports packages under their own names.
package pkg1

import (
	"encoding/json"
	json_2 "example.com/json"
	x "example.com/x/v3"
	"github.com/ngicks/kama/kamalib/pkg/importdecl/internal/pkg2/pkg2-2"
	"math/rand/v2"
)

var (
	_ = json.Marshal
	_ = rand.Int
)
`

const wantPkg4 = `// Package pkg4 has no imports.
package pkg4

import (
	"example.com/json"
	x "example.com/x/v3"
	"github.com/ngicks/kama/kamalib/pkg/importdecl/internal/pkg2/pkg2-2"
)
`

func TestAddMissingAst(t *testing.T) {
	pkgs, all := load(t)

	for name, want := range map[string]string{"pkg1": wantPkg1, "pkg4": wantPkg4} {
		t.Run(name, func(t *testing.T) {
			pkg := pkgs[name]
			p := NewParserFromPackages(all)
			requireExtra(t, p)
			m := parse(t, p, pkg)
			_, _ = m.Ident(pkg22Path)

			f := pkg.Syntax[0]
			m.AddMissingAst(pkg.Fset, f)
			m.AddMissingAst(pkg.Fset, f)

			var buf bytes.Buffer
			assert.NilError(t, format.Node(&buf, pkg.Fset, f))
			assert.Equal(t, buf.String(), formatSource(t, want))
		})
	}
}

func TestAddMissingDst(t *testing.T) {
	pkgs, all := load(t)

	for name, want := range map[string]string{"pkg1": wantPkg1, "pkg4": wantPkg4} {
		t.Run(name, func(t *testing.T) {
			pkg := pkgs[name]
			df, err := decorator.NewDecorator(pkg.Fset).DecorateFile(pkg.Syntax[0])
			assert.NilError(t, err)

			p := NewParserFromPackages(all)
			requireExtra(t, p)
			m, err := p.ParseDst(df.Imports)
			assert.NilError(t, err)

			_, _ = m.Ident(pkg22Path)

			m.AddMissingDst(df)
			m.AddMissingDst(df)
			assert.Equal(t, len(df.Imports), len(pkg.Syntax[0].Imports)+3)

			var buf bytes.Buffer
			assert.NilError(t, decorator.Fprint(&buf, df))
			assert.Equal(t, formatSource(t, buf.String()), formatSource(t, want))
		})
	}
}

// formatSource formats src and sorts its imports, so that sources differing
// only in import order compare equal.
func formatSource(t *testing.T, src string) string {
	t.Helper()

	out, err := format.Source([]byte(src))
	assert.NilError(t, err)

	return string(out)
}

func TestInferName(t *testing.T) {
	for pkgPath, want := range map[string]string{
		"encoding/json":       "json",
		"math/rand/v2":        "rand",
		"example.com/v2":      "examplecom",
		"example.com/x/v":     "v",
		"example.com/x/v2x":   "v2x",
		"example.com/go-yaml": "goyaml",
		"gopkg.in/yaml.v3":    "yamlv3",
		"example.com/a_b":     "a_b",
		"example.com/π-1":     "π1",
		"example.com/1st":     "_1st",
		"example.com/0":       "_0",
		"example.com/٣x":      "_٣x",
		"example.com/type":    "_type",
		"example.com/func":    "_func",
		"example.com/go":      "_go",
		"example.com/ty-pe":   "_type",
		"example.com/-":       "pkg",
		"example.com/.-.":     "pkg",
		"example.com/_":       "pkg",
		"example.com/_-":      "pkg",
		"example.com/__":      "__",
	} {
		t.Run(pkgPath, func(t *testing.T) {
			assert.Equal(t, inferName(pkgPath), want)
		})
	}
}

func TestInferNameKeywords(t *testing.T) {
	keywords := opiter.WrapSeq(
		opiter.Range(token.BREAK, token.VAR+1),
	).
		Filter(token.Token.IsKeyword)

	for tok := range keywords {
		kw := tok.String()
		assert.Equal(t, inferName("example.com/"+kw), "_"+kw)
	}
}

func TestRequireInferredNames(t *testing.T) {
	p := NewParser(types.NewPackage("example.com/p", "p"))
	assert.NilError(t, p.Require(
		Import{Package: Package{Path: "example.com/go-yaml"}},
		Import{Package: Package{Path: "gopkg.in/yaml.v3"}},
		Import{Package: Package{Path: "example.com/type"}},
		Import{Package: Package{Path: "example.com/ty-pe"}},
		Import{Package: Package{Path: "example.com/1st"}},
		// Every one of these drops to nothing, so they share pkg.
		Import{Package: Package{Path: "example.com/-"}},
		Import{Package: Package{Path: "example.com/x/-/v2"}},
		Import{Package: Package{Path: "example.com/y/_"}},
		Import{Package: Package{Path: "example.com/z/.."}},
		// A known name the path tells needs no explicit name.
		Import{Package: Package{Path: "example.com/check", Name: "check"}},
		// A known name the path does not tell is written explicitly.
		Import{Package: Package{Path: "example.com/go-check", Name: "check"}},
	))
	m, err := p.ParseAst(nil)
	assert.NilError(t, err)

	assert.DeepEqual(t, missing(m), []Import{
		{Name: "pkg", Package: Package{Path: "example.com/-"}},
		{Name: "_1st", Package: Package{Path: "example.com/1st"}},
		{Package: Package{Path: "example.com/check", Name: "check"}},
		{Name: "check_2", Package: Package{Path: "example.com/go-check", Name: "check"}},
		{Name: "goyaml", Package: Package{Path: "example.com/go-yaml"}},
		{Name: "_type", Package: Package{Path: "example.com/ty-pe"}},
		{Name: "_type_2", Package: Package{Path: "example.com/type"}},
		{Name: "pkg_2", Package: Package{Path: "example.com/x/-/v2"}},
		{Name: "pkg_3", Package: Package{Path: "example.com/y/_"}},
		{Name: "pkg_4", Package: Package{Path: "example.com/z/.."}},
		{Name: "yamlv3", Package: Package{Path: "gopkg.in/yaml.v3"}},
	})

	for pkgPath, want := range map[string]string{
		"example.com/-":      "pkg",
		"example.com/x/-/v2": "pkg_2",
		"example.com/y/_":    "pkg_3",
		"example.com/z/..":   "pkg_4",
	} {
		got, ok := m.Ident(pkgPath)
		assert.Assert(t, ok)
		assert.Equal(t, got, want, pkgPath)
	}

	// Every emitted spec, and a reference through it, parses.
	f := &ast.File{Name: ast.NewIdent("p")}
	fset := token.NewFileSet()
	m.AddMissingAst(fset, f)

	var buf bytes.Buffer
	assert.NilError(t, format.Node(&buf, fset, f))

	for imp := range m.Missing() {
		fmt.Fprintf(&buf, "\nvar _ = %s.X\n", imp.Ident())
	}

	formatSource(t, buf.String())
}

func TestRequireInvalidName(t *testing.T) {
	p := NewParser(types.NewPackage("example.com/p", "p"))

	for _, imp := range []Import{
		{Name: "go-yaml", Package: Package{Path: "example.com/go-yaml"}},
		{Name: "type", Package: Package{Path: "example.com/x"}},
		{Name: "_", Package: Package{Path: "example.com/x"}},
		{Name: ".", Package: Package{Path: "example.com/x"}},
		{Package: Package{Path: "example.com/x", Name: "1x"}},
	} {
		assert.ErrorIs(t, p.Require(imp), ErrInvalidName)
	}

	m, err := p.ParseAst(nil)
	assert.NilError(t, err)
	assert.Equal(t, len(missing(m)), 0, "a rejected Require adds nothing")
}

func TestFallbackSkipsTakenNames(t *testing.T) {
	t.Run("native name in the file", func(t *testing.T) {
		p := NewParser(types.NewPackage("example.com/p", "p"))
		assert.NilError(t, p.Require(
			Import{Package: Package{Path: "example.com/a/foo", Name: "foo"}},
			Import{Package: Package{Path: "example.com/b/foo", Name: "foo"}},
			Import{Package: Package{Path: "example.com/c/foo", Name: "foo"}},
		))
		m, err := p.ParseAst([]*ast.ImportSpec{{
			Name: ast.NewIdent("foo_2"),
			Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.com/native"`},
		}})
		assert.NilError(t, err)

		assert.DeepEqual(t, missing(m), []Import{
			{Package: Package{Path: "example.com/a/foo", Name: "foo"}},
			{Name: "foo_3", Package: Package{Path: "example.com/b/foo", Name: "foo"}},
			{Name: "foo_4", Package: Package{Path: "example.com/c/foo", Name: "foo"}},
		})
	})

	t.Run("native name required after the fallback took it", func(t *testing.T) {
		p := NewParser(types.NewPackage("example.com/p", "p"))
		assert.NilError(t, p.Require(
			Import{Package: Package{Path: "example.com/a/foo", Name: "foo"}},
			Import{Package: Package{Path: "example.com/b/foo", Name: "foo"}},
			Import{Package: Package{Path: "example.com/c/foo_2", Name: "foo_2"}},
		))
		m, err := p.ParseAst(nil)
		assert.NilError(t, err)

		assert.DeepEqual(t, missing(m), []Import{
			{Package: Package{Path: "example.com/a/foo", Name: "foo"}},
			{Name: "foo_2", Package: Package{Path: "example.com/b/foo", Name: "foo"}},
			{Name: "foo_2_2", Package: Package{Path: "example.com/c/foo_2", Name: "foo_2"}},
		})
	})

	t.Run("fallback through Ident", func(t *testing.T) {
		p := NewParser(types.NewPackage("example.com/p", "p"))
		m, err := p.ParseAst([]*ast.ImportSpec{
			{
				Name: ast.NewIdent("p"),
				Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.com/x"`},
			},
			{
				Name: ast.NewIdent("p_2"),
				Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.com/y"`},
			},
		})
		assert.NilError(t, err)

		got, ok := m.Ident("example.com/p")
		assert.Assert(t, ok)
		assert.Equal(t, got, "p_3")
	})
}

func TestReserve(t *testing.T) {
	p := NewParser(types.NewPackage("example.com/p", "p"))
	p.Reserve("foo", "foo_2", "bar")
	assert.NilError(t, p.Require(
		Import{Package: Package{Path: "example.com/foo", Name: "foo"}},
	))
	m, err := p.ParseAst([]*ast.ImportSpec{{
		// The file's own import keeps a reserved name.
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.com/bar"`},
		Name: ast.NewIdent("bar"),
	}})
	assert.NilError(t, err)

	assert.DeepEqual(t, missing(m), []Import{
		{Name: "foo_3", Package: Package{Path: "example.com/foo", Name: "foo"}},
	})
	got, ok := m.Ident("example.com/bar")
	assert.Assert(t, ok)
	assert.Equal(t, got, "bar")

	// A Map parsed later starts from the reserved names again.
	m, err = p.ParseAst(nil)
	assert.NilError(t, err)

	got, _ = m.Ident("example.com/foo")
	assert.Equal(t, got, "foo_3")
}

func TestImports(t *testing.T) {
	p := NewParser(types.NewPackage("example.com/p", "p"))
	assert.NilError(t, p.Require(
		Import{Package: Package{Path: "example.com/foo", Name: "foo"}},
		Import{Package: Package{Path: "example.com/other/foo", Name: "foo"}},
	))

	lit := func(path string) *ast.BasicLit {
		return &ast.BasicLit{Kind: token.STRING, Value: `"` + path + `"`}
	}
	m, err := p.ParseAst([]*ast.ImportSpec{
		{Path: lit("example.com/zzz"), Name: ast.NewIdent("z")},
		{Path: lit("example.com/foo")},
		// Blank and dot imports give no identifier.
		{Path: lit("example.com/blank"), Name: ast.NewIdent("_")},
		{Path: lit("example.com/dot"), Name: ast.NewIdent(".")},
		// The second import of a package is left out.
		{Path: lit("example.com/zzz"), Name: ast.NewIdent("again")},
	})
	assert.NilError(t, err)

	assert.DeepEqual(t, slices.Collect(m.Imports()), []Import{
		{Package: Package{Path: "example.com/foo", Name: "foo"}},
		{Name: "foo_2", Package: Package{Path: "example.com/other/foo", Name: "foo"}},
		{Name: "z", Package: Package{Path: "example.com/zzz"}},
	})
	assert.DeepEqual(t, missing(m), []Import{
		{Name: "foo_2", Package: Package{Path: "example.com/other/foo", Name: "foo"}},
	})

	empty, err := p.ParseAst(nil)
	assert.NilError(t, err)
	assert.Equal(t, len(slices.Collect(empty.Imports())), 2)
}
