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
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	procgopackages "github.com/ngicks/kama/kamalib/proc/gopackages"
)

func TestPackageOptionFileDecode(t *testing.T) {
	const src = "package x\n\ntype Server struct{ Addr string }\n\ntype Client struct{ C int }\n"

	_, _, loaded, err := loadOptions(t, map[string]string{"x.go": src}, optionDocs{pkg: `
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
`})
	assert.NilError(t, err)
	assert.DeepEqual(t, loaded.PackageFiles, map[string]string{
		modulePath(t): gentest.InlineDir(t) + "/kama_partial.yaml",
	})
	assert.DeepEqual(t, optionViews(loaded.Option, modulePath(t)), map[string]any{
		"all_types": true,
		"Server":    typeView{FieldMask: true},
		"Server.Addr": fieldView{
			Merge:     "append",
			Flag:      &FlagOptions{Name: "addr", Short: "a"},
			FieldMask: &FieldMaskOptions{Name: "address"},
		},
		"Client": typeView{Ignore: true},
	})
}

func TestPackageOptionFileDecodeErrors(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		// contains are substrings the error must hold after the file name.
		contains []string
	}{
		{name: "not a table", doc: "- 1\n", contains: []string{": the document is not a table"}},
		{
			name:     "missing version",
			doc:      "all_types: true\n",
			contains: []string{": version: version is required"},
		},
		{
			name:     "unsupported version",
			doc:      "version: 2\n",
			contains: []string{": version: unsupported version 2"},
		},
		{
			name:     "all_types not a boolean",
			doc:      "version: 1\nall_types: 1\n",
			contains: []string{": all_types: cannot decode a number into bool"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, _, err := loadOptions(
				t,
				map[string]string{"x.go": "package x\n\ntype S struct{ F int }\n"},
				optionDocs{pkg: c.doc},
			)
			assert.ErrorIs(t, err, ErrOptionFile)

			for _, s := range c.contains {
				assert.ErrorContains(t, err, gentest.InlineDir(t)+"/kama_partial.yaml"+s)
			}
		})
	}
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

	byFile := generatedWithOptions(t, map[string]string{"x.go": "package x\n" + models}, optionDocs{
		pkg: `
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
`,
	})

	for name, src := range byFile {
		assert.Assert(t, strings.Contains(
			src, "//\tpackage_option_file: \""+gentest.InlineDir(t)+"/kama_partial.yaml\"\n",
		))
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
		doc      string
		is       error
		contains []string
	}{
		{
			name:     "unknown type",
			doc:      "types:\n  Z:\n",
			is:       ErrOptionFile,
			contains: []string{"kama_partial.yaml: types.Z: type Z is not declared"},
		},
		{
			name:     "unknown field",
			doc:      "types:\n  A:\n    fields:\n      Z:\n",
			is:       ErrOptionFile,
			contains: []string{"kama_partial.yaml: types.A.fields.Z: field Z is not declared"},
		},
		{
			name:     "basic type listed",
			doc:      "types:\n  I:\n",
			is:       ErrUnsupported,
			contains: []string{"kama_partial.yaml: types.I: ", "underlying type int"},
		},
		{
			name: "strategy not allowed",
			doc:  "types:\n  A:\n    fields:\n      C:\n        merge: append\n",
			is:   ErrDirective,
			contains: []string{
				"kama_partial.yaml: types.A.fields.C.merge: package option file merge option" +
					" append on field",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := generateWithOptions(t, map[string]string{"x.go": src}, optionDocs{
				pkg: "version: 1\n" + c.doc,
			})
			assert.ErrorIs(t, err, c.is)

			for _, s := range c.contains {
				assert.ErrorContains(t, err, s)
			}
		})
	}
}

// TestPackageOptionFileOverrides checks that the package option file entry
// of a type replaces the directives on the type and the central option file
// entry of the type, with a warning naming both.
func TestPackageOptionFileOverrides(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// central is the central option file, none when empty.
		central string
		pkg     string
		// overridden is the source the warnings name besides the package
		// option file entry of A.
		overridden []string
		partials   []string
	}{
		{
			name: "merge by directive",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:merge append\n\tB []int\n}\n",
			pkg:        "types:\n  A:\n    fields:\n      B:\n        merge: overwrite\n",
			overridden: []string{"/x.go:3:1"},
			partials:   []string{"APartial"},
		},
		{
			name:       "marked by directive and ignored by package file",
			src:        "package x\n\n//kama:partial\ntype A struct{ B int }\n",
			pkg:        "types:\n  A:\n    ignore: true\n",
			overridden: []string{"/x.go:3:1"},
		},
		{
			name: "merge by central file",
			src:  "package x\n\ntype A struct{ B []int }\n",
			central: "packages:\n  $pkg:\n    types:\n      A:\n" +
				"        fields:\n          B:\n            merge: append\n",
			pkg:        "types:\n  A:\n    fields:\n      B:\n        merge: overwrite\n",
			overridden: []string{"opts.yaml: packages."},
			partials:   []string{"APartial"},
		},
		{
			name:       "ignored by central file and marked by package file",
			src:        "package x\n\ntype A struct{ B int }\n",
			central:    "packages:\n  $pkg:\n    types:\n      A:\n        ignore: true\n",
			pkg:        "types:\n  A:\n",
			overridden: []string{"opts.yaml: packages."},
			partials:   []string{"APartial"},
		},
		{
			name:    "all three",
			src:     "package x\n\n//kama:partial\ntype A struct{ B int }\n",
			central: "packages:\n  $pkg:\n    types:\n      A:\n        ignore: true\n",
			pkg:     "types:\n  A:\n",
			// The package option file wins over the central file, and the
			// files over the directive.
			overridden: []string{"opts.yaml: packages.", "/x.go:3:1"},
			partials:   []string{"APartial"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			docs := optionDocs{pkg: "version: 1\n" + c.pkg}
			if c.central != "" {
				docs.central = "version: 1\n" + c.central
			}

			files, warnings, err := generateWithOptions(t, map[string]string{"x.go": c.src}, docs)
			assert.NilError(t, err)
			assert.Equal(t, len(warnings), len(c.overridden), "%v", warnings)

			for i, w := range warnings {
				assert.ErrorIs(t, w, genopt.ErrOverridden)
				assert.ErrorContains(
					t,
					w,
					"type "+modulePath(
						t,
					)+".A: "+gentest.InlineDir(
						t,
					)+"/kama_partial.yaml: types.A overrides ",
				)
				assert.ErrorContains(t, w, c.overridden[i])
			}

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

// TestPackageOptionFilePackageEntry checks that the package entry of a
// package option file replaces that of the central option file with a
// warning, and that a package option file without package keys leaves it.
func TestPackageOptionFilePackageEntry(t *testing.T) {
	const src = "package x\n\ntype A struct{ B int }\n"

	files, warnings, err := generateWithOptions(t, map[string]string{"x.go": src}, optionDocs{
		central: "version: 1\npackages:\n  $pkg:\n    all_types: true\n",
		pkg:     "version: 1\nall_types: false\n",
	})
	assert.NilError(t, err)
	assert.Equal(t, len(files), 0)
	assert.Equal(t, len(warnings), 1, "%v", warnings)
	assert.ErrorIs(t, warnings[0], genopt.ErrOverridden)
	assert.ErrorContains(
		t,
		warnings[0],
		"package "+modulePath(
			t,
		)+": "+gentest.InlineDir(
			t,
		)+"/kama_partial.yaml overrides opts.yaml: ",
	)

	files, warnings, err = generateWithOptions(t, map[string]string{"x.go": src}, optionDocs{
		central: "version: 1\npackages:\n  $pkg:\n    all_types: true\n",
		pkg:     "version: 1\n",
	})
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)
	assert.Assert(t, len(files) > 0)
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

		i := newInstance(t, proc)
		assert.NilError(t, i.Param(ProcParamPartial{}))
		i.In(&procgopackages.Packages{Pkgs: load(pkgs...)})
		i.Out(f)

		return i.Run(ctx, gentest.NopReporter{})
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
}

// TestProcUnknownKeyWarning checks that Proc reports an unknown key of a
// package option file as a warning and applies the rest of the file.
func TestProcUnknownKeyWarning(t *testing.T) {
	e := newProcEnv(t, map[string]string{
		"x.go": "package x\n\ntype A struct{ B int }\n",
		"kama_partial.yaml": "version: 1\npackages:\ntypes:\n  A:\n    fields:\n" +
			"      B:\n        flag:\n          name: b\n          prefix: x\n",
	})

	r := e.run(gentest.Now())
	assert.Equal(t, len(r.Warnings), 2, "%v", r.Warnings)

	for _, w := range r.Warnings {
		assert.ErrorIs(t, w, genopt.ErrUnknownKey)
	}

	assert.ErrorContains(t, r.Warnings[0], e.dir+"/kama_partial.yaml: packages")
	assert.ErrorContains(t, r.Warnings[1], e.dir+"/kama_partial.yaml: types.A.fields.B.flag.prefix")
	assert.Assert(t, strings.Contains(e.read("x.kama_partial.go"), "RegisterFlags"))
}

// TestProcOverrideWarning checks that Proc reports the warnings of the
// loader.
func TestProcOverrideWarning(t *testing.T) {
	e := newProcEnv(t, map[string]string{
		"x.go":              "package x\n\n//kama:partial\ntype A struct{ B int }\n",
		"kama_partial.yaml": "version: 1\ntypes:\n  A:\n",
	})

	r := e.run(gentest.Now())
	assert.Equal(t, len(r.Warnings), 1, "%v", r.Warnings)
	assert.ErrorIs(t, r.Warnings[0], genopt.ErrOverridden)
	assert.ErrorContains(t, r.Warnings[0], e.dir+"/kama_partial.yaml: types.A overrides ")
}
