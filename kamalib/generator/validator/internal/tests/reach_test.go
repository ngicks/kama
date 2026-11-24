package tests

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamalib/pkg/validation"
)

func TestReachShape(t *testing.T) {
	for _, ok := range []string{
		`{"a": 1, "b c": [], "xy": "s"}`,
		`["a", 1, 0]`,
		`["a", 0, 0]`,
		`"long text"`,
	} {
		assert.NilError(t, model.Shape(ok).Validate(false), ok)
	}

	cases := []struct {
		text    string
		pointer string
		kind    error
	}{
		{`{"a": 1.5}`, "/a", validation.ErrKind},
		{`{"xy": 1}`, "/xy", validation.ErrKind},
		{`{"z": 1}`, "/z", validation.ErrOr},
		{`{"abcd": "x"}`, "/abcd", validation.ErrLength},
		{`[1]`, "/0", validation.ErrKind},
		{`["a", -1]`, "/1", validation.ErrCompare},
		{`["a", 1]`, "", validation.ErrContains},
		{`["a", 0, 0, 0]`, "", validation.ErrContains},
	}
	for _, tc := range cases {
		err := model.Shape(tc.text).Validate(false)
		assert.ErrorIs(t, err, tc.kind, tc.text)
		assert.Assert(t, len(validation.At(err, tc.pointer)) > 0, "%s: %v", tc.text, err)
	}
}

func TestReachDecode(t *testing.T) {
	assert.NilError(t, model.Holder(`{"bag": {"mixed": 1}}`).Validate(false))

	err := model.Holder(`{"bag": 1}`).Validate(false)
	assert.ErrorIs(t, err, validation.ErrDecode)
	assert.Assert(t, len(validation.At(err, "/bag")) > 0, "%v", err)
}

func TestReachTree(t *testing.T) {
	for _, ok := range []string{
		`{"a": 1, "n": {"b": 2, "x": null}}`,
		`{"b": 1, "n": {"a": {}}}`,
		`["a", "b"]`,
		`[null, null]`,
		`1`,
	} {
		assert.NilError(t, model.Tree(ok).Validate(false), ok)
	}

	cases := []struct {
		text    string
		pointer string
		kind    error
	}{
		// No branch of or evaluates x, which must then be null.
		{`{"a": 1, "x": 1}`, "/x", validation.ErrKind},
		{`{"n": {"a": 1, "x": 1}}`, "/n/x", validation.ErrKind},
		{`["a", 1]`, "/1", validation.ErrKind},
		{`[1, null]`, "/0", validation.ErrKind},
	}
	for _, tc := range cases {
		err := model.Tree(tc.text).Validate(false)
		assert.ErrorIs(t, err, tc.kind, tc.text)
		assert.Assert(t, len(validation.At(err, tc.pointer)) > 0, "%s: %v", tc.text, err)
	}
}

func TestReachEncoded(t *testing.T) {
	assert.NilError(t, model.Encoded{A: opt.None[string](), X: nil}.Validate(false))
	assert.NilError(
		t,
		model.Encoded{
			A: opt.Some("a"),
			X: map[string]jsontext.Value{"n": jsontext.Value("1")},
		}.Validate(
			false,
		),
	)
	assert.NilError(t, model.Pairs{1, 0, 2}.Validate(false))

	cases := []struct {
		name    string
		err     error
		pointer string
		kind    error
	}{
		{"field", model.Encoded{A: opt.Some("b"), X: nil}.Validate(false), "/a", validation.ErrNot},
		{
			"rest",
			model.Encoded{
				A: opt.None[string](),
				X: map[string]jsontext.Value{"n": jsontext.Value(`"s"`)},
			}.
				Validate(
					false,
				),
			"/n",
			validation.ErrKind,
		},
		{
			"not",
			model.Encoded{
				A: opt.None[string](),
				X: map[string]jsontext.Value{"x": jsontext.Value("1")},
			}.
				Validate(
					false,
				),
			"",
			validation.ErrNot,
		},
		{"slice", model.Pairs{0, 0}.Validate(false), "", validation.ErrContains},
	}
	for _, tc := range cases {
		assert.ErrorIs(t, tc.err, tc.kind, tc.name)
		assert.Assert(t, len(validation.At(tc.err, tc.pointer)) > 0, "%s: %v", tc.name, tc.err)
	}
}
