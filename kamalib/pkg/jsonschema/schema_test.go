package jsonschema

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"slices"
	"testing"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"
)

func TestSchemaKeepsSourceOrder(t *testing.T) {
	in := `{"$defs":{"z":true,"a":false,"m":{}},"properties":{"b":{},"a":{"type":"string"},"c":{}}}`

	var s Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(in), &s))

	names := func(m SchemaMap) []string {
		return opiter.WrapSeq(slices.Values(m)).
			Map(func(n NamedSchema) string { return n.Name }).
			CollectSlice()
	}
	assert.DeepEqual(t, names(s.Defs), []string{"z", "a", "m"})
	assert.DeepEqual(t, names(s.Properties), []string{"b", "a", "c"})

	out, err := jsonv2.Marshal(s, jsonv2.Deterministic(true))
	assert.NilError(t, err)
	assert.Equal(
		t,
		string(out),
		`{"$defs":{"z":true,"a":false,"m":{}},"properties":{"b":{},"a":{"type":"string"},"c":{}}}`,
	)
}

func TestSchemaKeepsUnknownKeywords(t *testing.T) {
	in := `{"x-big":12345678901234567890123,"x-float":1.0e0,"x-obj":{"b":2.50,"a":[1.0]}}`

	var s Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(in), &s))
	assert.DeepEqual(t, s.Unknown, map[string]jsontext.Value{
		"x-big":   jsontext.Value(`12345678901234567890123`),
		"x-float": jsontext.Value(`1.0e0`),
		"x-obj":   jsontext.Value(`{"b":2.50,"a":[1.0]}`),
	})

	out, err := jsonv2.Marshal(s, jsonv2.Deterministic(true))
	assert.NilError(t, err)
	assert.Equal(
		t,
		string(out),
		`{"x-big":12345678901234567890123,"x-float":1.0e0,"x-obj":{"b":2.50,"a":[1.0]}}`,
	)
}

// TestSchemaRoundTrip writes each input in the field order of Schema, so the output matches it
// byte for byte.
func TestSchemaRoundTrip(t *testing.T) {
	for _, in := range []string{
		`true`,
		`false`,
		`{}`,
		`{"enum":[],"const":null,"required":[],"default":null,"deprecated":false}`,
		`{"$defs":{},"allOf":[],"properties":{}}`,
		`{"type":"string"}`,
		`{"type":["string"]}`,
		`{"type":["integer","null"]}`,
		`{"multipleOf":0.0001,"maximum":2.0,"minLength":1e0}`,
		`{"not":{"title":""},"items":false,"additionalProperties":true}`,
	} {
		t.Run(in, func(t *testing.T) {
			var s Schema
			assert.NilError(t, jsonv2.Unmarshal([]byte(in), &s))

			out, err := jsonv2.Marshal(s)
			assert.NilError(t, err)
			assert.Equal(t, string(out), in)
		})
	}
}

func TestSchemaDecodesPresence(t *testing.T) {
	var s Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(`{"const":null,"required":[]}`), &s))
	assert.DeepEqual(t, s.Const, jsontext.Value(`null`))
	assert.Assert(t, s.Required != nil)
	assert.Assert(t, s.Default == nil)
	assert.Assert(t, s.Enum == nil)
	assert.Equal(t, s.Bool, opt.None[bool]())

	var b Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(`false`), &b))
	assert.Equal(t, b.Bool, opt.Some(false))
}

func TestSchemaRejectsInvalidShapes(t *testing.T) {
	for _, in := range []string{
		`1`,
		`"x"`,
		`null`,
		`{"items":1}`,
		`{"type":1}`,
		`{"properties":[]}`,
		`{"properties":{"a":1}}`,
	} {
		t.Run(in, func(t *testing.T) {
			var s Schema
			assert.Assert(t, jsonv2.Unmarshal([]byte(in), &s) != nil)
		})
	}
}

// TestSchemaRejectsNullSchema checks that null is no schema wherever a keyword takes one. The
// error locates the null.
func TestSchemaRejectsNullSchema(t *testing.T) {
	cases := []struct {
		in string
		at jsontext.Pointer
	}{
		{`{"items":null}`, "/items"},
		{`{"additionalProperties":null}`, "/additionalProperties"},
		{`{"not":null}`, "/not"},
		{`{"oneOf":[null]}`, "/oneOf/0"},
		{`{"allOf":[{},null]}`, "/allOf/1"},
		{`{"anyOf":[true,null]}`, "/anyOf/1"},
		{`{"properties":{"a":null}}`, "/properties/a"},
		{`{"$defs":{"a":null}}`, "/$defs/a"},
		{`{"properties":{"a":{"items":null}}}`, "/properties/a/items"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			var s Schema

			err := jsonv2.Unmarshal([]byte(tc.in), &s)

			se, ok := errors.AsType[*jsonv2.SemanticError](err)
			assert.Assert(t, ok, "%T: %v", err, err)
			assert.Equal(t, se.JSONPointer, tc.at)
			assert.Error(t, se.Err, "jsonschema: a schema must be an object or a boolean, got null")
		})
	}
}
