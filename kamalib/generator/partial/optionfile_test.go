package partial

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ngicks/kama/kamalib/generator/base"

	"github.com/ngicks/opiter"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/kamaopt"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	procgopackages "github.com/ngicks/kama/kamalib/proc/gopackages"
)

func TestDecodeOptionFileFormats(t *testing.T) {
	docs := map[fileloader.Format]string{
		fileloader.FormatYaml: `
version: 1
packages:
  example.com/gen/pb:
    all_types: true
    types:
      Server:
        fieldmask: true
        fields:
          Addr:
            merge: append
            flag: {name: addr, short: a, usage: listen address}
            fieldmask: {name: address}
          Port:
      Client:
        ignore: true
`,
		fileloader.FormatJson: `{
  "version": 1,
  "packages": {
    "example.com/gen/pb": {
      "all_types": true,
      "types": {
        "Server": {
          "fieldmask": true,
          "fields": {
            "Addr": {
              "merge": "append",
              "flag": {"name": "addr", "short": "a", "usage": "listen address"},
              "fieldmask": {"name": "address"}
            },
            "Port": {}
          }
        },
        "Client": {"ignore": true}
      }
    }
  }
}`,
		fileloader.FormatToml: `
version = 1

[packages."example.com/gen/pb"]
all_types = true

[packages."example.com/gen/pb".types.Server]
fieldmask = true

[packages."example.com/gen/pb".types.Server.fields.Addr]
merge = "append"
flag = { name = "addr", short = "a", usage = "listen address" }
fieldmask = { name = "address" }

[packages."example.com/gen/pb".types.Server.fields.Port]

[packages."example.com/gen/pb".types.Client]
ignore = true
`,
	}
	want := &OptionFile{
		Name: "partial",
		Packages: map[string]PackageOptions{
			"example.com/gen/pb": {
				AllTypes: true,
				Types: map[string]TypeOptions{
					"Server": {FieldMask: true, Fields: map[string]FieldOptions{
						"Addr": {
							Merge: "append",
							Flag: &FlagOptions{
								Name:  "addr",
								Short: "a",
								Usage: "listen address",
							},
							FieldMask: &FieldMaskOptions{Name: "address"},
						},
						"Port": {},
					}},
					"Client": {Ignore: true, Fields: map[string]FieldOptions{}},
				},
			},
		},
	}
	for format, doc := range docs {
		t.Run(string(format), func(t *testing.T) {
			var data any
			assert.NilError(t, fileloader.Decode(strings.NewReader(doc), format, &data))
			got, err := DecodeOptionFile("partial", data)
			assert.NilError(t, err)
			assert.DeepEqual(t, got, want)
		})
	}
}

func TestDecodeOptionFileErrors(t *testing.T) {
	const pb = "packages.\"example.com/gen/pb\""
	cases := []struct {
		name string
		doc  string
		// contains are substrings the error must hold besides the file name.
		contains []string
	}{
		{name: "not a table", doc: `[1]`, contains: []string{"not a table"}},
		{name: "missing version", doc: `packages: {}`, contains: []string{"version is required"}},
		{
			name:     "unsupported version",
			doc:      `version: 2`,
			contains: []string{"version: unsupported version 2"},
		},
		{
			name:     "version not an integer",
			doc:      `version: "1"`,
			contains: []string{"version: 1 is not an integer"},
		},
		{
			name:     "fractional version",
			doc:      `version: 1.5`,
			contains: []string{"version: 1.5 is not an integer"},
		},
		{
			name:     "unknown top key",
			doc:      "version: 1\nfiles: {}\n",
			contains: []string{"files: unknown key"},
		},
		{
			name:     "packages not a table",
			doc:      "version: 1\npackages: [a]\n",
			contains: []string{"packages: [a] is not a table"},
		},
		{
			name:     "unknown package key",
			doc:      "version: 1\npackages:\n  example.com/gen/pb:\n    file: true\n",
			contains: []string{pb + ".file: unknown key; want all_types, types"},
		},
		{
			name:     "all_types not a boolean",
			doc:      "version: 1\npackages:\n  example.com/gen/pb:\n    all_types: yes please\n",
			contains: []string{pb + ".all_types: yes please is not a boolean"},
		},
		{
			name: "unknown type key",
			doc: "version: 1\npackages:\n  example.com/gen/pb:\n" +
				"    types:\n      S:\n        merge: append\n",
			contains: []string{pb + ".types.S.merge: unknown key; want ignore, fieldmask, fields"},
		},
		{
			name: "ignore not a boolean",
			doc: "version: 1\npackages:\n  example.com/gen/pb:\n" +
				"    types:\n      S:\n        ignore: 1\n",
			contains: []string{pb + ".types.S.ignore: 1 is not a boolean"},
		},
		{
			name: "merge not a string",
			doc: "version: 1\npackages:\n  example.com/gen/pb:\n    types:\n      S:\n" +
				"        fields:\n          F:\n            merge: [append]\n",
			contains: []string{pb + ".types.S.fields.F.merge: [append] is not a string"},
		},
		{
			name: "unknown flag key",
			doc: "version: 1\npackages:\n  example.com/gen/pb:\n    types:\n      S:\n" +
				"        fields:\n          F:\n            flag: {name: f, prefix: x}\n",
			contains: []string{
				pb + ".types.S.fields.F.flag.prefix: unknown key; want name, short, usage",
			},
		},
		{
			name: "flag not a table",
			doc: "version: 1\npackages:\n  example.com/gen/pb:\n    types:\n      S:\n" +
				"        fields:\n          F:\n            flag: f\n",
			contains: []string{pb + ".types.S.fields.F.flag: f is not a table"},
		},
		{
			name: "empty short",
			doc: "version: 1\npackages:\n  example.com/gen/pb:\n    types:\n      S:\n" +
				"        fields:\n          F:\n            flag: {name: f, short: \"\"}\n",
			contains: []string{pb + ".types.S.fields.F.flag.short: empty shorthand"},
		},
		{
			name: "fieldmask not a boolean",
			doc: "version: 1\npackages:\n  example.com/gen/pb:\n" +
				"    types:\n      S:\n        fieldmask: yes please\n",
			contains: []string{pb + ".types.S.fieldmask: yes please is not a boolean"},
		},
		{
			name: "unknown fieldmask key",
			doc: "version: 1\npackages:\n  example.com/gen/pb:\n    types:\n      S:\n" +
				"        fields:\n          F:\n            fieldmask: {name: f, json: true}\n",
			contains: []string{
				pb + ".types.S.fields.F.fieldmask.json: unknown key; want name",
			},
		},
		{
			name: "fieldmask not a table",
			doc: "version: 1\npackages:\n  example.com/gen/pb:\n    types:\n      S:\n" +
				"        fields:\n          F:\n            fieldmask: f\n",
			contains: []string{pb + ".types.S.fields.F.fieldmask: f is not a table"},
		},
		{
			name:     "non-string key",
			doc:      "version: 1\npackages:\n  example.com/gen/pb:\n    types:\n      1: {}\n",
			contains: []string{pb + ".types: key 1 is not a string"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var data any
			assert.NilError(
				t,
				fileloader.Decode(strings.NewReader(c.doc), fileloader.FormatYaml, &data),
			)
			_, err := DecodeOptionFile("opts.yaml", data)
			assert.ErrorIs(t, err, ErrOptionFile)
			assert.ErrorContains(t, err, "opts.yaml: ")
			for _, s := range c.contains {
				assert.ErrorContains(t, err, s)
			}
		})
	}
}

func TestLoadOptionFile(t *testing.T) {
	fsys := fstest.MapFS{
		"conf/partial.json": {Data: []byte(`{"version": 1}`)},
		"conf/partial.toml": {Data: []byte("version = 2\n")},
	}
	f, err := LoadOptionFile(fsys, fileloader.PathInput{
		Base: "conf/partial", Formats: []fileloader.Format{fileloader.FormatJson},
	})
	assert.NilError(t, err)
	assert.DeepEqual(
		t,
		f,
		&OptionFile{Name: "conf/partial.json", Packages: map[string]PackageOptions{}},
	)

	_, err = LoadOptionFile(fsys, fileloader.PathInput{
		Base: "conf/partial", Formats: []fileloader.Format{fileloader.FormatToml},
	})
	assert.ErrorIs(t, err, ErrOptionFile)
	assert.ErrorContains(t, err, "conf/partial.toml: version")

	_, err = LoadOptionFile(fsys, fileloader.PathInput{
		Base: "missing", Formats: []fileloader.Format{fileloader.FormatYaml},
	})
	assert.ErrorIs(t, err, ErrOptionFile)
	assert.ErrorIs(t, err, fileloader.ErrNotFound)
}

// optionFile decodes doc, a YAML option file in which $pkg stands for the
// import path of the inline test directory of t, named opts.yaml.
func optionFile(t *testing.T, doc string) *OptionFile {
	t.Helper()
	doc = strings.ReplaceAll(doc, "$pkg", strconv.Quote(modulePath(t)))
	var data any
	assert.NilError(t, fileloader.Decode(strings.NewReader(doc), fileloader.FormatYaml, &data))
	f, err := DecodeOptionFile("opts.yaml", data)
	assert.NilError(t, err)
	return f
}

func TestOptionFileParity(t *testing.T) {
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
		OptionFile: optionFile(t, `
version: 1
packages:
  $pkg:
    all_types: true
    types:
      A:
        fields:
          B: {merge: append}
          C:
            flag: {name: c, short: c, usage: the c}
      D:
        fieldmask: true
        fields:
          N: {fieldmask: {name: n}}
      E: {ignore: true}
`),
	})
	for name, src := range byFile {
		byFile[name] = withoutRunParams(src)
	}
	for name, src := range byDirective {
		byDirective[name] = withoutRunParams(src)
	}
	assert.DeepEqual(t, byFile, byDirective)
}

func TestOptionFileErrors(t *testing.T) {
	const src = "package x\n\ntype A struct {\n\tB []int\n\tC int\n\tD D\n\tE int\n}\n\n" +
		"type D struct{ N int }\n\ntype I int\n\ntype L = A\n"
	cases := []struct {
		name     string
		doc      string
		is       error
		contains []string
	}{
		{
			name: "unknown package",
			doc:  "packages:\n  example.com/none:\n    all_types: true\n",
			is:   ErrOptionFile,
			contains: []string{
				`opts.yaml: packages."example.com/none": package example.com/none is not loaded`,
			},
		},
		{
			name:     "unknown type",
			doc:      "packages:\n  $pkg:\n    types:\n      Z:\n",
			is:       ErrOptionFile,
			contains: []string{".types.Z: type Z is not declared"},
		},
		{
			name: "unknown field",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n" +
				"        fields:\n          Z: {merge: append}\n",
			is:       ErrOptionFile,
			contains: []string{".types.A.fields.Z: field Z is not declared"},
		},
		{
			name:     "fields of a non-struct",
			doc:      "packages:\n  $pkg:\n    types:\n      I:\n        fields:\n          Z: {}\n",
			is:       ErrOptionFile,
			contains: []string{".types.I.fields: ", "is not a struct type"},
		},
		{
			name:     "basic type listed",
			doc:      "packages:\n  $pkg:\n    types:\n      I:\n",
			is:       ErrUnsupported,
			contains: []string{"opts.yaml: packages.", ".types.I: ", "underlying type int"},
		},
		{
			name:     "alias listed",
			doc:      "packages:\n  $pkg:\n    types:\n      L:\n",
			is:       ErrUnsupported,
			contains: []string{".types.L: ", "alias"},
		},
		{
			name: "unknown strategy",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n" +
				"        fields:\n          B: {merge: deep}\n",
			is: ErrDirective,
			contains: []string{
				".types.A.fields.B.merge: option file merge option: unknown strategy \"deep\"",
			},
		},
		{
			name: "strategy not allowed",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n" +
				"        fields:\n          C: {merge: append}\n",
			is: ErrDirective,
			contains: []string{
				".types.A.fields.C.merge: option file merge option append on field",
				"want overwrite",
			},
		},
		{
			name: "merge on an ignored type",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n      D:\n        ignore: true\n" +
				"        fields:\n          N: {merge: overwrite}\n",
			is: ErrDirective,
			contains: []string{
				".types.D.fields.N.merge: option file merge option on field",
				"no partial variant",
			},
		},
		{
			name: "flag without name",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fields:\n" +
				"          C:\n            flag: {short: c}\n",
			is: ErrDirective,
			contains: []string{
				".types.A.fields.C.flag: option file flag option on field",
				"name is required",
			},
		},
		{
			name: "flag on a nested partial",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fields:\n" +
				"          D:\n            flag: {name: d}\n",
			is: ErrDirective,
			contains: []string{
				".types.A.fields.D.flag: option file flag option on field",
				"nested partial",
			},
		},
		{
			name: "fieldmask without name",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fieldmask: true\n" +
				"        fields:\n          C:\n            fieldmask: {}\n",
			is: ErrDirective,
			contains: []string{
				".types.A.fields.C.fieldmask: option file fieldmask option on field",
				"name is required",
			},
		},
		{
			name: "fieldmask name on a type without field masks",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fields:\n" +
				"          C:\n            fieldmask: {name: c}\n",
			is: ErrDirective,
			contains: []string{
				".types.A.fields.C.fieldmask: option file fieldmask option on field",
				"A does not have field masks enabled",
			},
		},
		{
			name: "fieldmask on an ignored type",
			doc: "packages:\n  $pkg:\n    types:\n      D:\n" +
				"        ignore: true\n        fieldmask: true\n",
			is: ErrDirective,
			contains: []string{
				".types.D.fieldmask: option file fieldmask option on type",
				"D has no partial variant",
			},
		},
		{
			name: "flag name taken",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fields:\n" +
				"          C:\n            flag: {name: x}\n          E:\n            flag: {name: x}\n",
			is: ErrDirective,
			contains: []string{
				".types.A.fields.E.flag: ",
				`flag name "x" is taken by field C (opts.yaml: `,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"x.go": src}, Options{
				OptionFile: optionFile(t, "version: 1\n"+c.doc),
			})
			assert.ErrorIs(t, err, c.is)
			for _, s := range c.contains {
				assert.ErrorContains(t, err, s)
			}
		})
	}
}

func TestOptionFileConflicts(t *testing.T) {
	cases := []struct {
		name string
		src  string
		doc  string
		// contains are substrings the error must hold; empty means no
		// error.
		contains []string
	}{
		{
			name: "merge",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:merge append\n\tB []int\n}\n",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n" +
				"        fields:\n          B: {merge: overwrite}\n",
			contains: []string{
				`"append" by //kama:partial:merge (`,
				`x.go:5:2) and "overwrite"`,
				`"overwrite" by option file merge option (opts.yaml: packages.`,
				".types.A.fields.B.merge)",
			},
		},
		{
			name: "same merge",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:merge append\n\tB []int\n}\n",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n" +
				"        fields:\n          B: {merge: append}\n",
		},
		{
			name: "flag",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:flag name=b\n\tB int\n}\n",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fields:\n" +
				"          B:\n            flag: {name: b, usage: the b}\n",
			contains: []string{
				"by //kama:partial:flag (", "x.go:5:2) and",
				`usage="the b" by option file flag option (opts.yaml: `,
				".types.A.fields.B.flag)",
			},
		},
		{
			name: "same flag",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:flag name=b short=b\n\tB int\n}\n",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fields:\n" +
				"          B:\n            flag: {name: b, short: b}\n",
		},
		{
			name: "fieldmask name",
			src: "package x\n\n//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\t//kama:partial:fieldmask name=b\n\tB int\n}\n",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fields:\n" +
				"          B:\n            fieldmask: {name: c}\n",
			contains: []string{
				`the field mask name of field `,
				`"b" by //kama:partial:fieldmask (`, "x.go:6:2) and",
				`"c" by option file fieldmask option (opts.yaml: `,
				".types.A.fields.B.fieldmask)",
			},
		},
		{
			name: "same fieldmask",
			src: "package x\n\n//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\t//kama:partial:fieldmask name=b\n\tB int\n}\n",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fieldmask: true\n" +
				"        fields:\n          B:\n            fieldmask: {name: b}\n",
		},
		{
			name: "fieldmask type by file and name by directive",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:fieldmask name=b\n\tB int\n}\n",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fieldmask: true\n",
		},
		{
			name: "fieldmask type by directive and name by file",
			src:  "package x\n\n//kama:partial\n//kama:partial:fieldmask\ntype A struct{ B int }\n",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fields:\n" +
				"          B:\n            fieldmask: {name: b}\n",
		},
		{
			name: "fieldmask name by file conflicting with directive on a type enabled by both",
			src: "package x\n\n//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\t//kama:partial:fieldmask name=b\n\tB int\n}\n",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fieldmask: true\n" +
				"        fields:\n          B:\n            fieldmask: {name: c}\n",
			contains: []string{
				`"b" by //kama:partial:fieldmask (`,
				`"c" by option file fieldmask option (opts.yaml: `,
			},
		},
		{
			name: "marked by directive and ignored by file",
			src:  "package x\n\n//kama:partial\ntype A struct{ B int }\n",
			doc:  "packages:\n  $pkg:\n    types:\n      A: {ignore: true}\n",
			contains: []string{
				"marked by //kama:partial (", "x.go:3:1) and ignored",
				"ignored by option file type entry (opts.yaml: ",
				".types.A.ignore)",
			},
		},
		{
			name: "ignored by directive and marked by file",
			src:  "package x\n\n//kama:partial ignore\ntype A struct{ B int }\n",
			doc:  "packages:\n  $pkg:\n    types:\n      A:\n",
			contains: []string{
				"marked by option file type entry (opts.yaml: ",
				"ignored by //kama:partial (", "x.go:3:1)",
			},
		},
		{
			name: "same mark",
			src:  "package x\n\n//kama:partial package\n\n//kama:partial\ntype A struct{ B int }\n",
			doc:  "packages:\n  $pkg:\n    all_types: true\n    types:\n      A:\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"x.go": c.src}, Options{
				OptionFile: optionFile(t, "version: 1\n"+c.doc),
			})
			if len(c.contains) == 0 {
				assert.NilError(t, err)
				return
			}
			assert.ErrorIs(t, err, kamaopt.ErrConflict)
			for _, s := range c.contains {
				assert.ErrorContains(t, err, s)
			}
		})
	}
}

func TestProcOptionFile(t *testing.T) {
	g := &generator{}
	assert.ErrorIs(t, g.Configure(ProcParam{OptionFile: &fileloader.PathInput{}}), ErrOption)
	assert.ErrorIs(t, g.Configure(ProcParam{OptionFile: &fileloader.PathInput{
		Base: "partial", Formats: []fileloader.Format{"ini"},
	}}), ErrOption)
	assert.NilError(t, g.Configure(ProcParam{OptionFile: &fileloader.PathInput{Base: "partial"}}))
	assert.DeepEqual(t, g.optionFile, &fileloader.PathInput{
		Base: "partial",
		Formats: []fileloader.Format{
			fileloader.FormatYaml, fileloader.FormatJson, fileloader.FormatToml,
		},
	})

	ctx := context.Background()
	f, err := overlayfs.New(".")
	assert.NilError(t, err)
	defer f.Close()
	dir := "testdata/inline/" + t.Name()
	assert.NilError(t, f.Write(ctx, dir+"/x.go", []byte("package x\n\ntype A struct{ B int }\n")))
	pkgs, err := gopackages.Load(ctx, f, gopackages.Options{Patterns: []string{"./" + dir}})
	assert.NilError(t, err)

	run := func(optionFile string) error {
		i := base.NewInstance(newGenerator())
		assert.NilError(t, i.Param(ProcParam{OptionFile: &fileloader.PathInput{Base: optionFile}}))
		i.In(&procgopackages.Packages{Pkgs: pkgs})
		i.Out(f)
		return i.Run(ctx, nopReporter{})
	}
	err = run(dir + "/missing")
	assert.ErrorIs(t, err, fileloader.ErrNotFound)

	// The option file is staged only, so Run reads it through the overlay.
	assert.NilError(t, f.Write(ctx, dir+"/partial.yaml", []byte(
		"version: 1\npackages:\n  "+strconv.Quote(modulePath(t))+":\n    types:\n      A:\n",
	)))
	assert.NilError(t, run(dir+"/partial"))
	staged, err := f.Overlay()
	assert.NilError(t, err)
	names := slices.Collect(opiter.Filter(isXPartialFile, maps.Keys(staged)))
	assert.Equal(t, len(names), 1, names)
}

func isXPartialFile(name string) bool { return strings.HasSuffix(name, "x.kama_partial.go") }
