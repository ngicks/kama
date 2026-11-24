package dstgen_test

import (
	"bytes"
	"errors"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/genhelper/dstgen"
	"github.com/ngicks/kama/kamalib/pkg/gofragment"
	"github.com/ngicks/kama/kamalib/pkg/gofragment/dstfragment"
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

func TestTypeDst(t *testing.T) {
	pkg, opt := newPackage(t, nil)
	option := types.NewNamed(
		types.NewTypeName(token.NoPos, opt, "Option", nil),
		types.Typ[types.Int],
		nil,
	)

	e, err := dstgen.TypeDst(pkg, types.NewSlice(option))
	assert.NilError(t, err)
	assert.DeepEqual(t, missingPaths(pkg.Imports), []string{optPath})

	got, err := pkg.Type(types.NewSlice(option))
	assert.NilError(t, err)
	assert.Equal(t, got, "[]opt.Option")
	assert.Assert(t, e != nil)

	_, err = dstgen.TypeDst(&genhelper.Package{Path: destPath}, option)
	assert.ErrorIs(t, err, typeprint.ErrUnknownPackage)
}

func TestDstSteps(t *testing.T) {
	start := genhelper.NewBaseContext(genhelper.NewScope(), nil)

	t.Run("expr", func(t *testing.T) {
		want := marker()
		step := dstgen.DstExpr(func(_ ctx, e dst.Expr) (ctx, dst.Expr, error) {
			return want, &dst.CallExpr{Fun: dst.NewIdent("wrap"), Args: []dst.Expr{e}}, nil
		})
		c, b, err := step.Render(start, []byte("a.b"))
		assert.NilError(t, err)
		assert.Equal(t, string(b), "wrap(a.b)")
		assert.Equal(t, c.Scope(), want.Scope())
	})

	t.Run("stmts", func(t *testing.T) {
		want := marker()
		step := dstgen.DstStmts(func(_ ctx, stmts []dst.Stmt) (ctx, []dst.Stmt, error) {
			return want, append(stmts, &dst.ReturnStmt{}), nil
		})
		c, b, err := step.Render(start, []byte("// first\nf()\n"))
		assert.NilError(t, err)
		assert.Equal(t, string(b), "// first\nf()\nreturn\n")
		assert.Equal(t, c.Scope(), want.Scope())
	})

	t.Run("decls", func(t *testing.T) {
		want := marker()
		step := dstgen.DstDecls(func(_ ctx, decls []dst.Decl) (ctx, []dst.Decl, error) {
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
		step := dstgen.DstExpr(func(c ctx, e dst.Expr) (ctx, dst.Expr, error) {
			return c, e, errF
		})
		_, _, err := step.Render(start, []byte("a"))
		assert.Equal(t, err, errF)
	})

	t.Run("parse error", func(t *testing.T) {
		called := false
		step := dstgen.DstStmts(func(c ctx, stmts []dst.Stmt) (ctx, []dst.Stmt, error) {
			called = true
			return c, stmts, nil
		})
		_, _, err := step.Render(start, []byte("}\nfunc f() {"))
		assert.ErrorIs(t, err, gofragment.ErrNotPartial)
		assert.Assert(t, !called)
	})

	t.Run("print error", func(t *testing.T) {
		step := dstgen.DstExpr(func(c ctx, _ dst.Expr) (ctx, dst.Expr, error) {
			return c, &dst.Ident{Name: "X", Path: optPath}, nil
		})
		_, _, err := step.Render(start, []byte("a"))
		assert.ErrorContains(t, err, "gofragment: print expression")
	})
}

func TestAppendDecls(t *testing.T) {
	const src = "package dest\n\nvar x = 1\n"

	t.Run("appended", func(t *testing.T) {
		pkg, _ := newPackage(t, nil)
		f, err := decorator.Parse(src)
		assert.NilError(t, err)

		ident, ok := pkg.Imports.Ident(optPath)
		assert.Assert(t, ok)

		err = dstgen.AppendDecls(
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
			"import \"example.invalid/opt\"\n\n"+
			"var x = 1\n\n"+
			"// F returns none.\nfunc F() opt.Option {\n\treturn opt.None()\n}\n")
	})

	t.Run("parse error", func(t *testing.T) {
		pkg, _ := newPackage(t, nil)
		f, err := decorator.Parse(src)
		assert.NilError(t, err)

		_, _ = pkg.Imports.Ident(optPath)

		err = dstgen.AppendDecls(f, pkg, []byte("var y = 2\n\nimport \"fmt\"\n"))
		assert.Assert(t, err != nil)
		err = dstgen.AppendDecls(f, pkg, []byte("import \"fmt\"\n"))
		assert.ErrorIs(t, err, gofragment.ErrNotPartial)

		var buf bytes.Buffer
		assert.NilError(t, decorator.Fprint(&buf, f))
		assert.Equal(t, buf.String(), src)
	})
}

func TestAppendDeclsWithoutPackage(t *testing.T) {
	f, err := decorator.Parse("package p\n")
	assert.NilError(t, err)
	assert.NilError(t, dstgen.AppendDecls(f, nil, []byte("func f() {}\n")))
	assert.Equal(t, len(f.Decls), 1)
}

func TestParseDecls(t *testing.T) {
	decls, err := dstgen.ParseDecls([]byte("var x = 1\nfunc f() {}\n"))
	assert.NilError(t, err)
	assert.Equal(t, len(decls), 2)

	for _, d := range decls {
		assert.Equal(t, d.Decorations().Before, dst.EmptyLine)
	}

	_, err = dstgen.ParseDecls([]byte("import \"fmt\"\n"))
	assert.ErrorIs(t, err, gofragment.ErrNotPartial)
}

func TestStepsFuncError(t *testing.T) {
	errF := errors.New("f")
	start := genhelper.NewBaseContext(genhelper.NewScope(), nil)
	want := marker()

	type testCase struct {
		name string
		step genhelper.Step[ctx]
		src  string
	}
	for _, tc := range []testCase{
		{
			name: "DstExpr",
			step: dstgen.DstExpr(func(_ ctx, e dst.Expr) (ctx, dst.Expr, error) {
				return want, e, errF
			}),
			src: "x",
		},
		{
			name: "DstStmts",
			step: dstgen.DstStmts(func(_ ctx, s []dst.Stmt) (ctx, []dst.Stmt, error) {
				return want, s, errF
			}),
			src: "x()",
		},
		{
			name: "DstDecls",
			step: dstgen.DstDecls(func(_ ctx, d []dst.Decl) (ctx, []dst.Decl, error) {
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

// TestTypeMatchesDst spells every type of the model package of typeprint
// with genhelper.Package.Type, which prints an ast expression, and with
// TypeDst: both give the layout gofmt gives.
func TestTypeMatchesDst(t *testing.T) {
	const modelPath = "github.com/ngicks/kama/kamalib/pkg/typeprint/internal/model"

	all, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedImports |
			packages.NeedDeps | packages.NeedSyntax,
	}, "../../typeprint/internal/...")
	assert.NilError(t, err)

	isModel := func(p *packages.Package) bool { return p.PkgPath == modelPath }
	model := all[slices.IndexFunc(all, isModel)]

	var tys []types.Type

	for _, name := range model.Types.Scope().Names() {
		obj := model.Types.Scope().Lookup(name)
		switch {
		case strings.HasPrefix(name, "U"):
			tys = append(tys, obj.Type().Underlying())
		case !isTypeName(obj):
			tys = append(tys, obj.Type())
		}
	}

	assert.Assert(t, len(tys) > 30)

	for _, ty := range tys {
		imports, err := importdecl.NewParserFromPackages(all).ParseAst(model.Syntax[0].Imports)
		assert.NilError(t, err)

		pkg := &genhelper.Package{Imports: imports, Path: modelPath}

		e, err := dstgen.TypeDst(pkg, ty)
		assert.NilError(t, err)

		want, err := dstfragment.PrintDstExpr(e)
		assert.NilError(t, err)

		got, err := pkg.Type(ty)
		assert.NilError(t, err)
		assert.Equal(t, got, string(want))
	}
}

func isTypeName(obj types.Object) bool {
	_, ok := obj.(*types.TypeName)
	return ok
}
