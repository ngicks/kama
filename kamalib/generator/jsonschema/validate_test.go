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
			"raw property required by a key and requiring a member",
			`{"type":"object","required":["a"],"dependentRequired":{"a":["b"]},
			"properties":{"a":{"type":"object"},"b":{"required":["x"]}}}`,
			[]string{"\t//kama:validate and(required=`\"x\"`)\n\t//kama:validate required\n"},
		},
		{
			"raw JSON text of a schema of every kind",
			`{"minimum":1,"maxLength":2,"minProperties":1,"uniqueItems":true,
			"dependentRequired":{"a":["b","c"],"d":["e"]}}`,
			[]string{
				"//kama:validate cmp=\">=1\"\n//kama:validate runes=\"<=2\"\n" +
					"//kama:validate unique\n//kama:validate members=\">=1\"\n",
				"//kama:validate and(if(required=`\"a\"`, required=`\"b\" \"c\"`), " +
					"if(required=`\"d\"`, required=`\"e\"`))\n",
				"type Root jsontext.Value\n",
				"func (v Root) RawJson() jsontext.Value {",
			},
		},
		{
			"raw JSON text of several kinds and of mixed values",
			`{"type":"object","properties":{
				"k":{"type":["array","object"],"maxItems":1},
				"e":{"enum":[1,"a",null,{"b":[]}]}
			}}`,
			[]string{
				"\t//kama:validate kind=\"array object\"\n\t//kama:validate length=\"<=1\"\n\tK ",
				"\t//kama:validate const=\"1 `\\\"a\\\"` null `{\\\"b\\\":[]}`\"\n\tE ",
			},
		},
		{
			"null-only schema of a property named like a union keyword",
			`{"type":"object","properties":{"oneOf":{"type":"array","items":{"type":"null"}}}}`,
			[]string{
				"//kama:generated kama:jsonschema\n//kama:validate kind=\"null\"\n" +
					"//kama:validate decoded\ntype RootOneOfItem ",
			},
		},
		{
			"properties named like methods",
			`{"type":"object","required":["validate"],"properties":{
				"validate":{"type":"string"},"additionalProperties":{"type":"integer"}
			}}`,
			[]string{
				"plain{ValidateField: v.ValidateField, " +
					"AdditionalPropertiesField: v.AdditionalPropertiesField, AdditionalProperties: v._x}",
				"out.ValidateField, err = jsonschemart.Required(w.ValidateField)",
			},
		},
		{
			"empty member name in the validators of raw JSON text",
			`{"not":{"properties":{"":{"uniqueItems":true}}},"required":[""],
			"dependentRequired":{"":["a"]},"patternProperties":{"^$":{"type":"string"}}}`,
			[]string{
				"//kama:validate required=`\"\"`\n",
				"//kama:validate if(required=`\"\"`, required=`\"a\"`)\n",
				`//kama:validate props(match("^$", kind="string"))` + "\n",
				`//kama:validate not(props(field("", unique)))` + "\n",
			},
		},
		{
			"const next to enum in the validators of raw JSON text",
			`{"const":{"a":1},"enum":[{"a":2}]}`,
			[]string{"//kama:validate and(const=\"`{\\\"a\\\":1}`\", const=\"`{\\\"a\\\":2}`\")\n"},
		},
		{
			"uniqueItems of elements that are no Go scalars",
			`{"type":"object","properties":{
				"n":{"type":"array","items":{"type":"number"},"uniqueItems":true},
				"o":{"type":"array","items":{"type":"object"},"uniqueItems":true},
				"p":{"type":"array","prefixItems":[{"type":"number"}],"uniqueItems":true}
			}}`,
			[]string{
				"\t//kama:validate unique\n\tN ",
				"\t//kama:validate json(unique)\n\tO ",
				"\t//kama:validate json(unique, prefix(kind=\"number\"))\n\tP ",
			},
		},
		{
			"uniqueItems of a nullable array, also holding itself",
			`{"type":"object","properties":{
				"a":{"type":["array","null"],"items":{"type":"string"},"uniqueItems":true},
				"b":{"$ref":"#/$defs/n"}
			},"$defs":{"n":{"type":["array","null"],"items":{"$ref":"#/$defs/n"},"uniqueItems":true}}}`,
			[]string{
				"\t//kama:validate unique\n\tA ",
				"//kama:validate unique\n//kama:validate items(ref=N)\n",
			},
		},
		{
			"references under unevaluatedProperties",
			unevaluatedChain(2),
			[]string{
				"//kama:validate unevalprops(ref=D0, or())\n",
				"//kama:validate and(ref=D1, and(props(field(\"p0\")), ref=D1))\n",
				"//kama:validate props(field(\"z\"))\n",
			},
		},
		{
			"typed properties under unevaluatedProperties",
			`{"type":"object","properties":{"next":{"$ref":"#"},"v":{"type":"integer"}},
			"unevaluatedProperties":false}`,
			[]string{
				`//kama:validate json(unevalprops(and(kind="object", ` +
					`props(field("next"), field("v"))), or()))`,
			},
		},
		{
			"probes of union members",
			`{"oneOf":[{"$ref":"#/$defs/a"},{"$ref":"#/$defs/b"}],"$defs":{
				"a":{"type":"object","properties":{"n":{"$ref":"#"},"a":{"minimum":0}}},
				"b":{"type":"object","minProperties":2,"properties":{"n":{"$ref":"#"}}}
			}}`,
			[]string{
				"//kama:generated kama:jsonschema\n//kama:validate props(field(\"a\", cmp=\">=0\"))\n" +
					"type rootAProbe jsontext.Value\n",
				"//kama:generated kama:jsonschema\n//kama:validate members=\">=2\"\n" +
					"type rootBProbe jsontext.Value\n",
				"if err := jsonschemart.Validate(rootAProbe(raw), false); err != nil {",
			},
		},
		{
			"root over a union",
			`{"$ref":"#/$defs/u","$defs":{"u":{"oneOf":[{"type":"string"},{"type":"integer"}]}}}`,
			[]string{
				"type Root U\n",
				"if err := jsonschemart.ValidateShallow(out, false); err != nil {",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc jsonschema.Schema
			assert.NilError(t, jsonv2.Unmarshal([]byte(tc.schema), &doc))

			files, _, err := generateFiles(
				&doc,
				Options{Package: "p", TypeName: "Root", Validate: true},
			)
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
			"a":{"minimum":1,"pattern":"(?<=y)"},
			"b":{"type":"string","pattern":"(?<=x)","minLength":1},
			"c":{"type":"array","items":{"$ref":"#/$defs/n","maximum":3}},
			"u":{"oneOf":[{"type":"string"},{"type":"integer"}],"minLength":2}
		},
		"$defs":{"n":{"type":"integer"}}
	}`), &doc))

	_, warnings, err := generateFiles(&doc, Options{Package: "p", TypeName: "Root"})
	assert.NilError(t, err)

	got := opiter.WrapSeq(slices.Values(warnings)).
		Map(func(err error) string {
			assert.Assert(t, errors.Is(err, ErrUnenforced), "%v", err)
			return err.Error()
		}).
		CollectSlice()

	assert.DeepEqual(t, got, []string{
		"jsonschema: keyword not enforced: #/properties/a/pattern",
		"jsonschema: keyword not enforced: #/properties/b/pattern",
	})
}
