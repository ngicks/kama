package nullableenum

import (
	"encoding/json/v2"
	"errors"
	"reflect"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

// allFields compares unexported fields too, such as those of opt.Option.
var allFields = gocmp.Exporter(func(reflect.Type) bool { return true })

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
			Levels{
				Ref:   und.Null[Level](),
				Const: und.Null[LevelsConst](),
				Big:   und.Null[LevelsBig](),
			},
		},
		{
			"allowed",
			`{"ref":2,"const":"x","big":18446744073709551616,"number":1.5}`,
			Levels{
				Ref:    und.Defined[Level](2),
				Const:  und.Defined[LevelsConst]("x"),
				Big:    und.Defined[LevelsBig](18446744073709551616),
				Number: opt.Some(1.5),
			},
		},
		{"number written otherwise", `{"number":15e-1}`, Levels{Number: opt.Some(1.5)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Levels
			assert.NilError(t, json.Unmarshal([]byte(tc.in), &v))
			assert.DeepEqual(t, v, tc.want, allFields)
		})
	}
}

// TestDecodeFloatPrecision checks a number the float64 of an enum cannot
// tell apart from an allowed one, since the value compares as float64.
func TestDecodeFloatPrecision(t *testing.T) {
	var v Levels
	assert.NilError(t, json.Unmarshal([]byte(`{"number":18446744073709551617}`), &v))
	assert.Equal(t, v.Number.Value(), float64(18446744073709551616))
}

func TestDecodeError(t *testing.T) {
	cases := []struct {
		name string
		in   string
		at   string
	}{
		{"enum behind a reference in a nullable union", `{"ref":9}`, "/ref"},
		{"const in a nullable union", `{"const":"zzz"}`, "/const"},
		{"number in a nullable union", `{"big":1}`, "/big"},
		{"enum of numbers", `{"number":2}`, "/number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Levels

			err := json.Unmarshal([]byte(tc.in), &v)

			se, ok := errors.AsType[*json.SemanticError](err)
			assert.Assert(t, ok, "%T: %v", err, err)
			assert.DeepEqual(t, validator.Pointers(se.Err), []string{tc.at})
			assert.ErrorIs(t, se.Err, validator.ErrConst)
		})
	}
}
