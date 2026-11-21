package genopt

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
)

const filesSrc = `package a

type S struct {
	F int
	G string
	Embedded
}

type Embedded struct{}

type I int

type D S
`

// namesInput finds a fixed list of names.
type namesInput []string

func (n namesInput) Find(fs.FS) ([]string, error) { return n, nil }

func fileLoader(names ...string) Loader[leaf, leaf, leaf] {
	l := testLoader
	l.File = namesInput(names)

	return l
}

func filesPackage(t *testing.T) []*packages.Package {
	t.Helper()
	return []*packages.Package{typedPackage(t, "m/a", "a", map[string]string{"x.go": filesSrc})}
}

func TestLoadFilesFormats(t *testing.T) {
	fsys := fstest.MapFS{
		"opts.yaml": {Data: []byte(`
version: 1
packages:
  m/a:
    value: pkg
    types:
      S:
        value: s
        list:
          - 1
          - 2
        fields:
          F:
            value: f
          Embedded:
      I:
      D:
        fields:
          F:
`)},
		"opts.json": {Data: []byte(`{
  "version": 1,
  "packages": {
    "m/a": {
      "value": "pkg",
      "types": {
        "S": {
          "value": "s",
          "list": [1, 2],
          "fields": {"F": {"value": "f"}, "Embedded": {}}
        },
        "I": {},
        "D": {"fields": {"F": null}}
      }
    }
  }
}`)},
		"opts.toml": {Data: []byte(`
version = 1

[packages."m/a"]
value = "pkg"

[packages."m/a".types.S]
value = "s"
list = [1, 2]

[packages."m/a".types.S.fields.F]
value = "f"

[packages."m/a".types.S.fields.Embedded]

[packages."m/a".types.I]

[packages."m/a".types.D.fields.F]
`)},
	}

	for _, ext := range []string{"yaml", "json", "toml"} {
		t.Run(ext, func(t *testing.T) {
			name := "opts." + ext
			at := "from " + name + `: packages."m/a"`

			loaded, err := fileLoader(name).LoadFiles(fsys, testRoot, filesPackage(t))
			assert.NilError(t, err)
			assert.Equal(t, len(loaded.Warnings), 0)
			assert.DeepEqual(t, loaded.Files, []string{name})
			assert.DeepEqual(t, flatten(loaded.Option), map[string]string{
				"m/a package":    "value=pkg; " + at,
				"m/a.S":          name + `: packages."m/a".types.S fields=true`,
				"m/a.S type":     "value=s; list=[1 2]; " + at + ".types.S",
				"m/a.S.F":        "value=f; " + at + ".types.S.fields.F",
				"m/a.S.Embedded": at + ".types.S.fields.Embedded",
				"m/a.I":          name + `: packages."m/a".types.I fields=false`,
				"m/a.I type":     at + ".types.I",
				"m/a.D":          name + `: packages."m/a".types.D fields=true`,
				"m/a.D type":     at + ".types.D",
				"m/a.D.F":        at + ".types.D.fields.F",
			})
		})
	}
}

func TestLoadFilesErrors(t *testing.T) {
	const pkg = `opts.yaml: packages."m/a"`

	cases := []struct {
		name string
		doc  string
		// contains are substrings the error must hold.
		contains []string
	}{
		{
			name:     "not a table",
			doc:      "- 1\n",
			contains: []string{"opts.yaml: the document is not a table"},
		},
		{
			name:     "missing version",
			doc:      "packages:\n",
			contains: []string{"opts.yaml: version: version is required; want 1"},
		},
		{
			name:     "unsupported version",
			doc:      "version: 2\n",
			contains: []string{"opts.yaml: version: unsupported version 2; want 1"},
		},
		{
			name:     "version not an integer",
			doc:      "version: \"1\"\n",
			contains: []string{"opts.yaml: version: 1 is not an integer"},
		},
		{
			name:     "fractional version",
			doc:      "version: 1.5\n",
			contains: []string{"opts.yaml: version: 1.5 is not an integer"},
		},
		{
			name:     "unknown top key",
			doc:      "version: 1\nfiles:\n",
			contains: []string{"opts.yaml: files: unknown key; want version, packages"},
		},
		{
			name:     "packages not a table",
			doc:      "version: 1\npackages:\n  - a\n",
			contains: []string{"opts.yaml: packages: [a] is not a table"},
		},
		{
			name:     "package not loaded",
			doc:      "version: 1\npackages:\n  m/none:\n",
			contains: []string{`opts.yaml: packages."m/none": package m/none is not loaded`},
		},
		{
			name:     "unknown package key",
			doc:      "version: 1\npackages:\n  m/a:\n    file: true\n",
			contains: []string{pkg + ".file: unknown key"},
		},
		{
			name: "package value of the wrong type",
			doc:  "version: 1\npackages:\n  m/a:\n    value:\n      - 1\n",
			contains: []string{
				pkg + ".value: cannot decode an array into string",
			},
		},
		{
			name:     "types not a table",
			doc:      "version: 1\npackages:\n  m/a:\n    types: S\n",
			contains: []string{pkg + ".types: S is not a table"},
		},
		{
			name:     "type not a table",
			doc:      "version: 1\npackages:\n  m/a:\n    types:\n      S: 1\n",
			contains: []string{pkg + ".types.S: 1 is not a table"},
		},
		{
			name:     "type not declared",
			doc:      "version: 1\npackages:\n  m/a:\n    types:\n      Z:\n",
			contains: []string{pkg + ".types.Z: type Z is not declared in m/a"},
		},
		{
			name: "unknown type key",
			doc:  "version: 1\npackages:\n  m/a:\n    types:\n      S:\n        merge: append\n",
			contains: []string{
				pkg + ".types.S.merge: unknown key",
			},
		},
		{
			name: "type value of the wrong type",
			doc: "version: 1\npackages:\n  m/a:\n    types:\n      S:\n        list:\n" +
				"          - a\n",
			contains: []string{pkg + ".types.S.list.0: cannot decode a string into int"},
		},
		{
			name: "field not declared",
			doc: "version: 1\npackages:\n  m/a:\n    types:\n      S:\n        fields:\n" +
				"          Z:\n",
			contains: []string{pkg + ".types.S.fields.Z: field Z is not declared in m/a.S"},
		},
		{
			name: "fields of a type that is not a struct type",
			doc: "version: 1\npackages:\n  m/a:\n    types:\n      I:\n        fields:\n" +
				"          Z:\n",
			contains: []string{pkg + ".types.I.fields: m/a.I is not a struct type"},
		},
		{
			name: "unknown field key",
			doc: "version: 1\npackages:\n  m/a:\n    types:\n      S:\n        fields:\n" +
				"          F:\n            name: f\n",
			contains: []string{pkg + ".types.S.fields.F.name: unknown key"},
		},
		{
			name:     "non-string key",
			doc:      "version: 1\npackages:\n  m/a:\n    types:\n      1:\n",
			contains: []string{pkg + ".types: key 1 is not a string"},
		},
		{
			name: "several errors",
			doc: "version: 1\npackages:\n  m/a:\n    types:\n      Y:\n      Z:\n" +
				"  m/none:\n",
			contains: []string{".types.Y: ", ".types.Z: ", `packages."m/none": `},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := fstest.MapFS{"opts.yaml": {Data: []byte(c.doc)}}

			_, err := fileLoader("opts.yaml").LoadFiles(fsys, testRoot, filesPackage(t))
			assert.ErrorIs(t, err, ErrOptionFile)

			for _, s := range c.contains {
				assert.ErrorContains(t, err, s)
			}
		})
	}
}

func TestLoadFilesWithoutTypes(t *testing.T) {
	fsys := fstest.MapFS{"opts.yaml": {Data: []byte("version: 1\npackages:\n  m/a:\n")}}
	pkgs := []*packages.Package{parsedPackage(t, "m/a", "a", map[string]string{"x.go": filesSrc})}

	_, err := fileLoader("opts.yaml").LoadFiles(fsys, testRoot, pkgs)
	assert.ErrorIs(t, err, ErrOptionFile)
	assert.ErrorContains(t, err, "package m/a is loaded without types")
}

func TestLoadFilesInputErrors(t *testing.T) {
	l := testLoader
	l.File = fileloader.PathInput{Base: "opts", Formats: []fileloader.Format{"ini"}}

	_, err := l.LoadFiles(fstest.MapFS{}, testRoot, filesPackage(t))
	assert.ErrorIs(t, err, ErrOptionFile)
	assert.ErrorIs(t, err, fileloader.ErrUnknownFormat)

	_, err = fileLoader("opts.yaml").LoadFiles(fstest.MapFS{
		"opts.yaml": {Data: []byte("version: [\n")},
	}, testRoot, filesPackage(t))
	assert.ErrorIs(t, err, ErrOptionFile)
	assert.ErrorContains(t, err, `decode "opts.yaml"`)
}

func TestLoadFilesNone(t *testing.T) {
	l := testLoader
	l.File = fileloader.PathInput{Base: "opts", Formats: []fileloader.Format{"yaml"}}

	loaded, err := l.LoadFiles(fstest.MapFS{}, testRoot, filesPackage(t))
	assert.NilError(t, err)
	assert.Equal(t, len(loaded.Files), 0)
	assert.Equal(t, len(loaded.Option.Packages), 0)
}

const precedenceSrc = `package a

//kama:x file

//kama:x d
type S struct {
	//kama:x:f d
	F int
}

//kama:x d
type T int

//kama:x d
type U int
`

func TestLoadOptionPrecedence(t *testing.T) {
	fsys := fstest.MapFS{
		"c1.yaml": {Data: []byte(`
version: 1
packages:
  m/a:
    value: c1
    types:
      S:
        value: c1
      T:
        value: c1
`)},
		"c2.yaml": {Data: []byte(`
version: 1
packages:
  m/a:
    types:
      T:
        value: c2
`)},
		"a/kama_x.yaml": {Data: []byte(`
version: 1
value: p
types:
  S:
    value: p
`)},
	}

	l := fileLoader("c2.yaml", "c1.yaml")
	l.PackageFile = fileloader.PathInput{
		Base: "kama_x", Formats: []fileloader.Format{fileloader.FormatYaml},
	}
	pkgs := []*packages.Package{
		typedPackage(t, "m/a", "a", map[string]string{"x.go": precedenceSrc}),
	}

	loaded, err := l.LoadOption(fsys, testRoot, pkgs)
	assert.NilError(t, err)
	assert.DeepEqual(t, loaded.Files, []string{"c1.yaml", "c2.yaml"})
	assert.DeepEqual(t, loaded.PackageFiles, map[string]string{"m/a": "a/kama_x.yaml"})
	assert.DeepEqual(t, flatten(loaded.Option), map[string]string{
		"m/a package":   "value=p; from a/kama_x.yaml",
		"m/a file x.go": "[]file; at x.go:3:1 a free-floating comment",
		"m/a.S":         "a/kama_x.yaml: types.S fields=true",
		"m/a.S type":    "value=p; from a/kama_x.yaml: types.S",
		"m/a.T":         `c2.yaml: packages."m/a".types.T fields=false`,
		"m/a.T type":    `value=c2; from c2.yaml: packages."m/a".types.T`,
		"m/a.U":         "a/x.go:14:1 fields=false",
		"m/a.U type":    "[]d; at x.go:14:1 type m/a.U",
	})

	warnings := make([]string, len(loaded.Warnings))
	for i, w := range loaded.Warnings {
		assert.ErrorIs(t, w, ErrOverridden)
		warnings[i] = w.Error()
	}

	assert.DeepEqual(t, warnings, []string{
		`genopt: options overridden: type m/a.T: c2.yaml: packages."m/a".types.T overrides ` +
			`c1.yaml: packages."m/a".types.T`,
		`genopt: options overridden: package m/a: a/kama_x.yaml overrides ` +
			`c1.yaml: packages."m/a"`,
		`genopt: options overridden: type m/a.S: a/kama_x.yaml: types.S overrides ` +
			`c1.yaml: packages."m/a".types.S`,
		"genopt: options overridden: type m/a.S: a/kama_x.yaml: types.S overrides " +
			testRoot + "/a/x.go:5:1",
		`genopt: options overridden: type m/a.T: c2.yaml: packages."m/a".types.T overrides ` +
			testRoot + "/a/x.go:11:1",
	})
}

// TestLoadOptionPackageEntry checks that a package entry without keys of
// the leaf leaves the package entry of a file that comes later in place.
func TestLoadOptionPackageEntry(t *testing.T) {
	fsys := fstest.MapFS{
		"c.yaml":        {Data: []byte("version: 1\npackages:\n  m/a:\n    value: c\n")},
		"a/kama_x.yaml": {Data: []byte("version: 1\ntypes:\n  S:\n")},
	}

	l := fileLoader("c.yaml")
	l.PackageFile = fileloader.PathInput{
		Base: "kama_x", Formats: []fileloader.Format{fileloader.FormatYaml},
	}

	loaded, err := l.LoadOption(fsys, testRoot, filesPackage(t))
	assert.NilError(t, err)
	assert.Equal(t, len(loaded.Warnings), 0, "%v", loaded.Warnings)
	assert.Equal(
		t,
		flatten(loaded.Option)["m/a package"],
		`value=c; from c.yaml: packages."m/a"`,
	)
}
