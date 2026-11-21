package genopt

import (
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
)

func packageFileLoader() Loader[leaf, leaf, leaf] {
	l := testLoader
	l.PackageFile = fileloader.PathInput{
		Base:    "kama_x",
		Formats: []fileloader.Format{fileloader.FormatYaml, fileloader.FormatJson},
	}

	return l
}

func packageDoc(value string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("version: 1\nvalue: " + value + "\n")}
}

// packageValues returns the value of the package entry of each package of
// loaded and the file it comes from.
func packageValues(loaded *testLoaded) map[string][2]string {
	out := map[string][2]string{}
	for pkgPath, name := range loaded.PackageFiles {
		out[pkgPath] = [2]string{name, loaded.Option.Packages[pkgPath].Package.Value}
	}

	return out
}

func TestLoadPackageFile(t *testing.T) {
	fsys := fstest.MapFS{
		"a/kama_x.yaml":    packageDoc("a"),
		"a/opts/b.json":    {Data: []byte(`{"version":1,"value":"b"}`)},
		"a/b/kama_x.yaml":  packageDoc("default"),
		"a/b/local.yaml":   packageDoc("local"),
		"a/b/other.yaml":   packageDoc("other"),
		"shared/opts.toml": {Data: []byte("version = 1\nvalue = \"shared\"\n")},
		"top.yaml":         packageDoc("top"),
	}

	cases := []struct {
		name     string
		pkgs     func(t *testing.T) []*packages.Package
		want     map[string][2]string
		warnings []string
	}{
		{
			name: "default found",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{typedPackage(t, "m/a", "a", map[string]string{
					"x.go": "package a\n",
				})}
			},
			want: map[string][2]string{"m/a": {"a/kama_x.yaml", "a"}},
		},
		{
			name: "default missing",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{typedPackage(t, "m/c", "c", map[string]string{
					"x.go": "package c\n",
				})}
			},
			want: map[string][2]string{},
		},
		{
			name: "directive over default, with a parent directory and a quoted value",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{typedPackage(t, "m/a/b", "a/b", map[string]string{
					"x.go": "// Package b.\n//\n//kama:option-file x=\"../opts/b.json\"\npackage b\n",
				})}
			},
			want: map[string][2]string{"m/a/b": {"a/opts/b.json", "b"}},
		},
		{
			name: "directive in a free-floating comment of another file",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{typedPackage(t, "m/a/b", "a/b", map[string]string{
					"x.go": "package b\n",
					"y.go": "package b\n\n//kama:option-file x=local.yaml\n\ntype T int\n",
				})}
			},
			want: map[string][2]string{"m/a/b": {"a/b/local.yaml", "local"}},
		},
		{
			name: "other generators' keys ignored",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{typedPackage(t, "m/a/b", "a/b", map[string]string{
					"x.go": "//kama:option-file y=../../../out.ini \"x\"=local.yaml z=\n\npackage b\n",
				})}
			},
			want: map[string][2]string{"m/a/b": {"a/b/local.yaml", "local"}},
		},
		{
			name: "directive on two lines",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{typedPackage(t, "m/a/b", "a/b", map[string]string{
					"x.go": "//kama:option-file y=a.yaml\n//kama:option-file x=local.yaml\n\npackage b\n",
				})}
			},
			want: map[string][2]string{"m/a/b": {"a/b/local.yaml", "local"}},
		},
		{
			name: "generator named twice, the last one used",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{typedPackage(t, "m/a/b", "a/b", map[string]string{
					"x.go": "//kama:option-file x=local.yaml\n//kama:option-file x=other.yaml\n\npackage b\n",
				})}
			},
			want: map[string][2]string{"m/a/b": {"a/b/other.yaml", "other"}},
			warnings: []string{
				"genopt: options overridden: package m/a/b: //kama:option-file " +
					"x=other.yaml (" + testRoot + "/a/b/x.go:1:1) overrides " +
					"x=local.yaml (" + testRoot + "/a/b/x.go:1:1)",
			},
		},
		{
			name: "generator named in two files, the last one used",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{typedPackage(t, "m/a/b", "a/b", map[string]string{
					"y.go": "//kama:option-file x=local.yaml\n\npackage b\n",
					"x.go": "//kama:option-file x=other.yaml\n\npackage b\n",
				})}
			},
			want: map[string][2]string{"m/a/b": {"a/b/local.yaml", "local"}},
			warnings: []string{
				"genopt: options overridden: package m/a/b: //kama:option-file " +
					"x=local.yaml (" + testRoot + "/a/b/y.go:1:1) overrides " +
					"x=other.yaml (" + testRoot + "/a/b/x.go:1:1)",
			},
		},
		{
			name: "package at the root",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{typedPackage(t, "m", ".", map[string]string{
					"x.go": "//kama:option-file x=top.yaml\n\npackage m\n",
				})}
			},
			want: map[string][2]string{"m": {"top.yaml", "top"}},
		},
		{
			name: "two packages naming one file",
			pkgs: func(t *testing.T) []*packages.Package {
				return []*packages.Package{
					typedPackage(t, "m/q", "q", map[string]string{
						"x.go": "//kama:option-file x=../shared/opts.toml\n\npackage q\n",
					}),
					typedPackage(t, "m/p", "p", map[string]string{
						"x.go": "//kama:option-file x=../shared/opts.toml\n\npackage p\n",
					}),
				}
			},
			want: map[string][2]string{
				"m/p": {"shared/opts.toml", "shared"},
				"m/q": {"shared/opts.toml", "shared"},
			},
		},
		{
			name: "test variant counted once",
			pkgs: func(t *testing.T) []*packages.Package {
				variant := typedPackage(t, "m/a", "a", map[string]string{
					"x.go":      "package a\n",
					"x_test.go": "//kama:option-file x=../top.yaml\n\npackage a\n",
				})
				variant.ID = "m/a [m/a.test]"

				return []*packages.Package{
					variant,
					typedPackage(t, "m/a", "a", map[string]string{"x.go": "package a\n"}),
				}
			},
			want: map[string][2]string{"m/a": {"a/kama_x.yaml", "a"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			loaded, err := packageFileLoader().LoadFiles(fsys, testRoot, c.pkgs(t))
			assert.NilError(t, err)
			assert.DeepEqual(t, packageValues(loaded), c.want)

			warnings := make([]string, len(loaded.Warnings))
			for i, w := range loaded.Warnings {
				assert.ErrorIs(t, w, ErrOverridden)
				warnings[i] = w.Error()
			}

			assert.DeepEqual(t, warnings, append([]string{}, c.warnings...))
		})
	}
}

// globInput finds every name of a fixed list present in fsys.
type globInput []string

func (g globInput) Find(fsys fs.FS) ([]string, error) {
	var out []string

	for _, name := range g {
		if _, err := fs.Stat(fsys, name); err == nil {
			out = append(out, name)
		}
	}

	return out, nil
}

func TestLoadPackageFileSeveralDefaults(t *testing.T) {
	fsys := fstest.MapFS{
		"a/kama_x.yaml": packageDoc("yaml"),
		"a/kama_x.json": {Data: []byte(`{"version":1,"value":"json"}`)},
	}

	l := testLoader
	l.PackageFile = globInput{"kama_x.yaml", "kama_x.json", "kama_x.toml"}

	loaded, err := l.LoadFiles(fsys, testRoot, []*packages.Package{
		typedPackage(t, "m/a", "a", map[string]string{"x.go": "package a\n"}),
	})
	assert.NilError(t, err)
	assert.DeepEqual(t, packageValues(loaded), map[string][2]string{
		"m/a": {"a/kama_x.yaml", "yaml"},
	})
	assert.Equal(t, len(loaded.Warnings), 1)
	assert.ErrorIs(t, loaded.Warnings[0], ErrOverridden)
	assert.ErrorContains(
		t,
		loaded.Warnings[0],
		"package m/a: a/kama_x.yaml overrides a/kama_x.json",
	)
}

// TestLoadPackageFileSeveralDefaultsDecode checks that a file the last one
// overrides is read too.
func TestLoadPackageFileSeveralDefaultsDecode(t *testing.T) {
	l := testLoader
	l.PackageFile = globInput{"kama_x.json", "kama_x.yaml"}

	_, err := l.LoadFiles(fstest.MapFS{
		"a/kama_x.json": {Data: []byte("{")},
		"a/kama_x.yaml": packageDoc("yaml"),
	}, testRoot, []*packages.Package{
		typedPackage(t, "m/a", "a", map[string]string{"x.go": "package a\n"}),
	})
	assert.ErrorIs(t, err, ErrPackageFile)
	assert.ErrorContains(t, err, `decode "a/kama_x.json"`)
}

func TestLoadPackageFileErrors(t *testing.T) {
	fsys := fstest.MapFS{
		"a/opts.ini":  {Data: []byte("k = 1\n")},
		"a/bad.yaml":  {Data: []byte("k: [\n")},
		"a/opts.yaml": packageDoc("a"),
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
			pkgs := []*packages.Package{typedPackage(t, "m/a", "a", c.files)}

			// The filter does not apply to the option-file directive.
			l := packageFileLoader()
			l.Filter = func(Target) bool { return false }

			_, err := l.LoadFiles(fsys, testRoot, pkgs)
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

func TestLoadPackageFileOutsideRoot(t *testing.T) {
	outside := typedPackage(t, "m/out", "../out", map[string]string{"x.go": "package out\n"})

	loaded, err := packageFileLoader().LoadFiles(
		fstest.MapFS{}, testRoot, []*packages.Package{outside},
	)
	assert.NilError(t, err)
	assert.Equal(t, len(loaded.PackageFiles), 0)

	outside = typedPackage(t, "m/out", "../out", map[string]string{
		"x.go": "//kama:option-file x=opts.yaml\n\npackage out\n",
	})

	_, err = packageFileLoader().LoadFiles(fstest.MapFS{}, testRoot, []*packages.Package{outside})
	assert.ErrorIs(t, err, ErrPackageFile)
	assert.ErrorContains(t, err, "lies outside the root")
}

func TestLoadPackageFileInput(t *testing.T) {
	pkgs := []*packages.Package{typedPackage(t, "m/a", "a", map[string]string{
		"x.go": "package a\n",
	})}

	l := testLoader
	l.PackageFile = fileloader.PathInput{Base: "kama_x", Formats: []fileloader.Format{"ini"}}

	_, err := l.LoadFiles(fstest.MapFS{}, testRoot, pkgs)
	assert.ErrorIs(t, err, ErrPackageFile)
	assert.ErrorIs(t, err, fileloader.ErrUnknownFormat)

	loaded, err := testLoader.LoadFiles(fstest.MapFS{
		"a/kama_x.yaml": packageDoc("a"),
	}, testRoot, pkgs)
	assert.NilError(t, err)
	assert.Equal(t, len(loaded.PackageFiles), 0)
}

// TestLoadPackageFileLoaded checks that the directory of a package is known
// under the load mode the loader requires.
func TestLoadPackageFileLoaded(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedCompiledGoFiles | packages.NeedSyntax |
			packages.NeedTypes,
	}, "./internal/fixture")
	assert.NilError(t, err)

	root, err := filepath.Abs("internal")
	assert.NilError(t, err)

	// The directives of the fixture are those of the generator partial.
	l := packageFileLoader()
	l.Generator = "none"

	loaded, err := l.LoadFiles(fstest.MapFS{
		"fixture/kama_x.json": {Data: []byte(`{"version":1,"value":"j"}`)},
	}, root, pkgs)
	assert.NilError(t, err)
	assert.DeepEqual(t, packageValues(loaded), map[string][2]string{
		fixturePath: {"fixture/kama_x.json", "j"},
	})
}
