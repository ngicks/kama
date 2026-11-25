package tests

import (
	"errors"
	"slices"
	"testing"

	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamaruntime/validator"
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
		{"/even", validator.ErrMultiple, "want a multiple of 2, got 3"},
		{"/big", validator.ErrOr, "or: none of 2 alternatives holds\n" +
			"\tvalidation failed: const: want one of 0, got 5\n" +
			"\tvalidation failed: compare: want >= 10, got 5"},
		{"/word", validator.ErrNot, `not: want the value to fail regexp="^x"`},
		{"/either", validator.ErrOneOf, `oneof: want exactly one of 2 alternatives to hold,` +
			` got 2: regexp="^a", regexp="b$"`},
		{"/amount", validator.ErrRequired, `want a value, as "kind" has one`},
		{"/nested", validator.ErrOr, "none of 2 alternatives holds"},
		{"/held", validator.ErrOr, "none of 2 alternatives holds"},
	}

	var want []string

	for _, tc := range cases {
		want = append(want, tc.pointer)

		at := validator.At(err, tc.pointer)
		assert.Equal(t, len(at), 1, tc.pointer)
		assert.ErrorIs(t, at[0], tc.kind, tc.pointer)
		assert.ErrorContains(t, at[0], tc.msg, tc.pointer)
	}

	assert.DeepEqual(t, validator.Pointers(err), slices.Sorted(slices.Values(want)))

	// The failure of or is no failure of an alternative.
	big := validator.At(err, "/big")[0]
	assert.Assert(t, !errors.Is(big, validator.ErrCompare))

	causes := validator.Causes(big)
	assert.Equal(t, len(causes), 2)
	assert.ErrorIs(t, causes[0], validator.ErrConst)
	assert.ErrorIs(t, causes[1], validator.ErrCompare)

	// The causes of the nested or tell why each alternative fails, the
	// alternative failing not among them.
	nested := validator.Causes(validator.At(err, "/nested")[0])
	assert.Equal(t, len(nested), 2)
	assert.ErrorIs(t, nested[0], validator.ErrNot)
	assert.ErrorContains(t, nested[0], `want the value to fail or(const="5", const="7")`)
	assert.ErrorIs(t, nested[1], validator.ErrConst)

	first := invalidCombinators().Validate(true)
	assert.DeepEqual(t, validator.Pointers(first), []string{"/even"})
}

// TestCombinatorsStopWithinAnd fails both operands of one and. With
// stopOnFirstError it reports the first of them only.
func TestCombinatorsStopWithinAnd(t *testing.T) {
	c := validCombinators()
	c.Even = -1

	all := validator.At(c.Validate(false), "/even")
	assert.Equal(t, len(all), 2)
	assert.ErrorContains(t, all[0], "want >= 1, got -1")
	assert.ErrorContains(t, all[1], "want a multiple of 2, got -1")

	first := validator.At(c.Validate(true), "/even")
	assert.Equal(t, len(first), 1)
	assert.ErrorContains(t, first[0], "want >= 1, got -1")
}

// TestCombinatorsIf takes the branch cond selects. The error of cond never
// surfaces.
func TestCombinatorsIf(t *testing.T) {
	c := validCombinators()
	c.Kind = "other"

	at := validator.At(c.Validate(false), "/kind")
	assert.Equal(t, len(at), 1)
	assert.ErrorContains(t, at[0], `want one of "percent", "flat", got "other"`)
	assert.Assert(t, !errors.Is(at[0], validator.ErrRequired))
}

// TestCombinatorsOneOfNone carries the error of every alternative when no
// alternative holds.
func TestCombinatorsOneOfNone(t *testing.T) {
	c := validCombinators()
	c.Either = "x"

	at := validator.At(c.Validate(false), "/either")
	assert.Equal(t, len(at), 1)
	assert.ErrorContains(t, at[0], "oneof: none of 2 alternatives holds")
	assert.Equal(t, len(validator.Causes(at[0])), 2)
}

func TestSparse(t *testing.T) {
	one := 1

	assert.NilError(t, model.Sparse{}.Validate(false))
	assert.NilError(t, model.Sparse{A: opt.Some(1), B: &one}.Validate(false))

	err := model.Sparse{C: opt.Some(1)}.Validate(false)
	assert.ErrorIs(t, err, validator.ErrOr)
	assert.ErrorContains(t, err, "want member count == 0, got 1")
	assert.ErrorContains(t, err, "want member count >= 2, got 1")
	assert.DeepEqual(t, validator.Pointers(err), []string{""})
}

func TestCounted(t *testing.T) {
	assert.NilError(t, model.Counted{}.Validate(false))

	err := model.Counted{A: opt.Some(1)}.Validate(false)
	assert.ErrorIs(t, err, validator.ErrLength)
	assert.ErrorContains(t, err, "want member count <= 1, got 2")
}

// TestCombinatorsHeldOperand runs the operand cmp of if on the value the
// pointer Limit holds, while requires reads the pointer itself.
func TestCombinatorsHeldOperand(t *testing.T) {
	small, big := 5, 100

	c := validCombinators()
	c.Limit = &small
	assert.NilError(t, c.Validate(false))

	c.Limit, c.Note = &big, opt.Some("n")
	assert.NilError(t, c.Validate(false))

	c.Note = opt.None[string]()
	err := c.Validate(false)
	assert.ErrorIs(t, err, validator.ErrRequired)
	assert.DeepEqual(t, validator.Pointers(err), []string{"/note"})
}
