package gopackages

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ngicks/opiter"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

func TestLoadSeesStagedFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	assert.NilError(
		t,
		os.WriteFile(
			filepath.Join(root, "go.mod"),
			[]byte("module example.invalid/x\n\ngo 1.27\n"),
			0o644,
		),
	)
	assert.NilError(
		t,
		os.WriteFile(
			filepath.Join(root, "a.go"),
			[]byte("package x\n\nfunc A() int { return B() }\n"),
			0o644,
		),
	)

	f, err := overlayfs.New(root)
	assert.NilError(t, err)

	defer f.Close()

	// B exists only in the staged layer; type checking a.go needs it.
	assert.NilError(t, f.Write(ctx, "b.gen.go", []byte("package x\n\nfunc B() int { return 1 }\n")))

	pkgs, err := Load(ctx, f, Options{Patterns: []string{"./..."}})
	assert.NilError(t, err)

	assert.Equal(t, len(pkgs), 1)
	pkg := pkgs[0]

	files := opiter.SlicesValues(pkg.GoFiles).
		Map(filepath.Base).
		Collect(slices.Sorted)

	assert.DeepEqual(t, files, []string{"a.go", "b.gen.go"})
}

func TestTestPackages(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	for name, src := range map[string]string{
		"go.mod":          "module example.invalid/x\n\ngo 1.27\n",
		"x.go":            "package x\n\ntype T struct{}\n",
		"in_test.go":      "package x\n\ntype U struct{}\n",
		"ext_test.go":     "package x_test\n\ntype V struct{}\n",
		"y_test/y.go":     "package y_test\n\ntype W struct{}\n",
		"y_test/y2.go":    "package y_test\n\ntype W2 struct{}\n",
		"z/z.go":          "package z\n",
		"z/z_ext_test.go": "package z_test\n",
	} {
		assert.NilError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
		assert.NilError(t, os.WriteFile(filepath.Join(root, name), []byte(src), 0o644))
	}

	f, err := overlayfs.New(root)
	assert.NilError(t, err)

	defer f.Close()

	pkgs, err := Load(ctx, f, Options{Patterns: []string{"./..."}, Tests: true})
	assert.NilError(t, err)

	kinds := map[string]string{}

	for _, p := range pkgs {
		switch {
		case IsTestVariant(p):
			kinds[p.ID] = "variant"
		case IsExternalTest(p):
			kinds[p.ID] = "external"
		case IsTestMain(p):
			kinds[p.ID] = "main"
		default:
			kinds[p.ID] = "other"
		}
	}

	// A package whose path ends in _test is no external test package.
	assert.DeepEqual(t, kinds, map[string]string{
		"example.invalid/x":                                   "other",
		"example.invalid/x [example.invalid/x.test]":          "variant",
		"example.invalid/x_test [example.invalid/x.test]":     "external",
		"example.invalid/x.test":                              "main",
		"example.invalid/x/y_test":                            "other",
		"example.invalid/x/z":                                 "other",
		"example.invalid/x/z_test [example.invalid/x/z.test]": "external",
		"example.invalid/x/z.test":                            "main",
	})
}

func TestLoadFailsOnPackageErrors(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		opts  Options
		// want maps a package path to one message substring per error.
		want map[string][]string
	}{
		{
			name:  "type error",
			files: map[string]string{"a.go": "package x\n\nfunc A() int { return B() }\n"},
			opts:  Options{Patterns: []string{"./..."}},
			want:  map[string][]string{"example.invalid/x": {"undefined: B"}},
		},
		{
			name: "errors in several packages",
			files: map[string]string{
				"a/a.go": "package a\n\nfunc A() int { return B() }\n",
				"b/b.go": "package b\n\nvar C int = \"c\"\n",
			},
			opts: Options{Patterns: []string{"./..."}},
			want: map[string][]string{
				"example.invalid/x/a": {"undefined: B"},
				"example.invalid/x/b": {"cannot use \"c\""},
			},
		},
		{
			name: "errors in a dependency",
			files: map[string]string{
				"a/a.go": "package a\n\nimport _ \"example.invalid/x/b\"\n",
				"b/b.go": "package b\n\nfunc B() int { return C() }\n",
			},
			opts: Options{Patterns: []string{"./a"}},
			want: map[string][]string{"example.invalid/x/b": {"undefined: C"}},
		},
		{
			name: "test variants report once",
			files: map[string]string{
				"a.go":      "package x\n\nfunc A() int { return B() }\n",
				"a_test.go": "package x\n",
			},
			opts: Options{Patterns: []string{"./..."}, Tests: true},
			want: map[string][]string{"example.invalid/x": {"undefined: B"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()

			tc.files["go.mod"] = "module example.invalid/x\n\ngo 1.27\n"
			for name, src := range tc.files {
				p := filepath.Join(root, filepath.FromSlash(name))
				assert.NilError(t, os.MkdirAll(filepath.Dir(p), 0o755))
				assert.NilError(t, os.WriteFile(p, []byte(src), 0o644))
			}

			f, err := overlayfs.New(root)
			assert.NilError(t, err)

			defer f.Close()

			pkgs, err := Load(ctx, f, tc.opts)
			assert.Assert(t, pkgs == nil)

			var pkgErrs PackageErrors
			assert.Assert(t, errors.As(err, &pkgErrs), "err: %v", err)
			assert.DeepEqual(
				t,
				slices.Sorted(maps.Keys(pkgErrs)),
				slices.Sorted(maps.Keys(tc.want)),
			)

			for path, msgs := range tc.want {
				got := pkgErrs[path]
				assert.Equal(t, len(got), len(msgs), "%s: %v", path, got)

				for i, msg := range msgs {
					assert.Assert(t, strings.Contains(got[i].Msg, msg), "%s: %v", path, got[i])
				}
			}
		})
	}
}

func TestLoadAllowTypeErrors(t *testing.T) {
	cases := []struct {
		name     string
		files    map[string]string
		patterns []string
		// want maps a package path to one message substring per error Errors
		// reports.
		want map[string][]string
	}{
		{
			name:  "type error",
			files: map[string]string{"a.go": "package x\n\nfunc A() int { return B() }\n"},
			want:  map[string][]string{"example.invalid/x": {"undefined: B"}},
		},
		{
			name: "missing import",
			files: map[string]string{
				"a.go": "package x\n\nimport \"example.invalid/x/gone\"\n\nvar _ = gone.F\n",
			},
			want: map[string][]string{
				"example.invalid/x":      {"could not import example.invalid/x/gone"},
				"example.invalid/x/gone": {"no required module provides package"},
			},
		},
		{
			name: "errors in a dependency",
			files: map[string]string{
				"a/a.go": "package a\n\nimport _ \"example.invalid/x/b\"\n",
				"b/b.go": "package b\n\nfunc B() int {\n",
			},
			patterns: []string{"./a"},
			want: map[string][]string{
				"example.invalid/x/b": {"expected ';'", "expected '}'", "missing return"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := stageModule(t, tc.files)

			patterns := tc.patterns
			if patterns == nil {
				patterns = []string{"./..."}
			}

			pkgs, err := Load(t.Context(), f, Options{Patterns: patterns, AllowTypeErrors: true})
			assert.NilError(t, err)
			assert.Assert(t, len(pkgs) > 0)

			for _, p := range pkgs {
				assert.Assert(t, len(p.Syntax) > 0, "%s", p.PkgPath)
			}

			assertErrors(t, Errors(pkgs), tc.want)

			_, err = Load(t.Context(), f, Options{Patterns: patterns})

			var pkgErrs PackageErrors
			assert.Assert(t, errors.As(err, &pkgErrs), "err: %v", err)
		})
	}
}

func TestLoadAllowTypeErrorsFails(t *testing.T) {
	cases := []struct {
		name     string
		files    map[string]string
		patterns []string
		want     map[string][]string
	}{
		{
			name:     "missing pattern",
			patterns: []string{"./nonexistent"},
			want:     map[string][]string{"./nonexistent": {"directory not found"}},
		},
		{
			name: "file that does not parse",
			files: map[string]string{
				"a.go": "package x\n\nfunc A() int { return B() }\n",
				"b.go": "package x\n\nfunc B() int {\n",
			},
			patterns: []string{"./..."},
			want: map[string][]string{
				"example.invalid/x": {"expected ';'", "expected '}'"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := stageModule(t, tc.files)

			pkgs, err := Load(t.Context(), f, Options{Patterns: tc.patterns, AllowTypeErrors: true})
			assert.Assert(t, pkgs == nil)

			var pkgErrs PackageErrors
			assert.Assert(t, errors.As(err, &pkgErrs), "err: %v", err)
			assertErrors(t, pkgErrs, tc.want)
		})
	}
}

func TestLoadFailsOnMissingPattern(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	assert.NilError(
		t,
		os.WriteFile(
			filepath.Join(root, "go.mod"),
			[]byte("module example.invalid/x\n\ngo 1.27\n"),
			0o644,
		),
	)

	f, err := overlayfs.New(root)
	assert.NilError(t, err)

	defer f.Close()

	pkgs, err := Load(ctx, f, Options{Patterns: []string{"./nonexistent"}})
	assert.Assert(t, pkgs == nil)

	var pkgErrs PackageErrors
	assert.Assert(t, errors.As(err, &pkgErrs), "err: %v", err)
	assert.Assert(t, len(pkgErrs) > 0)
}

func TestPackageErrorsError(t *testing.T) {
	err := PackageErrors{
		"example.invalid/x/b": {{Pos: "b.go:1:1", Msg: "bad b"}},
		"example.invalid/x/a": {{Pos: "a.go:1:1", Msg: "bad a1"}, {Msg: "bad a2"}},
	}
	assert.Equal(
		t,
		err.Error(),
		"example.invalid/x/a:\n\ta.go:1:1: bad a1\n\t-: bad a2\n"+
			"example.invalid/x/b:\n\tb.go:1:1: bad b",
	)
}

// stageModule writes files, keyed by slash-separated path, and a go.mod of
// the module example.invalid/x into a temporary directory and opens it.
func stageModule(t *testing.T, files map[string]string) *overlayfs.Fs {
	t.Helper()

	root := t.TempDir()
	files = maps.Clone(files)

	if files == nil {
		files = map[string]string{}
	}

	files["go.mod"] = "module example.invalid/x\n\ngo 1.27\n"
	for name, src := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		assert.NilError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		assert.NilError(t, os.WriteFile(p, []byte(src), 0o644))
	}

	f, err := overlayfs.New(root)
	assert.NilError(t, err)
	t.Cleanup(func() { f.Close() })

	return f
}

// assertErrors checks that got has the paths of want, and for each path one
// error per substring of want, in order.
func assertErrors(t *testing.T, got PackageErrors, want map[string][]string) {
	t.Helper()

	assert.DeepEqual(t, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))

	for path, msgs := range want {
		errs := got[path]
		assert.Equal(t, len(errs), len(msgs), "%s: %v", path, errs)

		for i, msg := range msgs {
			assert.Assert(t, strings.Contains(errs[i].Msg, msg), "%s: %v", path, errs[i])
		}
	}
}
