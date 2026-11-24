package nullable

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"reflect"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/validation"
)

// allFields compares unexported fields too, such as those of opt.Option.
var allFields = gocmp.Exporter(func(reflect.Type) bool { return true })

const required = `"reqOneOf":null,"reqAnyOf":null,"reqType":null`

func TestDecode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Nullable
	}{
		{"required null, optional absent", `{` + required + `}`, Nullable{}},
		{
			"every value",
			`{"reqOneOf":{"n":1},"optOneOf":{"n":2},"reqAnyOf":3,"optAnyOf":4,
			"reqType":"a","optType":"b","optInline":{"s":"c"}}`,
			Nullable{
				ReqOneOf:  opt.Some(Item{N: 1}),
				OptOneOf:  und.Defined(Item{N: 2}),
				ReqAnyOf:  opt.Some[int64](3),
				OptAnyOf:  und.Defined[int64](4),
				ReqType:   opt.Some("a"),
				OptType:   und.Defined("b"),
				OptInline: und.Defined(NullableOptInline{S: "c"}),
			},
		},
		{
			"optional null",
			`{` + required + `,"optOneOf":null,"optAnyOf":null,"optType":null,"optInline":null}`,
			Nullable{
				OptOneOf:  und.Null[Item](),
				OptAnyOf:  und.Null[int64](),
				OptType:   und.Null[string](),
				OptInline: und.Null[NullableOptInline](),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Nullable
			assert.NilError(t, json.Unmarshal([]byte(tc.in), &v))
			assert.DeepEqual(t, v, tc.want, allFields)

			out, err := json.Marshal(v)
			assert.NilError(t, err)
			assert.Equal(t, canonical(t, out), canonical(t, []byte(tc.in)))
		})
	}
}

func TestDecodeError(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		pointers []string
		kind     error
	}{
		{
			"required missing",
			`{"reqOneOf":null}`,
			[]string{"/reqAnyOf", "/reqType"},
			validation.ErrRequired,
		},
		{
			"non-null member invalid",
			`{` + required + `,"optOneOf":{}}`,
			[]string{"/optOneOf/n"},
			validation.ErrRequired,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Nullable

			err := json.Unmarshal([]byte(tc.in), &v)
			assert.Assert(t, err != nil)

			se, ok := errors.AsType[*json.SemanticError](err)
			assert.Assert(t, ok, "%T: %v", err, err)
			assert.DeepEqual(t, validation.Pointers(se.Err), tc.pointers)
			assert.ErrorIs(t, se.Err, tc.kind)
		})
	}

	var v Nullable

	err := json.Unmarshal([]byte(`{`+required+`,"optType":5}`), &v)
	_, ok := errors.AsType[*json.SemanticError](err)
	assert.Assert(t, ok, "%v", err)
}

func canonical(t *testing.T, b []byte) string {
	t.Helper()

	v := jsontext.Value(b)
	assert.NilError(t, v.Canonicalize())

	return string(v)
}
