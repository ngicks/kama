package expr

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestPrint(t *testing.T) {
	a, b, c, d := name("a"), name("b"), name("c"), name("d")

	cases := []struct {
		args []Arg
		text string
	}{
		{nil, ""},
		{positional(a, call("f")), "a, f()"},
		{[]Arg{pos(a), kw("hook", name("off"))}, "a, hook=off"},
		{
			positional(call("length", kw("min", num("1")), kw("max", num("3")))),
			"length(min=1, max=3)",
		},
		{positional(and(a, b, c)), "a and b and c"},
		{positional(and(a, and(b, c))), "a and (b and c)"},
		{positional(and(and(a, b), c)), "(a and b) and c"},
		{positional(or(or(a, b), c)), "(a or b) or c"},
		{positional(or(a, and(b, c))), "a or b and c"},
		{positional(and(or(a, b), c)), "(a or b) and c"},
		{positional(not(and(a, b))), "not (a and b)"},
		{positional(not(or(a, b))), "not (a or b)"},
		{positional(not(not(a))), "not not a"},
		{positional(and(not(a), b)), "not a and b"},
		{positional(cond(a, b, c)), "a if b else c"},
		{positional(cond(cond(a, b, c), d, a)), "(a if b else c) if d else a"},
		{positional(cond(a, cond(b, c, d), a)), "a if (b if c else d) else a"},
		{positional(cond(a, b, cond(c, d, a))), "a if b else c if d else a"},
		{positional(cond(or(a, b), and(c, d), not(a))), "a or b if c and d else not a"},
		{positional(and(cond(a, b, c), d)), "(a if b else c) and d"},
		{positional(not(cond(a, b, c))), "not (a if b else c)"},
		{
			positional(call("f", pos(or(a, b)), kw("x", cond(a, b, c)))),
			"f(a or b, x=a if b else c)",
		},
		{positional(call("f", pos(list(cond(a, b, c))))), "f([a if b else c])"},
		{positional(str("plain")), `"plain"`},
		{positional(str("")), `""`},
		{positional(str(`say "hi"`)), `'say "hi"'`},
		{positional(str(`^[a-z]+\d$`)), `r"^[a-z]+\d$"`},
		{positional(str(`^a="b\d"$`)), `r'^a="b\d"$'`},
		{positional(str(`a"b'c`)), `"a\"b'c"`},
		{positional(str(`a"b'\d`)), `"a\"b'\\d"`},
		{positional(str(`ends\`)), `"ends\\"`},
		{positional(str(`a\\b`)), `r"a\\b"`},
		{positional(str("tab\there\n")), `"tab\there\n"`},
		{positional(str("é\u3000")), `"é\u3000"`},
		{positional(num("123456789012345678901234567890")), "123456789012345678901234567890"},
		{positional(num("-0.1"), num("1e-400"), num("0x1F")), "-0.1, 1e-400, 0x1F"},
		{positional(Null{}, Bool{Value: true}, Bool{}), "None, True, False"},
		{positional(list(), dict()), "[], {}"},
		{
			positional(dict(entry(str("k"), list(num("1"), Null{})), entry(str("j"), Bool{}))),
			`{"k": [1, None], "j": False}`,
		},
	}

	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			assert.Equal(t, Print(c.args), c.text)

			back, err := Parse(c.text)
			assert.NilError(t, err)
			assert.DeepEqual(t, stripArgs(back), c.args)
		})
	}
}

func TestPrintRoundTrip(t *testing.T) {
	for _, text := range []string{
		`required, hook=off, length(min=1, max=3), regexp(r"^x-\d+$")`,
		`cmp(">=10") or const(0), not const(5)`,
		`props(field("next", ref("Tree")), rest(kind("integer")))`,
		`required if kind("object") else (null or a and b)`,
		`const(1, 2.50, -1e30, 0o17, None, [True], {"a": {"b": [1]}})`,
	} {
		t.Run(text, func(t *testing.T) {
			args, err := Parse(text)
			assert.NilError(t, err)

			back, err := Parse(Print(args))
			assert.NilError(t, err)
			assert.DeepEqual(t, stripArgs(back), stripArgs(args))
		})
	}
}
