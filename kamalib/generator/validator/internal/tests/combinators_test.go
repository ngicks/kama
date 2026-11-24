package tests

import (
	"errors"
	"slices"
	"testing"

	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamalib/pkg/validation"
)

func validCombinators() model.Combinators {
	amount := 3

	return model.Combinators{
		Even:   2,
		Big:    0,
		Word:   "abc",
		Either: "ac",
		Kind:   "fixed",
		Amount: &amount,
		Nested: -1,
		Held:   opt.Some(10),
	}
}

// invalidCombinators breaks every combinator of Combinators once.
func invalidCombinators() model.Combinators {
	return model.Combinators{
		Even:   3,
		Big:    5,
		Word:   "xyz",
		Either: "axb",
		Kind:   "fixed",
		Nested: 5,
		Held:   opt.Some(5),
	}
}

func TestCombinatorsValid(t *testing.T) {
	for _, stop := range []bool{true, false} {
		assert.NilError(t, validCombinators().Validate(stop))
	}

	c := validCombinators()
	c.Big, c.Either, c.Kind, c.Amount, c.Nested, c.Held = 12, "xb", "flat", nil, 8, opt.None[int]()
	assert.NilError(t, c.Validate(false))
}

func TestCombinatorsInvalid(t *testing.T) {
	err := invalidCombinators().Validate(false)

	cases := []struct {
		pointer string
		kind    error
		msg     string
	}{
		{"/even", validation.ErrMultiple, "want a multiple of 2, got 3"},
		{"/big", validation.ErrOr, "or: none of 2 alternatives holds\n" +
			"\tvalidation failed: const: want one of 0, got 5\n" +
			"\tvalidation failed: compare: want >= 10, got 5"},
		{"/word", validation.ErrNot, `not: want the value to fail regexp="^x"`},
		{"/either", validation.ErrOneOf, `oneof: want exactly one of 2 alternatives to hold,` +
			` got 2: regexp="^a", regexp="b$"`},
		{"/amount", validation.ErrRequired, `want a value, as "kind" has one`},
		{"/nested", validation.ErrOr, "none of 2 alternatives holds"},
		{"/held", validation.ErrOr, "none of 2 alternatives holds"},
	}

	var want []string

	for _, tc := range cases {
		want = append(want, tc.pointer)

		at := validation.At(err, tc.pointer)
		assert.Equal(t, len(at), 1, tc.pointer)
		assert.ErrorIs(t, at[0], tc.kind, tc.pointer)
		assert.ErrorContains(t, at[0], tc.msg, tc.pointer)
	}

	assert.DeepEqual(t, validation.Pointers(err), slices.Sorted(slices.Values(want)))

	// The failure of or is no failure of an alternative.
	big := validation.At(err, "/big")[0]
	assert.Assert(t, !errors.Is(big, validation.ErrCompare))

	causes := validation.Causes(big)
	assert.Equal(t, len(causes), 2)
	assert.ErrorIs(t, causes[0], validation.ErrConst)
	assert.ErrorIs(t, causes[1], validation.ErrCompare)

	// The causes of the nested or tell why each alternative fails, the
	// alternative failing not among them.
	nested := validation.Causes(validation.At(err, "/nested")[0])
	assert.Equal(t, len(nested), 2)
	assert.ErrorIs(t, nested[0], validation.ErrNot)
	assert.ErrorContains(t, nested[0], `want the value to fail or(const="5", const="7")`)
	assert.ErrorIs(t, nested[1], validation.ErrConst)

	first := invalidCombinators().Validate(true)
	assert.DeepEqual(t, validation.Pointers(first), []string{"/even"})
}

// TestCombinatorsStopWithinAnd fails both operands of one and. With
// stopOnFirstError it reports the first of them only.
func TestCombinatorsStopWithinAnd(t *testing.T) {
	c := validCombinators()
	c.Even = -1

	all := validation.At(c.Validate(false), "/even")
	assert.Equal(t, len(all), 2)
	assert.ErrorContains(t, all[0], "want >= 1, got -1")
	assert.ErrorContains(t, all[1], "want a multiple of 2, got -1")

	first := validation.At(c.Validate(true), "/even")
	assert.Equal(t, len(first), 1)
	assert.ErrorContains(t, first[0], "want >= 1, got -1")
}

// TestCombinatorsIf takes the branch cond selects. The error of cond never
// surfaces.
func TestCombinatorsIf(t *testing.T) {
	c := validCombinators()
	c.Kind = "other"

	at := validation.At(c.Validate(false), "/kind")
	assert.Equal(t, len(at), 1)
	assert.ErrorContains(t, at[0], `want one of "percent", "flat", got "other"`)
	assert.Assert(t, !errors.Is(at[0], validation.ErrRequired))
}

// TestCombinatorsOneOfNone carries the error of every alternative when no
// alternative holds.
func TestCombinatorsOneOfNone(t *testing.T) {
	c := validCombinators()
	c.Either = "x"

	at := validation.At(c.Validate(false), "/either")
	assert.Equal(t, len(at), 1)
	assert.ErrorContains(t, at[0], "oneof: none of 2 alternatives holds")
	assert.Equal(t, len(validation.Causes(at[0])), 2)
}

func TestSparse(t *testing.T) {
	one := 1

	assert.NilError(t, model.Sparse{}.Validate(false))
	assert.NilError(t, model.Sparse{A: opt.Some(1), B: &one}.Validate(false))

	err := model.Sparse{C: opt.Some(1)}.Validate(false)
	assert.ErrorIs(t, err, validation.ErrOr)
	assert.ErrorContains(t, err, "want member count == 0, got 1")
	assert.ErrorContains(t, err, "want member count >= 2, got 1")
	assert.DeepEqual(t, validation.Pointers(err), []string{""})
}
