package prefilter

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

func TestSelect(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want ShapeKind
	}{
		{
			"circle",
			`{"kind":"circle","radius":1.5}`,
			ShapeCircle{Circle{Kind: CircleKindCircle, Radius: 1.5}},
		},
		{"square", `{"side":2}`, ShapeSquare{Square{}}},
		{"empty square", `{}`, ShapeSquare{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Shape
			assert.NilError(t, json.Unmarshal([]byte(tc.in), &v))

			out, err := json.Marshal(v)
			assert.NilError(t, err)
			assert.Equal(t, canonical(t, out), canonical(t, []byte(tc.in)))
			assert.Equal(t, kindName(v.Kind), kindName(tc.want))
		})
	}
}

// TestPrefilter decodes values that one member would decode were it not
// dropped first. Circle drops a value whose kind is not "circle" and Square
// a value holding a member it does not declare, so no member matches, the
// value is unknown and their decode errors stay out of the error Validate
// reports.
func TestPrefilter(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"other kind", `{"kind":"oval","radius":1,"side":2}`},
		{"extra member", `{"side":2,"color":"red"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Shape
			assert.NilError(t, json.Unmarshal([]byte(tc.in), &v))

			u, ok := v.Kind.(ShapeUnknown)
			assert.Assert(t, ok, "%T", v.Kind)
			assert.Equal(t, string(u.Value), tc.in)

			err := v.Validate(false)
			assert.ErrorIs(t, err, validator.ErrOneOf)
			assert.DeepEqual(t, validator.Pointers(err), []string{""})
			assert.Assert(t, !strings.Contains(err.Error(), "member "), err.Error())
		})
	}
}

func kindName(k ShapeKind) string {
	switch k.(type) {
	case ShapeCircle:
		return "circle"
	case ShapeSquare:
		return "square"
	}

	return ""
}

func canonical(t *testing.T, b []byte) string {
	t.Helper()

	v := jsontext.Value(b)
	assert.NilError(t, v.Canonicalize())

	return string(v)
}
