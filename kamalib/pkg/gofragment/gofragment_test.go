package gofragment_test

import (
	"go/ast"
	"go/token"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/gofragment"
)

func TestParsePrintAst(t *testing.T) {
	fset := token.NewFileSet()

	t.Run("expr", func(t *testing.T) {
		for _, src := range []string{"map[string][]T", "Set[T]", "func(x int) int {\n\treturn x\n}"} {
			e, err := gofragment.ParseAstExpr(fset, []byte(src))
			assert.NilError(t, err)
			got, err := gofragment.PrintAst(fset, e)
			assert.NilError(t, err)
			assert.Equal(t, string(got), src)
		}
	})

	t.Run("stmts", func(t *testing.T) {
		const src = "s := `a\n\tb`\nL:\nfor {\n\tbreak L\n}\n"

		stmts, err := gofragment.ParseAstStmts(fset, []byte(src))
		assert.NilError(t, err)
		assert.Equal(t, len(stmts), 2)

		got, err := gofragment.PrintAst(fset, stmts[0])
		assert.NilError(t, err)
		assert.Equal(t, string(got), "s := `a\n\tb`\n")

		got, err = gofragment.PrintAst(fset, stmts[1])
		assert.NilError(t, err)
		assert.Equal(t, string(got), "L:\nfor {\n\tbreak L\n}\n")
	})

	t.Run("decls", func(t *testing.T) {
		const src = "// F does nothing.\nfunc F() {}\n"

		decls, err := gofragment.ParseAstDecls(fset, []byte(src))
		assert.NilError(t, err)
		assert.Equal(t, len(decls), 1)
		got, err := gofragment.PrintAst(fset, decls[0])
		assert.NilError(t, err)
		assert.Equal(t, string(got), src)
	})

	t.Run("unsupported", func(t *testing.T) {
		_, err := gofragment.PrintAst(fset, &ast.File{})
		assert.ErrorContains(t, err, "unsupported")
	})

	t.Run("no file set", func(t *testing.T) {
		_, err := gofragment.ParseAstExpr(nil, []byte("a"))
		assert.ErrorIs(t, err, gofragment.ErrNoFileSet)
		_, err = gofragment.PrintAst(nil, ast.NewIdent("a"))
		assert.ErrorIs(t, err, gofragment.ErrNoFileSet)
	})
}

func TestPrintAstLists(t *testing.T) {
	fset := token.NewFileSet()
	stmts, err := gofragment.ParseAstStmts(fset, []byte("x := 1\nif x > 0 {\n\treturn\n}\n"))
	assert.NilError(t, err)
	b, err := gofragment.PrintAstStmts(fset, stmts)
	assert.NilError(t, err)
	assert.Equal(t, string(b), "x := 1\nif x > 0 {\n\treturn\n}\n")

	decls, err := gofragment.ParseAstDecls(fset, []byte("var x = 1\n\nfunc f() {}\n"))
	assert.NilError(t, err)
	b, err = gofragment.PrintAstDecls(fset, decls)
	assert.NilError(t, err)
	assert.Equal(t, string(b), "var x = 1\n\nfunc f() {}\n")

	_, err = gofragment.PrintAstStmts(nil, stmts)
	assert.ErrorIs(t, err, gofragment.ErrNoFileSet)
	_, err = gofragment.PrintAstDecls(nil, decls)
	assert.ErrorIs(t, err, gofragment.ErrNoFileSet)
}
