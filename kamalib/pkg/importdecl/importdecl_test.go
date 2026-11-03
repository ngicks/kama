package importdecl

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
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
			// The name is explicit because the path does not tell it.
			{Name: "pkg22", Package: Package{Path: pkg22Path, Name: "pkg22"}},
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
	p.Require(
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
	)
	m := parse(t, p, pkgs["pkg1"])

	assert.DeepEqual(t, missing(m), []Import{
		{Name: "foo", Package: Package{Path: "example.com/foo0", Name: "foo"}},
		{Name: "foo_1", Package: Package{Path: "example.com/foo1", Name: "foo"}},
		{Name: "foo_2", Package: Package{Path: "example.com/foo2", Name: "foo"}},
		{Name: "json_1", Package: Package{Path: "example.com/json", Name: "json"}},
		{Name: "x", Package: Package{Path: "example.com/x/v3"}},
		{Name: "renamed", Package: Package{Path: "example.com/y", Name: "y"}},
		{Package: Package{Path: "example.com/z/v2", Name: "z"}},
	})

	got, ok := m.Ident("example.com/foo1")
	assert.Assert(t, ok)
	assert.Equal(t, got, "foo_1")

	// The file's own import of a required package wins, and the package
	// name comes from the requirement.
	m, err := p.ParseAst([]*ast.ImportSpec{{
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.com/y"`},
	}})
	assert.NilError(t, err)
	got, _ = m.Ident("example.com/y")
	assert.Equal(t, got, "y")
	assert.Assert(t, !slices.ContainsFunc(missing(m), func(imp Import) bool {
		return imp.Package.Path == "example.com/y"
	}))

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

func requireExtra(p *Parser) {
	p.Require(
		Import{Package: Package{Path: "example.com/x/v3"}},
		Import{Package: Package{Path: "example.com/json", Name: "json"}},
	)
}

const wantPkg1 = `// Package pkg1 imports packages under their own names.
package pkg1

import (
	"encoding/json"
	json_1 "example.com/json"
	x "example.com/x/v3"
	pkg22 "github.com/ngicks/kama/kamalib/pkg/importdecl/internal/pkg2/pkg2-2"
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
	pkg22 "github.com/ngicks/kama/kamalib/pkg/importdecl/internal/pkg2/pkg2-2"
)
`

func TestAddMissingAst(t *testing.T) {
	pkgs, all := load(t)

	for name, want := range map[string]string{"pkg1": wantPkg1, "pkg4": wantPkg4} {
		t.Run(name, func(t *testing.T) {
			pkg := pkgs[name]
			p := NewParserFromPackages(all)
			requireExtra(p)
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
			requireExtra(p)
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
