package genhelper_test

import (
	"errors"
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"github.com/ngicks/opiter"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/gofragment"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/typeprint"
)

type ctx = genhelper.BaseContext

const (
	destPath = "example.invalid/dest"
	optPath  = "example.invalid/opt"
)

// newPackage returns a Package for a file of destPath importing nothing.
// The package can import optPath.
func newPackage(t *testing.T, fset *token.FileSet) (*genhelper.Package, *types.Package) {
	t.Helper()

	opt := types.NewPackage(optPath, "opt")
	dest := types.NewPackage(destPath, "dest")
	dest.SetImports([]*types.Package{opt})
	imports, err := importdecl.NewParser(dest).ParseAst(nil)
	assert.NilError(t, err)

	return &genhelper.Package{Imports: imports, Path: destPath, Fset: fset}, opt
}

func missingPaths(m *importdecl.Map) []string {
	return opiter.WrapSeq(m.Missing()).
		Map(func(imp importdecl.Import) string { return imp.Package.Path }).
		CollectSlice()
}

// marker is a context distinct from the one a step starts with.
func marker() ctx {
	return genhelper.NewBaseContext(genhelper.NewScope(), nil)
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
			types.NewPackage("example.invalid/unknown", "unknown"),
			"U",
			nil,
		),
		types.Typ[types.Int],
		nil,
	)
	_, err = pkg.Type(unknown)
	assert.ErrorIs(t, err, typeprint.ErrUnknownPackage)
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
