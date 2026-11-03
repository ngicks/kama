package structtag

import (
	"reflect"
	"testing"

	"gotest.tools/v3/assert"
)

const quotedTag = `json:"'\\xde\\xad\\xbe\\xef',omitzero,format:RFC3339,omitempty"`

func mustParse(t *testing.T, tag string) Tags {
	t.Helper()

	tags, err := Parse(reflect.StructTag(tag))
	assert.NilError(t, err)
	return tags
}

func TestParse(t *testing.T) {
	tags := mustParse(t, `  json:"a,omitempty"   yaml:"b" quote:"\"x\""  `)
	assert.DeepEqual(t, tags, Tags{
		{Key: "json", Value: "a,omitempty"},
		{Key: "yaml", Value: "b"},
		{Key: "quote", Value: `"x"`},
	})
	assert.Equal(
		t,
		tags.StructTag(),
		reflect.StructTag(`json:"a,omitempty" yaml:"b" quote:"\"x\""`),
	)

	tags = mustParse(t, "")
	assert.Equal(t, len(tags), 0)
	assert.Equal(t, tags.StructTag(), reflect.StructTag(""))
}

func TestParseErrors(t *testing.T) {
	for _, tag := range []string{`json`, `json:foo`, `json:"`, `:"x"`, `js on:"x"`} {
		t.Run(tag, func(t *testing.T) {
			_, err := Parse(reflect.StructTag(tag))
			assert.ErrorIs(t, err, ErrUnpairedKey)
		})
	}

	_, err := Parse(`json:"\q"`)
	assert.ErrorIs(t, err, ErrSyntax)
}

func TestGet(t *testing.T) {
	tags := mustParse(t, quotedTag)

	cases := []struct {
		option         string
		raw, unescaped string
	}{
		{"", `'\xde\xad\xbe\xef'`, "\xde\xad\xbe\xef"},
		{"omitzero", "omitzero", "omitzero"},
		{"format", "format:RFC3339", "format:RFC3339"},
		{"omitempty", "omitempty", "omitempty"},
	}
	for _, c := range cases {
		t.Run(c.option, func(t *testing.T) {
			raw, unescaped, err := tags.Get("json", c.option)
			assert.NilError(t, err)
			assert.Equal(t, raw, c.raw)
			assert.Equal(t, unescaped, c.unescaped)
		})
	}

	raw, unescaped, err := mustParse(t, `json:"mah,omitzero"`).Get("json", "")
	assert.NilError(t, err)
	assert.Equal(t, raw, "mah")
	assert.Equal(t, unescaped, "mah")

	// An empty quoted name unescapes to an empty string.
	raw, unescaped, err = mustParse(t, `json:"'',omitzero"`).Get("json", "")
	assert.NilError(t, err)
	assert.Equal(t, raw, "''")
	assert.Equal(t, unescaped, "")

	_, _, err = tags.Get("json", "nay")
	assert.ErrorIs(t, err, ErrNotFound)
	_, _, err = tags.Get("wah", "")
	assert.ErrorIs(t, err, ErrNotFound)
	_, _, err = mustParse(t, `json:",omitzero"`).Get("json", "")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestTrailingEmptyOption(t *testing.T) {
	dash := mustParse(t, `json:"-,"`)
	assert.DeepEqual(t, dash, Tags{{Key: "json", Value: "-,"}})

	raw, unescaped, err := dash.Get("json", "")
	assert.NilError(t, err)
	assert.Equal(t, raw, "-")
	assert.Equal(t, unescaped, "-")
	_, _, err = dash.Get("json", "omitzero")
	assert.ErrorIs(t, err, ErrNotFound)

	comma := mustParse(t, `json:","`)
	_, _, err = comma.Get("json", "")
	assert.ErrorIs(t, err, ErrNotFound)
	_, _, err = comma.Get("json", "omitzero")
	assert.ErrorIs(t, err, ErrNotFound)

	addCases := []struct {
		tag, option, value, want string
	}{
		{`json:"-,"`, "omitzero", "", `json:"-,omitzero"`},
		{`json:"-,"`, "format", "x", `json:"-,format:x"`},
		{`json:"-,"`, "", "a", `json:"-,"`},
		{`json:"a,omitempty,"`, "omitzero", "", `json:"a,omitempty,omitzero"`},
		{`json:"a,omitempty,"`, "omitempty", "", `json:"a,omitempty,"`},
		{`json:","`, "omitzero", "", `json:",omitzero"`},
		{`json:","`, "", "a", `json:"a,"`},
	}
	for _, c := range addCases {
		t.Run("Add "+c.tag+" "+c.option, func(t *testing.T) {
			got, err := mustParse(t, c.tag).Add("json", c.option, c.value)
			assert.NilError(t, err)
			assert.Equal(t, string(got.StructTag()), c.want)
		})
	}

	deleteCases := []struct {
		tag, option, want string
	}{
		{`json:"-,"`, "", `json:","`},
		{`json:"a,omitempty,"`, "omitempty", `json:"a,"`},
	}
	for _, c := range deleteCases {
		t.Run("Delete "+c.tag+" "+c.option, func(t *testing.T) {
			got, err := mustParse(t, c.tag).Delete("json", c.option)
			assert.NilError(t, err)
			assert.Equal(t, string(got.StructTag()), c.want)
		})
	}

	_, err = dash.Delete("json", "omitzero")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = comma.Delete("json", "")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGetSyntaxErrors(t *testing.T) {
	for _, value := range []string{
		`a,,b`,
		`a,,`,
		`,,`,
		`a,1x`,
		`a,format:`,
		`a,format:-`,
		`'unterminated,omitzero`,
		`a"b`,
	} {
		t.Run(value, func(t *testing.T) {
			_, _, err := Tags{{Key: "json", Value: value}}.Get("json", "nay")
			assert.ErrorIs(t, err, ErrSyntax)
		})
	}
}

func TestDelete(t *testing.T) {
	cases := []struct {
		tag, option, want string
	}{
		{quotedTag, "", `json:",omitzero,format:RFC3339,omitempty"`},
		{quotedTag, "omitzero", `json:"'\\xde\\xad\\xbe\\xef',format:RFC3339,omitempty"`},
		{quotedTag, "format", `json:"'\\xde\\xad\\xbe\\xef',omitzero,omitempty"`},
		{quotedTag, "omitempty", `json:"'\\xde\\xad\\xbe\\xef',omitzero,format:RFC3339"`},
		{`json:",omitzero"`, "omitzero", `json:""`},
		{`json:"a" yaml:"b,flow"`, "flow", `json:"a" yaml:"b"`},
		{`json:"a"`, "", `json:""`},
	}
	for _, c := range cases {
		t.Run(c.tag+" "+c.option, func(t *testing.T) {
			tags := mustParse(t, c.tag)
			got, err := tags.Delete(lastKey(tags), c.option)
			assert.NilError(t, err)
			assert.Equal(t, string(got.StructTag()), c.want)
			// The receiver is left unchanged.
			assert.Equal(t, string(tags.StructTag()), c.tag)
		})
	}

	_, err := mustParse(t, quotedTag).Delete("json", "nay")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = mustParse(t, quotedTag).Delete("yaml", "")
	assert.ErrorIs(t, err, ErrNotFound)
}

func lastKey(tags Tags) string { return tags[len(tags)-1].Key }

func TestAdd(t *testing.T) {
	cases := []struct {
		name                    string
		tag, key, option, value string
		want                    string
	}{
		{"option", `json:"foo"`, "json", "omitempty", "", `json:"foo,omitempty"`},
		{"after quoted name", `json:"'\\xde'"`, "json", "omitzero", "", `json:"'\\xde',omitzero"`},
		{"no name", `json:",omitzero"`, "json", "omitempty", "", `json:",omitzero,omitempty"`},
		{"existing option", `json:",omitzero"`, "json", "omitzero", "", `json:",omitzero"`},
		{"option value", `json:",omitempty"`, "json", "format", "x", `json:",omitempty,format:x"`},
		{"existing option value", `json:",format:a"`, "json", "format", "b", `json:",format:a"`},
		{"name", `json:",omitzero"`, "json", "", "'a,b'", `json:"'a,b',omitzero"`},
		{"existing name", `json:"a"`, "json", "", "b", `json:"a"`},
		{"missing key", `json:"foo"`, "bar", "baz", "", `json:"foo" bar:",baz"`},
		{"missing key name", ``, "json", "", "a", `json:"a"`},
		{"other key", `json:"foo" bar:",foo"`, "bar", "baz", "", `json:"foo" bar:",foo,baz"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tags := mustParse(t, c.tag)
			got, err := tags.Add(c.key, c.option, c.value)
			assert.NilError(t, err)
			assert.Equal(t, string(got.StructTag()), c.want)
			assert.Equal(t, string(tags.StructTag()), c.tag)
		})
	}

	_, err := Tags{{Key: "json", Value: "a,1"}}.Add("json", "omitempty", "")
	assert.ErrorIs(t, err, ErrSyntax)
}

func TestEscapeUnescape(t *testing.T) {
	cases := []struct {
		escaped, unescaped string
	}{
		{`'hello'`, `hello`},
		{`'hello\'world'`, `hello'world`},
		{`',\"'`, `,"`},
		{`'\\xde'`, `\xde`},
		{`'\xff\x00'`, "\xff\x00"},
		{`'\n\t'`, "\n\t"},
		{`''`, ``},
	}

	for _, c := range cases {
		t.Run(c.unescaped, func(t *testing.T) {
			got, err := Unescape(c.escaped)
			assert.NilError(t, err)
			assert.Equal(t, got, c.unescaped)
			assert.Equal(t, Escape(c.unescaped), c.escaped)
		})
	}

	// Unescape also takes a literal double quote.
	got, err := Unescape(`'"'`)
	assert.NilError(t, err)
	assert.Equal(t, got, `"`)

	for _, s := range []string{`hello`, `'hello`, `'hello'x`, `'\q'`, `'\`} {
		_, err := Unescape(s)
		assert.ErrorIs(t, err, ErrSyntax, s)
	}
}
