package jsonschema

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ngicks/opiter"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
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
			"remote ref",
			`{"type":"object","properties":{"a":{"$ref":"https://example.com/a.json"}}}`,
			ErrUnsupported,
			"#/properties/a/$ref:",
		},
		{
			"meta-schema of another draft",
			`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object"}`,
			ErrUnsupported,
			"#/$schema:",
		},
		{
			"union on a cycle through a value of raw JSON text",
			`{"properties":{"a":{"$ref":"#/$defs/u"}},"$defs":{"u":{"oneOf":[
				{"type":"string"},{"type":"array","items":{"$ref":"#/$defs/u"}}
			]}}}`,
			ErrUnsupported,
			"#/$defs/u/oneOf/1/items/$ref: reference closes a cycle through a union",
		},
		{
			"dynamicRef",
			`{"type":"object","properties":{"a":{"$dynamicRef":"#x"}}}`,
			ErrUnsupported,
			"#/properties/a/$dynamicRef:",
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
			"property named like the method of the validator",
			`{"type":"object","properties":{"validate":{}}}`,
			ErrConflict,
			"give the Go field name Validate",
		},
		{
			"property named like the method of additional members",
			`{"type":"object","properties":{"additional_properties":{}}}`,
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
			"const that is no Go constant of its type",
			`{"type":"object","properties":{"a":{"type":"integer","const":1.5}}}`,
			ErrUnsupported,
			"#/properties/a:",
		},
		{
			"minLength that is no integer",
			`{"type":"string","minLength":1.5}`,
			ErrUnsupported,
			"#/minLength: 1.5 is no non-negative integer",
		},
		{
			"multipleOf of zero",
			`{"type":"number","multipleOf":0}`,
			ErrUnsupported,
			"#/multipleOf: 0 is not above zero",
		},
		{
			"minimum that is no number",
			`{"type":"number","minimum":"1"}`,
			ErrUnsupported,
			`#/minimum: "1" is no number`,
		},
		{
			"enum of strings next to another type",
			`{"type":"object","properties":{"a":{"type":"integer","enum":["a"]}}}`,
			ErrUnsupported,
			"#/properties/a/type:",
		},
		{
			"oneOf members differing in unchecked keywords only",
			`{"type":"object","properties":{"s":{"oneOf":[
				{"type":"string","pattern":"(?<=a)"},{"type":"string","pattern":"(?<=b)"}
			]}}}`,
			ErrUnsupported,
			"#/properties/s/oneOf:",
		},
		{
			"oneOf member of unchecked keywords only",
			`{"oneOf":[{"type":"integer"},{"pattern":"(?<=a)"}]}`,
			ErrUnsupported,
			"#/oneOf/1:",
		},
		{
			"ref to contentSchema",
			`{"type":"object","properties":{"a":{"$ref":"#/properties/b/contentSchema"},
			"b":{"type":"string","contentSchema":{"type":"object"}}}}`,
			ErrUnsupported,
			`#/properties/a/$ref: reference "#/properties/b/contentSchema" names no schema ` +
				"the generator reads",
		},
		{
			"ref to additionalProperties false",
			`{"type":"object","properties":{"a":{"$ref":"#/additionalProperties"}},
			"additionalProperties":false}`,
			ErrUnsupported,
			`#/properties/a/$ref: reference "#/additionalProperties" names no schema ` +
				"the generator reads",
		},
		{
			"ref to an index with a leading zero",
			`{"type":"object","properties":{"a":{"$ref":"#/$defs/u/anyOf/01"}},
			"$defs":{"u":{"anyOf":[{"type":"string"},{"type":"integer"}]}}}`,
			ErrUnsupported,
			`#/properties/a/$ref: reference "#/$defs/u/anyOf/01" names no schema`,
		},
		{
			"ref to a missing definition",
			`{"type":"object","properties":{"a":{"$ref":"#/$defs/b"}},"$defs":{"c":{}}}`,
			ErrUnsupported,
			`#/properties/a/$ref: reference "#/$defs/b" names no schema`,
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
		{"array", `{"type":"array","items":{"type":"integer"}}`, []string{"type Root []int64"}},
		{
			"map",
			`{"type":"object","additionalProperties":{"type":"boolean"}}`,
			[]string{"type Root map[string]bool"},
		},
		{
			"items without type",
			`{"items":{"type":"integer"}}`,
			[]string{"//kama:validate items(kind=\"integer\")\n", "type Root jsontext.Value"},
		},
		{"nullable", `{"type":["number","null"]}`, []string{"type Root opt.Option[float64]"}},
		{"any", `{}`, []string{"type Root jsontext.Value"}},
		{
			"enum",
			`{"enum":[1,2]}`,
			[]string{"//kama:validate const=\"1 2\"\n", "type Root int64"},
		},
		{
			"const of items",
			`{"type":"array","items":{"const":1}}`,
			[]string{
				"type Root []RootItem",
				"//kama:validate const=\"1\"\n",
				"type RootItem int64",
			},
		},
		{
			"const of a nullable union of items",
			`{"type":"array","items":{"anyOf":[{"const":1},{"type":"null"}]}}`,
			[]string{"type Root []opt.Option[RootItem]", "type RootItem int64"},
		},
		{
			"keywords of a string",
			`{"type":"string","minLength":2.0,"maxLength":1e1,"pattern":"^a\""}`,
			[]string{"//kama:validate runes=\">=2 <=10\"\n//kama:validate regexp=`^a\"`\n"},
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

// TestGenerateRejectsNilSchema checks Generate over documents built in Go,
// which may hold a nil schema that decoding never gives.
func TestGenerateRejectsNilSchema(t *testing.T) {
	cases := []struct {
		name string
		doc  *jsonschema.Schema
		at   string
	}{
		{"document", nil, "#:"},
		{
			"oneOf member",
			&jsonschema.Schema{OneOf: []*jsonschema.Schema{{}, nil}},
			"#/oneOf/1:",
		},
		{
			"allOf member",
			&jsonschema.Schema{AllOf: []*jsonschema.Schema{nil, {}}},
			"#/allOf/0:",
		},
		{
			"property",
			&jsonschema.Schema{
				Type:       jsonschema.Types{Names: []string{"object"}},
				Properties: jsonschema.SchemaMap{{Name: "a", Schema: nil}},
			},
			"#/properties/a:",
		},
		{
			"definition",
			&jsonschema.Schema{Defs: jsonschema.SchemaMap{{Name: "a", Schema: nil}}},
			"#/$defs/a:",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Generate(tc.doc, Options{Package: "p", TypeName: "Root"})
			assert.ErrorIs(t, err, ErrUnsupported)
			assert.ErrorContains(t, err, tc.at)
		})
	}
}

// TestGenerateWarnsUnknownKeywords checks the warnings of keywords no
// vocabulary defines. An extension keyword starting with x- is no typo.
func TestGenerateWarnsUnknownKeywords(t *testing.T) {
	var doc jsonschema.Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(`{
		"type":"object","propertys":{},"x-ext":1,"definitions":{},
		"properties":{"a":{"type":"array","additionalItems":false,"items":{"x-ok":true}}}
	}`), &doc))

	_, warnings, err := Generate(&doc, Options{Package: "p", TypeName: "Root"})
	assert.NilError(t, err)

	got := opiter.WrapSeq(slices.Values(warnings)).
		Map(func(err error) string {
			assert.Assert(t, errors.Is(err, ErrUnknownKeyword), "%v", err)
			return err.Error()
		}).
		CollectSlice()

	assert.DeepEqual(t, got, []string{
		"jsonschema: unknown keyword: #/definitions",
		"jsonschema: unknown keyword: #/propertys",
		"jsonschema: unknown keyword: #/properties/a/additionalItems",
	})
}

// TestGenerateHostileAnnotations feeds annotations that would end a
// comment, start a directive or forge the marker of the generator if
// printed as they are. Every comment line of the output must start with
// "// " or be "//" or the marker, and every declaration carries the
// marker once.
func TestGenerateHostileAnnotations(t *testing.T) {
	// The text is JSON string content, so the escapes are those of JSON.
	hostile := `line one\n//go:generate sh -c evil\r//line evil.go:1\r\n` +
		`//kama:generated kama:jsonschema\n */ /* open\n\t//go:build ignore\n` +
		`   //export Evil\n\u0000\ufeff//go:embed x`
	schema := `{
		"title": "T",
		"description": "` + hostile + `",
		"type": "object",
		"properties": {
			"p": {"description": "` + hostile + `", "deprecated": true, "type": "string"},
			"u": {"description": "` + hostile + `", "oneOf": [{"type": "string"}, {"type": "integer"}]}
		},
		"$defs": {"x\n//go:generate evil": {"type": "object", "description": "*/"}}
	}`

	var doc jsonschema.Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(schema), &doc))

	files, _, err := Generate(&doc, Options{Package: "p", Schema: "schema.json", Now: gentest.Now})
	assert.NilError(t, err)

	content := files[0].Content

	assert.NilError(t, generateSuite(jsontext.Value(schema)))

	marker := GeneratorId.Marker()
	markers := 0

	var s scanner.Scanner

	fset := token.NewFileSet()
	s.Init(fset.AddFile("", -1, len(content)), content, nil, scanner.ScanComments)

	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}

		if tok != token.COMMENT {
			continue
		}

		assert.Assert(t, strings.HasPrefix(lit, "//"), "block comment %q", lit)

		switch {
		case lit == marker:
			markers++
		case strings.HasPrefix(lit, validateDirective+" "):
		case lit == "//", strings.HasPrefix(lit, "// "), strings.HasPrefix(lit, "//\t"):
		default:
			assert.Assert(t, false, "comment line %q", lit)
		}
	}

	f, err := parser.ParseFile(fset, "", content, 0)
	assert.NilError(t, err)

	decls := opiter.WrapSeq(slices.Values(f.Decls)).
		Filter(func(d ast.Decl) bool {
			g, ok := d.(*ast.GenDecl)
			return !ok || g.Tok != token.IMPORT
		}).
		Count()
	assert.Equal(t, markers, decls)
	assert.Assert(
		t,
		strings.Contains(string(content), "\n// line one\n// //go:generate sh -c evil\n"),
	)
}

// TestSameSchema compares many times, since a Go map encodes in an order
// that changes from run to run.
func TestSameSchema(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{
			"maps of keywords",
			`{"dependentRequired":{"a":["b"],"c":["d"],"e":["f"],"g":["h"]},"x-a":1,"x-b":2,"x-c":3}`,
			`{"x-c":3,"x-b":2,"x-a":1,"dependentRequired":{"g":["h"],"e":["f"],"c":["d"],"a":["b"]}}`,
			true,
		},
		{
			"annotations",
			`{"type":"string","title":"a","description":"a","deprecated":true,"examples":["a"],
			"$comment":"a","default":"a","readOnly":true,"x-a":1}`,
			`{"type":"string","properties":{"p":{"description":"b"}}}`,
			false,
		},
		{
			"annotations only",
			`{"type":"string","title":"a","description":"a","deprecated":true,"examples":["a"],
			"$comment":"a","default":"a","readOnly":true,"writeOnly":true,"x-a":1}`,
			`{"type":"string","description":"b"}`,
			true,
		},
		{
			"nested annotations",
			`{"type":"object","properties":{"description":{"description":"a"}}}`,
			`{"type":"object","properties":{"description":{"description":"b","title":"c"}}}`,
			true,
		},
		{"type", `{"type":"string"}`, `{"type":"integer"}`, false},
		{"validation", `{"type":"string","maxLength":1}`, `{"type":"string"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc jsonschema.Schema
			assert.NilError(t, jsonv2.Unmarshal(
				[]byte(`{"$defs":{"a":`+tc.a+`,"b":`+tc.b+`}}`), &doc,
			))

			for range 100 {
				assert.Equal(t, sameSchema(&doc, "/$defs/a", "/$defs/b"), tc.want)
			}
		})
	}
}

// TestGenerateNullableUnion checks when a union of a schema and null makes
// a nullable value. In a oneOf whose other member admits null, null would
// match both members, so it stays a union.
func TestGenerateNullableUnion(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		want   string
	}{
		{
			"oneOf of a member admitting null",
			`{"type":"object","properties":{"n":{"oneOf":[{},{"type":"null"}]}}}`,
			"N opt.Option[RootN] `json:\"n,omitzero\"`",
		},
		{
			"anyOf of a member admitting null",
			`{"type":"object","properties":{"n":{"anyOf":[{},{"type":"null"}]}}}`,
			"N und.Und[jsontext.Value] `json:\"n,omitzero\"`",
		},
		{
			"oneOf of a member not admitting null",
			`{"type":"object","properties":{"n":{"oneOf":[{"type":"string"},{"type":"null"}]}}}`,
			"N und.Und[string] `json:\"n,omitzero\"`",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc jsonschema.Schema
			assert.NilError(t, jsonv2.Unmarshal([]byte(tc.schema), &doc))

			files, _, err := Generate(&doc, Options{Package: "p", TypeName: "Root"})
			assert.NilError(t, err)
			// gofmt aligns the fields, so runs of blanks compare as one.
			content := regexp.MustCompile(`[ \t]+`).ReplaceAllString(string(files[0].Content), " ")
			assert.Assert(t, strings.Contains(content, tc.want), "%s", files[0].Content)
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

	for _, name := range []string{"pet", "a b", "1x", "My-Type"} {
		_, _, err = Generate(&doc, Options{Package: "p", TypeName: name})
		assert.ErrorIs(t, err, ErrOptions, name)
	}
}
