package jsonschema

import (
	jsonv2 "encoding/json/v2"
	"testing"

	"gotest.tools/v3/assert"
)

const refDoc = `{
	"title": "root",
	"$anchor": "top",
	"$defs": {
		"x": {"title": "x", "$anchor": "ax"},
		"a/b": {"title": "slash"},
		"m~n": {"title": "tilde"},
		"a b": {"title": "space"},
		"%25": {"title": "percent"},
		"emb": {"$id": "http://example.com/emb", "$anchor": "hidden", "title": "emb"},
		"dyn": {"$dynamicAnchor": "d", "title": "dyn"}
	},
	"properties": {
		"p": {"items": {"title": "item"}, "not": {"title": "not"}}
	},
	"allOf": [{"title": "all0"}, {"title": "all1"}],
	"anyOf": [{"title": "any0"}],
	"oneOf": [{"title": "one0"}, {"title": "one1", "$anchor": "deep"}],
	"prefixItems": [true, {"title": "prefix1"}],
	"additionalProperties": {"title": "addl"}
}`

func decodeRefDoc(t *testing.T) *Schema {
	t.Helper()

	var s Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(refDoc), &s))

	return &s
}

func TestResolvePointer(t *testing.T) {
	root := decodeRefDoc(t)

	for ptr, title := range map[string]string{
		"":                      "root",
		"/$defs/x":              "x",
		"/$defs/a~1b":           "slash",
		"/$defs/m~0n":           "tilde",
		"/$defs/a b":            "space",
		"/properties/p/items":   "item",
		"/properties/p/not":     "not",
		"/allOf/0":              "all0",
		"/allOf/1":              "all1",
		"/anyOf/0":              "any0",
		"/oneOf/1":              "one1",
		"/prefixItems/1":        "prefix1",
		"/additionalProperties": "addl",
	} {
		s, err := ResolvePointer(root, ptr)
		assert.NilError(t, err, ptr)
		assert.Equal(t, s.Title.Value(), title, ptr)
	}

	s, err := ResolvePointer(root, "/prefixItems/0")
	assert.NilError(t, err)
	assert.Assert(t, s.Bool.Value())
}

func TestResolvePointerUnresolved(t *testing.T) {
	root := decodeRefDoc(t)

	for _, ptr := range []string{
		"$defs/x",
		"/$defs/missing",
		"/$defs",
		"/$defs/x/title",
		"/allOf/2",
		"/allOf/01",
		"/allOf/-",
		"/allOf/-1",
		"/allOf/x",
		"/properties/p/items/items",
		"/prefixItems/0/items",
		"/unknown",
		"/$defs/bad~2escape",
		"/$defs/trailing~",
	} {
		_, err := ResolvePointer(root, ptr)
		assert.ErrorIs(t, err, ErrUnresolved, ptr)
	}
}

func TestResolveRef(t *testing.T) {
	root := decodeRefDoc(t)

	for ref, title := range map[string]string{
		"#":                    "root",
		"#/$defs/x":            "x",
		"#/$defs/a~1b":         "slash",
		"#/$defs/a%20b":        "space",
		"#/$defs/%2525":        "percent",
		"#/oneOf/1":            "one1",
		"#top":                 "root",
		"#ax":                  "x",
		"#deep":                "one1",
		"#d":                   "dyn",
		"#/$defs/emb":          "emb",
		"#/prefixItems/1":      "prefix1",
		"#/properties/p/items": "item",
	} {
		s, err := ResolveRef(root, ref)
		assert.NilError(t, err, ref)
		assert.Equal(t, s.Title.Value(), title, ref)
	}
}

func TestResolveRefErrors(t *testing.T) {
	root := decodeRefDoc(t)

	for _, ref := range []string{
		"#missing",
		"#hidden",
		"#/$defs/nope",
		"#/$defs/%zz",
		"#/$defs/x/title",
	} {
		_, err := ResolveRef(root, ref)
		assert.ErrorIs(t, err, ErrUnresolved, ref)
	}

	for _, ref := range []string{
		"other.json",
		"other.json#/$defs/x",
		"http://example.com/emb",
		"http://example.com/emb#hidden",
		"",
	} {
		_, err := ResolveRef(root, ref)
		assert.ErrorIs(t, err, ErrNonLocalRef, ref)
	}
}

func TestEscapePointerToken(t *testing.T) {
	for in, want := range map[string]string{
		"":    "",
		"a":   "a",
		"a/b": "a~1b",
		"m~n": "m~0n",
		"~1":  "~01",
		"/~/": "~1~0~1",
	} {
		assert.Equal(t, EscapePointerToken(in), want)

		got, err := unescapePointerToken(want)
		assert.NilError(t, err)
		assert.Equal(t, got, in)
	}
}

func TestResolvePointerErrorNamesTheMissingStep(t *testing.T) {
	root := decodeRefDoc(t)

	_, err := ResolvePointer(root, "/properties/p/items/not")
	assert.Error(
		t,
		err,
		`jsonschema: unresolved reference: pointer "/properties/p/items/not": `+
			`no schema at "/properties/p/items/not"`,
	)

	_, err = ResolvePointer(root, "/$defs/a~1b/allOf/0")
	assert.Error(
		t,
		err,
		`jsonschema: unresolved reference: pointer "/$defs/a~1b/allOf/0": `+
			`no schema at "/$defs/a~1b/allOf/0"`,
	)
}
