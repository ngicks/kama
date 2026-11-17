package partial

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

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
			name: "second flag",
			src: head + "//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:flag name=b\n\tB int //kama:partial:flag name=c\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:6:8", "x.go:5:2", "second", "A.B"},
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
