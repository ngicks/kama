package gofragment_test

import (
	"errors"
	"go/ast"
	"go/scanner"
	"go/token"
	"testing"

	"github.com/dave/dst"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/gofragment"
)

func TestParsePrintDstExpr(t *testing.T) {
	for _, src := range []string{
		"a.b(c, 1)",
		"map[string][]T",
		"*T",
		"[]*pkg.T",
		"func(int) error",
		"chan<- int",
		"<-chan int",
		"struct {\n\tA int `json:\"a\"`\n\tB string\n}",
		"interface{ M() }",
		"Set[T]",
		"Map[K, V]",
		"func(x int) int {\n\t// double\n\treturn x * 2\n}",
		"T{\n\tA: 1, // one\n\tB: `raw\n\tline\n  end`,\n}",
	} {
		t.Run(src, func(t *testing.T) {
			e, err := gofragment.ParseDstExpr([]byte(src))
			assert.NilError(t, err)
			got, err := gofragment.PrintDstExpr(e)
			assert.NilError(t, err)
			assert.Equal(t, string(got), src)
		})
	}
}

func TestPrintDstExprSurroundingText(t *testing.T) {
	e, err := gofragment.ParseDstExpr([]byte("\n\t f(x) // dropped\n\n"))
	assert.NilError(t, err)
	got, err := gofragment.PrintDstExpr(e)
	assert.NilError(t, err)
	assert.Equal(t, string(got), "f(x)")
}

func TestParsePrintDstStmts(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{name: "empty", src: ""},
		{name: "one", src: "x := 1\n"},
		{
			name: "comments",
			src:  "// leading\nx := 1 // trailing\n\n/* block */\ny(x)\n",
		},
		{
			name: "nested",
			src:  "for k, v := range m {\n\tif v {\n\t\t// skip\n\t\tcontinue\n\t}\n\tout[k] = v\n}\n",
		},
		{
			name: "raw string",
			src:  "s := `first\n\tindented\n  spaced\nlast`\nf(s)\n",
		},
		{
			name: "raw string in block",
			src:  "if ok {\n\ts := `a\n\tb\n\t\tc`\n\tf(s)\n}\n",
		},
		{
			name: "labeled",
			src:  "L:\nfor {\n\tbreak L\n}\nx++\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stmts, err := gofragment.ParseDstStmts([]byte(tc.src))
			assert.NilError(t, err)
			assert.Assert(t, stmts != nil)
			got, err := gofragment.PrintDstStmts(stmts)
			assert.NilError(t, err)
			assert.Equal(t, string(got), tc.src)
		})
	}
}

func TestParsePrintDstDecls(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{name: "empty", src: ""},
		{name: "type", src: "type T struct {\n\tA int\n}\n"},
		{
			name: "doc comments",
			src: "// F does nothing.\nfunc F() {}\n\n// leading\nvar x = 1 // trailing\n\n" +
				"// M is a method.\nfunc (t T) M() string {\n\treturn `a\n\tb`\n}\n",
		},
		{name: "grouped", src: "const (\n\tA = iota\n\tB\n)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decls, err := gofragment.ParseDstDecls([]byte(tc.src))
			assert.NilError(t, err)
			assert.Assert(t, decls != nil)
			got, err := gofragment.PrintDstDecls(decls)
			assert.NilError(t, err)
			assert.Equal(t, string(got), tc.src)
		})
	}
}

func TestParseDstComments(t *testing.T) {
	stmts, err := gofragment.ParseDstStmts([]byte("// leading\nx := 1\n"))
	assert.NilError(t, err)
	assert.DeepEqual(t, []string(stmts[0].Decorations().Start), []string{"// leading"})

	decls, err := gofragment.ParseDstDecls([]byte("// F does nothing.\nfunc F() {}\n"))
	assert.NilError(t, err)
	assert.DeepEqual(t, []string(decls[0].Decorations().Start), []string{"// F does nothing."})
}

func TestParseRejects(t *testing.T) {
	type parseFunc func(b []byte) error

	expr := func(b []byte) error { _, err := gofragment.ParseDstExpr(b); return err }
	stmts := func(b []byte) error { _, err := gofragment.ParseDstStmts(b); return err }
	decls := func(b []byte) error { _, err := gofragment.ParseDstDecls(b); return err }
	astExpr := func(b []byte) error {
		_, err := gofragment.ParseAstExpr(token.NewFileSet(), b)
		return err
	}
	astStmts := func(b []byte) error {
		_, err := gofragment.ParseAstStmts(token.NewFileSet(), b)
		return err
	}
	astDecls := func(b []byte) error {
		_, err := gofragment.ParseAstDecls(token.NewFileSet(), b)
		return err
	}

	for _, tc := range []struct {
		name  string
		parse []parseFunc
		src   string
		// syntax marks an error of the Go parser instead of ErrNotPartial.
		syntax bool
	}{
		{name: "expr then decl", parse: []parseFunc{expr, astExpr}, src: "a; var x = 1"},
		{name: "expr then func", parse: []parseFunc{expr, astExpr}, src: "a\n\nfunc f() {}"},
		{name: "two exprs", parse: []parseFunc{expr, astExpr}, src: "a, b"},
		{name: "expr semicolon", parse: []parseFunc{expr, astExpr}, src: "a;"},
		{name: "empty expr", parse: []parseFunc{expr, astExpr}, src: " \n\t", syntax: true},
		{name: "expr syntax", parse: []parseFunc{expr, astExpr}, src: "a +", syntax: true},
		{
			name:  "stmts close body",
			parse: []parseFunc{stmts, astStmts},
			src:   "x := 1\n} ; func other() {",
		},
		{
			name:  "stmts close body twice",
			parse: []parseFunc{stmts, astStmts},
			src:   "}\n\nfunc _() {",
		},
		{name: "stmts syntax", parse: []parseFunc{stmts, astStmts}, src: "x :=", syntax: true},
		{name: "import", parse: []parseFunc{decls, astDecls}, src: "import \"fmt\"\n"},
		{
			name:  "import group",
			parse: []parseFunc{decls, astDecls},
			src:   "import (\n\t\"fmt\"\n)\n\nvar x = fmt.Sprint()\n",
		},
		{
			name:   "import after decl",
			parse:  []parseFunc{decls, astDecls},
			src:    "var x = 1\n\nimport \"fmt\"\n",
			syntax: true,
		},
		{name: "package clause", parse: []parseFunc{decls, astDecls}, src: "package q\n", syntax: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, parse := range tc.parse {
				err := parse([]byte(tc.src))
				assert.Assert(t, err != nil)

				var list scanner.ErrorList
				if tc.syntax {
					assert.Assert(t, errors.As(err, &list), "%v", err)
				} else {
					assert.ErrorIs(t, err, gofragment.ErrNotPartial)
				}
			}
		})
	}
}

func TestParseEmpty(t *testing.T) {
	for _, src := range []string{"", " \n\t\n"} {
		stmts, err := gofragment.ParseDstStmts([]byte(src))
		assert.NilError(t, err)
		assert.Equal(t, len(stmts), 0)

		decls, err := gofragment.ParseDstDecls([]byte(src))
		assert.NilError(t, err)
		assert.Equal(t, len(decls), 0)

		astStmts, err := gofragment.ParseAstStmts(token.NewFileSet(), []byte(src))
		assert.NilError(t, err)
		assert.Assert(t, astStmts != nil)
		assert.Equal(t, len(astStmts), 0)

		astDecls, err := gofragment.ParseAstDecls(token.NewFileSet(), []byte(src))
		assert.NilError(t, err)
		assert.Assert(t, astDecls != nil)
		assert.Equal(t, len(astDecls), 0)
	}
}

func TestPrintDstTwice(t *testing.T) {
	const src = "// leading\nx := f(func() {\n\ty()\n})\n"

	stmts, err := gofragment.ParseDstStmts([]byte(src))
	assert.NilError(t, err)

	for range 2 {
		got, err := gofragment.PrintDstStmts(stmts)
		assert.NilError(t, err)
		assert.Equal(t, string(got), src)
	}

	e, err := gofragment.ParseDstExpr([]byte("a[b]"))
	assert.NilError(t, err)

	for range 2 {
		got, err := gofragment.PrintDstExpr(e)
		assert.NilError(t, err)
		assert.Equal(t, string(got), "a[b]")
	}

	decls, err := gofragment.ParseDstDecls([]byte("var x = 1\n"))
	assert.NilError(t, err)

	before := decls[0].Decorations().Before
	for range 2 {
		got, err := gofragment.PrintDstDecls(decls)
		assert.NilError(t, err)
		assert.Equal(t, string(got), "var x = 1\n")
	}

	assert.Equal(t, decls[0].Decorations().Before, before)
}

func TestPrintDstNoMutation(t *testing.T) {
	stmt := &dst.ExprStmt{X: &dst.CallExpr{Fun: dst.NewIdent("f")}}
	decl := &dst.GenDecl{Tok: token.VAR, Specs: []dst.Spec{&dst.ValueSpec{
		Names:  []*dst.Ident{dst.NewIdent("x")},
		Values: []dst.Expr{dst.NewIdent("y")},
	}}}

	for range 2 {
		got, err := gofragment.PrintDstStmts([]dst.Stmt{stmt})
		assert.NilError(t, err)
		assert.Equal(t, string(got), "f()\n")

		got, err = gofragment.PrintDstDecls([]dst.Decl{decl})
		assert.NilError(t, err)
		assert.Equal(t, string(got), "var x = y\n")
	}

	assert.Equal(t, stmt.Decs.Before, dst.None)
	assert.Equal(t, decl.Decs.Before, dst.None)
}

func TestTrailingComments(t *testing.T) {
	stmts, err := gofragment.ParseDstStmts([]byte("x := 1\n// tail\n"))
	assert.NilError(t, err)
	got, err := gofragment.PrintDstStmts(stmts)
	assert.NilError(t, err)
	assert.Equal(t, string(got), "x := 1\n// tail\n")

	decls, err := gofragment.ParseDstDecls([]byte("var x = 1\n\n// tail\n"))
	assert.NilError(t, err)
	got, err = gofragment.PrintDstDecls(decls)
	assert.NilError(t, err)
	assert.Equal(t, string(got), "var x = 1\n\n// tail\n")
}

func TestPrintDstBuilt(t *testing.T) {
	call := &dst.ExprStmt{X: &dst.CallExpr{Fun: dst.NewIdent("f")}}
	call.Decs.Before = dst.NewLine
	call.Decs.Start.Append("// call f")
	got, err := gofragment.PrintDstStmts([]dst.Stmt{
		&dst.AssignStmt{
			Lhs: []dst.Expr{dst.NewIdent("x")},
			Tok: token.DEFINE,
			Rhs: []dst.Expr{&dst.BasicLit{Kind: token.INT, Value: "1"}},
		},
		call,
		&dst.ReturnStmt{},
	})
	assert.NilError(t, err)
	assert.Equal(t, string(got), "x := 1\n// call f\nf()\nreturn\n")
}

func TestPrintDstPath(t *testing.T) {
	_, err := gofragment.PrintDstExpr(&dst.Ident{Name: "X", Path: "example.com/x"})
	assert.ErrorContains(t, err, "package path")
}

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
