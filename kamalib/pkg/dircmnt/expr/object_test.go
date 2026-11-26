package expr

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"testing"

	"gotest.tools/v3/assert"
)

func parseCall(t *testing.T, text string) Call {
	t.Helper()

	args, err := Parse(text)
	assert.NilError(t, err)
	assert.Equal(t, len(args), 1)

	return args[0].Value.(Call)
}

func TestObject(t *testing.T) {
	cases := []struct {
		text       string
		positional []string
		variadic   string
		obj        string
		rules      []Arg
	}{
		{"required", nil, "", `{}`, nil},
		{"length(min=1, max=3)", []string{"min", "max"}, "", `{"max":3,"min":1}`, nil},
		{"length(1, max=3)", []string{"min", "max"}, "", `{"max":3,"min":1}`, nil},
		{"const(1, 2.50, -0x1F)", nil, "values", `{"values":[1,2.50,-31]}`, nil},
		{`format("date")`, []string{"name"}, "", `{"name":"date"}`, nil},
		{
			`field("name", required, kind("string"))`,
			[]string{"name"},
			"operands",
			`{"name":"name"}`,
			[]Arg{
				kw("operands", name("required")),
				kw("operands", call("kind", pos(str("string")))),
			},
		},
		{
			`contains(kind("string"), min=2)`,
			nil, "operands",
			`{"min":2}`,
			[]Arg{kw("operands", call("kind", pos(str("string"))))},
		},
		{
			`if_(const("a"), then=required, else_=kind("null"))`,
			[]string{"cond"}, "",
			`{}`,
			[]Arg{
				kw("cond", call("const", pos(str("a")))),
				kw("then", name("required")),
				kw("else_", call("kind", pos(str("null")))),
			},
		},
		{"f(x=a and b, y=not c)", nil, "", `{}`, []Arg{
			kw("x", and(name("a"), name("b"))),
			kw("y", not(name("c"))),
		}},
		{
			`const([1, "a"], {"k": None, "j": True})`,
			nil, "values",
			`{"values":[[1,"a"],{"k":null,"j":true}]}`,
			nil,
		},
		{"f(unknown=1)", []string{"a"}, "", `{"unknown":1}`, nil},
	}

	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			obj, rules, err := Object(parseCall(t, c.text), c.positional, c.variadic)
			assert.NilError(t, err)
			assert.Equal(t, string(obj), c.obj)
			assert.DeepEqual(t, stripArgs(rules), c.rules)
		})
	}
}

func TestObjectErrors(t *testing.T) {
	cases := []struct {
		text       string
		positional []string
		variadic   string
		err        string
	}{
		{"length(1, 2, 3)", []string{"min", "max"}, "", "length takes 2 positional args, got more"},
		{"length(1, min=2)", []string{"min"}, "", "length: min given twice"},
		{"const(1, values=[2])", nil, "values", "const: values given twice"},
		{"f([a])", []string{"x"}, "", "f: x: expr: args do not fit: a is no literal"},
		{"f({1: 2})", []string{"x"}, "", "key 1 is no string"},
		{`f({"a": 1, "a": 2})`, []string{"x"}, "", "duplicate object member name"},
		{`f(Call)`, nil, "", "f takes 0 positional args"},
	}

	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			_, _, err := Object(parseCall(t, c.text), c.positional, c.variadic)
			assert.ErrorContains(t, err, c.err)
			assert.Assert(t, errors.Is(err, ErrArgs))
		})
	}

	_, _, err := Object(Call{Name: "f", Args: []Arg{pos(str("\xff"))}}, []string{"s"}, "")
	assert.Assert(t, errors.Is(err, ErrArgs))
}

func TestJsonNumber(t *testing.T) {
	cases := []struct {
		text, json string
	}{
		{"0", "0"},
		{"-0", "-0"},
		{"123456789012345678901234567890", "123456789012345678901234567890"},
		{"0.1", "0.1"},
		{"2.50", "2.50"},
		{".5", "0.5"},
		{"-.5", "-0.5"},
		{"1.", "1"},
		{"1.e3", "1e3"},
		{"1E+5", "1E+5"},
		{"1e-400", "1e-400"},
		{"00.5", "0.5"},
		{"0x1F", "31"},
		{"-0X1f", "-31"},
		{"0o17", "15"},
		{"0b101", "5"},
		{"0xFFFFFFFFFFFFFFFFFFFF", "1208925819614629174706175"},
	}

	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			got, err := Json(Number{Text: c.text})
			assert.NilError(t, err)
			assert.Equal(t, string(got), c.json)
		})
	}

	_, err := Json(Number{Text: "1e"})
	assert.Assert(t, errors.Is(err, ErrArgs))
}

// lengthOptions stands for the options struct of a rule.
type lengthOptions struct {
	Min     int64          `json:"min"`
	Max     float64        `json:"max"`
	Big     jsontext.Value `json:"big"`
	Values  []int64        `json:"values"`
	Unknown map[string]any `json:",embed"`
}

func TestObjectDecodes(t *testing.T) {
	c := parseCall(
		t,
		"length(-9007199254740993, 0.1, 7, 8, big=123456789012345678901234567890, typo=1)",
	)

	obj, rules, err := Object(c, []string{"min", "max"}, "values")
	assert.NilError(t, err)
	assert.Equal(t, len(rules), 0)

	var o lengthOptions
	assert.NilError(t, jsonv2.Unmarshal(obj, &o))

	assert.Equal(t, o.Min, int64(-9007199254740993))
	assert.Equal(t, o.Max, 0.1)
	assert.Equal(t, string(o.Big), "123456789012345678901234567890")
	assert.DeepEqual(t, o.Values, []int64{7, 8})
	assert.DeepEqual(t, o.Unknown, map[string]any{"typo": 1.0})
}

func TestFromJson(t *testing.T) {
	for _, text := range []string{
		`null`, `true`, `false`, `"a"`, `"it's \"q\" \\ \n"`, `0`, `-0`, `1.50`, `1E+2`, `-3e-7`,
		`123456789012345678901234567890`, `[]`, `[1, "a", [null]]`, `{}`, `{"b": 1, "a": {"c": []}}`,
	} {
		t.Run(text, func(t *testing.T) {
			n, err := FromJson(jsontext.Value(text))
			assert.NilError(t, err)

			args, err := Parse(Print([]Arg{{Value: n}}))
			assert.NilError(t, err)

			got, err := Json(args[0].Value)
			assert.NilError(t, err)

			want := jsontext.Value(text)
			assert.NilError(t, want.Compact())
			assert.Equal(t, string(got), string(want))
		})
	}

	for _, text := range []string{`1e400`, `1 2`, `[1`} {
		_, err := FromJson(jsontext.Value(text))
		assert.Assert(t, errors.Is(err, ErrArgs), "%s: %v", text, err)
	}
}
