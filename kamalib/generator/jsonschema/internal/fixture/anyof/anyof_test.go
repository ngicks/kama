package anyof

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"reflect"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema/jsonschemart"
	"github.com/ngicks/kama/kamaruntime/validator"
)

// allFields compares unexported fields too, such as those of opt.Option.
var allFields = gocmp.Exporter(func(reflect.Type) bool { return true })

func TestDecode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Doc
		out  string
	}{
		{
			"first member wins and keeps the members of the others",
			`{"label":{"name":"a","tag":"b"},"value":"s"}`,
			Doc{
				Label: Label{Kind: LabelNamed{Named: Named{
					Name:                 "a",
					AdditionalProperties: map[string]jsontext.Value{"tag": jsontext.Value(`"b"`)},
				}}},
				Value: DocValue{Kind: DocValueString{String: "s"}},
			},
			`{"label":{"name":"a","tag":"b"},"value":"s"}`,
		},
		{
			"second member",
			`{"label":{"tag":"b"},"value":3}`,
			Doc{
				Label: Label{Kind: LabelTagged{Tagged: Tagged{Tag: "b"}}},
				Value: DocValue{Kind: DocValueInteger{Integer: 3}},
			},
			`{"label":{"tag":"b"},"value":3}`,
		},
		{
			"null member",
			`{"label":{"name":"a"},"value":null,"alt":null}`,
			Doc{
				Label: Label{Kind: LabelNamed{Named: Named{Name: "a"}}},
				Value: DocValue{Kind: DocValueNull{}},
				Alt:   opt.Some(DocValue{Kind: DocValueNull{}}),
			},
			`{"label":{"name":"a"},"value":null,"alt":null}`,
		},
		{
			"optional union",
			`{"label":{"name":"a"},"value":null,"alt":"x"}`,
			Doc{
				Label: Label{Kind: LabelNamed{Named: Named{Name: "a"}}},
				Value: DocValue{Kind: DocValueNull{}},
				Alt:   opt.Some(DocValue{Kind: DocValueString{String: "x"}}),
			},
			`{"label":{"name":"a"},"value":null,"alt":"x"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Doc
			assert.NilError(t, json.Unmarshal([]byte(tc.in), &v))
			assert.DeepEqual(t, v, tc.want, allFields)

			out, err := json.Marshal(v)
			assert.NilError(t, err)
			assert.Equal(t, canonical(t, out), canonical(t, []byte(tc.out)))
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
		{"missing", `{"label":{"name":"a"}}`, []string{"/value"}, validator.ErrRequired},
		{
			"no member",
			`{"label":{"name":"a"},"value":true}`,
			[]string{"/value"},
			jsonschemart.ErrNoMatch,
		},
		{
			"member fails",
			`{"label":{"name":"a"},"value":1.5}`,
			[]string{"/value"},
			jsonschemart.ErrNoMatch,
		},
		{
			"object member fails",
			`{"label":{"name":5},"value":null}`,
			[]string{"/label", "/label/name"},
			jsonschemart.ErrNoMatch,
		},
		{"empty object", `{"label":{},"value":null}`, []string{"/label"}, jsonschemart.ErrNoMatch},
		{
			"optional union",
			`{"label":{"name":"a"},"value":null,"alt":[]}`,
			[]string{"/alt"},
			jsonschemart.ErrNoMatch,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Doc

			err := json.Unmarshal([]byte(tc.in), &v)
			assert.Assert(t, err != nil)

			se, ok := errors.AsType[*json.SemanticError](err)
			assert.Assert(t, ok, "%T: %v", err, err)
			assert.DeepEqual(t, validator.Pointers(se.Err), tc.pointers)
			assert.ErrorIs(t, se.Err, tc.kind)
		})
	}
}

func TestMarshalNilKind(t *testing.T) {
	_, err := json.Marshal(Doc{Label: Label{Kind: LabelNamed{}}})
	assert.ErrorIs(t, err, jsonschemart.ErrNilKind)
}

func canonical(t *testing.T, b []byte) string {
	t.Helper()

	v := jsontext.Value(b)
	assert.NilError(t, v.Canonicalize())

	return string(v)
}
