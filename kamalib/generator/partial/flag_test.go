package partial

import (
	"encoding/json/v2"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// parseFlag decodes args as the args of //kama:partial:flag.
func parseFlag(args string) (flagOption, error) {
	kv, err := dircmnt.ParseArgsKeyValue(args)
	if err != nil {
		return flagOption{}, err
	}

	return flagFromKeyValues(kv)
}

func TestParseFlag(t *testing.T) {
	o, err := parseFlag(`name=dry-run short=d usage="do \"nothing\""`)
	assert.NilError(t, err)
	assert.Equal(t, o.name, "dry-run")
	assert.Equal(t, o.short, "d")
	assert.Equal(t, o.usage, `do "nothing"`)

	for args, want := range map[string]string{
		"":                   "name is required",
		"name":               "name is required",
		"short=a":            "name is required",
		"name=a prefix=x":    `unknown key "prefix"`,
		"name=-a":            "leading -",
		"name=a=b":           "no =",
		`name="a\u00a0b"`:    `name "a\u00a0b"`,
		`name="a\u0085b"`:    `name "a\u0085b"`,
		`name="a\u009fb"`:    `name "a\u009fb"`,
		"name=a short=ab":    `short "ab"`,
		"name=a short=":      `short ""`,
		"name=a short=-":     `short "-"`,
		"name=a short=\"é\"": "ASCII",
		"name=a name=b":      "duplicate",
		`name=a usage="x`:    "unterminated",
	} {
		_, err := parseFlag(args)
		assert.ErrorContains(t, err, want, args)
	}
}

func TestFlagErrors(t *testing.T) {
	const head = "package x\n\n"

	cases := []struct {
		name     string
		src      string
		is       error
		contains []string
	}{
		{
			name: "missing name",
			src: head + "//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:flag short=a\n\tB int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:5:2", "_missing_name.A.B", "name is required"},
		},
		{
			name: "unknown key",
			src: head + "//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:flag name=b x=1\n\tB int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:5:2", "A.B", `unknown key "x"`},
		},
		{
			name: "name on two lines",
			src: head + "//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:flag name=b\n\tB int //kama:partial:flag name=c\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:5:2", `duplicate name "name"`, "A.B"},
		},
		{
			name:     "on a type",
			src:      head + "//kama:partial\n//kama:partial:flag name=a\ntype A struct{}\n",
			is:       ErrDirective,
			contains: []string{"x.go:4:1", "struct fields only"},
		},
		{
			name: "on a type without partial variant",
			src: head + "//kama:partial\ntype A struct{}\n\n" +
				"type B struct {\n\t//kama:partial:flag name=c\n\tC int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:7:2", "_partial_variant.B.C", "B has no partial variant"},
		},
		{
			name: "duplicate name",
			src: head + "//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:flag name=n\n\tB int\n\t//kama:partial:flag name=n\n\tC int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:7:2", "A.C", `flag name "n"`, "field B (", "x.go:5:2"},
		},
		{
			name: "duplicate short",
			src: head + "//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:flag name=b short=s\n\tB int\n" +
				"\t//kama:partial:flag name=c short=s\n\tC int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:7:2", `flag shorthand "s"`, "field B ("},
		},
		{
			name: "unsupported type",
			src: head + "//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:flag name=b\n\tB []int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:5:2", "_unsupported_type.A.B", "unsupported type []int"},
		},
		{
			name: "unsupported int32",
			src: head + "//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:flag name=b\n\tB int32\n}\n",
			is:       ErrDirective,
			contains: []string{"unsupported type int32"},
		},
		{
			name: "nested partial",
			src: head + "//kama:partial\ntype A struct {\n\t//kama:partial:flag name=b\n\tB B\n}\n\n" +
				"type B struct{ I int }\n",
			is:       ErrDirective,
			contains: []string{"x.go:5:2", "A.B", "nested partial"},
		},
		{
			name: "embedded partial",
			src: head + "//kama:partial\ntype A struct {\n\t//kama:partial:flag name=b\n\tB\n}\n\n" +
				"type B struct{ I int }\n",
			is:       ErrDirective,
			contains: []string{"x.go:5:2", "nested partial"},
		},
		{
			name: "pointer to option",
			src: head + "import \"github.com/ngicks/option/opt\"\n\n" +
				"//kama:partial\ntype A struct {\n\t//kama:partial:flag name=b\n\tB *opt.Option[int]\n}\n",
			is:       ErrDirective,
			contains: []string{"unsupported type"},
		},
		{
			name: "elastic",
			src: head + "import \"github.com/ngicks/option/elastic\"\n\n" +
				"//kama:partial\ntype A struct {\n\t//kama:partial:flag name=b\n" +
				"\tB elastic.Elastic[int]\n}\n",
			is: ErrDirective,
			contains: []string{
				"x.go:7:2",
				"A.B",
				"unsupported type github.com/ngicks/option/elastic.Elastic[int]",
			},
		},
		{
			name: "blank field",
			src: head + "//kama:partial\ntype A struct {\n\t//kama:partial:flag name=b\n\t_ int\n" +
				"\tC int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:5:2", "A._", "a blank field cannot be set"},
		},
		{
			name: "field named like a flag method",
			src: head + "//kama:partial\ntype A struct {\n\t//kama:partial:flag name=b\n\tB int\n" +
				"\tRegisterFlags int\n}\n",
			is:       ErrConflict,
			contains: []string{"method RegisterFlags"},
		},
		{
			name: "flag set declared",
			src: head + "//kama:partial\ntype A struct {\n\t//kama:partial:flag name=b\n\tB int\n}\n\n" +
				"type PartialFlagSet int\n",
			is:       ErrConflict,
			contains: []string{"x.go:9:6", "PartialFlagSet"},
		},
		{
			name:     "flag set declared without flags",
			src:      head + "//kama:partial\ntype A struct {\n\tB int\n}\n\ntype PartialFlagSet int\n",
			is:       ErrConflict,
			contains: []string{"x.go:8:6", "PartialFlagSet"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"x.go": c.src}, Options{})
			assert.ErrorIs(t, err, c.is)

			for _, s := range c.contains {
				assert.ErrorContains(t, err, s)
			}
		})
	}
}

func TestFlagSetFile(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ B int }\n",
	})
	assert.DeepEqual(t, slices.Sorted(maps.Keys(files)), []string{
		"a.kama_partial.go", FlagSetFile,
	})
	assert.Assert(t, !strings.Contains(files["a.kama_partial.go"], "RegisterFlags"))
	unflagged := files[FlagSetFile]

	files = generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct {\n" +
			"\t//kama:partial:flag name=b\n\tB string\n\tC int\n}\n",
	})
	assert.DeepEqual(t, slices.Sorted(maps.Keys(files)), []string{
		"a.kama_partial.go", FlagSetFile,
	})
	src := files[FlagSetFile]
	assert.Equal(t, src, unflagged)
	assert.Assert(t, strings.HasPrefix(src, testHeader+"\n\npackage x\n\nimport \"time\"\n"), src)
	assert.Assert(
		t,
		strings.Contains(src, "\n//kama:generated kama-partial\ntype PartialFlagSet interface {\n"+
			"\tBoolP(name, shorthand string, value bool, usage string) *bool\n"+
			"\tChanged(name string) bool\n"+
			"\tDurationP(name, shorthand string, value time.Duration, usage string) *time.Duration\n"),
		src,
	)

	for name := range flagSetSignatures() {
		assert.Assert(t, strings.Contains(src, "\t"+name+"("), name)
	}

	fs := funcs(t, files["a.kama_partial.go"])
	assert.Equal(
		t,
		fs["APartial.RegisterFlags"],
		"func (p *APartial) RegisterFlags(f PartialFlagSet) {\n"+
			"\tf.StringP(\"b\", \"\", \"\", \"\")\n}",
	)
	assert.Assert(
		t,
		!strings.Contains(fs["APartial.FromFlagSet"], `"c"`),
		fs["APartial.FromFlagSet"],
	)
}

// TestFlagSetFileBuildVariant generates without the build tag of a file
// whose partial type has a duration flag; the flag set must keep the
// methods that file calls.
func TestFlagSetFileBuildVariant(t *testing.T) {
	files := map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ B int }\n",
		"b.go": "//go:build kamavariant\n\npackage x\n\nimport \"time\"\n\n" +
			"//kama:partial\ntype B struct {\n\t//kama:partial:flag name=wait\n\tWait time.Duration\n}\n",
	}
	out := generated(t, files)
	_, ok := out["b.kama_partial.go"]
	assert.Assert(t, !ok)

	src := out[FlagSetFile]
	assert.Assert(t, strings.Contains(src, "\tDurationP("), src)
	assert.Assert(t, strings.Contains(src, "\tGetDuration("), src)
}

func TestFlagSetFileConflict(t *testing.T) {
	_, err := generate(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct {\n" +
			"\t//kama:partial:flag name=b\n\tB string\n}\n",
		FlagSetFile: "package x\n\nfunc stray() {}\n",
	}, Options{})
	assert.ErrorIs(t, err, ErrConflict)
	assert.ErrorContains(t, err, FlagSetFile+" holds a declaration")
}

// TestFlagSetMarkedVar takes a package-level variable named like the flag
// set as generated when it carries the marker, as any marked declaration.
func TestFlagSetMarkedVar(t *testing.T) {
	// The marked variable makes a.go look like a stale generated file, which
	// only warns.
	_, _, err := Generate(load(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ B int }\n\n" +
			"// PartialFlagSet is stale.\n//\n//kama:generated kama-partial\nvar PartialFlagSet int\n",
	}), Options{Registry: registry(t), Now: testNow})
	assert.NilError(t, err)

	_, err = generate(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ B int }\n\nvar PartialFlagSet int\n",
	}, Options{})
	assert.ErrorIs(t, err, ErrConflict)
	assert.ErrorContains(t, err, "PartialFlagSet is already declared for the flag set")
}

// TestFlagSetOmitted leaves the flag set file out on request, and rejects
// a flag option then, since its methods take the flag set.
func TestFlagSetOmitted(t *testing.T) {
	files := generatedWith(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ N int }\n",
	}, Options{OmitFlagSet: true})
	assert.DeepEqual(t, slices.Sorted(maps.Keys(files)), []string{"a.kama_partial.go"})

	_, err := generate(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct {\n" +
			"\t//kama:partial:flag name=n\n\tN int\n}\n",
	}, Options{OmitFlagSet: true})
	assert.ErrorIs(t, err, ErrOption)
	assert.ErrorContains(t, err, "TestFlagSetOmitted.A.N")
	assert.ErrorContains(t, err, "flag_set")
}

// TestProcParamDefaults merges the param of a spec onto DefaultProcParam: a
// key left out takes its default, and a key that is set wins, false too.
func TestProcParamDefaults(t *testing.T) {
	i := &instance{}
	assert.NilError(t, i.Param(ProcParamPartial{}))
	assert.Equal(t, i.opts.Merge, MergeOverwrite)
	assert.Equal(t, i.opts.MergeNested, MergeNestedRecursive)
	assert.Assert(t, !i.opts.OmitFlagSet)
	assert.Assert(t, i.optionFile == nil)

	assert.NilError(t, i.Param(ProcParamPartial{
		MergeStrategy: und.Defined("append"),
		FlagSet:       und.Defined(false),
	}))
	assert.Equal(t, i.opts.Merge, MergeAppend)
	assert.Equal(t, i.opts.MergeNested, MergeNestedRecursive)
	assert.Assert(t, i.opts.OmitFlagSet)
}

// TestProcParamFlagSetFromSpec decodes the param of a spec as the workflow
// does: flag_set left out keeps the flag set file, and false leaves it out.
func TestProcParamFlagSetFromSpec(t *testing.T) {
	for param, omit := range map[string]bool{
		`{}`:                 false,
		`{"flag_set":true}`:  false,
		`{"flag_set":false}`: true,
	} {
		var p ProcParamPartial
		assert.NilError(t, json.Unmarshal([]byte(param), &p), param)

		i := &instance{}
		assert.NilError(t, i.Param(p), param)
		assert.Equal(t, i.opts.OmitFlagSet, omit, param)
	}
}
