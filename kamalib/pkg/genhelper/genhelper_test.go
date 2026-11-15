package genhelper_test

import (
	"go/token"
	"go/types"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/typeprint"
)

func TestRendererPrependKeepsArgs(t *testing.T) {
	r := genhelper.Renderer[ctx]{
		Context: genhelper.NewBaseContext(genhelper.NewScope(), nil),
		Steps:   genhelper.Steps[ctx]{genhelper.Stmt[ctx]("c")},
	}
	args := make([]genhelper.Step[ctx], 2, 4)
	args[0] = genhelper.Stmt[ctx]("a")
	args[1] = genhelper.Stmt[ctx]("b")
	spare := args[:4]
	spare[2] = genhelper.Stmt[ctx]("spare")

	r.Prepend(args...)

	assert.Equal(t, len(args), 2)
	_, b, err := genhelper.Steps[ctx](spare[2:3]).Render(r.Context, nil)
	assert.NilError(t, err)
	assert.Equal(t, string(b), "spare\n")
	_, b, err = r.Render(nil)
	assert.NilError(t, err)
	assert.Equal(t, string(b), "a\nb\nc\n")
}

func TestBlockNilWrap(t *testing.T) {
	_, b, err := genhelper.Block[ctx]{
		Declare: func(c ctx) (ctx, error) {
			c.Scope().Declare("v")
			return c, nil
		},
		Body: genhelper.Stmt[ctx]("_ = %s", genhelper.Ref("v")),
	}.Render(genhelper.NewBaseContext(genhelper.NewScope(), nil), []byte("x\n"))
	assert.NilError(t, err)
	assert.Equal(t, string(b), "x\n_ = v\n")

	_, b, err = genhelper.Block[ctx]{}.Render(
		genhelper.NewBaseContext(genhelper.NewScope(), nil),
		[]byte("x\n"),
	)
	assert.NilError(t, err)
	assert.Equal(t, string(b), "x\n")
}

func TestPackageNilImports(t *testing.T) {
	const destPath = "example.com/dest"
	dest := types.NewPackage(destPath, "dest")
	other := types.NewPackage("example.com/other", "other")
	named := func(pkg *types.Package, name string) types.Type {
		return types.NewNamed(
			types.NewTypeName(token.NoPos, pkg, name, nil),
			types.Typ[types.Int],
			nil,
		)
	}
	pkg := &genhelper.Package{Path: destPath}

	got, err := pkg.Qual(destPath, "A")
	assert.NilError(t, err)
	assert.Equal(t, got, "A")
	_, err = pkg.Qual(other.Path(), "A")
	assert.ErrorIs(t, err, importdecl.ErrUnknownImport)

	got, err = pkg.Type(types.NewSlice(named(dest, "A")))
	assert.NilError(t, err)
	assert.Equal(t, got, "[]A")
	got, err = pkg.Type(types.Typ[types.String])
	assert.NilError(t, err)
	assert.Equal(t, got, "string")

	_, err = pkg.Type(named(other, "B"))
	assert.ErrorIs(t, err, typeprint.ErrUnknownPackage)
	_, err = pkg.TypeDst(named(other, "B"))
	assert.ErrorIs(t, err, typeprint.ErrUnknownPackage)
	_, err = pkg.TypeAst(named(other, "B"))
	assert.ErrorIs(t, err, typeprint.ErrUnknownPackage)
}
