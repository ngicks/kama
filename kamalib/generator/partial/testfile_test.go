package partial

import (
	"context"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ngicks/opiter"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// testFiles declares targets in a file of the package x, in a _test.go file
// of x, in the external test package x_test and in the package y importing
// x. The fields of type N call helpers: a.go and a_test.go share one, and
// a_test.go and ext_test.go each call one only they call.
func testFiles(t *testing.T) map[string]string {
	t.Helper()

	x := modulePath(t)

	return map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ N *N }\n\n" +
			"type N struct{ I int }\n\n//kama:partial\ntype M struct{ I int }\n",
		"a_test.go": "package x\n\n//kama:partial\ntype B struct {\n\tN *N\n\tM *M\n}\n",
		"ext_test.go": "package x_test\n\nimport x \"" + x + "\"\n\n" +
			"//kama:partial\ntype C struct {\n\tA *x.A\n\tB x.B\n}\n",
		"y/y.go": "package y\n\nimport x \"" + x + "\"\n\n//kama:partial\ntype D struct{ M x.M }\n",
	}
}

// generateTests generates from the packages opts loads from f with their
// tests when tests is set, and fails on a warning.
func generateTests(t *testing.T, f *overlayfs.Fs, opts gopackages.Options, tests bool) []File {
	t.Helper()

	opts.Tests = tests
	pkgs, err := gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)

	out, warnings, err := Generate(pkgs, Options{Registry: registry(t), Now: gentest.Now})
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)

	return out
}

// relPaths returns the paths of files relative to the inline directory of
// t, sorted.
func relPaths(t *testing.T, f *overlayfs.Fs, files []File) []string {
	t.Helper()

	return opiter.WrapSeq(slices.Values(files)).
		Map(func(file File) string {
			rel, err := filepath.Rel(filepath.Join(f.Root(), gentest.InlineDir(t)), file.Path)
			assert.NilError(t, err)

			return filepath.ToSlash(rel)
		}).
		Collect(slices.Sorted)
}

func TestTestFiles(t *testing.T) {
	f, opts := gentest.Stage(t, testFiles(t))

	out := generateTests(t, f, opts, true)
	assert.DeepEqual(t, relPaths(t, f, out), []string{
		"a.kama_partial.go",
		"a.kama_partial_test.go",
		"ext.kama_partial_test.go",
		"y/y.kama_partial.go",
		"y/zz_gen.flagset.kama_partial.go",
		"y/zz_gen.helper.kama_partial.go",
		"zz_gen.flagset.kama_partial.go",
		"zz_gen.flagset.kama_partial_test.go",
		"zz_gen.helper.a.kama_partial_test.go",
		"zz_gen.helper.kama_partial.go",
		"zz_gen.helper.kama_partial_test.go",
	})

	files := gentest.ByBase(out)
	h := helperIn(t, slices.Collect(maps.Values(files))...)
	fromN := "\nfunc " + h("FromTarget", "converts *N into und.Und[NPartial],") + "("
	fromM := "\nfunc " + h("FromTarget", "converts *M into und.Und[MPartial],") + "("

	// A helper a.go and a_test.go share stays in the build of x.
	assert.Assert(t, strings.Contains(files[HelpersFile], fromN), files[HelpersFile])
	// A helper only a_test.go calls stays out of it.
	own := files["zz_gen.helper.a.kama_partial_test.go"]
	assert.Assert(t, strings.Contains(own, fromM), own)
	assert.Assert(t, !strings.Contains(files[HelpersFile], fromM), files[HelpersFile])
	assert.Assert(t, strings.Contains(files["a.kama_partial_test.go"], "type BPartial struct"))

	for _, name := range []string{
		"ext.kama_partial_test.go",
		"zz_gen.flagset.kama_partial_test.go",
		"zz_gen.helper.kama_partial_test.go",
	} {
		assert.Assert(t, strings.Contains(files[name], "\npackage x_test\n"), name)
	}

	assert.Assert(t, strings.Contains(files["ext.kama_partial_test.go"], ".BPartial] `"))
	// y imports x, not the test variant standing for it, and still finds
	// M a target.
	assert.Assert(t, strings.Contains(files["y.kama_partial.go"], ".MPartial] `"))

	gentest.Write(t, f, out)

	opts.Tests = true
	assert.NilError(t, gentest.Build(f, opts))

	// A second run reads the files of the first, and of the external test
	// package too, as generated.
	again := generateTests(t, f, opts, true)
	assert.DeepEqual(t, gentest.ByBase(again), files)
}

// TestTestFilesWithoutTests checks that a run without the tests produces
// the files of the packages as a run with them does, and finds none of the
// files of the tests stale.
func TestTestFilesWithoutTests(t *testing.T) {
	f, opts := gentest.Stage(t, testFiles(t))

	withTests := generateTests(t, f, opts, true)
	gentest.Write(t, f, withTests)

	without := generateTests(t, f, opts, false)
	assert.DeepEqual(t, relPaths(t, f, without), []string{
		"a.kama_partial.go",
		"y/y.kama_partial.go",
		"y/zz_gen.flagset.kama_partial.go",
		"y/zz_gen.helper.kama_partial.go",
		"zz_gen.flagset.kama_partial.go",
		"zz_gen.helper.kama_partial.go",
	})

	want := opiter.WrapSeq(slices.Values(withTests)).
		Filter(func(file File) bool { return !strings.HasSuffix(file.Path, "_test.go") }).
		CollectSlice()

	assert.DeepEqual(t, without, want)
}

// TestTestFilesOptionFiles checks that the package option file of a
// directory names the types of the package there only, and that a central
// option file names the types of the external test package by its path.
func TestTestFilesOptionFiles(t *testing.T) {
	f, opts := gentest.Stage(t, map[string]string{
		"a.go":        "package x\n\ntype A struct{ I int }\n",
		"a_test.go":   "package x\n\ntype B struct{ I int }\n",
		"ext_test.go": "package x_test\n\ntype C struct{ I int }\n",
	})
	opts.Tests = true
	pkgs, err := gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)

	x := modulePath(t)
	fsys := fstest.MapFS{
		gentest.InlineDir(t) + "/kama_partial.yaml": {
			Data: []byte("version: 1\ntypes:\n  A: {}\n  B: {}\n"),
		},
		"opts.yaml": {
			Data: []byte("version: 1\npackages:\n  " + x + "_test:\n    types:\n      C: {}\n"),
		},
	}
	root, err := filepath.Abs(".")
	assert.NilError(t, err)

	loaded, err := NewLoader(fileloader.PathInput{
		Base:    "opts",
		Formats: []fileloader.Format{fileloader.FormatYaml},
	}).LoadOption(fsys, root, base.SourcePackages(pkgs))
	assert.NilError(t, err)
	assert.DeepEqual(t, loaded.PackageFiles, map[string]string{
		x: gentest.InlineDir(t) + "/kama_partial.yaml",
	})

	out, warnings, err := Generate(
		pkgs,
		Options{Registry: registry(t), Now: gentest.Now, Loaded: loaded},
	)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)

	files := gentest.ByBase(out)
	assert.Assert(t, strings.Contains(files["a.kama_partial.go"], "type APartial struct"))
	assert.Assert(t, strings.Contains(files["a.kama_partial_test.go"], "type BPartial struct"))
	assert.Assert(t, strings.Contains(files["ext.kama_partial_test.go"], "type CPartial struct"))

	// Entries naming types of the tests need the tests loaded.
	opts.Tests = false
	pkgs, err = gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)

	_, err = NewLoader(fileloader.PathInput{
		Base:    "opts",
		Formats: []fileloader.Format{fileloader.FormatYaml},
	}).LoadOption(fsys, root, base.SourcePackages(pkgs))
	assert.ErrorContains(t, err, "type B is not declared in")
	assert.ErrorContains(t, err, x+"_test")
}

// TestTestFilesOnly checks a package whose targets all lie in its test
// files. Its flag set file is the one of the package, and a run without
// the tests keeps it and finds nothing stale.
func TestTestFilesOnly(t *testing.T) {
	f, opts := gentest.Stage(t, map[string]string{
		"a.go":      "package x\n",
		"a_test.go": "package x\n\n//kama:partial\ntype B struct{ I int }\n",
	})

	out := generateTests(t, f, opts, true)
	assert.DeepEqual(t, relPaths(t, f, out), []string{
		"a.kama_partial_test.go",
		"zz_gen.flagset.kama_partial.go",
	})

	gentest.Write(t, f, out)

	assert.Equal(t, len(generateTests(t, f, opts, false)), 0)
}

// TestTestFilesReach checks that a target of a test file reaches the types
// of the files of the package, which get partial variants there. A run
// without the tests does not reach them and finds their file stale.
func TestTestFilesReach(t *testing.T) {
	f, opts := gentest.Stage(t, map[string]string{
		"a.go":      "package x\n\ntype N struct{ I int }\n",
		"a_test.go": "package x\n\n//kama:partial\ntype B struct{ N *N }\n",
	})

	out := generateTests(t, f, opts, true)
	assert.DeepEqual(t, relPaths(t, f, out), []string{
		"a.kama_partial.go",
		"a.kama_partial_test.go",
		"zz_gen.flagset.kama_partial.go",
		"zz_gen.helper.a.kama_partial_test.go",
	})
	assert.Assert(
		t,
		strings.Contains(gentest.ByBase(out)["a.kama_partial.go"], "type NPartial struct"),
	)

	gentest.Write(t, f, out)

	pkgs, err := gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)

	files, warnings, err := Generate(pkgs, Options{Registry: registry(t), Now: gentest.Now})
	assert.NilError(t, err)
	assert.Equal(t, len(files), 0)
	assert.Equal(t, len(warnings), 1)
	assert.ErrorIs(t, warnings[0], ErrStale)
	assert.ErrorContains(t, warnings[0], "a.kama_partial.go")
}
