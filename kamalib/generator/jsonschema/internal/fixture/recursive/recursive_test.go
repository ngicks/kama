package recursive

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"testing"

	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

const chain = `"chain":{"name":"c","link":{"label":"l"}}`

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"minimal", `{"trees":[],` + chain + `}`},
		{
			"tree",
			`{"trees":[{"label":"a","children":[{"label":"b","children":[{"label":"c"}]}]}],` +
				chain + `}`,
		},
		{"groves", `{"trees":[],"groves":[{"trees":[{"label":"g"}],` + chain + `}],` + chain + `}`},
		{
			"linked list",
			`{"trees":[],"list":{"value":1,"next":{"value":2,"next":{"value":3}}},` + chain + `}`,
		},
		{
			"mutual cycle",
			`{"trees":[],"chain":{"name":"a","link":{"label":"x",
			"chain":{"name":"b","link":{"label":"y"}}}}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Forest
			assert.NilError(t, json.Unmarshal([]byte(tc.in), &v))

			out, err := json.Marshal(v)
			assert.NilError(t, err)
			assert.Equal(t, canonical(t, out), canonical(t, []byte(tc.in)))
		})
	}
}

func TestDecodeValues(t *testing.T) {
	var v Forest
	assert.NilError(t, json.Unmarshal(
		[]byte(`{"trees":[],"list":{"value":1,"next":{"value":2}},`+chain+`}`),
		&v,
	))

	l := v.List.Value()
	assert.Equal(t, l.Value, int64(1))
	assert.Equal(t, l.Next.Value, int64(2))
	assert.Assert(t, l.Next.Next == nil)
	assert.Equal(t, v.Chain.Link.Label, "l")
	assert.Assert(t, v.Chain.Link.Chain == nil)
}

func TestDecodeError(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		pointers []string
		kind     error
	}{
		{
			"deep list",
			`{"trees":[],"list":{"value":1,"next":{"value":2,"next":{}}},` + chain + `}`,
			[]string{"/list/next/next/value"},
			validator.ErrRequired,
		},
		{
			"null optional pointer",
			`{"trees":[],"list":{"value":1,"next":null},` + chain + `}`,
			[]string{"/list/next"},
			validator.ErrNull,
		},
		{
			"missing required pointer",
			`{"trees":[],"chain":{"name":"c"}}`,
			[]string{"/chain/link"},
			validator.ErrRequired,
		},
		{
			"null required pointer",
			`{"trees":[],"chain":{"name":"c","link":null}}`,
			[]string{"/chain/link"},
			validator.ErrNull,
		},
		{
			"deep tree",
			`{"trees":[{"label":"a"},{"label":"b","children":[{}]}],` + chain + `}`,
			[]string{"/trees/1/children/0/label"},
			validator.ErrRequired,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Forest

			err := json.Unmarshal([]byte(tc.in), &v)
			assert.Assert(t, err != nil)

			se, ok := errors.AsType[*json.SemanticError](err)
			assert.Assert(t, ok, "%T: %v", err, err)
			assert.DeepEqual(t, validator.Pointers(se.Err), tc.pointers)
			assert.ErrorIs(t, se.Err, tc.kind)
		})
	}
}

func TestMarshalNilRequiredPointer(t *testing.T) {
	cases := []struct {
		name string
		v    any
	}{
		{"direct", Chain{Name: "c"}},
		{"nested", Forest{Chain: Chain{Name: "c"}}},
		{
			"through optional pointer",
			Forest{Chain: Chain{Name: "c", Link: &Link{Chain: &Chain{Name: "d"}}}},
		},
		{"in option", opt.Some(Chain{})},
		{"self reference", Ouroboros{Tail: &Ouroboros{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := json.Marshal(tc.v)
			assert.ErrorIs(t, err, validator.ErrNil)
		})
	}
}

// TestRequiredSelfReference decodes a type that holds itself through a required member. No
// finite JSON value satisfies it, so every decode fails at the innermost object.
func TestRequiredSelfReference(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		pointers []string
		kind     error
	}{
		{"missing", `{"tail":{"tail":{}}}`, []string{"/tail/tail/tail"}, validator.ErrRequired},
		{"null", `{"tail":null}`, []string{"/tail"}, validator.ErrNull},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Ouroboros

			err := json.Unmarshal([]byte(tc.in), &v)

			se, ok := errors.AsType[*json.SemanticError](err)
			assert.Assert(t, ok, "%T: %v", err, err)
			assert.DeepEqual(t, validator.Pointers(se.Err), tc.pointers)
			assert.ErrorIs(t, se.Err, tc.kind)
		})
	}
}

func canonical(t *testing.T, b []byte) string {
	t.Helper()

	v := jsontext.Value(b)
	assert.NilError(t, v.Canonicalize())

	return string(v)
}

// TestPartialRequiredPointer leaves the value alone when a required member
// held by pointer fails to decode.
func TestPartialRequiredPointer(t *testing.T) {
	var v Forest

	err := json.Unmarshal([]byte(`{"trees":[],"chain":{"name":"c","link":{"label":1}}}`), &v)

	se, ok := errors.AsType[*json.SemanticError](err)
	assert.Assert(t, ok, "%T: %v", err, err)
	assert.DeepEqual(t, validator.Pointers(se.Err), []string{"/chain/link/label"})
	assert.ErrorIs(t, se.Err, validator.ErrKind)
	assert.Assert(t, v.Chain.Link == nil)
}
