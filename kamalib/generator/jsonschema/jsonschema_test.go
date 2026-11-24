package jsonschema

import (
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

func TestGenerateRejects(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		err    error
		at     string
	}{
		{
			"if then else",
			`{"type":"object","properties":{"a":{"if":{},"then":{},"else":{}}}}`,
			ErrUnsupported,
			"#/properties/a/if:",
		},
		{"not", `{"type":"object","not":{}}`, ErrUnsupported, "#/not:"},
		{
			"prefixItems",
			`{"type":"object","properties":{"a":{"type":"array","prefixItems":[{}]}}}`,
			ErrUnsupported,
			"#/properties/a/prefixItems:",
		},
		{
			"patternProperties",
			`{"type":"object","patternProperties":{"^a":{}}}`,
			ErrUnsupported,
			"#/patternProperties:",
		},
		{
			"remote ref",
			`{"type":"object","properties":{"a":{"$ref":"https://example.com/a.json"}}}`,
			ErrUnsupported,
			"#/properties/a/$ref:",
		},
		{
			"dynamicRef",
			`{"type":"object","properties":{"a":{"$dynamicRef":"#x"}}}`,
			ErrUnsupported,
			"#/properties/a/$dynamicRef:",
		},
		{
			"keyword next to ref",
			`{"type":"object","properties":{"a":{"$ref":"#","type":"object"}}}`,
			ErrUnsupported,
			"#/properties/a/type:",
		},
		{
			"ref cycle",
			`{"type":"object","properties":{"a":{"$ref":"#/$defs/x"}},
			"$defs":{"x":{"$ref":"#/$defs/y"},"y":{"$ref":"#/$defs/x"}}}`,
			ErrUnsupported,
			"references itself",
		},
		{
			"array holding itself",
			`{"type":"object","properties":{"a":{"$ref":"#/$defs/l"}},
			"$defs":{"l":{"type":"array","items":{"$ref":"#/$defs/l"}}}}`,
			ErrUnsupported,
			"#/$defs/l:",
		},
		{
			"closed allOf member",
			`{"allOf":[{"$ref":"#/$defs/a"},{"$ref":"#/$defs/b"}],
			"$defs":{"a":{"type":"object","additionalProperties":false},"b":{"type":"object"}}}`,
			ErrUnsupported,
			"#/allOf/0:",
		},
		{
			"allOf member property declared twice",
			`{"allOf":[
				{"type":"object","properties":{"x":{"type":"string"}}},
				{"type":"object","properties":{"x":{"type":"integer"}}}
			]}`,
			ErrConflict,
			"#/allOf/0/properties/x and #/allOf/1/properties/x",
		},
		{
			"type names collide",
			`{"type":"object","$defs":{"a-b":{"type":"object"},"aB":{"type":"object"}}}`,
			ErrConflict,
			"#/$defs/a-b and #/$defs/aB both give the Go name AB",
		},
		{
			"field names collide",
			`{"type":"object","properties":{"a_b":{},"aB":{}}}`,
			ErrConflict,
			"give the Go field name AB",
		},
		{
			"property named like the wire field of rejected members",
			`{"type":"object","properties":{"unknown":{}},"additionalProperties":false}`,
			ErrConflict,
			"give the Go field name Unknown",
		},
		{
			"property named like the field of kept members",
			`{"type":"object","properties":{"additional_properties":{}},
			"additionalProperties":{"type":"string"}}`,
			ErrConflict,
			"give the Go field name AdditionalProperties",
		},
		{
			"property named like a method",
			`{"type":"object","properties":{"unmarshalJSONFrom":{}}}`,
			ErrConflict,
			"give the Go field name UnmarshalJSONFrom",
		},
		{
			"empty property name",
			`{"type":"object","properties":{"":{}}}`,
			ErrUnsupported,
			"#/properties/:",
		},
		{
			"property name of separators only",
			`{"type":"object","properties":{"-":{}}}`,
			ErrUnsupported,
			"#/properties/-:",
		},
		{
			"property name starting with a digit",
			`{"type":"object","properties":{"1a":{}}}`,
			ErrUnsupported,
			"#/properties/1a:",
		},
		{
			"property name a tag cannot hold",
			`{"type":"object","properties":{"a,b":{}}}`,
			ErrUnsupported,
			"#/properties/a,b:",
		},
		{
			"required member without property",
			`{"type":"object","required":["a"]}`,
			ErrUnsupported,
			"#/required:",
		},
		{
			"false property",
			`{"type":"object","properties":{"a":false}}`,
			ErrUnsupported,
			"#/properties/a:",
		},
		{
			"const outside a property",
			`{"type":"object","properties":{"a":{"type":"array","items":{"const":1}}}}`,
			ErrUnsupported,
			"#/properties/a/items:",
		},
		{
			"const that is no Go constant of its type",
			`{"type":"object","properties":{"a":{"type":"integer","const":1.5}}}`,
			ErrUnsupported,
			"#/properties/a:",
		},
		{
			"object type of several kinds",
			`{"type":"object","properties":{"a":{"type":["object","string"]}}}`,
			ErrUnsupported,
			"#/properties/a/type:",
		},
		{
			"root holding itself through no declared type",
			`{"type":"array","items":{"$ref":"#"}}`,
			ErrUnsupported,
			"#:",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc jsonschema.Schema
			assert.NilError(t, jsonv2.Unmarshal([]byte(tc.schema), &doc))

			_, _, err := Generate(&doc, Options{Package: "p", TypeName: "Root"})
			assert.ErrorIs(t, err, tc.err)
			assert.Assert(t, strings.Contains(err.Error(), tc.at), "%v", err)
		})
	}
}

// TestGenerateRootType checks the type a root declaring no type of its own
// is defined over, and that it checks the const or the enum of the root.
func TestGenerateRootType(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		want   []string
	}{
		{"string", `{"type":"string"}`, []string{"type Root string"}},
		{"array", `{"items":{"type":"integer"}}`, []string{"type Root []int64"}},
		{
			"map",
			`{"additionalProperties":{"type":"boolean"}}`,
			[]string{"type Root map[string]bool"},
		},
		{"nullable", `{"type":["number","null"]}`, []string{"type Root opt.Option[float64]"}},
		{"any", `{}`, []string{"type Root jsontext.Value"}},
		{
			"enum",
			`{"enum":[1,2]}`,
			[]string{"type Root int64", "jsonschemart.Enum(out, 1, 2)"},
		},
		{"ref", `{"$ref":"#/$defs/a","$defs":{"a":{"type":"object"}}}`, []string{"type Root A"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc jsonschema.Schema
			assert.NilError(t, jsonv2.Unmarshal([]byte(tc.schema), &doc))

			files, _, err := Generate(&doc, Options{Package: "p", TypeName: "Root"})
			assert.NilError(t, err)

			for _, want := range tc.want {
				assert.Assert(t, strings.Contains(string(files[0].Content), want), want)
			}
		})
	}
}

func TestGenerateRootName(t *testing.T) {
	cases := []struct {
		name     string
		schema   string
		typeName string
		want     string
	}{
		{"title", `{"title":"my thing","type":"object"}`, "", "type MyThing struct"},
		{"type name wins", `{"title":"my thing","type":"object"}`, "Other", "type Other struct"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc jsonschema.Schema
			assert.NilError(t, jsonv2.Unmarshal([]byte(tc.schema), &doc))

			files, _, err := Generate(&doc, Options{Package: "p", TypeName: tc.typeName})
			assert.NilError(t, err)
			assert.Assert(t, strings.Contains(string(files[0].Content), tc.want))
		})
	}

	var doc jsonschema.Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(`{"type":"object"}`), &doc))

	_, _, err := Generate(&doc, Options{Package: "p"})
	assert.ErrorIs(t, err, ErrUnsupported)

	_, _, err = Generate(&doc, Options{Package: "not a name", TypeName: "T"})
	assert.ErrorIs(t, err, ErrOptions)
}
