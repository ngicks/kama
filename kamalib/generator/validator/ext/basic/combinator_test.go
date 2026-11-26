package basic

import (
	"go/types"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

func TestAndParse(t *testing.T) {
	r, err := And{}.Parse(`cmp=">=1", not(const=3)`, ext.DefaultRegistry())
	assert.NilError(t, err)

	o, ok := r.(AndOptions)
	assert.Assert(t, ok, "%T", r)
	assert.Equal(t, len(o.Operands), 2)
	assert.Equal(t, o.Operands[0].Text, `cmp=">=1"`)
	assert.DeepEqual(t, o.Operands[0].Rule, ext.Rule(CmpOptions{
		Comparisons: []Comparison{{Op: ">=", Operand: "1"}},
	}))
	assert.Equal(t, o.Operands[1].Text, `not(const="3")`)
	assert.Equal(t, o.Operands[1].Rule.(NotOptions).Operand.Text, `const="3"`)

	r, err = And{}.Parse("", ext.DefaultRegistry())
	assert.NilError(t, err)
	assert.DeepEqual(t, r, ext.Rule(AndOptions{Operands: []Operand{}}))
}

func TestCombinatorParseErrors(t *testing.T) {
	reg := ext.DefaultRegistry()

	cases := []struct {
		v        ext.Validator
		arg, msg string
	}{
		{And{}, "x", "operand x: unknown validator x; known: and, closed, cmp,"},
		{Or{}, "cmp=a b", `want , after cmp=a, got "b"`},
		{Or{}, `cmp=">=1", length`, "operand length: want one or more comparisons"},
		{Not{}, "", "want one operand, got 0"},
		{Not{}, "zero, nil", "want one operand, got 2"},
		{If{}, "zero", "want the operands cond, then and an optional else, got 1 operands"},
		{If{}, "zero, nil, nil, zero", "got 4 operands"},
		{OneOf{}, "or(and(not(x)))", "operand or(and(not(x))): operand and(not(x)):" +
			" operand not(x): operand x: unknown validator x"},
		{And{}, `requires=a, members=">=1"`, "operands read the siblings of a struct field" +
			" and validate a struct as a whole"},
	}

	for _, c := range cases {
		t.Run(c.v.Name()+"("+c.arg+")", func(t *testing.T) {
			_, err := c.v.Parse(c.arg, reg)
			assert.ErrorIs(t, err, ext.ErrArgs)
			assert.ErrorContains(t, err, c.msg)
		})
	}

	_, err := And{}.Parse("x", reg)
	assert.ErrorIs(t, err, ext.ErrUnknown)
}

func TestCombinatorParseShapes(t *testing.T) {
	reg := ext.DefaultRegistry()

	r, err := If{}.Parse("zero, nil", reg)
	assert.NilError(t, err)
	assert.Assert(t, r.(IfOptions).Else == nil)

	r, err = If{}.Parse("zero, nil, and()", reg)
	assert.NilError(t, err)
	assert.Equal(t, r.(IfOptions).Else.Text, "and()")

	r, err = OneOf{}.Parse("zero, nil", reg)
	assert.NilError(t, err)
	assert.Equal(t, len(r.(OneOfOptions).Operands), 2)

	r, err = Or{}.Parse("", reg)
	assert.NilError(t, err)
	assert.Equal(t, len(r.(OrOptions).Operands), 0)
}

// named is Required registered under another name.
type named struct {
	Required
	name string
}

func (n named) Name() string { return n.name }

// TestCombinatorResolvesThroughRegistry parses the operands through the
// registry given, whose validators replace the built-ins.
func TestCombinatorResolvesThroughRegistry(t *testing.T) {
	reg := ext.DefaultRegistry().Clone()
	reg.Register(named{name: "cmp"})

	r, err := And{}.Parse("cmp", reg)
	assert.NilError(t, err)
	assert.DeepEqual(
		t,
		r,
		ext.Rule(AndOptions{Operands: []Operand{{Text: "cmp", Rule: RequiredOptions{}}}}),
	)

	_, err = And{}.Parse("cmp", ext.DefaultRegistry())
	assert.ErrorContains(t, err, "want one or more comparisons")
}

func TestCombinatorCheck(t *testing.T) {
	reg := ext.DefaultRegistry()
	str, ptr := types.Typ[types.String], types.NewPointer(types.Typ[types.Int])

	for _, v := range []ext.Validator{And{}, Or{}, OneOf{}, If{}} {
		r, err := v.Parse(`regexp="^a", length=">=1"`, reg)
		assert.NilError(t, err)
		assert.NilError(t, r.Check(str), v.Name())

		err = r.Check(ptr)
		assert.ErrorContains(
			t,
			err,
			`operand regexp="^a": *int is neither a string nor a []byte type`,
			v.Name(),
		)
		assert.ErrorContains(
			t,
			err,
			`operand length=">=1": *int is not a string, slice, array, map or channel type`,
			v.Name(),
		)
	}

	r, err := Not{}.Parse("nil", reg)
	assert.NilError(t, err)
	assert.NilError(t, r.Check(ptr))
	assert.ErrorContains(t, r.Check(str), "operand nil: string is not nillable")
}

// TestCombinatorSiblings binds a combinator holding requires to the
// siblings of the field. The other operands are located at the field when
// the combinator returns their errors, and the combinator itself is when
// it returns an error of its own.
func TestCombinatorSiblings(t *testing.T) {
	reg := ext.DefaultRegistry()
	s := testStruct{
		names: []string{"a", "b"},
		types: map[string]types.Type{"a": types.Typ[types.Int], "b": types.Typ[types.Int]},
	}

	r, err := If{}.Parse("const=1, requires=b", reg)
	assert.NilError(t, err)

	sr, ok := r.(ext.SiblingRule)
	assert.Assert(t, ok, "%T", r)
	assert.ErrorContains(t, r.Check(types.Typ[types.Int]), "an operand reads the siblings")

	bound, err := sr.Siblings(s, "a")
	assert.NilError(t, err)

	o := bound.(IfOptions)
	assert.Equal(t, o.Cond.Rule.(atField).field, "a")
	_, ok = o.Then.Rule.(requiresOf)
	assert.Assert(t, ok, "%T", o.Then.Rule)
	assert.NilError(t, bound.Check(types.Typ[types.Int]))

	r, err = Or{}.Parse("const=1, requires=b", reg)
	assert.NilError(t, err)

	bound, err = r.(ext.SiblingRule).Siblings(s, "a")
	assert.NilError(t, err)

	at := bound.(atField)
	assert.Equal(t, at.field, "a")
	_, ok = at.rule.(OrOptions).Operands[0].Rule.(onHeld).rule.(ConstOptions)
	assert.Assert(t, ok, "%T", at.rule.(OrOptions).Operands[0].Rule)

	r, err = And{}.Parse("requires=z", reg)
	assert.NilError(t, err)

	_, err = r.(ext.SiblingRule).Siblings(s, "a")
	assert.ErrorContains(t, err, `operand requires="z": no sibling field encodes as the member "z"`)

	r, err = And{}.Parse("const=1", reg)
	assert.NilError(t, err)

	_, ok = r.(ext.SiblingRule)
	assert.Assert(t, !ok, "%T", r)
}

func TestCombinatorStruct(t *testing.T) {
	reg := ext.DefaultRegistry()

	r, err := Not{}.Parse(`members=">=2"`, reg)
	assert.NilError(t, err)

	sr, ok := r.(ext.StructRule)
	assert.Assert(t, ok, "%T", r)
	assert.ErrorContains(
		t,
		r.Check(types.Typ[types.Int]),
		"an operand validates a struct as a whole",
	)

	bound, err := sr.Struct(testStruct{})
	assert.NilError(t, err)

	_, ok = bound.(NotOptions).Operand.Rule.(membersOf)
	assert.Assert(t, ok, "%T", bound.(NotOptions).Operand.Rule)

	_, err = sr.Struct(testStruct{fallback: types.Typ[types.String]})
	assert.ErrorContains(t, err, `operand members=">=2": the embedded fallback field`)
}
