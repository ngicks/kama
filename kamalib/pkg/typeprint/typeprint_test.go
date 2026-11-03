package typeprint

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/scanner"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/importdecl"
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
	}, "./internal/...")
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

func formatAst(t *testing.T, expr ast.Expr) string {
	t.Helper()

	var buf bytes.Buffer
	assert.NilError(t, format.Node(&buf, token.NewFileSet(), expr))
	return buf.String()
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

// tokens scans src into its tokens separated by spaces, dropping
// semicolons since newlines insert them.
func tokens(t *testing.T, src string) string {
	t.Helper()

	var s scanner.Scanner
	fset := token.NewFileSet()
	s.Init(fset.AddFile("", -1, len(src)), []byte(src), func(pos token.Position, msg string) {
		t.Fatalf("%s: %s", pos, msg)
	}, 0)

	var toks []string
	for {
		_, tok, lit := s.Scan()
		switch {
		case tok == token.EOF:
			return strings.Join(toks, " ")
		case tok == token.SEMICOLON:
		case lit != "":
			toks = append(toks, lit)
		default:
			toks = append(toks, tok.String())
		}
	}
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

	for _, decl := range pkg.Syntax[0].Decls {
		g, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range g.Specs {
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
	}
	return cases
}

func TestModel(t *testing.T) {
	pkgs, all := load(t)
	model := pkgs[modelPath]

	cases := modelCases(t, model)
	assert.Assert(t, len(cases) > 30)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("ast", func(t *testing.T) {
				expr, err := Ast(tc.ty, newMap(t, all, model), modelPath)
				assert.NilError(t, err)
				// go/printer lays out a struct or interface without brace
				// positions over several lines, so only the tokens match
				// the source.
				assert.Equal(t, tokens(t, formatAst(t, expr)), tokens(t, tc.want))
			})
			t.Run("dst", func(t *testing.T) {
				expr, err := Dst(tc.ty, newMap(t, all, model), modelPath)
				assert.NilError(t, err)
				assert.Equal(t, formatDst(t, expr), tc.want)
			})
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
	astExpr, err := Ast(ty, m, otherPath)
	assert.NilError(t, err)
	assert.Equal(t, formatAst(t, astExpr), "model.G[string, *model.Local]")

	dstExpr, err := Dst(ty, m, otherPath)
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

	for name, print := range map[string]func(*importdecl.Map) (string, error){
		"ast": func(m *importdecl.Map) (string, error) {
			expr, err := Ast(ty, m, modelPath)
			if err != nil {
				return "", err
			}
			return formatAst(t, expr), nil
		},
		"dst": func(m *importdecl.Map) (string, error) {
			expr, err := Dst(ty, m, modelPath)
			if err != nil {
				return "", err
			}
			return formatDst(t, expr), nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMap(t, all, model)
			got, err := print(m)
			assert.NilError(t, err)
			assert.Equal(t, got, "bytes.Buffer")
			assert.DeepEqual(t, slices.Collect(m.Missing()), []importdecl.Import{
				{Package: importdecl.Package{Path: "bytes", Name: "bytes"}},
			})
		})
	}
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

			astExpr, err := Ast(ty, newMap(t, all, other), otherPath)
			assert.NilError(t, err)
			assert.Equal(t, tokens(t, formatAst(t, astExpr)), tokens(t, tc.want))

			dstExpr, err := Dst(ty, newMap(t, all, other), otherPath)
			assert.NilError(t, err)
			assert.Equal(t, formatDst(t, dstExpr), tc.want)
		})
	}
}

func TestErrors(t *testing.T) {
	pkgs, all := load(t)
	model, other := pkgs[modelPath], pkgs[otherPath]

	unknownPkg := types.NewPackage("example.com/unknown", "unknown")
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
		{"unknown package", unknown, ErrUnknownPackage},
		{"unknown package in element", types.NewSlice(unknown), ErrUnknownPackage},
		{"unexported", lookup(other, "Unexported"), ErrUnexported},
		{"unexported field", lookup(other, "UnexportedField"), ErrUnexported},
		{"unexported embedded field", lookup(other, "UnexportedEmbedded"), ErrUnexported},
		{"unexported method", lookup(other, "UnexportedMethod"), ErrUnexported},
		{"uninstantiated generic", lookup(model, "G"), ErrUnsupported},
		{"uninstantiated generic alias", lookup(model, "AG"), ErrUnsupported},
		{"uninstantiated generic in element", types.NewSlice(lookup(model, "G")), ErrUnsupported},
		{"untyped", types.Typ[types.UntypedInt], ErrUnsupported},
		{"untyped nil", types.Typ[types.UntypedNil], ErrUnsupported},
		{"invalid", types.Typ[types.Invalid], ErrUnsupported},
		{"tuple", types.NewTuple(), ErrUnsupported},
		{"generic signature", genericSig, ErrUnsupported},
		{"local type", local, ErrUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Ast(tc.ty, newMap(t, all, model), modelPath)
			assert.ErrorIs(t, err, tc.err)
			_, err = Dst(tc.ty, newMap(t, all, model), modelPath)
			assert.ErrorIs(t, err, tc.err)
		})
	}
}
