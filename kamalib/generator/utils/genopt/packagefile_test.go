package genopt

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
)

// testRoot is the root the packages of the tests lie under. No file is read
// from it.
var testRoot = filepath.FromSlash("/proj")

var testInput = PackageFileInput{
	Generator: "x",
	Default: fileloader.PathInput{
		Base:    "kama_x",
		Formats: []fileloader.Format{fileloader.FormatYaml, fileloader.FormatJson},
	},
}

// parsedPackage returns the package pkgPath of the files, keyed by name,
// in dir, a slash-separated path relative to testRoot.
func parsedPackage(t *testing.T, pkgPath, dir string, files map[string]string) *packages.Package {
	t.Helper()

	p := &packages.Package{
		ID:      pkgPath,
		PkgPath: pkgPath,
		Dir:     filepath.Join(testRoot, filepath.FromSlash(dir)),
		Fset:    token.NewFileSet(),
	}

	for name, src := range files {
		f, err := parser.ParseFile(p.Fset, filepath.Join(p.Dir, name), src, parser.ParseComments)
		assert.NilError(t, err)

		p.Syntax = append(p.Syntax, f)
	}

	return p
}

func TestLoadPackageFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"a/kama_x.yaml":    {Data: []byte("k: a\n")},
		"a/opts/b.json":    {Data: []byte(`{"k":"b"}`)},
		"a/b/kama_x.yaml":  {Data: []byte("k: default\n")},
		"a/b/local.yaml":   {Data: []byte("k: local\n")},
		"shared/opts.toml": {Data: []byte("k = \"shared\"\n")},
		"top.yaml":         {Data: []byte("k: top\n")},
	}
	doc := func(k string) any { return map[string]any{"k": k} }

	cases := []struct {
		name string
		pkgs func(t *testing.T) []*packages.Package
		want []PackageFile
	}{
		{
			name: "default found",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{parsedPackage(t, "m/a", "a", map[string]string{
					"x.go": "package a\n",
				})}
			},
			want: []PackageFile{{PkgPath: "m/a", Name: "a/kama_x.yaml", Data: doc("a")}},
		},
		{
			name: "default missing",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{parsedPackage(t, "m/c", "c", map[string]string{
					"x.go": "package c\n",
				})}
			},
		},
		{
			name: "directive over default, with a parent directory and a quoted value",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{parsedPackage(t, "m/a/b", "a/b", map[string]string{
					"x.go": "// Package b.\n//\n//kama:option-file x=\"../opts/b.json\"\npackage b\n",
				})}
			},
			want: []PackageFile{{PkgPath: "m/a/b", Name: "a/opts/b.json", Data: doc("b")}},
		},
		{
			name: "directive in a free-floating comment of another file",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{parsedPackage(t, "m/a/b", "a/b", map[string]string{
					"x.go": "package b\n",
					"y.go": "package b\n\n//kama:option-file x=local.yaml\n\ntype T int\n",
				})}
			},
			want: []PackageFile{{PkgPath: "m/a/b", Name: "a/b/local.yaml", Data: doc("local")}},
		},
		{
			name: "other generators' keys ignored",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{parsedPackage(t, "m/a/b", "a/b", map[string]string{
					"x.go": "//kama:option-file y=../../../out.ini \"x\"=local.yaml z=\n\npackage b\n",
				})}
			},
			want: []PackageFile{{PkgPath: "m/a/b", Name: "a/b/local.yaml", Data: doc("local")}},
		},
		{
			name: "package at the root",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{parsedPackage(t, "m", ".", map[string]string{
					"x.go": "//kama:option-file x=top.yaml\n\npackage m\n",
				})}
			},
			want: []PackageFile{{PkgPath: "m", Name: "top.yaml", Data: doc("top")}},
		},
		{
			name: "two packages naming one file, sorted by path",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{
					parsedPackage(t, "m/q", "q", map[string]string{
						"x.go": "//kama:option-file x=../shared/opts.toml\n\npackage q\n",
					}),
					parsedPackage(t, "m/p", "p", map[string]string{
						"x.go": "//kama:option-file x=../shared/opts.toml\n\npackage p\n",
					}),
				}
			},
			want: []PackageFile{
				{PkgPath: "m/p", Name: "shared/opts.toml", Data: doc("shared")},
				{PkgPath: "m/q", Name: "shared/opts.toml", Data: doc("shared")},
			},
		},
		{
			name: "test variant counted once",
			pkgs: func(t *testing.T) []*packages.Package {
				variant := parsedPackage(t, "m/a", "a", map[string]string{
					"x.go":      "package a\n",
					"x_test.go": "//kama:option-file x=../top.yaml\n\npackage a\n",
				})
				variant.ID = "m/a [m/a.test]"

				return []*packages.Package{
					variant,
					parsedPackage(t, "m/a", "a", map[string]string{"x.go": "package a\n"}),
				}
			},
			want: []PackageFile{{PkgPath: "m/a", Name: "a/kama_x.yaml", Data: doc("a")}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := LoadPackageFiles(fsys, testRoot, c.pkgs(t), "kama", testInput)
			assert.NilError(t, err)
			assert.DeepEqual(t, got, c.want)
		})
	}
}

func TestLoadPackageFilesErrors(t *testing.T) {
	fsys := fstest.MapFS{
		"a/opts.ini":  {Data: []byte("k = 1\n")},
		"a/bad.yaml":  {Data: []byte("k: [\n")},
		"a/opts.yaml": {Data: []byte("k: 1\n")},
	}

	cases := []struct {
		name  string
		files map[string]string
		is    error
		// contains are substrings the error must hold.
		contains []string
	}{
		{
			name: "path leaving the root",
			files: map[string]string{
				"x.go": "//kama:option-file x=../../out.yaml\n\npackage a\n",
			},
			contains: []string{"x.go:1:1: ", `path "../../out.yaml" leaves the root`},
		},
		{
			name: "absolute path",
			files: map[string]string{
				"x.go": "//kama:option-file x=/etc/opts.yaml\n\npackage a\n",
			},
			contains: []string{"x.go:1:1: ", `absolute path "/etc/opts.yaml"`},
		},
		{
			name:     "unknown extension",
			files:    map[string]string{"x.go": "//kama:option-file x=opts.ini\n\npackage a\n"},
			is:       fileloader.ErrUnknownFormat,
			contains: []string{"x.go:1:1: ", `"a/opts.ini"`},
		},
		{
			name:     "missing named file",
			files:    map[string]string{"x.go": "//kama:option-file x=none.yaml\n\npackage a\n"},
			is:       fs.ErrNotExist,
			contains: []string{"x.go:1:1: ", `"a/none.yaml"`},
		},
		{
			name:     "file failing to decode",
			files:    map[string]string{"x.go": "//kama:option-file x=bad.yaml\n\npackage a\n"},
			contains: []string{"x.go:1:1: ", `decode "a/bad.yaml"`},
		},
		{
			name: "generator twice in one comment",
			files: map[string]string{
				"x.go": "//kama:option-file x=opts.yaml x=opts.yaml\n\npackage a\n",
			},
			contains: []string{"second file for x in package m/a; the first is at "},
		},
		{
			name: "generator in two files",
			files: map[string]string{
				"x.go": "//kama:option-file x=opts.yaml\n\npackage a\n",
				"y.go": "//kama:option-file x=opts.yaml\n\npackage a\n",
			},
			contains: []string{"y.go:1:1: ", "second file for x", "the first is at ", "x.go:1:1"},
		},
		{
			name:     "key without value",
			files:    map[string]string{"x.go": "//kama:option-file x=opts.yaml y\n\npackage a\n"},
			contains: []string{"x.go:1:1: ", `"y" has no path; want <generator>=<path>`},
		},
		{
			name:     "empty path",
			files:    map[string]string{"x.go": "//kama:option-file x=\n\npackage a\n"},
			contains: []string{"x.go:1:1: ", "empty path for x"},
		},
		{
			name:     "unparsable args",
			files:    map[string]string{"x.go": "//kama:option-file x=\"opts.yaml\n\npackage a\n"},
			contains: []string{"x.go:1:1: ", "unterminated quoted string"},
		},
		{
			name: "directive on a type",
			files: map[string]string{
				"x.go": "package a\n\n//kama:option-file x=opts.yaml\ntype T int\n",
			},
			contains: []string{"x.go:3:1: ", "applies to free-floating comments only"},
		},
		{
			name: "directive on a field",
			files: map[string]string{
				"x.go": "package a\n\ntype T struct {\n\t//kama:option-file x=opts.yaml\n\tF int\n}\n",
			},
			contains: []string{"x.go:4:2: ", "applies to free-floating comments only"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pkgs := []*packages.Package{parsedPackage(t, "m/a", "a", c.files)}

			_, err := LoadPackageFiles(fsys, testRoot, pkgs, "kama", testInput)
			assert.ErrorIs(t, err, ErrPackageFile)

			if c.is != nil {
				assert.ErrorIs(t, err, c.is)
			}

			for _, s := range c.contains {
				assert.ErrorContains(t, err, s)
			}
		})
	}
}

func TestLoadPackageFilesOutsideRoot(t *testing.T) {
	outside := parsedPackage(t, "m/out", "../out", map[string]string{"x.go": "package out\n"})

	got, err := LoadPackageFiles(fstest.MapFS{}, testRoot, []*packages.Package{outside}, "kama",
		testInput)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0)

	outside = parsedPackage(t, "m/out", "../out", map[string]string{
		"x.go": "//kama:option-file x=opts.yaml\n\npackage out\n",
	})

	_, err = LoadPackageFiles(fstest.MapFS{}, testRoot, []*packages.Package{outside}, "kama",
		testInput)
	assert.ErrorIs(t, err, ErrPackageFile)
	assert.ErrorContains(t, err, "lies outside the root")
}

func TestLoadPackageFilesInput(t *testing.T) {
	pkgs := []*packages.Package{parsedPackage(t, "m/a", "a", map[string]string{
		"x.go": "package a\n",
	})}

	_, err := LoadPackageFiles(fstest.MapFS{}, testRoot, pkgs, "kama", PackageFileInput{
		Generator: "x", Default: fileloader.PathInput{Base: "kama_x"},
	})
	assert.ErrorIs(t, err, ErrPackageFile)

	_, err = LoadPackageFiles(fstest.MapFS{}, testRoot, pkgs, "kama", PackageFileInput{
		Generator: "x",
		Default:   fileloader.PathInput{Base: "kama_x", Formats: []fileloader.Format{"ini"}},
	})
	assert.ErrorIs(t, err, fileloader.ErrUnknownFormat)

	got, err := LoadPackageFiles(fstest.MapFS{
		"a/kama_x.yaml": {Data: []byte("k: 1\n")},
	}, testRoot, pkgs, "kama", PackageFileInput{Generator: "x"})
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0)
}

// TestLoadPackageFilesLoaded checks that the directory of a package is
// known under the load mode Collect requires.
func TestLoadPackageFilesLoaded(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedCompiledGoFiles | packages.NeedSyntax,
	}, "./internal/fixture")
	assert.NilError(t, err)

	root, err := filepath.Abs("internal")
	assert.NilError(t, err)

	got, err := LoadPackageFiles(fstest.MapFS{
		"fixture/kama_x.json": {Data: []byte(`{"k":1}`)},
	}, root, pkgs, "kama", testInput)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, []PackageFile{{
		PkgPath: fixturePath, Name: "fixture/kama_x.json", Data: map[string]any{"k": 1.0},
	}})
}
