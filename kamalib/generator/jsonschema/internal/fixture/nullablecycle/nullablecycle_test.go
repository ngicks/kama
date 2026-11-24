package nullablecycle

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/validation"
)

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"null parent", `{"value":1,"parent":null}`, `{"value":1,"parent":null}`},
		{
			"chain",
			`{"value":1,"parent":{"value":0,"parent":null},"next":{"value":2,"parent":null}}`,
			`{"value":1,"parent":{"value":0,"parent":null},"next":{"value":2,"parent":null}}`,
		},
		// A nil pointer stands for both an absent and a null optional member,
		// so null comes back absent.
		{"null next", `{"value":1,"parent":null,"next":null}`, `{"value":1,"parent":null}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Node
			assert.NilError(t, json.Unmarshal([]byte(tc.in), &v))

			out, err := json.Marshal(v)
			assert.NilError(t, err)
			assert.Equal(t, canonical(t, out), canonical(t, []byte(tc.want)))
		})
	}
}

func TestDecodeValues(t *testing.T) {
	var v Node
	assert.NilError(t, json.Unmarshal(
		[]byte(`{"value":1,"parent":{"value":0,"parent":null},"next":{"value":2,"parent":null}}`),
		&v,
	))

	assert.Equal(t, v.Parent.Value, int64(0))
	assert.Assert(t, v.Parent.Parent == nil)
	assert.Equal(t, v.Next.Value, int64(2))
	assert.Assert(t, v.Next.Next == nil)
}

func TestDecodeError(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		pointers []string
		kind     error
	}{
		{"missing parent", `{"value":1}`, []string{"/parent"}, validation.ErrRequired},
		{
			"missing deep parent",
			`{"value":1,"parent":{"value":0}}`,
			[]string{"/parent/parent"},
			validation.ErrRequired,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Node

			err := json.Unmarshal([]byte(tc.in), &v)

			se, ok := errors.AsType[*json.SemanticError](err)
			assert.Assert(t, ok, "%T: %v", err, err)
			assert.DeepEqual(t, validation.Pointers(se.Err), tc.pointers)
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
