package tests

import (
	"slices"
	"testing"

	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/parity"
	fileparity "github.com/ngicks/kama/kamalib/generator/validator/internal/fixturefile/parity"
	"github.com/ngicks/kama/kamalib/pkg/validation"
)

type validatable interface {
	Validate(stopOnFirstError bool) error
}

// parityValues are values of the types of one parity package.
type parityValues struct {
	valid      validatable
	invalid    validatable
	emptyLines validatable
	batch      validatable
	errNote    error
}

// TestParity runs the Validate methods of the parity package of the fixture,
// generated from directives, and of the fixturefile, generated from option
// files. Both behave the same.
func TestParity(t *testing.T) {
	order := parity.Order{
		Id:       "ABC-1",
		Lines:    []parity.Line{{Qty: 100, Url: opt.Some("https://example.com/a")}},
		Currency: "eur",
		Note:     "short",
	}
	bad := parity.Order{
		Id:       "abc",
		Lines:    []parity.Line{{Qty: 101, Url: opt.Some("/relative")}, {Qty: 0}},
		Currency: "EUR",
		Note:     "far too long",
	}
	empty := order
	empty.Lines = nil

	fileOrder := fileparity.Order{
		Id:       "ABC-1",
		Lines:    []fileparity.Line{{Qty: 100, Url: opt.Some("https://example.com/a")}},
		Currency: "eur",
		Note:     "short",
	}
	fileBad := fileparity.Order{
		Id:       "abc",
		Lines:    []fileparity.Line{{Qty: 101, Url: opt.Some("/relative")}, {Qty: 0}},
		Currency: "EUR",
		Note:     "far too long",
	}
	fileEmpty := fileOrder
	fileEmpty.Lines = nil

	cases := map[string]parityValues{
		"directives": {
			valid:      order,
			invalid:    bad,
			emptyLines: empty,
			batch:      parity.Batch{Orders: map[string]parity.Order{"b": bad, "a": order}},
			errNote:    parity.ErrNote,
		},
		"option files": {
			valid:      fileOrder,
			invalid:    fileBad,
			emptyLines: fileEmpty,
			batch: fileparity.Batch{
				Orders: map[string]fileparity.Order{"b": fileBad, "a": fileOrder},
			},
			errNote: fileparity.ErrNote,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) { testParity(t, tc) })
	}
}

func testParity(t *testing.T, tc parityValues) {
	for _, stop := range []bool{true, false} {
		assert.NilError(t, tc.valid.Validate(stop))
	}

	err := tc.invalid.Validate(false)

	cases := []struct {
		pointer string
		kind    error
		msg     string
	}{
		{"/id", validation.ErrPattern, `want a match of "^[A-Z]{3}-[0-9]+$", got "abc"`},
		{"/lines/0/qty", validation.ErrCompare, "want <= 100, got 101"},
		{"/lines/0/url", validation.ErrFormat, "want format url"},
		{"/lines/1/qty", validation.ErrCompare, "want > 0, got 0"},
		{"/currency", validation.ErrConst, `want one of "eur", "usd", got "EUR"`},
		{"/note", tc.errNote, tc.errNote.Error()},
	}

	var want []string

	for _, c := range cases {
		want = append(want, c.pointer)

		at := validation.At(err, c.pointer)
		assert.Equal(t, len(at), 1, c.pointer)
		assert.ErrorIs(t, at[0], c.kind)
		assert.ErrorContains(t, at[0], c.msg)
	}

	assert.DeepEqual(t, validation.Pointers(err), slices.Sorted(slices.Values(want)))
	assert.DeepEqual(t, validation.Pointers(tc.invalid.Validate(true)), []string{"/id"})

	at := validation.At(tc.emptyLines.Validate(false), "/lines")
	assert.Equal(t, len(at), 1)
	assert.ErrorIs(t, at[0], validation.ErrLength)

	assert.DeepEqual(t, validation.Pointers(tc.batch.Validate(true)), []string{"/orders/b/id"})
}
