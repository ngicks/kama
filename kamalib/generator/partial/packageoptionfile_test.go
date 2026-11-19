package partial

import (
	"context"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/ngicks/opiter"

	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	procgopackages "github.com/ngicks/kama/kamalib/proc/gopackages"
)

func TestDecodePackageOptionFile(t *testing.T) {
	var data any
	assert.NilError(t, fileloader.Decode(strings.NewReader(`
version: 1
all_types: true
types:
  Server:
    fieldmask: true
    fields:
      Addr:
        merge: append
        flag:
          name: addr
          short: a
        fieldmask:
          name: address
  Client:
    ignore: true
`), fileloader.FormatYaml, &data))

	got, err := DecodePackageOptionFile("pb/kama_partial.yaml", "example.com/gen/pb", data)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, &OptionFile{
		Name:    "pb/kama_partial.yaml",
		Package: "example.com/gen/pb",
		Packages: map[string]PackageOptions{
			"example.com/gen/pb": {
				AllTypes: true,
				Types: map[string]TypeOptions{
					"Server": {FieldMask: true, Fields: map[string]FieldOptions{
						"Addr": {
							Merge:     "append",
							Flag:      &FlagOptions{Name: "addr", Short: "a"},
							FieldMask: &FieldMaskOptions{Name: "address"},
						},
					}},
					"Client": {Ignore: true, Fields: map[string]FieldOptions{}},
				},
			},
		},
	})
}

func TestDecodePackageOptionFileErrors(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		// contains are substrings the error must hold besides the file name.
		contains []string
	}{
		{name: "not a table", doc: `[1]`, contains: []string{"the document is not a table"}},
		{
			name:     "missing version",
			doc:      `all_types: true`,
			contains: []string{"version is required"},
		},
		{
			name:     "unsupported version",
			doc:      `version: 2`,
			contains: []string{"opts.yaml: version: unsupported version 2"},
		},
		{
			name:     "packages level",
			doc:      "version: 1\npackages: {}\n",
			contains: []string{"opts.yaml: packages: unknown key; want version, all_types, types"},
		},
		{
			name:     "all_types not a boolean",
			doc:      "version: 1\nall_types: 1\n",
			contains: []string{"opts.yaml: all_types: 1 is not a boolean"},
		},
		{
			name: "unknown field key",
			doc: "version: 1\ntypes:\n  S:\n    fields:\n      F:\n" +
				"        flag: {name: f, prefix: x}\n",
			contains: []string{
				"opts.yaml: types.S.fields.F.flag.prefix: unknown key; want name, short, usage",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var data any
			assert.NilError(
				t,
				fileloader.Decode(strings.NewReader(c.doc), fileloader.FormatYaml, &data),
			)
			_, err := DecodePackageOptionFile("opts.yaml", "example.com/gen/pb", data)
			assert.ErrorIs(t, err, ErrOptionFile)
			assert.Assert(t, !strings.Contains(err.Error(), "example.com/gen/pb"), err)

			for _, s := range c.contains {
				assert.ErrorContains(t, err, s)
			}
		})
	}
}

// packageOptionFile decodes doc, a YAML package option file of the inline
// test directory of t, named kama_partial.yaml.
func packageOptionFile(t *testing.T, doc string) *OptionFile {
	t.Helper()

	var data any
	assert.NilError(t, fileloader.Decode(strings.NewReader(doc), fileloader.FormatYaml, &data))
	f, err := DecodePackageOptionFile("kama_partial.yaml", modulePath(t), data)
	assert.NilError(t, err)

	return f
}

func TestPackageOptionFileParity(t *testing.T) {
	const models = "\ntype A struct {\n\tB []int\n\tC string\n\tD D\n}\n\n" +
		"type D struct{ N int }\n\ntype E struct{ M int }\n"

	byDirective := generated(t, map[string]string{
		"x.go": "package x\n\n//kama:partial package\n\n//kama:partial\ntype A struct {\n" +
			"\t//kama:partial:merge append\n\tB []int\n" +
			"\t//kama:partial:flag name=c short=c usage=\"the c\"\n\tC string\n\tD D\n}\n\n" +
			"//kama:partial:fieldmask\ntype D struct {\n" +
			"\t//kama:partial:fieldmask name=n\n\tN int\n}\n\n" +
			"//kama:partial ignore\ntype E struct{ M int }\n",
	})

	byFile := generatedWith(t, map[string]string{"x.go": "package x\n" + models}, Options{
		PackageOptionFiles: []*OptionFile{packageOptionFile(t, `
version: 1
all_types: true
types:
  A:
    fields:
      B:
        merge: append
      C:
        flag:
          name: c
          short: c
          usage: the c
  D:
    fieldmask: true
    fields:
      N:
        fieldmask:
          name: n
  E:
    ignore: true
`)},
	})

	for name, src := range byFile {
		assert.Assert(t, strings.Contains(src, "//\tpackage_option_file: \"kama_partial.yaml\"\n"))
		byFile[name] = withoutRunParams(src)
	}

	for name, src := range byDirective {
		byDirective[name] = withoutRunParams(src)
	}

	assert.DeepEqual(t, byFile, byDirective)
}

func TestPackageOptionFileErrors(t *testing.T) {
	const src = "package x\n\ntype A struct {\n\tB []int\n\tC int\n}\n\ntype I int\n"

	cases := []struct {
		name     string
		file     func(t *testing.T) *OptionFile
		is       error
		contains []string
	}{
		{
			name: "unknown type",
			file: func(t *testing.T) *OptionFile {
				return packageOptionFile(t, "version: 1\ntypes:\n  Z:\n")
			},
			is:       ErrOptionFile,
			contains: []string{"kama_partial.yaml: types.Z: type Z is not declared"},
		},
		{
			name: "unknown field",
			file: func(t *testing.T) *OptionFile {
				return packageOptionFile(t, "version: 1\ntypes:\n  A:\n    fields:\n      Z: {}\n")
			},
			is:       ErrOptionFile,
			contains: []string{"kama_partial.yaml: types.A.fields.Z: field Z is not declared"},
		},
		{
			name: "package not loaded",
			file: func(t *testing.T) *OptionFile {
				f := packageOptionFile(t, "version: 1\nall_types: true\n")
				f.Package = "example.com/none"
				f.Packages = map[string]PackageOptions{"example.com/none": {AllTypes: true}}

				return f
			},
			is:       ErrOptionFile,
			contains: []string{"kama_partial.yaml: package example.com/none is not loaded"},
		},
		{
			name: "basic type listed",
			file: func(t *testing.T) *OptionFile {
				return packageOptionFile(t, "version: 1\ntypes:\n  I:\n")
			},
			is:       ErrUnsupported,
			contains: []string{"kama_partial.yaml: types.I: ", "underlying type int"},
		},
		{
			name: "strategy not allowed",
			file: func(t *testing.T) *OptionFile {
				return packageOptionFile(
					t, "version: 1\ntypes:\n  A:\n    fields:\n      C: {merge: append}\n",
				)
			},
			is: ErrDirective,
			contains: []string{
				"kama_partial.yaml: types.A.fields.C.merge: package option file merge option" +
					" append on field",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"x.go": src}, Options{
				PackageOptionFiles: []*OptionFile{c.file(t)},
			})
			assert.ErrorIs(t, err, c.is)

			for _, s := range c.contains {
				assert.ErrorContains(t, err, s)
			}
		})
	}
}

func TestPackageOptionFileConflicts(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// central is the central option file, none when empty.
		central string
		pkg     string
		// contains are substrings the error must hold; empty means no
		// error.
		contains []string
	}{
		{
			name: "merge by directive",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:merge append\n\tB []int\n}\n",
			pkg: "types:\n  A:\n    fields:\n      B: {merge: overwrite}\n",
			contains: []string{
				`"append" by //kama:partial:merge (`, `x.go:5:2) and "overwrite"`,
				`"overwrite" by package option file merge option ` +
					`(kama_partial.yaml: types.A.fields.B.merge)`,
			},
		},
		{
			name: "same merge by directive",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:merge append\n\tB []int\n}\n",
			pkg: "types:\n  A:\n    fields:\n      B: {merge: append}\n",
		},
		{
			name: "marked by directive and ignored by package file",
			src:  "package x\n\n//kama:partial\ntype A struct{ B int }\n",
			pkg:  "types:\n  A: {ignore: true}\n",
			contains: []string{
				"marked by //kama:partial (", "x.go:3:1) and ignored",
				"ignored by package option file type entry (kama_partial.yaml: types.A.ignore)",
			},
		},
		{
			name: "merge by central file",
			src:  "package x\n\ntype A struct{ B []int }\n",
			central: "packages:\n  $pkg:\n    types:\n      A:\n" +
				"        fields:\n          B: {merge: append}\n",
			pkg: "types:\n  A:\n    fields:\n      B: {merge: overwrite}\n",
			contains: []string{
				`"append" by option file merge option (opts.yaml: packages.`,
				`.types.A.fields.B.merge) and "overwrite"`,
				`"overwrite" by package option file merge option ` +
					`(kama_partial.yaml: types.A.fields.B.merge)`,
			},
		},
		{
			name: "same merge by central file",
			src:  "package x\n\ntype A struct{ B []int }\n",
			central: "packages:\n  $pkg:\n    types:\n      A:\n" +
				"        fields:\n          B: {merge: append}\n",
			pkg: "types:\n  A:\n    fields:\n      B: {merge: append}\n",
		},
		{
			name: "flag by central file",
			src:  "package x\n\ntype A struct{ B int }\n",
			central: "packages:\n  $pkg:\n    types:\n      A:\n        fields:\n" +
				"          B:\n            flag: {name: b}\n",
			pkg: "types:\n  A:\n    fields:\n      B:\n        flag: {name: b, short: b}\n",
			contains: []string{
				"by option file flag option (opts.yaml: ",
				"by package option file flag option (kama_partial.yaml: types.A.fields.B.flag)",
			},
		},
		{
			name:    "fieldmask name by central file",
			src:     "package x\n\ntype A struct{ B int }\n",
			central: "packages:\n  $pkg:\n    types:\n      A:\n        fieldmask: true\n",
			pkg: "types:\n  A:\n    fieldmask: true\n    fields:\n" +
				"      B: {fieldmask: {name: c}}\n",
		},
		{
			name: "fieldmask names by central file and package file",
			src:  "package x\n\ntype A struct{ B int }\n",
			central: "packages:\n  $pkg:\n    types:\n      A:\n        fieldmask: true\n" +
				"        fields:\n          B: {fieldmask: {name: b}}\n",
			pkg: "types:\n  A:\n    fields:\n      B: {fieldmask: {name: c}}\n",
			contains: []string{
				`"b" by option file fieldmask option (opts.yaml: `,
				`"c" by package option file fieldmask option ` +
					`(kama_partial.yaml: types.A.fields.B.fieldmask)`,
			},
		},
		{
			name:    "ignored by central file and marked by package file",
			src:     "package x\n\ntype A struct{ B int }\n",
			central: "packages:\n  $pkg:\n    types:\n      A: {ignore: true}\n",
			pkg:     "types:\n  A:\n",
			contains: []string{
				"marked by package option file type entry (kama_partial.yaml: types.A)",
				"ignored by option file type entry (opts.yaml: packages.",
			},
		},
		{
			name:    "same mark by central file",
			src:     "package x\n\ntype A struct{ B int }\n",
			central: "packages:\n  $pkg:\n    all_types: true\n",
			pkg:     "all_types: true\ntypes:\n  A:\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := Options{
				PackageOptionFiles: []*OptionFile{packageOptionFile(t, "version: 1\n"+c.pkg)},
			}
			if c.central != "" {
				opts.OptionFile = optionFile(t, "version: 1\n"+c.central)
			}

			_, err := generate(t, map[string]string{"x.go": c.src}, opts)
			if len(c.contains) == 0 {
				assert.NilError(t, err)
				return
			}

			assert.ErrorIs(t, err, genopt.ErrConflict)

			for _, s := range c.contains {
				assert.ErrorContains(t, err, s)
			}
		})
	}
}

func TestValidatePackageOptionFiles(t *testing.T) {
	a := &OptionFile{Name: "a.yaml", Package: "example.com/a"}
	b := &OptionFile{Name: "b.yaml", Package: "example.com/b"}

	assert.NilError(t, Options{PackageOptionFiles: []*OptionFile{a, b}}.Validate())
	assert.ErrorIs(t, Options{PackageOptionFiles: []*OptionFile{
		{Package: "example.com/a"}, {Package: "example.com/a"},
	}}.Validate(), ErrOption)
	assert.ErrorIs(t, Options{PackageOptionFiles: []*OptionFile{nil}}.Validate(), ErrOption)
	assert.ErrorIs(
		t,
		Options{PackageOptionFiles: []*OptionFile{{Name: "c.yaml"}}}.Validate(),
		ErrOption,
	)

	err := Options{
		PackageOptionFiles: []*OptionFile{a, {Name: "a2.yaml", Package: "example.com/a"}},
	}.Validate()
	assert.ErrorIs(t, err, ErrOption)
	assert.ErrorContains(t, err, "package example.com/a has two option files, a.yaml and a2.yaml")
}

func TestProcPackageOptionFile(t *testing.T) {
	ctx := context.Background()

	f, err := overlayfs.New(".")
	assert.NilError(t, err)

	defer f.Close()

	dir := "testdata/inline/" + t.Name()
	write := func(name, content string) {
		assert.NilError(t, f.Write(ctx, dir+"/"+name, []byte(content)))
	}

	write("a/x.go", "package a\n\ntype A struct{ B int }\n")
	write("b/x.go", "//kama:option-file partial=../opts/b.toml\n\npackage b\n\n"+
		"type B struct{ C int }\n")
	write("c/x.go", "//kama:option-file partial=../opts/missing.yaml\n\npackage c\n")
	write("d/x.go", "package d\n\ntype D struct{ E int }\n")

	load := func(pkgs ...string) []*packages.Package {
		t.Helper()

		patterns := opiter.WrapSeq(slices.Values(pkgs)).
			Map(func(p string) string { return "./" + dir + "/" + p }).
			CollectSlice()

		loaded, err := gopackages.Load(ctx, f, gopackages.Options{Patterns: patterns})
		assert.NilError(t, err)

		return loaded
	}
	run := func(pkgs ...string) error {
		t.Helper()

		i := &instance{}
		assert.NilError(t, i.Param(ProcParam{}))
		i.In(&procgopackages.Packages{Pkgs: load(pkgs...)})
		i.Out(f)

		return i.Run(ctx, nopReporter{})
	}
	staged := func() map[string]string {
		t.Helper()

		overlay, err := f.Overlay()
		assert.NilError(t, err)

		out := map[string]string{}

		for name, content := range overlay {
			if strings.HasSuffix(name, "x.kama_partial.go") {
				out[name[strings.Index(name, dir)+len(dir)+1:]] = string(content)
			}
		}

		return out
	}

	// The option files are staged only, so Run reads them through the
	// overlay.
	write("a/kama_partial.yaml", "version: 1\ntypes:\n  A:\n")
	write("opts/b.toml", "version = 1\nall_types = true\n")
	assert.NilError(t, run("a", "b", "d"))

	got := staged()
	assert.DeepEqual(
		t,
		slices.Sorted(maps.Keys(got)),
		[]string{"a/x.kama_partial.go", "b/x.kama_partial.go"},
	)
	assert.Assert(t, strings.Contains(got["a/x.kama_partial.go"],
		"//\tpackage_option_file: \""+dir+"/a/kama_partial.yaml\"\n"))
	assert.Assert(t, strings.Contains(got["b/x.kama_partial.go"],
		"//\tpackage_option_file: \""+dir+"/opts/b.toml\"\n"))

	err = run("c")
	assert.ErrorIs(t, err, ErrOptionFile)
	assert.ErrorIs(t, err, genopt.ErrPackageFile)
	assert.ErrorIs(t, err, fs.ErrNotExist)

	write("d/kama_partial.yaml", "version: 1\ntypes:\n  Z:\n")

	err = run("d")
	assert.ErrorIs(t, err, ErrOptionFile)
	assert.ErrorContains(t, err, dir+"/d/kama_partial.yaml: types.Z: type Z is not declared")

	write("d/kama_partial.yaml", "version: 1\npackages: {}\n")

	err = run("d")
	assert.ErrorIs(t, err, ErrOptionFile)
	assert.ErrorContains(t, err, dir+"/d/kama_partial.yaml: packages: unknown key")
}
