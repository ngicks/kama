package tests

import (
	"encoding/json/jsontext"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamalib/pkg/validation"
)

func TestRawKinds(t *testing.T) {
	for _, ok := range []string{`1`, `1.5`, `"abc"`} {
		assert.NilError(t, model.Loose(ok).Validate(false), ok)
	}

	for text, kind := range map[string]error{
		`0.5`:    validation.ErrCompare,
		`"abcd"`: validation.ErrLength,
		`null`:   validation.ErrKind,
		`[]`:     validation.ErrKind,
	} {
		assert.ErrorIs(t, model.Loose(text).Validate(false), kind, text)
	}
}

func validRawBag() model.RawBag {
	return model.RawBag{
		Mixed:  jsontext.Value(`[1, "x"]`),
		Object: jsontext.Value(`{"a": 1}`),
		Fixed:  jsontext.Value(`{"b": []}`),
		Below:  model.Loose(`3`),
	}
}

func TestRawFields(t *testing.T) {
	assert.NilError(t, validRawBag().Validate(false))

	for _, mixed := range []string{`1.5`, `"abc"`, `{"x": 1}`, `true`} {
		b := validRawBag()
		b.Mixed = jsontext.Value(mixed)
		assert.NilError(t, b.Validate(false), mixed)
	}

	b := validRawBag()
	b.Fixed = jsontext.Value(`1.0`)
	assert.NilError(t, b.Validate(false))

	// cmp holds on a string, so not(cmp=">=5") fails on it, as not with
	// minimum does in JSON Schema.
	b = validRawBag()
	b.Below = model.Loose(`"ab"`)
	assert.ErrorIs(t, b.Validate(false), validation.ErrNot)

	cases := []struct {
		set     func(b *model.RawBag)
		pointer string
		kind    error
	}{
		{
			func(b *model.RawBag) { b.Mixed = jsontext.Value(`1.25`) },
			"/mixed",
			validation.ErrMultiple,
		},
		{
			func(b *model.RawBag) { b.Mixed = jsontext.Value(`"b"`) },
			"/mixed",
			validation.ErrPattern,
		},
		{
			func(b *model.RawBag) { b.Mixed = jsontext.Value(`[1, 2, 3]`) },
			"/mixed",
			validation.ErrLength,
		},
		{
			func(b *model.RawBag) { b.Mixed = jsontext.Value(`[1, 1.0]`) },
			"/mixed",
			validation.ErrUnique,
		},
		{
			func(b *model.RawBag) { b.Object = jsontext.Value(`{"a": 1, "b": 2}`) },
			"/object",
			validation.ErrLength,
		},
		{
			func(b *model.RawBag) { b.Object = jsontext.Value(`{"b": 2}`) },
			"/object/a",
			validation.ErrRequired,
		},
		{func(b *model.RawBag) { b.Fixed = jsontext.Value(`"b"`) }, "/fixed", validation.ErrConst},
		{func(b *model.RawBag) { b.Below = model.Loose(`7`) }, "/below", validation.ErrNot},
	}

	for _, c := range cases {
		b := validRawBag()
		c.set(&b)

		err := b.Validate(false)
		assert.DeepEqual(t, validation.Pointers(err), []string{c.pointer})
		assert.ErrorIs(t, err, c.kind, c.pointer)
	}
}
