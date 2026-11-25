package validation

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

const valid = `{"id":"AB1","items":["a","b"],"count":4,"ratio":0.5,"labels":{"x":"y"},
	"level":2,"coupon":"c","discount":1,"closed":{"a":true},"typed":{"n":-1,"m":3},"pick":"abcd"}`

func TestDecodeValid(t *testing.T) {
	var v Order
	assert.NilError(t, json.Unmarshal([]byte(valid), &v))

	assert.DeepEqual(t, v.Typed.Value().AdditionalProperties, map[string]OrderTypedValue{"m": 3})
	assert.DeepEqual(t, v.Pick.Value().Kind, OrderPickKind(OrderPickOption2{Option2: "abcd"}))

	out, err := json.Marshal(v)
	assert.NilError(t, err)
	assert.Equal(t, canonical(t, out), canonical(t, []byte(valid)))
}

// TestDecodeInvalid checks every keyword fails the decoding at the member
// holding it, through Validate.
func TestDecodeInvalid(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		pointers []string
		kind     error
	}{
		{"minLength", `{"id":"AB","items":["a"]}`, []string{"/id"}, validator.ErrLength},
		{"pattern", `{"id":"ab1","items":["a"]}`, []string{"/id"}, validator.ErrPattern},
		{
			"minimum of an integer",
			`{"id":"ABC","items":["a"],"count":0}`,
			[]string{"/count"},
			validator.ErrCompare,
		},
		{
			"exclusiveMaximum",
			`{"id":"ABC","items":["a"],"count":100}`,
			[]string{"/count"},
			validator.ErrCompare,
		},
		{
			"multipleOf",
			`{"id":"ABC","items":["a"],"count":3}`,
			[]string{"/count"},
			validator.ErrMultiple,
		},
		{
			"exclusiveMinimum",
			`{"id":"ABC","items":["a"],"ratio":0}`,
			[]string{"/ratio"},
			validator.ErrCompare,
		},
		{"minItems", `{"id":"ABC","items":[]}`, []string{"/items"}, validator.ErrLength},
		{"uniqueItems", `{"id":"ABC","items":["a","a"]}`, []string{"/items"}, validator.ErrUnique},
		{
			"pattern of items",
			`{"id":"ABC","items":["A"]}`,
			[]string{"/items/0"},
			validator.ErrPattern,
		},
		{
			"maxProperties of a map",
			`{"id":"ABC","items":["a"],"labels":{"a":"1","b":"2","c":"3"}}`,
			[]string{"/labels"},
			validator.ErrLength,
		},
		{
			"minLength of map values",
			`{"id":"ABC","items":["a"],"labels":{"a":""}}`,
			[]string{"/labels/a"},
			validator.ErrLength,
		},
		{"enum", `{"id":"ABC","items":["a"],"level":4}`, []string{"/level"}, validator.ErrConst},
		{
			"dependentRequired",
			`{"id":"ABC","items":["a"],"coupon":"c"}`,
			[]string{"/discount"},
			validator.ErrRequired,
		},
		{
			"maxProperties",
			`{"id":"ABC","items":["a"],"a":1,"b":2,"c":3,"d":4,"e":5,"f":6,"g":7,"h":8,"i":9,"j":10}`,
			[]string{""},
			validator.ErrLength,
		},
		{
			"additionalProperties false",
			`{"id":"ABC","items":["a"],"closed":{"b":1}}`,
			[]string{"/closed/b"},
			validator.ErrClosed,
		},
		{
			"additionalProperties schema",
			`{"id":"ABC","items":["a"],"typed":{"m":-1}}`,
			[]string{"/typed/m"},
			validator.ErrCompare,
		},
		{
			"no member of a oneOf",
			`{"id":"ABC","items":["a"],"pick":"abc"}`,
			[]string{"/pick"},
			validator.ErrOneOf,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Order

			err := json.Unmarshal([]byte(tc.in), &v)

			se, ok := errors.AsType[*json.SemanticError](err)
			assert.Assert(t, ok, "%T: %v", err, err)
			assert.DeepEqual(t, validator.Pointers(se.Err), tc.pointers)
			assert.ErrorIs(t, se.Err, tc.kind)
		})
	}
}

// TestUnknownMember checks a value of a oneOf no member matches: decoding
// the union alone keeps it, Validate reports it and encoding writes it
// back.
func TestUnknownMember(t *testing.T) {
	var p OrderPick
	assert.NilError(t, json.Unmarshal([]byte(`"abc"`), &p))

	u, ok := p.Kind.(OrderPickUnknown)
	assert.Assert(t, ok, "%T", p.Kind)
	assert.Equal(t, string(u.Value), `"abc"`)

	err := p.Validate(false)
	assert.ErrorIs(t, err, validator.ErrOneOf)
	assert.ErrorContains(t, err, "member Option1")
	assert.ErrorContains(t, err, "member Option2")

	out, err := json.Marshal(p)
	assert.NilError(t, err)
	assert.Equal(t, string(out), `"abc"`)

	assert.ErrorIs(t, OrderPick{Kind: OrderPickUnknown{Value: jsontext.Value(`1`)}}.Validate(false),
		validator.ErrOneOf)
	assert.ErrorIs(t, OrderPick{}.Validate(false), validator.ErrNil)
}

func canonical(t *testing.T, b []byte) string {
	t.Helper()

	v := jsontext.Value(b)
	assert.NilError(t, v.Canonicalize())

	return string(v)
}
