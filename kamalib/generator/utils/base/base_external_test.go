package base_test

import (
	"context"
	"errors"
	"go/ast"
	"go/types"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// source returns a generated file of package p with the header of now.
func source(t *testing.T, now time.Time, body string) []byte {
	t.Helper()

	hdr, err := base.Header("example.invalid/gen", map[string]any{"mode": "x"}, now)
	assert.NilError(t, err)

	return []byte(strings.Join(hdr, "\n") + "\n\npackage p\n\n" + body + "\n")
}

func TestStage(t *testing.T) {
	ctx := context.Background()
	f, err := overlayfs.New(t.TempDir())
	assert.NilError(t, err)

	defer f.Close()

	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	files := func(now time.Time, a string) []base.File {
		return []base.File{
			{Path: "a.go", Content: source(t, now, a)},
			{Path: "sub/b.go", Content: source(t, now, "var B = 1")},
		}
	}
	read := func(name string) string {
		b, err := fs.ReadFile(f.FS(), name)
		assert.NilError(t, err)

		return string(b)
	}

	r := &gentest.RecordReporter{}
	assert.NilError(t, base.Stage(ctx, f, files(first, "var A = 1"), r))
	assert.DeepEqual(t, r.Messages, []string{"2 files written, 0 files unchanged"})

	a, b := read("a.go"), read("sub/b.go")

	// A later run changing nothing but the time keeps both files.
	r = &gentest.RecordReporter{}
	assert.NilError(t, base.Stage(ctx, f, files(first.Add(time.Hour), "var A = 1"), r))
	assert.DeepEqual(t, r.Messages, []string{"0 files written, 2 files unchanged"})
	assert.Equal(t, read("a.go"), a)

	r = &gentest.RecordReporter{}
	assert.NilError(t, base.Stage(ctx, f, files(first.Add(time.Hour), "var A = 2"), r))
	assert.DeepEqual(t, r.Messages, []string{"1 file written, 1 file unchanged"})
	assert.Assert(t, strings.Contains(read("a.go"), "var A = 2"))
	assert.Assert(t, strings.Contains(read("a.go"), "generated: 2026-01-01T01:00:00Z"))
	assert.Equal(t, read("sub/b.go"), b)
}

func TestStageWithWarnings(t *testing.T) {
	f, err := overlayfs.New(t.TempDir())
	assert.NilError(t, err)

	defer f.Close()

	errA, errB := errors.New("a"), errors.New("b")
	files := []base.File{{Path: "a.go", Content: source(t, time.Now(), "var A = 1")}}

	r := &gentest.RecordReporter{}
	assert.NilError(
		t,
		base.StageWithWarnings(context.Background(), f, files, []error{errA, errB}, r),
	)
	assert.Assert(t, slices.Equal(r.Warnings, []error{errA, errB}))
	assert.DeepEqual(t, r.Messages, []string{"1 file written, 0 files unchanged"})

	_, err = fs.Stat(f.FS(), "a.go")
	assert.NilError(t, err)
}

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

	err := base.CheckPredeclared(pkgs[0], []base.File{
		{Path: "a.gen.go", Content: []byte("package x\n\nvar V string\n")},
		{Path: "b.gen.go", Content: []byte("package x\n\nfunc f() error { return nil }\n")},
	}, errConflict)
	assert.ErrorIs(t, err, errConflict)
	assert.ErrorContains(t, err, pkgs[0].PkgPath+".error shadows the predeclared error")
	assert.ErrorContains(t, err, pkgs[0].PkgPath+".string shadows the predeclared string")

	assert.NilError(t, base.CheckPredeclared(pkgs[0], []base.File{
		{Path: "a.gen.go", Content: []byte("package x\n\nvar V int\n")},
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

func TestStaleCheck(t *testing.T) {
	const gen = "base-test-gen"

	reg := genmark.NewRegistry(base.ToolName)
	assert.NilError(t, reg.Register(gen))
	assert.NilError(t, reg.Register("other-gen"))

	marked := func(generator, name string) string {
		return "package x\n\n//kama:generated " + generator + "\ntype " + name + " int\n"
	}
	pkgs := base.SourcePackages(gentest.Load(t, map[string]string{
		"a.go":       "package x\n\ntype A int\n",
		"made.go":    marked(gen, "Made"),
		"stale.go":   marked(gen, "Stale"),
		"kept.go":    marked(gen, "Kept"),
		"foreign.go": marked("other-gen", "Foreign"),
	}))
	pkg, made := fileOf(t, pkgs, "made.go")
	produced := []base.File{{Path: pkg.Fset.File(made.Pos()).Name()}}

	check := base.StaleCheck{Registry: reg, Generator: gen}
	warnings := check.Files(pkgs, produced)
	assert.Equal(t, len(warnings), 2, "%v", warnings)
	assert.ErrorIs(t, warnings[0], base.ErrStale)
	assert.ErrorContains(t, warnings[0], "/kept.go was generated by "+gen)
	assert.ErrorContains(t, warnings[1], "/stale.go was generated by "+gen)
	// The path is relative to the module root.
	assert.Assert(
		t,
		strings.Contains(warnings[1].Error(), ": generator/utils/base/testdata/"),
		warnings[1],
	)

	check.Keep = func(_ *packages.Package, p string) bool { return filepath.Base(p) == "kept.go" }
	warnings = check.Files(pkgs, produced)
	assert.Equal(t, len(warnings), 1, "%v", warnings)
	assert.ErrorContains(t, warnings[0], "/stale.go")
}
