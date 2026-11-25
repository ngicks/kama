package tests

import (
	"encoding/json/jsontext"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamaruntime/validator"
)

func TestRawKinds(t *testing.T) {
	for _, ok := range []string{`1`, `1.5`, `"abc"`} {
		assert.NilError(t, model.Loose(ok).Validate(false), ok)
	}

	for text, kind := range map[string]error{
		`0.5`:    validator.ErrCompare,
		`"abcd"`: validator.ErrLength,
		`null`:   validator.ErrKind,
		`[]`:     validator.ErrKind,
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

	formats := validRawBag()
	formats.Id = jsontext.Value(`"123e4567-e89b-12d3-a456-426614174000"`)
	formats.Ipv4 = jsontext.Value(`"192.168.0.1"`)
	formats.Ipv6 = jsontext.Value(`"::1"`)
	assert.NilError(t, formats.Validate(false))

	// A format checks strings only.
	formats.Id, formats.Ipv4, formats.Ipv6 = jsontext.Value(
		`1`,
	), jsontext.Value(
		`[]`,
	), jsontext.Value(
		`null`,
	)
	assert.NilError(t, formats.Validate(false))

	for _, set := range []func(b *model.RawBag){
		func(b *model.RawBag) { b.Id = jsontext.Value(`"123e4567-e89b-12d3-a456-42661417400"`) },
		func(b *model.RawBag) { b.Id = jsontext.Value(`"123e4567xe89b-12d3-a456-426614174000"`) },
		func(b *model.RawBag) { b.Ipv4 = jsontext.Value(`"192.168.0.01"`) },
		func(b *model.RawBag) { b.Ipv4 = jsontext.Value(`"::1"`) },
		func(b *model.RawBag) { b.Ipv6 = jsontext.Value(`"192.168.0.1"`) },
		func(b *model.RawBag) { b.Ipv6 = jsontext.Value(`"fe80::1%eth0"`) },
	} {
		b := validRawBag()
		set(&b)
		assert.ErrorIs(t, b.Validate(false), validator.ErrFormat)
	}

	b := validRawBag()
	b.Fixed = jsontext.Value(`1.0`)
	assert.NilError(t, b.Validate(false))

	// cmp holds on a string, so not(cmp=">=5") fails on it, as not with
	// minimum does in JSON Schema.
	b = validRawBag()
	b.Below = model.Loose(`"ab"`)
	assert.ErrorIs(t, b.Validate(false), validator.ErrNot)

	cases := []struct {
		set     func(b *model.RawBag)
		pointer string
		kind    error
	}{
		{
			func(b *model.RawBag) { b.Mixed = jsontext.Value(`1.25`) },
			"/mixed",
			validator.ErrMultiple,
		},
		{
			func(b *model.RawBag) { b.Mixed = jsontext.Value(`"b"`) },
			"/mixed",
			validator.ErrPattern,
		},
		{
			func(b *model.RawBag) { b.Mixed = jsontext.Value(`[1, 2, 3]`) },
			"/mixed",
			validator.ErrLength,
		},
		{
			func(b *model.RawBag) { b.Mixed = jsontext.Value(`[1, 1.0]`) },
			"/mixed",
			validator.ErrUnique,
		},
		{
			func(b *model.RawBag) { b.Object = jsontext.Value(`{"a": 1, "b": 2}`) },
			"/object",
			validator.ErrLength,
		},
		{
			func(b *model.RawBag) { b.Object = jsontext.Value(`{"b": 2}`) },
			"/object/a",
			validator.ErrRequired,
		},
		{func(b *model.RawBag) { b.Fixed = jsontext.Value(`"b"`) }, "/fixed", validator.ErrConst},
		{func(b *model.RawBag) { b.Below = model.Loose(`7`) }, "/below", validator.ErrNot},
	}

	for _, c := range cases {
		b := validRawBag()
		c.set(&b)

		err := b.Validate(false)
		assert.DeepEqual(t, validator.Pointers(err), []string{c.pointer})
		assert.ErrorIs(t, err, c.kind, c.pointer)
	}
}
