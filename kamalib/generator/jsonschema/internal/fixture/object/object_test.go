package object

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

var minimal = [][2]string{
	{"reqName", `"n"`},
	{"reqNullName", `null`},
	{"reqPoint", `{"x":1,"y":2}`},
	{"reqNullPoint", `null`},
	{"color", `"dark-blue"`},
	{"tags", `[]`},
	{"inline", `{"x":1.5}`},
	{"anything", `null`},
}

func TestRoundTrip(t *testing.T) {
	full := `{
		"reqName":"n","optName":"o","reqNullName":"rn","optNullName":null,
		"reqPoint":{"x":1,"y":2},"optPoint":{"x":3,"y":4},
		"reqNullPoint":{"x":5,"y":6},"optNullPoint":null,
		"color":"red","tags":["a","b"],"counts":{"a":1,"b":2},
		"inline":{"x":1.5},"closed":{"a":true},"open":{"id":7,"p":"q","r":"s"},
		"anything":{"k":[1,null]},"extra":5
	}`

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"full", full, full},
		{"minimal", doc(), doc()},
		{"unknown member of root is kept", doc("zzz", `1`), doc("zzz", `1`)},
		{"opt null name defined", doc("optNullName", `"x"`), doc("optNullName", `"x"`)},
		{"optional raw null", doc("extra", `null`), doc("extra", `null`)},
		{"anything raw", doc("anything", `{"a":[1,null]}`), doc("anything", `{"a":[1,null]}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Object
			assert.NilError(t, json.Unmarshal([]byte(tc.in), &v))

			out, err := json.Marshal(v)
			assert.NilError(t, err)
			assert.Equal(t, canonical(t, out), canonical(t, []byte(tc.want)))
		})
	}
}

func TestDecodeValues(t *testing.T) {
	var v Object
	assert.NilError(t, json.Unmarshal([]byte(doc("extra", `null`, "anything", `1.50`)), &v))

	assert.Assert(t, v.ReqNullName.IsNone())
	assert.Assert(t, v.OptName.IsNone())
	assert.Assert(t, v.OptNullName.IsUndefined())
	assert.Equal(t, string(v.Extra.Value()), `null`)
	assert.Equal(t, v.Color, ObjectColorDarkBlue)
	assert.Equal(t, string(v.Anything), `1.50`)
}

func TestAdditionalProperties(t *testing.T) {
	var v Object
	assert.NilError(t, json.Unmarshal([]byte(doc(
		"zzz", `[1]`,
		"inline", `{"x":1,"y":2}`,
		"open", `{"id":1,"p":"q"}`,
	)), &v))

	assert.DeepEqual(t, v.AdditionalProperties, map[string]jsontext.Value{"zzz": []byte(`[1]`)})
	assert.DeepEqual(
		t,
		v.Inline.AdditionalProperties,
		map[string]jsontext.Value{"y": []byte(`2`)},
	)
	assert.DeepEqual(t, v.Open.Value().AdditionalProperties, map[string]string{"p": "q"})
}

func TestDecodeError(t *testing.T) {
	var errSemantic = errors.New("json semantic error")

	cases := []struct {
		name     string
		in       string
		pointers []string
		kind     error
	}{
		{"missing required", doc("reqName", ""), []string{"/reqName"}, validator.ErrRequired},
		{"null required", doc("reqName", `null`), []string{"/reqName"}, validator.ErrNull},
		{"null optional", doc("optName", `null`), []string{"/optName"}, validator.ErrNull},
		{
			"missing required nullable",
			doc("reqNullName", ""),
			[]string{"/reqNullName"},
			validator.ErrRequired,
		},
		{"wrong type", doc("reqName", `5`), []string{"/reqName"}, errSemantic},
		{
			"nested missing",
			doc("reqPoint", `{"x":1}`),
			[]string{"/reqPoint/y"},
			validator.ErrRequired,
		},
		{"nested null", doc("optPoint", `null`), []string{"/optPoint"}, validator.ErrNull},
		{"nested not object", doc("reqPoint", `[]`), []string{"/reqPoint"}, validator.ErrKind},
		{"enum", doc("color", `"blue"`), []string{"/color"}, validator.ErrConst},
		{"enum null", doc("color", `null`), []string{"/color"}, validator.ErrNull},
		{"null item", doc("tags", `["a",null]`), []string{"/tags/1"}, validator.ErrNull},
		{"null map value", doc("counts", `{"a":null}`), []string{"/counts/a"}, validator.ErrNull},
		{
			"unknown member of a closed object",
			doc("closed", `{"b":1,"a":true,"c":2}`),
			[]string{"/closed/b", "/closed/c"},
			validator.ErrClosed,
		},
		{"additional property type", doc("open", `{"z":5}`), []string{"/open/z"}, errSemantic},
		{"missing raw", doc("anything", ""), []string{"/anything"}, validator.ErrRequired},
		{
			"every failing member",
			doc("reqName", "", "anything", "", "optName", `null`),
			[]string{"/anything", "/optName", "/reqName"},
			validator.ErrRequired,
		},
		{
			"nested failure stops decoding",
			doc("reqName", "", "inline", `{}`),
			[]string{"/inline/x"},
			validator.ErrRequired,
		},
		{"not object", `[]`, []string{""}, validator.ErrKind},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := decodeError(t, tc.in)

			assert.DeepEqual(t, validator.Pointers(err), tc.pointers)

			if tc.kind == errSemantic {
				_, ok := errors.AsType[*json.SemanticError](err)
				assert.Assert(t, ok, "%v", err)
			} else {
				assert.ErrorIs(t, err, tc.kind)
			}
		})
	}
}

// doc returns the minimal valid document with the members of pairs replaced. A pair with an
// empty value removes the member, and a pair naming no minimal member appends it.
func doc(pairs ...string) string {
	members := append([][2]string(nil), minimal...)

	for i := 0; i < len(pairs); i += 2 {
		name, value := pairs[i], pairs[i+1]

		found := false

		for j, m := range members {
			if m[0] == name {
				members[j][1] = value
				found = true
			}
		}

		if !found {
			members = append(members, [2]string{name, value})
		}
	}

	var parts []string

	for _, m := range members {
		if m[1] != "" {
			parts = append(parts, `"`+m[0]+`":`+m[1])
		}
	}

	return "{" + strings.Join(parts, ",") + "}"
}

func decodeError(t *testing.T, in string) error {
	t.Helper()

	var v Object

	err := json.Unmarshal([]byte(in), &v)
	assert.Assert(t, err != nil)

	se, ok := errors.AsType[*json.SemanticError](err)
	assert.Assert(t, ok, "%T: %v", err, err)

	return se.Err
}

func canonical(t *testing.T, b []byte) string {
	t.Helper()

	v := jsontext.Value(b)
	assert.NilError(t, v.Canonicalize())

	return string(v)
}
