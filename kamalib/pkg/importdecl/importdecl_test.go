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

		_, ok := m.Ident("example.invalid/unknown")
		assert.Assert(t, !ok)
		assert.Assert(t, m.AstExpr(QualifiedType{"example.invalid/unknown", "T"}) == nil)
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
		Import{Package: Package{Path: "example.invalid/foo1", Name: "foo"}},
		Import{Package: Package{Path: "example.invalid/foo0", Name: "foo"}},
		Import{Package: Package{Path: "example.invalid/foo0", Name: "foo"}},
		Import{Package: Package{Path: "example.invalid/foo2", Name: "foo"}},
		// The path does not tell the package name foo, so foo0 is named
		// explicitly too.
		// example.invalid/json conflicts with the file's own import.
		Import{Package: Package{Path: "example.invalid/json", Name: "json"}},
		// The file already imports it.
		Import{Package: Package{Path: "encoding/json", Name: "json"}},
		// The name is inferred from the path and made explicit.
		Import{Package: Package{Path: "example.invalid/x/v3"}},
		Import{Name: "renamed", Package: Package{Path: "example.invalid/y", Name: "y"}},
		Import{Package: Package{Path: "example.invalid/z/v2", Name: "z"}},
	))
	m := parse(t, p, pkgs["pkg1"])

	assert.DeepEqual(t, missing(m), []Import{
		{Name: "foo", Package: Package{Path: "example.invalid/foo0", Name: "foo"}},
		{Name: "foo_2", Package: Package{Path: "example.invalid/foo1", Name: "foo"}},
		{Name: "foo_3", Package: Package{Path: "example.invalid/foo2", Name: "foo"}},
		{Name: "json_2", Package: Package{Path: "example.invalid/json", Name: "json"}},
		{Name: "x", Package: Package{Path: "example.invalid/x/v3"}},
		{Name: "renamed", Package: Package{Path: "example.invalid/y", Name: "y"}},
		{Package: Package{Path: "example.invalid/z/v2", Name: "z"}},
	})

	got, ok := m.Ident("example.invalid/foo1")
	assert.Assert(t, ok)
	assert.Equal(t, got, "foo_2")

	// The file's own import of a required package wins, and the package
	// name comes from the requirement.
	m, err := p.ParseAst([]*ast.ImportSpec{{
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.invalid/y"`},
	}})
	assert.NilError(t, err)

	got, _ = m.Ident("example.invalid/y")
	assert.Equal(t, got, "y")

	yMissing := opiter.WrapSeq(m.Missing()).
		ContainsFunc(func(imp Import) bool { return imp.Package.Path == "example.invalid/y" })

	assert.Assert(t, !yMissing)

	// A requirement without a package name does not tell the name of an
	// unnamed import.
	_, err = p.ParseAst([]*ast.ImportSpec{{
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.invalid/x/v3"`},
	}})
	assert.ErrorIs(t, err, ErrUnknownImport)
}

func TestParseErrors(t *testing.T) {
	p := NewParser(types.NewPackage("example.invalid/p", "p"))

	_, err := p.ParseAst([]*ast.ImportSpec{{
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.invalid/unknown"`},
	}})
	assert.ErrorIs(t, err, ErrUnknownImport)

	// A named import needs no package information.
	m, err := p.ParseAst([]*ast.ImportSpec{{
		Name: ast.NewIdent("u"),
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.invalid/unknown"`},
	}})
	assert.NilError(t, err)

	got, _ := m.Ident("example.invalid/unknown")
	assert.Equal(t, got, "u")

	_, err = p.ParseAst([]*ast.ImportSpec{{
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"unterminated`},
	}})
	assert.ErrorContains(t, err, "import path")
}

func TestMapReserve(t *testing.T) {
	pkg := types.NewPackage("example.invalid/p", "p")
	pkg.SetImports([]*types.Package{
		types.NewPackage("example.invalid/bar", "bar"),
		types.NewPackage("example.invalid/foo", "foo"),
		types.NewPackage("example.invalid/qux", "qux"),
	})
	p := NewParser(pkg)
	p.Reserve("parsed")
	m, err := p.ParseAst([]*ast.ImportSpec{{
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.invalid/bar"`},
	}})
	assert.NilError(t, err)

	got, _ := m.Ident("example.invalid/qux")
	assert.Equal(t, got, "qux")

	m.Reserve("bar", "foo", "qux", "local")

	// Imports the map already holds keep their identifiers.
	got, _ = m.Ident("example.invalid/bar")
	assert.Equal(t, got, "bar")
	got, _ = m.Ident("example.invalid/qux")
	assert.Equal(t, got, "qux")

	got, _ = m.Ident("example.invalid/foo")
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
	assert.Assert(t, !QualifiedType{"example.invalid/other", "Pkg22"}.Is(ty))
	assert.Assert(t, QualifiedType{"", "error"}.Is(types.Universe.Lookup("error").Type()))
	assert.Assert(t, !QualifiedType{"", "int"}.Is(types.Typ[types.Int]))
}

func requireExtra(t *testing.T, p *Parser) {
	t.Helper()

	assert.NilError(t, p.Require(
		Import{Package: Package{Path: "example.invalid/x/v3"}},
		Import{Package: Package{Path: "example.invalid/json", Name: "json"}},
	))
}

const wantPkg1 = `// Package pkg1 imports packages under their own names.
package pkg1

import (
	"encoding/json"
	json_2 "example.invalid/json"
	x "example.invalid/x/v3"
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
	"example.invalid/json"
	x "example.invalid/x/v3"
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
		"encoding/json":           "json",
		"math/rand/v2":            "rand",
		"example.invalid/v2":      "exampleinvalid",
		"example.invalid/x/v":     "v",
		"example.invalid/x/v2x":   "v2x",
		"example.invalid/go-yaml": "goyaml",
		"gopkg.in/yaml.v3":        "yamlv3",
		"example.invalid/a_b":     "a_b",
		"example.invalid/π-1":     "π1",
		"example.invalid/1st":     "_1st",
		"example.invalid/0":       "_0",
		"example.invalid/٣x":      "_٣x",
		"example.invalid/type":    "_type",
		"example.invalid/func":    "_func",
		"example.invalid/go":      "_go",
		"example.invalid/ty-pe":   "_type",
		"example.invalid/-":       "pkg",
		"example.invalid/.-.":     "pkg",
		"example.invalid/_":       "pkg",
		"example.invalid/_-":      "pkg",
		"example.invalid/__":      "__",
	} {
		t.Run(pkgPath, func(t *testing.T) {
			assert.Equal(t, inferName(pkgPath), want)
		})
	}
}

func TestInferNameKeywords(t *testing.T) {
	keywords := opiter.WrapSeq(opiter.Range(token.BREAK, token.VAR+1)).
		Filter(token.Token.IsKeyword)

	for tok := range keywords {
		kw := tok.String()
		assert.Equal(t, inferName("example.invalid/"+kw), "_"+kw)
	}
}

func TestRequireInferredNames(t *testing.T) {
	p := NewParser(types.NewPackage("example.invalid/p", "p"))
	assert.NilError(t, p.Require(
		Import{Package: Package{Path: "example.invalid/go-yaml"}},
		Import{Package: Package{Path: "gopkg.in/yaml.v3"}},
		Import{Package: Package{Path: "example.invalid/type"}},
		Import{Package: Package{Path: "example.invalid/ty-pe"}},
		Import{Package: Package{Path: "example.invalid/1st"}},
		// Every one of these drops to nothing, so they share pkg.
		Import{Package: Package{Path: "example.invalid/-"}},
		Import{Package: Package{Path: "example.invalid/x/-/v2"}},
		Import{Package: Package{Path: "example.invalid/y/_"}},
		Import{Package: Package{Path: "example.invalid/z/.."}},
		// A known name the path tells needs no explicit name.
		Import{Package: Package{Path: "example.invalid/check", Name: "check"}},
		// A known name the path does not tell is written explicitly.
		Import{Package: Package{Path: "example.invalid/go-check", Name: "check"}},
	))
	m, err := p.ParseAst(nil)
	assert.NilError(t, err)

	assert.DeepEqual(t, missing(m), []Import{
		{Name: "pkg", Package: Package{Path: "example.invalid/-"}},
		{Name: "_1st", Package: Package{Path: "example.invalid/1st"}},
		{Package: Package{Path: "example.invalid/check", Name: "check"}},
		{Name: "check_2", Package: Package{Path: "example.invalid/go-check", Name: "check"}},
		{Name: "goyaml", Package: Package{Path: "example.invalid/go-yaml"}},
		{Name: "_type", Package: Package{Path: "example.invalid/ty-pe"}},
		{Name: "_type_2", Package: Package{Path: "example.invalid/type"}},
		{Name: "pkg_2", Package: Package{Path: "example.invalid/x/-/v2"}},
		{Name: "pkg_3", Package: Package{Path: "example.invalid/y/_"}},
		{Name: "pkg_4", Package: Package{Path: "example.invalid/z/.."}},
		{Name: "yamlv3", Package: Package{Path: "gopkg.in/yaml.v3"}},
	})

	for pkgPath, want := range map[string]string{
		"example.invalid/-":      "pkg",
		"example.invalid/x/-/v2": "pkg_2",
		"example.invalid/y/_":    "pkg_3",
		"example.invalid/z/..":   "pkg_4",
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
	p := NewParser(types.NewPackage("example.invalid/p", "p"))

	for _, imp := range []Import{
		{Name: "go-yaml", Package: Package{Path: "example.invalid/go-yaml"}},
		{Name: "type", Package: Package{Path: "example.invalid/x"}},
		{Name: "_", Package: Package{Path: "example.invalid/x"}},
		{Name: ".", Package: Package{Path: "example.invalid/x"}},
		{Package: Package{Path: "example.invalid/x", Name: "1x"}},
	} {
		assert.ErrorIs(t, p.Require(imp), ErrInvalidName)
	}

	m, err := p.ParseAst(nil)
	assert.NilError(t, err)
	assert.Equal(t, len(missing(m)), 0, "a rejected Require adds nothing")
}

func TestFallbackSkipsTakenNames(t *testing.T) {
	t.Run("native name in the file", func(t *testing.T) {
		p := NewParser(types.NewPackage("example.invalid/p", "p"))
		assert.NilError(t, p.Require(
			Import{Package: Package{Path: "example.invalid/a/foo", Name: "foo"}},
			Import{Package: Package{Path: "example.invalid/b/foo", Name: "foo"}},
			Import{Package: Package{Path: "example.invalid/c/foo", Name: "foo"}},
		))
		m, err := p.ParseAst([]*ast.ImportSpec{{
			Name: ast.NewIdent("foo_2"),
			Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.invalid/native"`},
		}})
		assert.NilError(t, err)

		assert.DeepEqual(t, missing(m), []Import{
			{Package: Package{Path: "example.invalid/a/foo", Name: "foo"}},
			{Name: "foo_3", Package: Package{Path: "example.invalid/b/foo", Name: "foo"}},
			{Name: "foo_4", Package: Package{Path: "example.invalid/c/foo", Name: "foo"}},
		})
	})

	t.Run("native name required after the fallback took it", func(t *testing.T) {
		p := NewParser(types.NewPackage("example.invalid/p", "p"))
		assert.NilError(t, p.Require(
			Import{Package: Package{Path: "example.invalid/a/foo", Name: "foo"}},
			Import{Package: Package{Path: "example.invalid/b/foo", Name: "foo"}},
			Import{Package: Package{Path: "example.invalid/c/foo_2", Name: "foo_2"}},
		))
		m, err := p.ParseAst(nil)
		assert.NilError(t, err)

		assert.DeepEqual(t, missing(m), []Import{
			{Package: Package{Path: "example.invalid/a/foo", Name: "foo"}},
			{Name: "foo_2", Package: Package{Path: "example.invalid/b/foo", Name: "foo"}},
			{Name: "foo_2_2", Package: Package{Path: "example.invalid/c/foo_2", Name: "foo_2"}},
		})
	})

	t.Run("fallback through Ident", func(t *testing.T) {
		p := NewParser(types.NewPackage("example.invalid/p", "p"))
		m, err := p.ParseAst([]*ast.ImportSpec{
			{
				Name: ast.NewIdent("p"),
				Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.invalid/x"`},
			},
			{
				Name: ast.NewIdent("p_2"),
				Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.invalid/y"`},
			},
		})
		assert.NilError(t, err)

		got, ok := m.Ident("example.invalid/p")
		assert.Assert(t, ok)
		assert.Equal(t, got, "p_3")
	})
}

func TestReserve(t *testing.T) {
	p := NewParser(types.NewPackage("example.invalid/p", "p"))
	p.Reserve("foo", "foo_2", "bar")
	assert.NilError(t, p.Require(
		Import{Package: Package{Path: "example.invalid/foo", Name: "foo"}},
	))
	m, err := p.ParseAst([]*ast.ImportSpec{{
		// The file's own import keeps a reserved name.
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.invalid/bar"`},
		Name: ast.NewIdent("bar"),
	}})
	assert.NilError(t, err)

	assert.DeepEqual(t, missing(m), []Import{
		{Name: "foo_3", Package: Package{Path: "example.invalid/foo", Name: "foo"}},
	})
	got, ok := m.Ident("example.invalid/bar")
	assert.Assert(t, ok)
	assert.Equal(t, got, "bar")

	// A Map parsed later starts from the reserved names again.
	m, err = p.ParseAst(nil)
	assert.NilError(t, err)

	got, _ = m.Ident("example.invalid/foo")
	assert.Equal(t, got, "foo_3")
}

func TestImports(t *testing.T) {
	p := NewParser(types.NewPackage("example.invalid/p", "p"))
	assert.NilError(t, p.Require(
		Import{Package: Package{Path: "example.invalid/foo", Name: "foo"}},
		Import{Package: Package{Path: "example.invalid/other/foo", Name: "foo"}},
	))

	lit := func(path string) *ast.BasicLit {
		return &ast.BasicLit{Kind: token.STRING, Value: `"` + path + `"`}
	}
	m, err := p.ParseAst([]*ast.ImportSpec{
		{Path: lit("example.invalid/zzz"), Name: ast.NewIdent("z")},
		{Path: lit("example.invalid/foo")},
		// Blank and dot imports give no identifier.
		{Path: lit("example.invalid/blank"), Name: ast.NewIdent("_")},
		{Path: lit("example.invalid/dot"), Name: ast.NewIdent(".")},
		// The second import of a package is left out.
		{Path: lit("example.invalid/zzz"), Name: ast.NewIdent("again")},
	})
	assert.NilError(t, err)

	assert.DeepEqual(t, slices.Collect(m.Imports()), []Import{
		{Package: Package{Path: "example.invalid/foo", Name: "foo"}},
		{Name: "foo_2", Package: Package{Path: "example.invalid/other/foo", Name: "foo"}},
		{Name: "z", Package: Package{Path: "example.invalid/zzz"}},
	})
	assert.DeepEqual(t, missing(m), []Import{
		{Name: "foo_2", Package: Package{Path: "example.invalid/other/foo", Name: "foo"}},
	})

	empty, err := p.ParseAst(nil)
	assert.NilError(t, err)
	assert.Equal(t, opiter.Count(empty.Imports()), 2)
}
