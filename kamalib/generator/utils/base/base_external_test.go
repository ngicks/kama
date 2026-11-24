package base_test

import (
	"context"
	"errors"
	"go/ast"
	"go/types"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
)

func fileOf(t *testing.T, pkgs []*packages.Package, name string) (*packages.Package, *ast.File) {
	t.Helper()

	for _, pkg := range pkgs {
		for _, f := range pkg.Syntax {
			if filepath.Base(pkg.Fset.File(f.Pos()).Name()) == name {
				return pkg, f
			}
		}
	}

	t.Fatalf("no file %s", name)

	return nil, nil
}

func TestSourcePackages(t *testing.T) {
	x := "github.com/ngicks/kama/kamalib/generator/utils/base/" + gentest.InlineDir(t)
	f, opts := gentest.Stage(t, map[string]string{
		"a.go":        "package x\n\ntype A int\n",
		"a_test.go":   "package x\n\ntype B int\n",
		"ext_test.go": "package x_test\n\nimport x \"" + x + "\"\n\ntype C x.A\n",
		"y/y.go":      "package y\n\nimport x \"" + x + "\"\n\ntype D x.A\n",
	})
	opts.Tests = true
	pkgs, err := gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)

	src := base.SourcePackages(pkgs)

	ids := opiter.WrapSeq(slices.Values(src)).
		Map(func(p *packages.Package) string { return p.ID }).
		Collect(slices.Sorted)

	assert.DeepEqual(t, ids, []string{
		x + " [" + x + ".test]",
		x + "/y",
		x + "_test [" + x + ".test]",
	})

	// y imports x, not the test variant standing for it.
	y := src[slices.IndexFunc(src, func(p *packages.Package) bool { return p.PkgPath == x+"/y" })]
	objA := y.Imports[x].Types.Scope().Lookup("A")
	pkg := base.DeclaringPackage(src, objA)
	assert.Equal(t, pkg.ID, x+" ["+x+".test]")
	assert.Assert(t, pkg.Types.Scope().Lookup("A") != objA)
	assert.Equal(t, pkg.Types.Scope().Lookup("A").Pos(), objA.Pos())
	assert.Assert(t, base.DeclaringPackage(src, types.Universe.Lookup("int")) == nil)
}

func TestInputScope(t *testing.T) {
	f, pkgs := gentest.LoadFs(t, map[string]string{
		"a.go":   "package x\n\ntype A int\n",
		"c.go":   "//go:build kama_base_unset\n\npackage x\n\ntype C int\n",
		"y/y.go": "package y\n",
	})
	in := base.Input[struct{}, struct{}, struct{}, struct{}]{
		Packages: base.SourcePackages(pkgs),
		Root:     f.Root(),
	}

	dir := gentest.InlineDir(t)
	files := slices.Sorted(slices.Values(in.Scope().Files))

	// A file of a build variant the load excludes is out of the scope.
	assert.DeepEqual(t, files, []string{dir + "/a.go", dir + "/y/y.go"})
}

func TestPackageFile(t *testing.T) {
	f, opts := gentest.Stage(t, map[string]string{
		"a.go":        "package x\n",
		"a_test.go":   "package x\n",
		"ext_test.go": "package x_test\n",
	})
	opts.Tests = true
	pkgs, err := gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)

	names := map[string]string{}
	for _, p := range base.SourcePackages(pkgs) {
		names[p.Name] = base.PackageFile(p, "zz_gen.helper.kama_x.go")
	}

	assert.DeepEqual(t, names, map[string]string{
		"x":      "zz_gen.helper.kama_x.go",
		"x_test": "zz_gen.helper.kama_x_test.go",
	})
}

func TestBuildConstraint(t *testing.T) {
	pkgs := gentest.Load(t, map[string]string{
		"a.go": "// Package x.\npackage x\n\ntype A int\n",
		"b.go": "// Copyright.\n\n//go:build tagged || !tagged\n\n// Package x.\npackage x\n",
	})
	_, a := fileOf(t, pkgs, "a.go")
	_, ok := base.BuildConstraint(a)
	assert.Assert(t, !ok)
	_, b := fileOf(t, pkgs, "b.go")
	line, ok := base.BuildConstraint(b)
	assert.Assert(t, ok)
	assert.Equal(t, line, "//go:build tagged || !tagged")
}

func TestShadowedPredeclared(t *testing.T) {
	pkgs := base.SourcePackages(gentest.Load(t, map[string]string{
		"a.go": "package x\n\ntype string int\n\ntype error int\n\ntype other int\n",
	}))
	names := func(contents ...string) []string {
		t.Helper()

		in := opiter.WrapSeq(slices.Values(contents)).
			Map(func(s string) []byte { return []byte(s) }).
			CollectSlice()

		objs, err := base.ShadowedPredeclared(pkgs[0], in)
		assert.NilError(t, err)

		return opiter.WrapSeq(slices.Values(objs)).
			Map(func(o types.Object) string { return o.Name() }).
			CollectSlice()
	}
	assert.DeepEqual(t, names("package x\n\nvar V string\n\nfunc f() error { return nil }\n"),
		[]string{"error", "string"})
	// Selected names, fields and declared names are not uses.
	assert.Equal(t, len(names(
		"package x\n\nimport \"y\"\n\ntype T struct{ string int }\n\nvar V = y.error\n",
	)), 0)

	_, err := base.ShadowedPredeclared(pkgs[0], [][]byte{[]byte("not go")})
	assert.ErrorContains(t, err, "parse generated file")
}

func TestCheckPredeclared(t *testing.T) {
	pkgs := base.SourcePackages(gentest.Load(t, map[string]string{
		"a.go": "package x\n\ntype string int\n\ntype error int\n",
	}))
	errConflict := errors.New("conflict")

	err := base.CheckPredeclared(pkgs[0], []base.Composed{
		{Package: "x", Parts: []base.Part{base.Decl("var V string\n")}},
		{Package: "x", Parts: []base.Part{base.Text("func f() error { return nil }")}},
	}, errConflict)
	assert.ErrorIs(t, err, errConflict)
	assert.ErrorContains(t, err, pkgs[0].PkgPath+".error shadows the predeclared error")
	assert.ErrorContains(t, err, pkgs[0].PkgPath+".string shadows the predeclared string")

	assert.NilError(t, base.CheckPredeclared(pkgs[0], []base.Composed{
		{Package: "x", Parts: []base.Part{base.Decl("var V int")}},
	}, errConflict))
}

func TestMethodTypeParams(t *testing.T) {
	pkgs := base.SourcePackages(gentest.Load(t, map[string]string{
		"a.go": "package x\n\ntype T0 int\n\ntype G[_ any, _ any, K comparable] int\n",
	}))
	scope := pkgs[0].Types.Scope()
	tparams := scope.Lookup("G").Type().(*types.Named).TypeParams()

	assert.DeepEqual(t, base.MethodTypeParams(scope, tparams, nil), []string{"T0_", "T1", "K"})
	assert.DeepEqual(
		t,
		base.MethodTypeParams(scope, tparams, []string{"T1", "T1_"}),
		[]string{"T0_", "T1__", "K"},
	)
}
