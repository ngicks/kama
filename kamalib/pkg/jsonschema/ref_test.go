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
		"":                              "root",
		"#":                             "root",
		"#/$defs/x":                     "x",
		"#/$defs/a~1b":                  "slash",
		"#/$defs/a%20b":                 "space",
		"#/$defs/%2525":                 "percent",
		"#/oneOf/1":                     "one1",
		"#top":                          "root",
		"#ax":                           "x",
		"#deep":                         "one1",
		"#d":                            "dyn",
		"#/$defs/emb":                   "emb",
		"#/prefixItems/1":               "prefix1",
		"#/properties/p/items":          "item",
		"http://example.com/emb":        "emb",
		"http://example.com/emb#":       "emb",
		"http://example.com/emb#hidden": "emb",
	} {
		ptr, err := ResolveRef(root, "", "", ref)
		assert.NilError(t, err, ref)

		s, err := ResolvePointer(root, ptr)
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
		"http://example.com/emb#top",
	} {
		_, err := ResolveRef(root, "", "", ref)
		assert.ErrorIs(t, err, ErrUnresolved, ref)
	}

	for _, ref := range []string{
		"other.json",
		"other.json#/$defs/x",
		"http://example.com/other",
	} {
		_, err := ResolveRef(root, "", "", ref)
		assert.ErrorIs(t, err, ErrNonLocalRef, ref)
	}
}

const resourceDoc = `{
	"$id": "http://example.com/root.json",
	"$anchor": "a",
	"$defs": {
		"rel": {
			"$id": "dir/rel.json",
			"$anchor": "a",
			"$defs": {
				"inner": {"$id": "inner.json", "title": "inner"},
				"leaf": {"title": "rel-leaf"}
			},
			"title": "rel"
		},
		"urn": {
			"$id": "urn:uuid:deadbeef-1234-ffff-ffff-4321feebdaed",
			"$defs": {"leaf": {"$anchor": "u", "title": "urn-leaf"}},
			"title": "urn"
		},
		"enum": {"enum": [{"$id": "http://example.com/not-an-id"}], "title": "enum"},
		"unknown": {"x-keyword": {"$id": "http://example.com/unknown"}, "title": "unknown"},
		"leaf": {"title": "root-leaf"}
	},
	"title": "root"
}`

func TestResolveRefAcrossResources(t *testing.T) {
	var root Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(resourceDoc), &root))

	for _, c := range []struct {
		ptr, ref, title string
	}{
		{"", "#a", "root"},
		{"/$defs/rel", "#a", "rel"},
		{"/$defs/rel/$defs/leaf", "#a", "rel"},
		{"/$defs/rel/$defs/leaf", "#/$defs/leaf", "rel-leaf"},
		{"/$defs/leaf", "#/$defs/leaf", "root-leaf"},
		{"", "dir/rel.json", "rel"},
		{"", "dir/inner.json", "inner"},
		{"/$defs/rel", "inner.json", "inner"},
		{"/$defs/rel", "../root.json#a", "root"},
		{"", "#/$defs/rel/$defs/inner", "inner"},
		{"", "dir/rel.json#/$defs/inner", "inner"},
		{"", "urn:uuid:deadbeef-1234-ffff-ffff-4321feebdaed#u", "urn-leaf"},
		{"/$defs/urn", "#/$defs/leaf", "urn-leaf"},
		{"/$defs/urn/$defs/leaf", "#u", "urn-leaf"},
	} {
		ptr, err := ResolveRef(&root, "", c.ptr, c.ref)
		assert.NilError(t, err, "%s %s", c.ptr, c.ref)

		s, err := ResolvePointer(&root, ptr)
		assert.NilError(t, err)
		assert.Equal(t, s.Title.Value(), c.title, "%s %s", c.ptr, c.ref)
	}

	for _, ref := range []string{"http://example.com/not-an-id", "http://example.com/unknown"} {
		_, err := ResolveRef(&root, "", "", ref)
		assert.ErrorIs(t, err, ErrNonLocalRef, ref)
	}

	_, err := ResolveRef(&root, "", "/$defs/urn", "#a")
	assert.ErrorIs(t, err, ErrUnresolved)
}

func TestBaseUri(t *testing.T) {
	var root Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(resourceDoc), &root))

	for ptr, want := range map[string]string{
		"":                       "http://example.com/root.json",
		"/$defs/leaf":            "http://example.com/root.json",
		"/$defs/rel":             "http://example.com/dir/rel.json",
		"/$defs/rel/$defs/inner": "http://example.com/dir/inner.json",
		"/$defs/urn/$defs/leaf":  "urn:uuid:deadbeef-1234-ffff-ffff-4321feebdaed",
		"/$defs/enum":            "http://example.com/root.json",
	} {
		got, err := BaseUri(&root, "", ptr)
		assert.NilError(t, err, ptr)
		assert.Equal(t, got, want, ptr)
	}

	var bare Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(`{"$defs": {"a": {"$id": "a.json"}}}`), &bare))

	got, err := BaseUri(&bare, "http://example.com/dir/doc.json", "/$defs/a")
	assert.NilError(t, err)
	assert.Equal(t, got, "http://example.com/dir/a.json")

	got, err = BaseUri(&bare, "http://example.com/dir/doc.json", "")
	assert.NilError(t, err)
	assert.Equal(t, got, "http://example.com/dir/doc.json")

	// Without a base URI a relative URI stays relative.
	got, err = BaseUri(&bare, "", "/$defs/a")
	assert.NilError(t, err)
	assert.Equal(t, got, "a.json")

	got, err = RefUri(&bare, "", "", "other.json#/x")
	assert.NilError(t, err)
	assert.Equal(t, got, "other.json#/x")
}

func TestResources(t *testing.T) {
	var root Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(resourceDoc), &root))

	var got []string
	for uri, ptr := range Resources(&root, "") {
		got = append(got, uri+" "+ptr)
	}

	assert.DeepEqual(t, got, []string{
		"http://example.com/root.json ",
		"http://example.com/dir/rel.json /$defs/rel",
		"http://example.com/dir/inner.json /$defs/rel/$defs/inner",
		"urn:uuid:deadbeef-1234-ffff-ffff-4321feebdaed /$defs/urn",
	})
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

func TestResourcesAlong(t *testing.T) {
	var root Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(resourceDoc), &root))

	for ptr, want := range map[string][]string{
		"":                       {""},
		"/$defs/leaf":            {""},
		"/$defs/rel":             {"", "/$defs/rel"},
		"/$defs/rel/$defs/leaf":  {"", "/$defs/rel"},
		"/$defs/rel/$defs/inner": {"", "/$defs/rel", "/$defs/rel/$defs/inner"},
	} {
		got, err := ResourcesAlong(&root, ptr)
		assert.NilError(t, err, ptr)
		assert.DeepEqual(t, got, want)
	}
}

func TestDynamicAnchor(t *testing.T) {
	var root Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(`{
		"$dynamicAnchor": "top",
		"$defs": {
			"a": {"$dynamicAnchor": "node"},
			"b": {"$anchor": "plain"},
			"inner": {
				"$id": "inner.json",
				"$dynamicAnchor": "inner",
				"$defs": {"c": {"$dynamicAnchor": "deep"}}
			}
		}
	}`), &root))

	for _, c := range []struct {
		res, name, want string
		ok              bool
	}{
		{"", "top", "", true},
		{"", "node", "/$defs/a", true},
		{"", "plain", "", false},
		{"", "deep", "", false},
		{"/$defs/inner", "deep", "/$defs/inner/$defs/c", true},
		{"/$defs/inner", "inner", "/$defs/inner", true},
		{"/$defs/inner", "node", "", false},
	} {
		got, ok := DynamicAnchor(&root, c.res, c.name)
		assert.Equal(t, ok, c.ok, "%s %s", c.res, c.name)
		assert.Equal(t, got, c.want, "%s %s", c.res, c.name)
	}
}
