package genhelper_test

import (
	"bytes"
	"errors"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/gofragment"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/typeprint"
)

type ctx = genhelper.BaseContext

const (
	destPath = "example.com/dest"
	optPath  = "example.com/opt"
)

// newPackage returns a Package for a file of destPath importing nothing.
// The package can import optPath.
func newPackage(t *testing.T, fset *token.FileSet) (*genhelper.Package, *types.Package) {
	t.Helper()

	opt := types.NewPackage(optPath, "opt")
	dest := types.NewPackage(destPath, "dest")
	dest.SetImports([]*types.Package{opt})
	imports, err := importdecl.NewParser(dest).ParseDst(nil)
	assert.NilError(t, err)
	return &genhelper.Package{Imports: imports, Path: destPath, Fset: fset}, opt
}

func missingPaths(m *importdecl.Map) []string {
	var out []string
	for imp := range m.Missing() {
		out = append(out, imp.Package.Path)
	}
	return out
}

// marker is a context distinct from the one a step starts with.
func marker() ctx {
	return genhelper.NewBaseContext(genhelper.NewScope(), nil)
}

func TestDstSteps(t *testing.T) {
	start := genhelper.NewBaseContext(genhelper.NewScope(), nil)

	t.Run("expr", func(t *testing.T) {
		want := marker()
		step := genhelper.DstExpr(func(_ ctx, e dst.Expr) (ctx, dst.Expr, error) {
			return want, &dst.CallExpr{Fun: dst.NewIdent("wrap"), Args: []dst.Expr{e}}, nil
		})
		c, b, err := step.Render(start, []byte("a.b"))
		assert.NilError(t, err)
		assert.Equal(t, string(b), "wrap(a.b)")
		assert.Equal(t, c.Scope(), want.Scope())
	})

	t.Run("stmts", func(t *testing.T) {
		want := marker()
		step := genhelper.DstStmts(func(_ ctx, stmts []dst.Stmt) (ctx, []dst.Stmt, error) {
			return want, append(stmts, &dst.ReturnStmt{}), nil
		})
		c, b, err := step.Render(start, []byte("// first\nf()\n"))
		assert.NilError(t, err)
		assert.Equal(t, string(b), "// first\nf()\nreturn\n")
		assert.Equal(t, c.Scope(), want.Scope())
	})

	t.Run("decls", func(t *testing.T) {
		want := marker()
		step := genhelper.DstDecls(func(_ ctx, decls []dst.Decl) (ctx, []dst.Decl, error) {
			slices.Reverse(decls)
			return want, decls, nil
		})
		c, b, err := step.Render(start, []byte("var a = 1\n\n// B is two.\nvar B = 2\n"))
		assert.NilError(t, err)
		assert.Equal(t, string(b), "// B is two.\nvar B = 2\n\nvar a = 1\n")
		assert.Equal(t, c.Scope(), want.Scope())
	})

	t.Run("f error", func(t *testing.T) {
		errF := errors.New("f")
		step := genhelper.DstExpr(func(c ctx, e dst.Expr) (ctx, dst.Expr, error) {
			return c, e, errF
		})
		_, _, err := step.Render(start, []byte("a"))
		assert.Equal(t, err, errF)
	})

	t.Run("parse error", func(t *testing.T) {
		called := false
		step := genhelper.DstStmts(func(c ctx, stmts []dst.Stmt) (ctx, []dst.Stmt, error) {
			called = true
			return c, stmts, nil
		})
		_, _, err := step.Render(start, []byte("}\nfunc f() {"))
		assert.ErrorIs(t, err, gofragment.ErrNotPartial)
		assert.Assert(t, !called)
	})

	t.Run("print error", func(t *testing.T) {
		step := genhelper.DstExpr(func(c ctx, _ dst.Expr) (ctx, dst.Expr, error) {
			return c, &dst.Ident{Name: "X", Path: optPath}, nil
		})
		_, _, err := step.Render(start, []byte("a"))
		assert.ErrorContains(t, err, "gofragment: print expression")
	})
}

func TestAstSteps(t *testing.T) {
	fset := token.NewFileSet()
	pkg, _ := newPackage(t, fset)
	start := genhelper.NewBaseContext(genhelper.NewScope(), pkg)

	t.Run("expr", func(t *testing.T) {
		want := marker()
		step := genhelper.AstExpr(func(_ ctx, e ast.Expr) (ctx, ast.Expr, error) {
			return want, &ast.StarExpr{X: e}, nil
		})
		c, b, err := step.Render(start, []byte("map[string]T"))
		assert.NilError(t, err)
		assert.Equal(t, string(b), "*map[string]T")
		assert.Equal(t, c.Scope(), want.Scope())
	})

	t.Run("stmts", func(t *testing.T) {
		want := marker()
		step := genhelper.AstStmts(func(_ ctx, stmts []ast.Stmt) (ctx, []ast.Stmt, error) {
			return want, stmts[1:], nil
		})
		c, b, err := step.Render(start, []byte("a()\nif x {\n\tb()\n}\n"))
		assert.NilError(t, err)
		assert.Equal(t, string(b), "if x {\n\tb()\n}\n")
		assert.Equal(t, c.Scope(), want.Scope())
	})

	t.Run("decls", func(t *testing.T) {
		want := marker()
		step := genhelper.AstDecls(func(_ ctx, decls []ast.Decl) (ctx, []ast.Decl, error) {
			return want, decls[:1], nil
		})
		c, b, err := step.Render(start, []byte("func F() {}\n\nvar x = 1\n"))
		assert.NilError(t, err)
		assert.Equal(t, string(b), "func F() {}\n")
		assert.Equal(t, c.Scope(), want.Scope())
	})

	t.Run("f error", func(t *testing.T) {
		errF := errors.New("f")
		step := genhelper.AstStmts(func(c ctx, stmts []ast.Stmt) (ctx, []ast.Stmt, error) {
			return c, stmts, errF
		})
		_, _, err := step.Render(start, []byte("a()"))
		assert.Equal(t, err, errF)
	})

	t.Run("parse error", func(t *testing.T) {
		step := genhelper.AstDecls(func(c ctx, decls []ast.Decl) (ctx, []ast.Decl, error) {
			return c, decls, nil
		})
		_, _, err := step.Render(start, []byte("import \"fmt\"\n"))
		assert.ErrorIs(t, err, gofragment.ErrNotPartial)
	})

	t.Run("no file set", func(t *testing.T) {
		noFset, _ := newPackage(t, nil)
		for _, c := range []ctx{
			genhelper.NewBaseContext(genhelper.NewScope(), nil),
			genhelper.NewBaseContext(genhelper.NewScope(), noFset),
		} {
			step := genhelper.AstExpr(func(c ctx, e ast.Expr) (ctx, ast.Expr, error) {
				return c, e, nil
			})
			_, _, err := step.Render(c, []byte("a"))
			assert.ErrorIs(t, err, gofragment.ErrNoFileSet)
		}
	})
}

func TestPackageType(t *testing.T) {
	pkg, opt := newPackage(t, nil)
	option := types.NewNamed(
		types.NewTypeName(token.NoPos, opt, "Option", nil),
		types.Typ[types.Int],
		nil,
	)

	got, err := pkg.Type(types.NewMap(types.Typ[types.String], types.NewSlice(option)))
	assert.NilError(t, err)
	assert.Equal(t, got, "map[string][]opt.Option")
	assert.DeepEqual(t, missingPaths(pkg.Imports), []string{optPath})

	unknown := types.NewNamed(
		types.NewTypeName(
			token.NoPos,
			types.NewPackage("example.com/unknown", "unknown"),
			"U",
			nil,
		),
		types.Typ[types.Int],
		nil,
	)
	_, err = pkg.Type(unknown)
	assert.ErrorIs(t, err, typeprint.ErrUnknownPackage)
}

func TestAppendDecls(t *testing.T) {
	const src = "package dest\n\nvar x = 1\n"

	t.Run("appended", func(t *testing.T) {
		pkg, _ := newPackage(t, nil)
		f, err := decorator.Parse(src)
		assert.NilError(t, err)
		ident, ok := pkg.Imports.Ident(optPath)
		assert.Assert(t, ok)

		err = genhelper.AppendDecls(
			f,
			pkg,
			[]byte(
				"// F returns none.\nfunc F() "+ident+".Option {\n\treturn "+ident+".None()\n}\n",
			),
		)
		assert.NilError(t, err)

		var buf bytes.Buffer
		assert.NilError(t, decorator.Fprint(&buf, f))
		assert.Equal(t, buf.String(), "package dest\n\n"+
			"import \"example.com/opt\"\n\n"+
			"var x = 1\n\n"+
			"// F returns none.\nfunc F() opt.Option {\n\treturn opt.None()\n}\n")
	})

	t.Run("parse error", func(t *testing.T) {
		pkg, _ := newPackage(t, nil)
		f, err := decorator.Parse(src)
		assert.NilError(t, err)
		_, _ = pkg.Imports.Ident(optPath)

		err = genhelper.AppendDecls(f, pkg, []byte("var y = 2\n\nimport \"fmt\"\n"))
		assert.Assert(t, err != nil)
		err = genhelper.AppendDecls(f, pkg, []byte("import \"fmt\"\n"))
		assert.ErrorIs(t, err, gofragment.ErrNotPartial)

		var buf bytes.Buffer
		assert.NilError(t, decorator.Fprint(&buf, f))
		assert.Equal(t, buf.String(), src)
	})
}

func TestNodeStepsFuncError(t *testing.T) {
	errF := errors.New("f")
	pkg, _ := newPackage(t, token.NewFileSet())
	start := genhelper.NewBaseContext(genhelper.NewScope(), pkg)
	want := marker()

	type testCase struct {
		name string
		step genhelper.Step[ctx]
		src  string
	}
	for _, tc := range []testCase{
		{
			name: "DstExpr",
			step: genhelper.DstExpr(func(_ ctx, e dst.Expr) (ctx, dst.Expr, error) {
				return want, e, errF
			}),
			src: "x",
		},
		{
			name: "DstStmts",
			step: genhelper.DstStmts(func(_ ctx, s []dst.Stmt) (ctx, []dst.Stmt, error) {
				return want, s, errF
			}),
			src: "x()",
		},
		{
			name: "DstDecls",
			step: genhelper.DstDecls(func(_ ctx, d []dst.Decl) (ctx, []dst.Decl, error) {
				return want, d, errF
			}),
			src: "var x = 1",
		},
		{
			name: "AstExpr",
			step: genhelper.AstExpr(func(_ ctx, e ast.Expr) (ctx, ast.Expr, error) {
				return want, e, errF
			}),
			src: "x",
		},
		{
			name: "AstStmts",
			step: genhelper.AstStmts(func(_ ctx, s []ast.Stmt) (ctx, []ast.Stmt, error) {
				return want, s, errF
			}),
			src: "x()",
		},
		{
			name: "AstDecls",
			step: genhelper.AstDecls(func(_ ctx, d []ast.Decl) (ctx, []ast.Decl, error) {
				return want, d, errF
			}),
			src: "var x = 1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, b, err := tc.step.Render(start, []byte(tc.src))
			assert.Equal(t, err, errF)
			assert.Assert(t, b == nil)
			assert.Equal(t, c.Scope(), want.Scope())
		})
	}
}

func TestAppendDeclsWithoutPackage(t *testing.T) {
	f, err := decorator.Parse("package p\n")
	assert.NilError(t, err)
	assert.NilError(t, genhelper.AppendDecls(f, nil, []byte("func f() {}\n")))
	assert.Equal(t, len(f.Decls), 1)
}
