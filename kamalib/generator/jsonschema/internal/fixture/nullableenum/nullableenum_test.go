package nullableenum

import (
	"encoding/json/v2"
	"errors"
	"testing"

	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema/jsonschemart"
	"github.com/ngicks/kama/kamalib/pkg/validation"
)

func TestDecode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Levels
	}{
		{"absent", `{}`, Levels{}},
		{
			"null",
			`{"ref":null,"const":null,"big":null}`,
			Levels{Ref: und.Null[int64](), Const: und.Null[string](), Big: und.Null[float64]()},
		},
		{
			"allowed",
			`{"ref":2,"const":"x","big":18446744073709551616,"number":1.5}`,
			Levels{
				Ref:    und.Defined[int64](2),
				Const:  und.Defined("x"),
				Big:    und.Defined[float64](18446744073709551616),
				Number: opt.Some(1.5),
			},
		},
		{"number written otherwise", `{"number":15e-1}`, Levels{Number: opt.Some(1.5)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Levels
			assert.NilError(t, json.Unmarshal([]byte(tc.in), &v))
			assert.Equal(t, v, tc.want)
		})
	}
}

func TestDecodeError(t *testing.T) {
	cases := []struct {
		name string
		in   string
		at   string
	}{
		{"enum behind a reference in a nullable union", `{"ref":9}`, "/ref"},
		{"const in a nullable union", `{"const":"zzz"}`, "/const"},
		{"number above 2^53 in a nullable union", `{"big":18446744073709551617}`, "/big"},
		{"enum of numbers", `{"number":2}`, "/number"},
		{"enum of numbers above 2^53", `{"number":18446744073709551617}`, "/number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Levels

			err := json.Unmarshal([]byte(tc.in), &v)

			se, ok := errors.AsType[*json.SemanticError](err)
			assert.Assert(t, ok, "%T: %v", err, err)
			assert.DeepEqual(t, validation.Pointers(se.Err), []string{tc.at})
			assert.ErrorIs(t, se.Err, jsonschemart.ErrEnum)
		})
	}
}
