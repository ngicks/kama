package dstimport_test

import (
	"bytes"
	"go/format"
	"go/token"
	"go/types"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/importdecl/dstimport"
)

// parser returns a parser of a file of example.invalid/p requiring the
// package example.invalid/q.
func parser(t *testing.T) *importdecl.Parser {
	t.Helper()

	p := importdecl.NewParser(types.NewPackage("example.invalid/p", "p"))
	assert.NilError(t, p.Require(importdecl.Import{
		Package: importdecl.Package{Path: "example.invalid/q", Name: "q"},
	}))

	return p
}

func TestParse(t *testing.T) {
	// A named import needs no package information.
	m, err := dstimport.Parse(parser(t), []*dst.ImportSpec{{
		Name: dst.NewIdent("u"),
		Path: &dst.BasicLit{Kind: token.STRING, Value: `"example.invalid/unknown"`},
	}})
	assert.NilError(t, err)

	got, _ := m.Ident("example.invalid/unknown")
	assert.Equal(t, got, "u")

	_, err = dstimport.Parse(parser(t), []*dst.ImportSpec{{
		Path: &dst.BasicLit{Kind: token.STRING, Value: `"example.invalid/unknown"`},
	}})
	assert.ErrorIs(t, err, importdecl.ErrUnknownImport)
}

func TestExpr(t *testing.T) {
	m, err := dstimport.Parse(parser(t), nil)
	assert.NilError(t, err)

	e := dstimport.Expr(
		m,
		importdecl.QualifiedType{PackagePath: "example.invalid/q", TypeName: "T"},
	)
	assert.Equal(t, e.X.(*dst.Ident).Name, "q")
	assert.Equal(t, e.Sel.Name, "T")

	assert.Assert(t, dstimport.Expr(m, importdecl.QualifiedType{
		PackagePath: "example.invalid/unknown", TypeName: "T",
	}) == nil)
}

func TestAddMissing(t *testing.T) {
	// The parser knows no package of the file, so the import names it.
	f, err := decorator.Parse("package p\n\nimport fmt \"fmt\"\n\nvar _ = fmt.Sprint\n")
	assert.NilError(t, err)

	p := parser(t)
	p.Reserve("q")
	m, err := dstimport.Parse(p, f.Imports)
	assert.NilError(t, err)

	dstimport.AddMissing(m, f)
	dstimport.AddMissing(m, f)
	assert.Equal(t, len(f.Imports), 2)

	var buf bytes.Buffer
	assert.NilError(t, decorator.Fprint(&buf, f))

	got, err := format.Source(buf.Bytes())
	assert.NilError(t, err)
	assert.Equal(
		t,
		string(got),
		"package p\n\nimport (\n\tfmt \"fmt\"\n\n\tq_2 \"example.invalid/q\"\n)\n\n"+
			"var _ = fmt.Sprint\n",
	)
}
