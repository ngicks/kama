package partial

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestFieldMaskErrors(t *testing.T) {
	const head = "package x\n\n"

	cases := []struct {
		name     string
		src      string
		is       error
		contains []string
		// excludes are substrings the error must not hold.
		excludes []string
	}{
		{
			name:     "free-floating",
			src:      head + "//kama:partial:fieldmask\n\n//kama:partial\ntype A struct{ B int }\n",
			is:       ErrDirective,
			contains: []string{"x.go:3:1", "applies to types and struct fields only"},
		},
		{
			name:     "args on a type",
			src:      head + "//kama:partial\n//kama:partial:fieldmask json\ntype A struct{ B int }\n",
			is:       ErrDirective,
			contains: []string{"x.go:4:1", `unknown args "json"`, "_args_on_a_type.A; want none"},
		},
		{
			name: "args on a second line on a type",
			src: head + "//kama:partial\n//kama:partial:fieldmask\n//kama:partial:fieldmask json\n" +
				"type A struct{ B int }\n",
			is:       ErrDirective,
			contains: []string{"x.go:4:1", `unknown args "json"`},
		},
		{
			name:     "type without partial variant",
			src:      head + "//kama:partial:fieldmask\ntype A struct{ B int }\n",
			is:       ErrDirective,
			contains: []string{"x.go:3:1", "A has no partial variant"},
		},
		{
			name:     "slice target",
			src:      head + "//kama:partial\n//kama:partial:fieldmask\ntype A []int\n",
			is:       ErrDirective,
			contains: []string{"x.go:4:1", "field masks apply to struct targets only"},
		},
		{
			name: "name on a type without field masks",
			src: head + "//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:fieldmask name=b\n\tB int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:5:2", "masks.A.B", "A does not have field masks enabled"},
		},
		{
			name: "name on a type without partial variant",
			src: head + "//kama:partial\ntype A struct{}\n\n" +
				"type C struct {\n\t//kama:partial:fieldmask name=b\n\tB int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:7:2", "C has no partial variant"},
		},
		{
			name: "missing name",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\t//kama:partial:fieldmask\n\tB int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:6:2", "on field", "name is required"},
		},
		{
			name: "unknown key",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\t//kama:partial:fieldmask name=b json=c\n\tB int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:6:2", `unknown key "json"; want name`},
		},
		{
			name: "dotted name",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\t//kama:partial:fieldmask name=b.c\n\tB int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:6:2", `name "b.c"`, "without a dot"},
		},
		{
			name: "name on two lines of a field",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\t//kama:partial:fieldmask name=b\n\tB int //kama:partial:fieldmask name=c\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:6:2", `duplicate name "name"`},
		},
		{
			name: "blank field",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\t//kama:partial:fieldmask name=b\n\t_ int\n\tC int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:6:2", "a blank field has no path"},
		},
		{
			name: "unknown member fallback",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\t//kama:partial:fieldmask name=b\n\tB map[string]any `json:\",embed\"`\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:6:2", "a field catching unknown members has no path"},
		},
		{
			name: "name on an embedded partial",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\t//kama:partial:fieldmask name=b\n\tB\n}\n\n" +
				"//kama:partial:fieldmask\ntype B struct{ I int }\n",
			is:       ErrDirective,
			contains: []string{"x.go:6:2", "embedded partial value", "no path segment of its own"},
		},
		{
			name: "duplicate segment",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\tB int\n\t//kama:partial:fieldmask name=B\n\tC int\n}\n",
			is: ErrDirective,
			contains: []string{
				"segment.A (", `segment "B" of field A.C`, "x.go:8:2", "taken by field A.B (",
				"x.go:6:2",
			},
		},
		{
			name: "duplicate segment across an embedded partial",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\tE\n\tI int\n}\n\n//kama:partial:fieldmask\ntype E struct{ I int }\n",
			is: ErrDirective,
			contains: []string{
				`segment "I" of field A.I`, "x.go:7:2", "taken by field E.I (", "x.go:11:16",
			},
		},
		{
			name: "duplicate segment across an embedded pointer",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\t//kama:partial:fieldmask name=J\n\tI int\n\t*E\n}\n\n" +
				"//kama:partial:fieldmask\ntype E struct{ J int }\n",
			is:       ErrDirective,
			contains: []string{`segment "J" of field E.J`, "taken by field A.I ("},
		},
		{
			name: "embedded without field masks",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\t*E\n}\n\ntype E struct{ I int }\n",
			is: ErrDirective,
			contains: []string{
				"without_field_masks.A (", "x.go:6:3", "embedded ", "without_field_masks.E",
				"paths of A",
			},
		},
		{
			name: "embedded in a type without field masks",
			src: head + "//kama:partial\ntype A struct {\n\tE\n}\n\n" +
				"//kama:partial:fieldmask\ntype E struct{ I int }\n",
			is: ErrDirective,
			contains: []string{
				"without_field_masks.A (", "x.go:5:2", "embedded ",
				"without_field_masks.E has field masks enabled",
				"so A needs //kama:partial:fieldmask too",
			},
		},
		{
			name: "embedded pointer in a type without field masks",
			src: head + "//kama:partial\ntype A struct {\n\tI int\n\t*E\n}\n\n" +
				"//kama:partial:fieldmask\ntype E struct{ J int }\n",
			is: ErrDirective,
			contains: []string{
				"x.go:6:3", "field_masks.E has field masks enabled",
				"so A needs //kama:partial:fieldmask too",
			},
		},
		{
			name: "embedded value without field masks",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\tE\n}\n\ntype E struct{ I int }\n",
			is: ErrDirective,
			contains: []string{
				"x.go:6:2", "field_masks.E does not have field masks enabled", "paths of A",
			},
		},
		{
			name: "embedded two levels deep without field masks",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\tB\n}\n\n//kama:partial:fieldmask\ntype B struct {\n\t*C\n}\n\n" +
				"type C struct{ I int }\n",
			is: ErrDirective,
			contains: []string{
				"x.go:11:3", "field_masks.B (", "field_masks.C does not have field masks enabled",
				"paths of B",
			},
		},
		{
			name: "embedded two levels deep below a type without field masks",
			src: head + "//kama:partial\ntype A struct {\n\tB\n}\n\n" +
				"//kama:partial:fieldmask\ntype B struct {\n\t*C\n}\n\n" +
				"//kama:partial:fieldmask\ntype C struct{ I int }\n",
			is: ErrDirective,
			contains: []string{
				"x.go:5:2", "field_masks.B has field masks enabled",
				"so A needs //kama:partial:fieldmask too",
			},
		},
		{
			name: "embedded two levels deep through a type without field masks",
			src: head + "//kama:partial\ntype A struct {\n\t*B\n}\n\n" +
				"type B struct {\n\tC\n}\n\n" +
				"//kama:partial:fieldmask\ntype C struct{ I int }\n",
			is: ErrDirective,
			contains: []string{
				"x.go:9:2", "field_masks.C has field masks enabled",
				"so B needs //kama:partial:fieldmask too",
			},
			excludes: []string{"so A needs"},
		},
		{
			name: "duplicate segment across two levels of embedded partials",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\tI int\n\tB\n}\n\n//kama:partial:fieldmask\ntype B struct{ C }\n\n" +
				"//kama:partial:fieldmask\ntype C struct {\n" +
				"\t//kama:partial:fieldmask name=I\n\tJ int\n}\n",
			is: ErrDirective,
			contains: []string{
				`segment "I" of field C.J`, "x.go:16:2", "taken by field A.I (", "x.go:6:2",
			},
		},
		{
			name: "field named like a method",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
				"\tFieldMask []string\n}\n",
			is:       ErrConflict,
			contains: []string{"collides with the method FieldMask"},
		},
		{
			name: "method declared by hand",
			src: head + "//kama:partial\n//kama:partial:fieldmask\ntype A struct{ B int }\n\n" +
				"//kama:generated kama-partial\ntype APartial struct{}\n\n" +
				"func (APartial) FromValueWithMask() {}\n",
			is:       ErrConflict,
			contains: []string{"x.go:10:17", "method FromValueWithMask of"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"x.go": c.src}, Options{})
			assert.ErrorIs(t, err, c.is)

			for _, s := range c.contains {
				assert.ErrorContains(t, err, s)
			}

			for _, s := range c.excludes {
				assert.Assert(t, !strings.Contains(err.Error(), s), "%q in %v", s, err)
			}
		})
	}
}

// TestFieldMaskEmbeddingByOptionFile checks that field masks enabled by the
// option file count for the rule that an embedding type and its embedded
// partial values agree on field masks.
func TestFieldMaskEmbeddingByOptionFile(t *testing.T) {
	const src = "package x\n\n//kama:partial\ntype A struct {\n\t*E\n}\n\n" +
		"type E struct{ I int }\n"

	t.Run("embedded only", func(t *testing.T) {
		_, _, err := generateWithOptions(t, map[string]string{"x.go": src}, optionDocs{
			central: "version: 1\npackages:\n  $pkg:\n    types:\n" +
				"      E:\n        fieldmask: true\n",
		})
		assert.ErrorIs(t, err, ErrDirective)
		assert.ErrorContains(t, err, "so A needs //kama:partial:fieldmask too")
	})
	t.Run("embedding by directive", func(t *testing.T) {
		src := strings.Replace(
			src,
			"//kama:partial\n",
			"//kama:partial\n//kama:partial:fieldmask\n",
			1,
		)
		files := generatedWithOptions(t, map[string]string{"x.go": src}, optionDocs{
			central: "version: 1\npackages:\n  $pkg:\n    types:\n" +
				"      E:\n        fieldmask: true\n",
		})
		fs := funcs(t, files["x.kama_partial.go"])
		assert.Assert(t, fs["APartial.FromValueWithMask"] != "")
		assert.Assert(t, fs["EPartial.FromValueWithMask"] != "")
	})
	t.Run("both by option file", func(t *testing.T) {
		// The entry of A marks it, and replaces the directive on A.
		src := strings.Replace(src, "//kama:partial\n", "", 1)
		files := generatedWithOptions(t, map[string]string{"x.go": src}, optionDocs{
			central: "version: 1\npackages:\n  $pkg:\n    types:\n" +
				"      A:\n        fieldmask: true\n      E:\n        fieldmask: true\n",
		})
		fs := funcs(t, files["x.kama_partial.go"])
		assert.Assert(t, fs["APartial.FromValueWithMask"] != "")
	})
}

// TestFieldMaskOptIn checks that a type without field masks gets neither
// the methods nor their imports, and that fields named like the methods are
// left to it.
func TestFieldMaskOptIn(t *testing.T) {
	const src = "package x\n\n//kama:partial\ntype A struct {\n" +
		"\tFieldMask         []string\n\tFromValueWithMask int\n\tB B\n}\n\n" +
		"//kama:partial:fieldmask\ntype B struct{ I int }\n"

	files := generated(t, map[string]string{"a.go": src})
	out := files["a.kama_partial.go"]
	fs := funcs(t, out)
	_, ok := fs["APartial.FieldMask"]
	assert.Assert(t, !ok, out)
	_, ok = fs["APartial.FromValueWithMask"]
	assert.Assert(t, !ok, out)
	assert.Assert(t, fs["BPartial.FieldMask"] != "", out)
	assert.Assert(t, fs["BPartial.FromValueWithMask"] != "", out)

	files = generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ B int }\n",
	})

	out = files["a.kama_partial.go"]
	for _, s := range []string{"FieldMask", "FromValueWithMask", `"slices"`, `"strings"`} {
		assert.Assert(t, !strings.Contains(out, s), "%s in\n%s", s, out)
	}
}

func TestFieldMaskMethods(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n" +
			"\t//kama:partial:fieldmask name=b\n\tB int\n\tC *C\n}\n\n" +
			"//kama:partial:fieldmask\ntype C struct{ D int }\n",
	})
	fs := funcs(t, files["a.kama_partial.go"])
	assert.Equal(t, fs["APartial.FieldMask"], `func (p APartial) FieldMask() []string {
	var paths []string
	if !p.B.IsUndefined() {
		paths = append(paths, "b")
	}
	if p.C.IsNull() {
		paths = append(paths, "C")
	}
	for _, path := range p.C.Value().FieldMask() {
		paths = append(paths, "C."+path)
	}
	slices.Sort(paths)
	return paths
}`)

	got := fs["APartial.FromValueWithMask"]
	for _, s := range []string{
		"func (p *APartial) FromValueWithMask(v A, paths []string) error {\n\tvar x APartial\n",
		"\t\tcase \"b\":\n\t\t\tif nested {\n",
		"\t\t\tx.B = und.Defined(v.B)\n",
		"\t\t\terr := y.FromValueWithMask(*new(C), subC)\n",
		"\t\t\tx.C = und.Null[CPartial]()\n",
		"\t\treturn fmt.Errorf(\"%s.%w\", \"C\", err)\n",
		"\t*p = x\n\treturn nil\n}",
	} {
		assert.Assert(t, strings.Contains(got, s), "%q in\n%s", s, got)
	}
}

// TestFieldMaskBuild builds the methods of struct targets without fields
// to address, with fields of every kind and with branches and embedded
// partial values of another package.
func TestFieldMaskBuild(t *testing.T) {
	assert.NilError(t, generateAndBuild(t, map[string]string{
		"a.go": `package x

import (
	"github.com/ngicks/option/opt"

	"` + modulePath(t) + `/sub"
)

//kama:partial
//kama:partial:fieldmask
type Empty struct{}

//kama:partial
//kama:partial:fieldmask
type Leaves struct {
	_ int
	O opt.Option[int]
	S []Empty
	L List
	M Map
}

type List []Empty

type Map map[string]*Empty

//kama:partial
//kama:partial:fieldmask
type EmbedsEmpty struct {
	Empty
	*Leaves
}

//kama:partial
//kama:partial:fieldmask
type Foreign struct {
	V sub.V
	P *sub.V
	G sub.G[int]
	*sub.E
}
`,
		"sub/sub.go": `package sub

//kama:partial
//kama:partial:fieldmask
type V struct{ N int }

//kama:partial
//kama:partial:fieldmask
type G[T any] struct{ T *T }

//kama:partial
//kama:partial:fieldmask
type E struct{ S string }
`,
	}))
}
