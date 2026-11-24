package jsonschema

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ngicks/opiter"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// TestGenerateDirectives checks the directives the validation keywords
// give: each value spelled on one line and quoted, so that no text of the
// schema starts a comment line of its own.
func TestGenerateDirectives(t *testing.T) {
	long := strings.Repeat("a", 120)

	cases := []struct {
		name   string
		schema string
		want   []string
	}{
		{
			"dependentRequired of an optional and of a required member",
			`{"type":"object","required":["k"],"properties":{"k":{},"d":{},"o":{},"e":{}},
			"dependentRequired":{"k":["d"],"o":["e","k"]}}`,
			[]string{
				"\t//kama:validate required\n\tD ",
				"\t//kama:validate requires=`\"e\"`\n\tO ",
			},
		},
		{
			"hostile pattern",
			`{"type":"string","pattern":"a\n//go:generate evil"}`,
			[]string{`//kama:validate regexp="a\n//go:generate evil"` + "\n"},
		},
		{
			"hostile enum value",
			`{"type":"object","properties":{"s":{"const":"x\n//go:generate evil"}}}`,
			[]string{"//kama:validate const=`\"x\\n//go:generate evil\"`\n"},
		},
		{
			"long directive",
			`{"type":"string","pattern":"` + long + `"}`,
			[]string{"//kama:validate regexp=\"" + long + "\"\n"},
		},
		{
			"root over a union",
			`{"$ref":"#/$defs/u","$defs":{"u":{"oneOf":[{"type":"string"},{"type":"integer"}]}}}`,
			[]string{"type Root U\n", "if err := jsonschemart.Validate(out, false); err != nil {"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc jsonschema.Schema
			assert.NilError(t, jsonv2.Unmarshal([]byte(tc.schema), &doc))

			files, _, err := Generate(&doc, Options{Package: "p", TypeName: "Root", Validate: true})
			assert.NilError(t, err)

			content := string(files[0].Content)
			for _, want := range tc.want {
				assert.Assert(t, strings.Contains(content, want), "want %q in\n%s", want, content)
			}

			assert.Assert(t, !strings.Contains(content, "\n//go:generate"), content)
		})
	}
}

// TestGenerateWarnsUnenforced checks the warning of each validation
// keyword the generated code does not check, named by its pointer.
func TestGenerateWarnsUnenforced(t *testing.T) {
	var doc jsonschema.Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(`{
		"type":"object",
		"dependentRequired":{"a":["zz"]},
		"properties":{
			"a":{"minimum":1},
			"b":{"type":"string","pattern":"(?<=x)","minLength":1},
			"c":{"type":"array","items":{"$ref":"#/$defs/n","maximum":3}},
			"u":{"oneOf":[{"type":"string"},{"type":"integer"}],"minLength":2}
		},
		"$defs":{"n":{"type":"integer"}}
	}`), &doc))

	_, warnings, err := Generate(&doc, Options{Package: "p", TypeName: "Root"})
	assert.NilError(t, err)

	got := opiter.WrapSeq(slices.Values(warnings)).
		Map(func(err error) string {
			assert.Assert(t, errors.Is(err, ErrUnenforced), "%v", err)
			return err.Error()
		}).
		CollectSlice()

	assert.DeepEqual(t, got, []string{
		"jsonschema: keyword not enforced: #/dependentRequired",
		"jsonschema: keyword not enforced: #/properties/a/minimum",
		"jsonschema: keyword not enforced: #/properties/b/pattern",
		"jsonschema: keyword not enforced: #/properties/c/items/maximum",
		"jsonschema: keyword not enforced: #/properties/u/minLength",
	})
}
