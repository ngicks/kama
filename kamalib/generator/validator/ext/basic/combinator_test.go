package basic

import (
	"go/types"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// resolve resolves text, one rule item, through reg.
func resolve(t *testing.T, text string, reg ext.Resolver) (ext.Rule, error) {
	t.Helper()

	args, err := expr.Parse(text)
	assert.NilError(t, err)
	assert.Equal(t, len(args), 1, text)

	return ext.Resolve(reg, args[0].Value)
}

// built returns the combinator r keeps the nodes of, built of its operands.
func built(t *testing.T, r ext.Rule) ext.Rule {
	t.Helper()

	c, ok := r.(combination)
	assert.Assert(t, ok, "%T", r)

	b, err := c.rule(nil)
	assert.NilError(t, err)

	return b
}

func TestAndResolve(t *testing.T) {
	r, err := resolve(t, `cmp(ge=1) and not const(3)`, ext.DefaultRegistry())
	assert.NilError(t, err)

	o, ok := built(t, r).(AndOptions)
	assert.Assert(t, ok, "%T", r)
	assert.Equal(t, len(o.Operands), 2)
	assert.Equal(t, o.Operands[0].Text, `cmp(ge=1)`)
	assert.DeepEqual(t, o.Operands[0].Rule, ext.Rule(CmpOptions{
		Comparisons: []Comparison{{Op: ">=", Operand: "1"}},
	}), ignoreRegistry)
	assert.Equal(t, o.Operands[1].Text, `not const(3)`)
	assert.Equal(t, built(t, o.Operands[1].Rule).(NotOptions).Operand.Text, `const(3)`)

	r, err = resolve(t, "always", ext.DefaultRegistry())
	assert.NilError(t, err)
	assert.DeepEqual(t, r, ext.Rule(AndOptions{}))
}

func TestCombinatorResolveErrors(t *testing.T) {
	reg := ext.DefaultRegistry()

	cases := []struct{ text, msg string }{
		{"x and zero", "operand x: unknown validator x; known: always, closed, cmp,"},
		{`cmp(ge=1) or length`, "operand length: want one or more comparisons"},
		{"oneof(zero or (always and not x))", "operand zero or always and not x:" +
			" operand always and not x: operand not x: operand x: unknown validator x"},
		{`requires("a") and members(ge=1)`, "operands read the siblings of a struct field" +
			" and validate a struct as a whole"},
		{"always(1)", "always takes 0 positional args"},
	}

	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			_, err := resolve(t, c.text, reg)
			assert.ErrorIs(t, err, ext.ErrArgs)
			assert.ErrorContains(t, err, c.msg)
		})
	}

	_, err := resolve(t, "x and zero", reg)
	assert.ErrorIs(t, err, ext.ErrUnknown)
}

func TestCombinatorResolveShapes(t *testing.T) {
	reg := ext.DefaultRegistry()

	r, err := resolve(t, "nil if zero else always", reg)
	assert.NilError(t, err)
	assert.Assert(t, built(t, r).(IfOptions).Else == nil)

	r, err = resolve(t, "nil if zero else never", reg)
	assert.NilError(t, err)
	assert.Equal(t, built(t, r).(IfOptions).Else.Text, "never")

	r, err = resolve(t, "oneof(zero, nil)", reg)
	assert.NilError(t, err)
	assert.Equal(t, len(built(t, r).(OneOfOptions).Operands), 2)

	r, err = resolve(t, "never", reg)
	assert.NilError(t, err)
	assert.DeepEqual(t, r, ext.Rule(OrOptions{}))
}

// named is Required registered under another name.
type named struct {
	Required
	name string
}

func (n named) Name() string { return n.name }

// TestCombinatorResolvesThroughRegistry resolves the operands through the
// registry given, whose validators replace the built-ins.
func TestCombinatorResolvesThroughRegistry(t *testing.T) {
	reg := ext.DefaultRegistry().Clone()
	reg.Register(named{name: "cmp"})

	r, err := resolve(t, "cmp and zero", reg)
	assert.NilError(t, err)
	assert.DeepEqual(
		t,
		built(t, r).(AndOptions).Operands[0],
		Operand{Text: "cmp", Rule: RequiredOptions{}},
		ignoreRegistry,
	)

	_, err = resolve(t, "cmp and zero", ext.DefaultRegistry())
	assert.ErrorContains(t, err, "want one or more comparisons")
}

func TestCombinatorCheck(t *testing.T) {
	reg := ext.DefaultRegistry()
	str, ptr := types.Typ[types.String], types.NewPointer(types.Typ[types.Int])

	for _, text := range []string{
		`regexp("^a") and length(ge=1)`,
		`regexp("^a") or length(ge=1)`,
		`oneof(regexp("^a"), length(ge=1))`,
		`length(ge=1) if regexp("^a") else always`,
	} {
		r, err := resolve(t, text, reg)
		assert.NilError(t, err)
		assert.NilError(t, r.Check(str), text)

		err = r.Check(ptr)
		assert.ErrorContains(
			t,
			err,
			`operand regexp("^a"): *int is neither a string nor a []byte type`,
			text,
		)
		assert.ErrorContains(
			t,
			err,
			`operand length(ge=1): *int is not a string, slice, array, map or channel type`,
			text,
		)
	}

	r, err := resolve(t, "not nil", reg)
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

	r, err := resolve(t, `requires("b") if const(1) else always`, reg)
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

	r, err = resolve(t, `const(1) or requires("b")`, reg)
	assert.NilError(t, err)

	bound, err = r.(ext.SiblingRule).Siblings(s, "a")
	assert.NilError(t, err)

	at := bound.(atField)
	assert.Equal(t, at.field, "a")
	_, ok = at.rule.(OrOptions).Operands[0].Rule.(onHeld).rule.(ConstOptions)
	assert.Assert(t, ok, "%T", at.rule.(OrOptions).Operands[0].Rule)

	r, err = resolve(t, `requires("z") and always`, reg)
	assert.NilError(t, err)

	_, err = r.(ext.SiblingRule).Siblings(s, "a")
	assert.ErrorContains(
		t,
		err,
		`operand requires("z"): no sibling field encodes as the member "z"`,
	)

	r, err = resolve(t, "const(1) and always", reg)
	assert.NilError(t, err)

	_, ok = r.(ext.SiblingRule)
	assert.Assert(t, !ok, "%T", r)
}

func TestCombinatorStruct(t *testing.T) {
	reg := ext.DefaultRegistry()

	r, err := resolve(t, `not members(ge=2)`, reg)
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
	assert.ErrorContains(t, err, `operand members(ge=2): the embedded fallback field`)
}

// TestCombinatorWarnsOnce reports the unknown arg of an operand when the
// combinator is resolved, not each time it is used.
func TestCombinatorWarnsOnce(t *testing.T) {
	var warnings []error

	reg := ext.WithWarn(ext.DefaultRegistry(), func(w error) { warnings = append(warnings, w) })

	r, err := resolve(t, "length(ge=1, nope=2) or zero", reg)
	assert.NilError(t, err)
	assert.NilError(t, r.Check(types.Typ[types.String]))
	assert.Equal(t, len(warnings), 1)
	assert.ErrorContains(t, warnings[0], "length: unknown arg nope=2")
}
