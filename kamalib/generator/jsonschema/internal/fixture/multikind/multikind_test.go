package multikind

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"testing"

	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

func TestRoundTrip(t *testing.T) {
	for _, in := range []string{
		`{"value":"s"}`,
		`{"value":3}`,
		`{"value":true,"label":"l"}`,
		`{"value":"s","label":1.5}`,
		`{"value":"s","label":null}`,
	} {
		t.Run(in, func(t *testing.T) {
			var v Cell
			assert.NilError(t, json.Unmarshal([]byte(in), &v))

			out, err := json.Marshal(v)
			assert.NilError(t, err)
			assert.Equal(t, canonical(t, out), canonical(t, []byte(in)))
		})
	}
}

func TestDecodeValues(t *testing.T) {
	var v Cell
	assert.NilError(t, json.Unmarshal([]byte(`{"value":3,"label":null}`), &v))

	assert.Equal(t, v.Value.Kind, CellValueKind(CellValueInteger{Integer: 3}))
	assert.Equal(t, v.Label, opt.Some(CellLabel{Kind: CellLabelNull{}}))
}

func TestDecodeError(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		pointers []string
		kind     error
	}{
		{"null value", `{"value":null}`, []string{"/value"}, validator.ErrNull},
		{"fraction", `{"value":1.5}`, []string{"/value"}, validator.ErrOr},
		{"object label", `{"value":"s","label":{}}`, []string{"/label"}, validator.ErrOr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Cell

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
