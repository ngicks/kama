package dsttype_test

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"iter"
	"slices"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/typeprint"
	"github.com/ngicks/kama/kamalib/pkg/typeprint/dsttype"
)

const (
	internalPath = "github.com/ngicks/kama/kamalib/pkg/typeprint/internal/"
	modelPath    = internalPath + "model"
	otherPath    = internalPath + "other"
)

func load(t *testing.T) (map[string]*packages.Package, []*packages.Package) {
	t.Helper()

	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedImports |
			packages.NeedDeps | packages.NeedSyntax,
	}, "../internal/...")
	assert.NilError(t, err)

	byPath := map[string]*packages.Package{}

	for _, p := range pkgs {
		assert.Equal(t, len(p.Errors), 0, "%v", p.Errors)
		byPath[p.PkgPath] = p
	}

	return byPath, pkgs
}

func newMap(t *testing.T, all []*packages.Package, pkg *packages.Package) *importdecl.Map {
	t.Helper()

	m, err := importdecl.NewParserFromPackages(all).ParseAst(pkg.Syntax[0].Imports)
	assert.NilError(t, err)

	return m
}

func formatDst(t *testing.T, expr dst.Expr) string {
	t.Helper()

	const prefix = "package p\n\ntype X "

	f := &dst.File{
		Name: dst.NewIdent("p"),
		Decls: []dst.Decl{&dst.GenDecl{
			Tok:   token.TYPE,
			Specs: []dst.Spec{&dst.TypeSpec{Name: dst.NewIdent("X"), Type: expr}},
		}},
	}

	var buf bytes.Buffer
	assert.NilError(t, decorator.Fprint(&buf, f))
	out, ok := strings.CutPrefix(buf.String(), prefix)
	assert.Assert(t, ok, "%s", buf.String())

	return strings.TrimSuffix(out, "\n")
}

type testCase struct {
	name string
	ty   types.Type
	want string
}

// modelCases returns a case for each variable of the model package, and for
// each type whose name starts with U by its underlying type. The source
// spelling of the type expression is the expected output.
func modelCases(t *testing.T, pkg *packages.Package) []testCase {
	t.Helper()

	var cases []testCase

	add := func(name string, ty types.Type, expr ast.Expr) {
		var buf bytes.Buffer
		assert.NilError(t, format.Node(&buf, pkg.Fset, expr))
		cases = append(cases, testCase{name: name, ty: ty, want: buf.String()})
	}

	specs := opiter.SlicesValues(pkg.Syntax[0].Decls).
		FlatMap(func(d ast.Decl) iter.Seq[ast.Spec] { return slices.Values(genDeclSpecs(d)) })

	for spec := range specs {
		switch spec := spec.(type) {
		case *ast.ValueSpec:
			name := spec.Names[0].Name
			add(name, pkg.Types.Scope().Lookup(name).Type(), spec.Type)
		case *ast.TypeSpec:
			name := spec.Name.Name
			if strings.HasPrefix(name, "U") {
				add(name, pkg.Types.Scope().Lookup(name).Type().Underlying(), spec.Type)
			}
		}
	}

	return cases
}

func genDeclSpecs(d ast.Decl) []ast.Spec {
	if g, ok := d.(*ast.GenDecl); ok {
		return g.Specs
	}

	return nil
}

func TestModel(t *testing.T) {
	pkgs, all := load(t)
	model := pkgs[modelPath]

	cases := modelCases(t, model)
	assert.Assert(t, len(cases) > 30)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := dsttype.Dst(tc.ty, newMap(t, all, model), modelPath)
			assert.NilError(t, err)
			assert.Equal(t, formatDst(t, expr), tc.want)
		})
	}
}

func lookup(pkg *packages.Package, name string) types.Type {
	return pkg.Types.Scope().Lookup(name).Type()
}

func TestQualifyCurrentPackage(t *testing.T) {
	pkgs, all := load(t)
	model := pkgs[modelPath]
	ty := lookup(model, "VGenericLocal")

	m := newMap(t, all, model)
	dstExpr, err := dsttype.Dst(ty, m, otherPath)
	assert.NilError(t, err)
	assert.Equal(t, formatDst(t, dstExpr), "model.G[string, *model.Local]")

	assert.DeepEqual(t, slices.Collect(m.Missing()), []importdecl.Import{
		{Package: importdecl.Package{Path: modelPath, Name: "model"}},
	})
}

func TestMissingImport(t *testing.T) {
	pkgs, all := load(t)
	model, other := pkgs[modelPath], pkgs[otherPath]
	// model does not import bytes, the package of the field type.
	ty := lookup(other, "Holder").Underlying().(*types.Struct).Field(0).Type()

	m := newMap(t, all, model)
	expr, err := dsttype.Dst(ty, m, modelPath)
	assert.NilError(t, err)
	assert.Equal(t, formatDst(t, expr), "bytes.Buffer")
	assert.DeepEqual(t, slices.Collect(m.Missing()), []importdecl.Import{
		{Package: importdecl.Package{Path: "bytes", Name: "bytes"}},
	})
}

func TestUnexportedNameInDeclaringPackage(t *testing.T) {
	pkgs, all := load(t)
	other := pkgs[otherPath]

	for _, tc := range []struct {
		name string
		want string
	}{
		{"UnexportedField", "struct{ x int }"},
		{"UnexportedEmbedded", "struct{ int }"},
		{"UnexportedMethod", "interface{ seal() }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ty := lookup(other, tc.name)

			dstExpr, err := dsttype.Dst(ty, newMap(t, all, other), otherPath)
			assert.NilError(t, err)
			assert.Equal(t, formatDst(t, dstExpr), tc.want)
		})
	}
}

func TestErrors(t *testing.T) {
	pkgs, all := load(t)
	model, other := pkgs[modelPath], pkgs[otherPath]

	unknownPkg := types.NewPackage("example.invalid/unknown", "unknown")
	unknown := types.NewNamed(
		types.NewTypeName(token.NoPos, unknownPkg, "T", nil),
		types.Typ[types.Int],
		nil,
	)

	localScope := types.NewScope(model.Types.Scope(), token.NoPos, token.NoPos, "func")
	localObj := types.NewTypeName(token.NoPos, model.Types, "inner", nil)
	localScope.Insert(localObj)
	local := types.NewNamed(localObj, types.Typ[types.Int], nil)

	tparam := types.NewTypeParam(
		types.NewTypeName(token.NoPos, model.Types, "T", nil),
		types.NewInterfaceType(nil, nil),
	)
	genericSig := types.NewSignatureType(
		nil, nil, []*types.TypeParam{tparam}, types.NewTuple(), types.NewTuple(), false,
	)

	for _, tc := range []struct {
		name string
		ty   types.Type
		err  error
	}{
		{"unknown package", unknown, typeprint.ErrUnknownPackage},
		{"unknown package in element", types.NewSlice(unknown), typeprint.ErrUnknownPackage},
		{"unexported", lookup(other, "Unexported"), typeprint.ErrUnexported},
		{"unexported field", lookup(other, "UnexportedField"), typeprint.ErrUnexported},
		{"unexported embedded field", lookup(other, "UnexportedEmbedded"), typeprint.ErrUnexported},
		{"unexported method", lookup(other, "UnexportedMethod"), typeprint.ErrUnexported},
		{"uninstantiated generic", lookup(model, "G"), typeprint.ErrUnsupported},
		{"uninstantiated generic alias", lookup(model, "AG"), typeprint.ErrUnsupported},
		{
			"uninstantiated generic in element",
			types.NewSlice(lookup(model, "G")),
			typeprint.ErrUnsupported,
		},
		{"untyped", types.Typ[types.UntypedInt], typeprint.ErrUnsupported},
		{"untyped nil", types.Typ[types.UntypedNil], typeprint.ErrUnsupported},
		{"invalid", types.Typ[types.Invalid], typeprint.ErrUnsupported},
		{"tuple", types.NewTuple(), typeprint.ErrUnsupported},
		{"generic signature", genericSig, typeprint.ErrUnsupported},
		{"local type", local, typeprint.ErrUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := dsttype.Dst(tc.ty, newMap(t, all, model), modelPath)
			assert.ErrorIs(t, err, tc.err)
		})
	}
}
