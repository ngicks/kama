package tests

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixtureext/textext"
	"github.com/ngicks/kama/kamaruntime/validator"
)

func validOrder() textext.Order {
	p := 1

	return textext.Order{
		Counts: textext.NewBag(0, 2),
		Items:  textext.NewBag(textext.Item{P: &p}),
		Note:   textext.Some("abc"),
		Held:   textext.Some(textext.Item{P: &p}),
		Entry:  textext.Pair[string, int]{Key: "k", Value: 1, Set: true},
		Tag:    textext.NewLabel("x"),
	}
}

func TestTextExtensionValid(t *testing.T) {
	o := validOrder()

	for _, stop := range []bool{true, false} {
		assert.NilError(t, o.Validate(stop))
	}

	// The value of Entry is not set, so its guard skips v=cmp(ge=1).
	o.Entry = textext.Pair[string, int]{Key: "k", Value: 0}
	assert.NilError(t, o.Validate(false))
}

func TestTextExtensionInvalid(t *testing.T) {
	o := textext.Order{
		Counts: textext.NewBag(1, -1),
		Items:  textext.NewBag(textext.Item{}, textext.Item{}),
		Held:   textext.Some(textext.Item{}),
		Entry:  textext.Pair[string, int]{Value: 0, Set: true},
		Tag:    textext.NewLabel("none"),
	}

	err := o.Validate(false)

	for _, tc := range []struct {
		pointer string
		kind    error
	}{
		{"/Counts/1", validator.ErrCompare},
		{"/Items/0/P", validator.ErrRequired},
		{"/Items/1/P", validator.ErrRequired},
		{"/Note", validator.ErrRequired},
		{"/Held/P", validator.ErrRequired},
		{"/Entry/key", validator.ErrLength},
		{"/Entry/value", validator.ErrCompare},
		{"/Tag", validator.ErrCompare},
	} {
		at := validator.At(err, tc.pointer)
		assert.Equal(t, len(at), 1, "%s in %v", tc.pointer, validator.Pointers(err))
		assert.ErrorIs(t, at[0], tc.kind, tc.pointer)
	}

	assert.Equal(t, len(validator.Pointers(err)), 8, "%v", validator.Pointers(err))

	// The length of Counts counts through the helper of ext.toml, and the
	// zero test of Tag trims through strings.
	o = validOrder()
	o.Counts = textext.NewBag[int]()
	o.Note = textext.Some("abcd")
	o.Tag = textext.NewLabel("  ")

	err = o.Validate(false)
	assert.ErrorIs(t, validator.At(err, "/Counts")[0], validator.ErrLength)
	assert.ErrorIs(t, validator.At(err, "/Note")[0], validator.ErrLength)
	assert.Equal(t, len(validator.At(err, "/Tag")), 1, "%v", validator.Pointers(err))
}
