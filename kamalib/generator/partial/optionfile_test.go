package partial

import (
	"context"
	"maps"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ngicks/opiter"

	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/base"
	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	procgopackages "github.com/ngicks/kama/kamalib/proc/gopackages"
)

// optionDocs are the option files of a test, in which $pkg stands for the
// import path of the inline test directory. Empty means none.
type optionDocs struct {
	// central is the central option file opts plus ext, YAML when ext is
	// empty.
	central, ext string
	// pkg is the YAML package option file kama_partial.yaml of the inline
	// test directory.
	pkg string
}

// loadOptions loads files as load does, and the options of their packages
// from the directives and from docs.
func loadOptions(
	t *testing.T,
	files map[string]string,
	docs optionDocs,
) ([]*packages.Package, *LoadedOptions, error) {
	t.Helper()

	pkgs := load(t, files)
	expand := func(doc string) *fstest.MapFile {
		return &fstest.MapFile{
			Data: []byte(strings.ReplaceAll(doc, "$pkg", strconv.Quote(modulePath(t)))),
		}
	}

	fsys := fstest.MapFS{}

	var central fileloader.Input

	if docs.central != "" {
		ext := docs.ext
		if ext == "" {
			ext = "yaml"
		}

		fsys["opts."+ext] = expand(docs.central)
		central = fileloader.PathInput{
			Base:    "opts",
			Formats: []fileloader.Format{fileloader.Format(ext)},
		}
	}

	if docs.pkg != "" {
		fsys[inlineDir(t)+"/kama_partial.yaml"] = expand(docs.pkg)
	}

	root, err := filepath.Abs(".")
	assert.NilError(t, err)

	loaded, err := NewLoader(central).LoadOption(fsys, root, base.SourcePackages(pkgs))

	return pkgs, loaded, loadError(err)
}

// generateWithOptions runs Generate on files with the options of their
// directives and of docs, a registry of this generator only and testNow.
func generateWithOptions(t *testing.T, files map[string]string, docs optionDocs) (
	[]File, []error, error,
) {
	t.Helper()

	pkgs, loaded, err := loadOptions(t, files, docs)
	if err != nil {
		return nil, nil, err
	}

	return Generate(pkgs, Options{Registry: registry(t), Now: testNow, Loaded: loaded})
}

// generatedWithOptions returns the files generateWithOptions gives keyed by
// base name, and fails on an error or a warning.
func generatedWithOptions(
	t *testing.T,
	files map[string]string,
	docs optionDocs,
) map[string]string {
	t.Helper()

	out, warnings, err := generateWithOptions(t, files, docs)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)

	m := map[string]string{}
	for _, f := range out {
		m[filepath.Base(f.Path)] = string(f.Content)
	}

	return m
}

// typeView and fieldView are the exported parts of TypeOptions and
// FieldOptions.
type (
	typeView struct {
		Ignore, FieldMask bool
	}
	fieldView struct {
		Merge     string
		Flag      *FlagOptions
		FieldMask *FieldMaskOptions
	}
)

// optionViews spells the options of the package at pkgPath of o by key:
// all_types, <type> and <type>.<field>.
func optionViews(
	o genopt.Option[PackageOptions, TypeOptions, FieldOptions],
	pkgPath string,
) map[string]any {
	po := o.Packages[pkgPath]
	out := map[string]any{"all_types": po.Package.AllTypes}

	for name, to := range po.Types {
		out[name] = typeView{Ignore: to.Type.Ignore, FieldMask: to.Type.FieldMask}

		fields, _ := to.Fields.Unpack()
		for field, f := range fields {
			out[name+"."+field] = fieldView{Merge: f.Merge, Flag: f.Flag, FieldMask: f.FieldMask}
		}
	}

	return out
}

func TestOptionFileFormats(t *testing.T) {
	const src = "package x\n\ntype Server struct {\n\tAddr string\n\tPort int\n}\n\n" +
		"type Client struct{ C int }\n"

	docs := map[string]string{
		"yaml": `
version: 1
packages:
  $pkg:
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
              usage: listen address
            fieldmask:
              name: address
          Port:
      Client:
        ignore: true
`,
		"json": `{
  "version": 1,
  "packages": {
    $pkg: {
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
		"toml": `
version = 1

[packages.$pkg]
all_types = true

[packages.$pkg.types.Server]
fieldmask = true

[packages.$pkg.types.Server.fields.Addr]
merge = "append"
flag = { name = "addr", short = "a", usage = "listen address" }
fieldmask = { name = "address" }

[packages.$pkg.types.Server.fields.Port]

[packages.$pkg.types.Client]
ignore = true
`,
	}

	for ext, doc := range docs {
		t.Run(ext, func(t *testing.T) {
			_, loaded, err := loadOptions(
				t,
				map[string]string{"x.go": src},
				optionDocs{central: doc, ext: ext},
			)
			assert.NilError(t, err)
			assert.DeepEqual(t, loaded.Files, []string{"opts." + ext})
			assert.DeepEqual(t, optionViews(loaded.Option, modulePath(t)), map[string]any{
				"all_types": true,
				"Server":    typeView{FieldMask: true},
				"Server.Addr": fieldView{
					Merge:     "append",
					Flag:      &FlagOptions{Name: "addr", Short: "a", Usage: "listen address"},
					FieldMask: &FieldMaskOptions{Name: "address"},
				},
				"Server.Port": fieldView{},
				"Client":      typeView{Ignore: true},
			})
		})
	}
}

func TestOptionFileDecodeErrors(t *testing.T) {
	const src = "package x\n\ntype S struct{ F int }\n"

	cases := []struct {
		name string
		doc  string
		// contains are substrings the error must hold after the key path
		// of the package entry.
		contains []string
	}{
		{
			name:     "all_types not a boolean",
			doc:      "    all_types: yes please\n",
			contains: []string{".all_types: cannot decode a string into bool"},
		},
		{
			name:     "unknown package key",
			doc:      "    file: true\n",
			contains: []string{".file: unknown key"},
		},
		{
			name:     "unknown type key",
			doc:      "    types:\n      S:\n        merge: append\n",
			contains: []string{".types.S.merge: unknown key"},
		},
		{
			name:     "ignore not a boolean",
			doc:      "    types:\n      S:\n        ignore: 1\n",
			contains: []string{".types.S.ignore: cannot decode a number into bool"},
		},
		{
			name:     "fieldmask not a boolean",
			doc:      "    types:\n      S:\n        fieldmask: yes please\n",
			contains: []string{".types.S.fieldmask: cannot decode a string into bool"},
		},
		{
			name: "merge not a string",
			doc: "    types:\n      S:\n        fields:\n          F:\n            merge:\n" +
				"              - append\n",
			contains: []string{".types.S.fields.F.merge: cannot decode an array into string"},
		},
		{
			name: "unknown flag key",
			doc: "    types:\n      S:\n        fields:\n          F:\n            flag:\n" +
				"              name: f\n              prefix: x\n",
			contains: []string{".types.S.fields.F.flag.prefix: unknown key"},
		},
		{
			name: "flag not a table",
			doc:  "    types:\n      S:\n        fields:\n          F:\n            flag: f\n",
			contains: []string{
				".types.S.fields.F.flag: cannot decode a string into ",
			},
		},
		{
			name: "empty short",
			doc: "    types:\n      S:\n        fields:\n          F:\n            flag:\n" +
				"              name: f\n              short: \"\"\n",
			contains: []string{".types.S.fields.F.flag: empty shorthand"},
		},
		{
			name: "unknown fieldmask key",
			doc: "    types:\n      S:\n        fields:\n          F:\n            fieldmask:\n" +
				"              name: f\n              json: true\n",
			contains: []string{".types.S.fields.F.fieldmask.json: unknown key"},
		},
		{
			name: "fieldmask not a table",
			doc:  "    types:\n      S:\n        fields:\n          F:\n            fieldmask: f\n",
			contains: []string{
				".types.S.fields.F.fieldmask: cannot decode a string into partial.FieldMaskOptions",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := loadOptions(t, map[string]string{"x.go": src}, optionDocs{
				central: "version: 1\npackages:\n  $pkg:\n" + c.doc,
			})
			assert.ErrorIs(t, err, ErrOptionFile)
			assert.ErrorIs(t, err, genopt.ErrOptionFile)

			for _, s := range c.contains {
				assert.ErrorContains(t, err, "opts.yaml: packages."+strconv.Quote(modulePath(t))+s)
			}
		})
	}
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

	byFile := generatedWithOptions(t, map[string]string{"x.go": "package x\n" + models}, optionDocs{
		central: `
version: 1
packages:
  $pkg:
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
`,
	})
	for name, src := range byFile {
		assert.Assert(t, strings.Contains(src, "//\toption_file: \"opts.yaml\"\n"))
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
				"        fields:\n          Z:\n            merge: append\n",
			is:       ErrOptionFile,
			contains: []string{".types.A.fields.Z: field Z is not declared"},
		},
		{
			name: "fields of a non-struct",
			doc: "packages:\n  $pkg:\n    types:\n      I:\n        fields:\n" +
				"          Z:\n",
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
				"        fields:\n          B:\n            merge: deep\n",
			is: ErrDirective,
			contains: []string{
				".types.A.fields.B.merge: option file merge option: unknown strategy \"deep\"",
			},
		},
		{
			name: "strategy not allowed",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n" +
				"        fields:\n          C:\n            merge: append\n",
			is: ErrDirective,
			contains: []string{
				".types.A.fields.C.merge: option file merge option append on field",
				"want overwrite",
			},
		},
		{
			name: "merge on an ignored type",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n      D:\n        ignore: true\n" +
				"        fields:\n          N:\n            merge: overwrite\n",
			is: ErrDirective,
			contains: []string{
				".types.D.fields.N.merge: option file merge option on field",
				"no partial variant",
			},
		},
		{
			name: "flag without name",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fields:\n" +
				"          C:\n            flag:\n              short: c\n",
			is: ErrDirective,
			contains: []string{
				".types.A.fields.C.flag: option file flag option on field",
				"name is required",
			},
		},
		{
			name: "flag on a nested partial",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fields:\n" +
				"          D:\n            flag:\n              name: d\n",
			is: ErrDirective,
			contains: []string{
				".types.A.fields.D.flag: option file flag option on field",
				"nested partial",
			},
		},
		{
			name: "fieldmask without name",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fieldmask: true\n" +
				"        fields:\n          C:\n            fieldmask:\n",
			is: ErrDirective,
			contains: []string{
				".types.A.fields.C.fieldmask: option file fieldmask option on field",
				"name is required",
			},
		},
		{
			name: "fieldmask name on a type without field masks",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fields:\n" +
				"          C:\n            fieldmask:\n              name: c\n",
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
				"          C:\n            flag:\n              name: x\n" +
				"          E:\n            flag:\n              name: x\n",
			is: ErrDirective,
			contains: []string{
				".types.A.fields.E.flag: ",
				`flag name "x" is taken by field C (opts.yaml: `,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := generateWithOptions(t, map[string]string{"x.go": src}, optionDocs{
				central: "version: 1\n" + c.doc,
			})
			assert.ErrorIs(t, err, c.is)

			for _, s := range c.contains {
				assert.ErrorContains(t, err, s)
			}
		})
	}
}

// TestOptionFileOverrides checks that the option file entry of a type
// replaces every directive on the type, its group and its fields, with a
// warning naming both.
func TestOptionFileOverrides(t *testing.T) {
	cases := []struct {
		name string
		src  string
		doc  string
		// at is the line of the first directive of A.
		at string
		// partials are the partial types generated.
		partials []string
		// contains are substrings the error must hold; empty means no
		// error.
		contains []string
	}{
		{
			name: "merge",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:merge append\n\tB []int\n}\n",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n" +
				"        fields:\n          B:\n            merge: overwrite\n",
			at:       "x.go:3:1",
			partials: []string{"APartial"},
		},
		{
			name: "same merge",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:merge append\n\tB []int\n}\n",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n" +
				"        fields:\n          B:\n            merge: append\n",
			at:       "x.go:3:1",
			partials: []string{"APartial"},
		},
		{
			name: "marked by directive and ignored by file",
			src:  "package x\n\n//kama:partial\ntype A struct{ B int }\n",
			doc:  "packages:\n  $pkg:\n    types:\n      A:\n        ignore: true\n",
			at:   "x.go:3:1",
		},
		{
			name:     "ignored by directive and marked by file",
			src:      "package x\n\n//kama:partial ignore\ntype A struct{ B int }\n",
			doc:      "packages:\n  $pkg:\n    types:\n      A:\n",
			at:       "x.go:3:1",
			partials: []string{"APartial"},
		},
		{
			name: "fieldmask type by directive and name by file",
			src:  "package x\n\n//kama:partial\n//kama:partial:fieldmask\ntype A struct{ B int }\n",
			doc: "packages:\n  $pkg:\n    types:\n      A:\n        fields:\n" +
				"          B:\n            fieldmask:\n              name: b\n",
			contains: []string{"A does not have field masks enabled"},
		},
		{
			name: "package marks kept beside a type entry",
			src: "package x\n\n//kama:partial package\n\n//kama:partial\ntype A struct{ B int }\n\n" +
				"type B struct{ C int }\n",
			doc:      "packages:\n  $pkg:\n    types:\n      A:\n        ignore: true\n",
			at:       "x.go:5:1",
			partials: []string{"BPartial"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files, warnings, err := generateWithOptions(
				t,
				map[string]string{"x.go": c.src},
				optionDocs{central: "version: 1\n" + c.doc},
			)
			if len(c.contains) > 0 {
				assert.ErrorIs(t, err, ErrDirective)

				for _, s := range c.contains {
					assert.ErrorContains(t, err, s)
				}

				return
			}

			assert.NilError(t, err)
			assert.Equal(t, len(warnings), 1, "%v", warnings)
			assert.ErrorIs(t, warnings[0], genopt.ErrOverridden)
			assert.ErrorContains(
				t,
				warnings[0],
				"type "+modulePath(t)+".A: opts.yaml: packages."+strconv.Quote(modulePath(t))+
					".types.A overrides ",
			)
			assert.ErrorContains(t, warnings[0], inlineDir(t)+"/"+c.at)

			var partials []string

			for _, f := range files {
				if strings.HasSuffix(f.Path, "x.kama_partial.go") {
					partials = partialTypes(t, string(f.Content))
				}
			}

			assert.DeepEqual(t, partials, c.partials)
		})
	}
}

func TestProcOptionFile(t *testing.T) {
	i := &instance{}
	assert.ErrorIs(t, i.Param(ProcParam{OptionFile: &fileloader.PathInput{}}), ErrOption)
	assert.ErrorIs(t, i.Param(ProcParam{OptionFile: &fileloader.PathInput{
		Base: "partial", Formats: []fileloader.Format{"ini"},
	}}), ErrOption)
	assert.NilError(t, i.Param(ProcParam{OptionFile: &fileloader.PathInput{Base: "partial"}}))
	assert.DeepEqual(t, i.optionFile, &fileloader.PathInput{
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
		i := &instance{}
		assert.NilError(t, i.Param(ProcParam{OptionFile: &fileloader.PathInput{Base: optionFile}}))
		i.In(&procgopackages.Packages{Pkgs: pkgs})
		i.Out(f)

		return i.Run(ctx, nopReporter{})
	}
	err = run(dir + "/missing")
	assert.ErrorIs(t, err, ErrOptionFile)
	assert.ErrorIs(t, err, fileloader.ErrNotFound)

	assert.NilError(t, f.Write(ctx, dir+"/bad.toml", []byte("version = 2\n")))
	err = run(dir + "/bad")
	assert.ErrorIs(t, err, ErrOptionFile)
	assert.ErrorContains(t, err, dir+"/bad.toml: version: unsupported version 2")

	// The option file is staged only, so Run reads it through the overlay.
	assert.NilError(t, f.Write(ctx, dir+"/partial.yaml", []byte(
		"version: 1\npackages:\n  "+strconv.Quote(modulePath(t))+":\n    types:\n      A:\n",
	)))
	assert.NilError(t, run(dir+"/partial"))

	staged, err := f.Overlay()
	assert.NilError(t, err)

	names := opiter.WrapSeq(maps.Keys(staged)).
		Filter(func(name string) bool { return strings.HasSuffix(name, "x.kama_partial.go") }).
		CollectSlice()

	assert.Equal(t, len(names), 1, names)
}
