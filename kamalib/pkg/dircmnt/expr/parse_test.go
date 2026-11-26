package expr

import (
	"errors"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func name(n string) Call               { return Call{Name: n} }
func call(n string, args ...Arg) Call  { return Call{Name: n, Parens: true, Args: args} }
func pos(v Node) Arg                   { return Arg{Value: v} }
func kw(n string, v Node) Arg          { return Arg{Name: n, Value: v} }
func str(s string) String              { return String{Value: s} }
func num(s string) Number              { return Number{Text: s} }
func and(ops ...Node) And              { return And{Operands: ops} }
func or(ops ...Node) Or                { return Or{Operands: ops} }
func not(op Node) Not                  { return Not{Operand: op} }
func cond(then, ifNode, els Node) Cond { return Cond{If: ifNode, Then: then, Else: els} }
func list(elems ...Node) List          { return List{Elems: elems} }
func entry(k, v Node) Entry            { return Entry{Key: k, Value: v} }
func dict(entries ...Entry) Dict       { return Dict{Entries: entries} }
func positional(vs ...Node) (out []Arg) {
	for _, v := range vs {
		out = append(out, pos(v))
	}

	return out
}

// stripArgs zeroes the positions of args.
func stripArgs(args []Arg) []Arg {
	var out []Arg

	for _, a := range args {
		out = append(out, Arg{Name: a.Name, Value: strip(a.Value)})
	}

	return out
}

func strip(n Node) Node {
	switch x := n.(type) {
	case Call:
		return Call{Name: x.Name, Parens: x.Parens, Args: stripArgs(x.Args)}
	case And:
		return And{Operands: stripAll(x.Operands)}
	case Or:
		return Or{Operands: stripAll(x.Operands)}
	case Not:
		return Not{Operand: strip(x.Operand)}
	case Cond:
		return Cond{If: strip(x.If), Then: strip(x.Then), Else: strip(x.Else)}
	case String:
		return String{Value: x.Value}
	case Number:
		return Number{Text: x.Text}
	case Bool:
		return Bool{Value: x.Value}
	case Null:
		return Null{}
	case List:
		return List{Elems: stripAll(x.Elems)}
	case Dict:
		var out Dict
		for _, e := range x.Entries {
			out.Entries = append(out.Entries, entry(strip(e.Key), strip(e.Value)))
		}

		return out
	}

	return n
}

func stripAll(ns []Node) []Node {
	var out []Node

	for _, n := range ns {
		out = append(out, strip(n))
	}

	return out
}

func TestParse(t *testing.T) {
	cases := []struct {
		text string
		want []Arg
	}{
		{"", nil},
		{"  ", nil},
		{"required", positional(name("required"))},
		{"required,", positional(name("required"))},
		{"f()", positional(call("f"))},
		{
			"required, hook=off, length(min=1, max=3)",
			[]Arg{
				pos(name("required")),
				kw("hook", name("off")),
				pos(call("length", kw("min", num("1")), kw("max", num("3")))),
			},
		},
		{"pre=f, post=g", []Arg{kw("pre", name("f")), kw("post", name("g"))}},
		// The items of a directive mix positional and keyword args freely.
		{"hook=off, required", []Arg{kw("hook", name("off")), pos(name("required"))}},
		{`regexp(r"^[a-z]+\d$")`, positional(call("regexp", pos(str(`^[a-z]+\d$`))))},
		{`regexp('^a="b c"$')`, positional(call("regexp", pos(str(`^a="b c"$`))))},
		{"a and b and c", positional(and(name("a"), name("b"), name("c")))},
		{"a and (b and c)", positional(and(name("a"), and(name("b"), name("c"))))},
		{"(a and b) and c", positional(and(and(name("a"), name("b")), name("c")))},
		{"a or b and c", positional(or(name("a"), and(name("b"), name("c"))))},
		{"(a or b) and c", positional(and(or(name("a"), name("b")), name("c")))},
		{"((a))", positional(name("a"))},
		{"not a and b", positional(and(not(name("a")), name("b")))},
		{"not (a and b)", positional(not(and(name("a"), name("b"))))},
		{"not not a", positional(not(not(name("a"))))},
		{
			`requires("amount") if const("fixed") else kind("null")`,
			positional(cond(
				call("requires", pos(str("amount"))),
				call("const", pos(str("fixed"))),
				call("kind", pos(str("null"))),
			)),
		},
		{
			"a if b else c if d else e",
			positional(cond(name("a"), name("b"), cond(name("c"), name("d"), name("e")))),
		},
		{
			"f(x=a or b, y=not c)",
			positional(call("f", kw("x", or(name("a"), name("b"))), kw("y", not(name("c"))))),
		},
		{
			"const(1, -2, +3, 0.1, 1e-3, -0x1F, 123456789012345678901234567890)",
			positional(call("const",
				positional(num("1"), num("-2"), num("3"), num("0.1"), num("1e-3"),
					num("-0x1F"), num("123456789012345678901234567890"))...,
			)),
		},
		{
			`const(None, True, False, [1, "a"], {"k": [None]})`,
			positional(call("const",
				positional(
					Null{}, Bool{Value: true}, Bool{},
					list(num("1"), str("a")),
					dict(entry(str("k"), list(Null{}))),
				)...,
			)),
		},
		{"a # note", positional(name("a"))},
		{"a, # note\nb", positional(name("a"), name("b"))},
		{"f(a,\n  b)", positional(call("f", pos(name("a")), pos(name("b"))))},
	}

	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			got, err := Parse(c.text)
			assert.NilError(t, err)
			assert.DeepEqual(t, stripArgs(got), c.want)
		})
	}
}

func TestParsePositions(t *testing.T) {
	got, err := Parse("a, é=f(x), \n  not b")
	assert.NilError(t, err)

	assert.Equal(t, got[0].Start, Position{Offset: 0, Line: 1, Column: 1})
	assert.Equal(t, got[1].Start, Position{Offset: 3, Line: 1, Column: 4})
	assert.Equal(t, got[1].Value.Pos(), Position{Offset: 6, Line: 1, Column: 6})
	assert.Equal(t, got[1].Value.(Call).Args[0].Start, Position{Offset: 8, Line: 1, Column: 8})
	assert.Equal(t, got[2].Start, Position{Offset: 15, Line: 2, Column: 3})
	assert.Equal(t, got[2].Value.(Not).Operand.Pos(), Position{Offset: 19, Line: 2, Column: 7})

	got, err = Parse("x=-1, y=a and b")
	assert.NilError(t, err)
	assert.Equal(t, got[0].Value.Pos().Offset, 2)
	assert.Equal(t, got[1].Value.Pos().Offset, 8)
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		text   string
		offset int
		msg    string
	}{
		{"k=1, k=2", 5, "keyword arg k repeated"},
		{"f(k=1, k=2)", 7, "keyword arg k repeated"},
		{"f(k=1, b)", 7, "positional arg after keyword arg"},
		{"a) or (b", 1, "unbalanced )"},
		{"a), _(b", 1, "unbalanced )"},
		{"a) (b", 1, "unbalanced )"},
		{"f(a", 3, "want ')'"},
		{"a if b", 2, "conditional expression without else clause"},
		{"a == 1", 2, "operator == is not supported"},
		{"a in b", 2, "operator in is not supported"},
		{"a + 1", 2, "operator + is not supported"},
		{"-a", 0, "sign - before no number"},
		{"--1", 0, "sign - before no number"},
		{"-(1)", 0, "sign - before no number"},
		{"(a, b)", 0, "tuple is not supported"},
		{"()", 0, "tuple is not supported"},
		{"a.b", 0, "selector is not supported"},
		{"a[1]", 0, "index is not supported"},
		{"a[1:2]", 0, "slice is not supported"},
		{"[x for x in y]", 0, "comprehension is not supported"},
		{"lambda x: x", 0, "lambda is not supported"},
		{`b"x"`, 0, "is not supported"},
		{"f(a)(b)", 4, "call of call result is not supported"},
		{"f(*a)", 2, "* arg is not supported"},
		{"f(**a)", 2, "** arg is not supported"},
		{"None()", 0, "None is not callable"},
		{"True=1", 0, "True is no name"},
		{"f(None=1)", 2, "None is no name"},
		{"if(a, b)", 0, "got if, want primary expression"},
		{"hook=on hook=off", 12, "got identifier"},
		// A newline separates no items.
		{"a\nb", 3, "got identifier"},
		{"1e400", 5, "invalid float literal"},
		{"é, `x`", 4, "unexpected input character"},
		{"a\n)", 3, "unexpected ')'"},
	}

	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			_, err := Parse(c.text)
			assert.ErrorContains(t, err, c.msg)
			assert.Assert(t, errors.Is(err, ErrSyntax))

			var e *Error
			assert.Assert(t, errors.As(err, &e))
			assert.Equal(t, e.Pos.Offset, c.offset)
		})
	}
}

func TestIsName(t *testing.T) {
	for _, s := range []string{"required", "_x", "é", "assert", "off", "null"} {
		assert.Assert(t, IsName(s), s)
	}

	for _, s := range []string{"", "a b", "a.b", "1a", "a-b", "f()", "None", "True", "False"} {
		assert.Assert(t, !IsName(s), s)
	}

	for w := range strings.FieldsSeq(reservedWords) {
		assert.Assert(t, !IsName(w), w)
	}
}
