package ext

import (
	"encoding/json/jsontext"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

func call(t *testing.T, text string) expr.Call {
	t.Helper()

	args, err := expr.Parse(text)
	assert.NilError(t, err)

	return args[0].Value.(expr.Call)
}

func TestParamsBind(t *testing.T) {
	type options struct {
		Name string `json:"name"`
		Max  int    `json:"max"`
	}

	p := Params{
		Positional: []string{"name"},
		Variadic:   "operands",
		Keywords:   []string{"max"},
		Rules:      []string{"operands"},
	}

	var warnings []error

	r := WithWarn(NewRegistry(), func(w error) { warnings = append(warnings, w) })

	var o options

	rules, err := p.Bind(r, call(t, `field("a", required, null, max=3, nope=1, nope2=x)`), &o)
	assert.NilError(t, err)
	assert.DeepEqual(t, o, options{Name: "a", Max: 3})
	assert.Equal(t, expr.Print(rules), "operands=required, operands=null")
	assert.Equal(t, len(warnings), 2)
	assert.ErrorContains(t, warnings[0], "field: unknown arg nope=1")
	assert.ErrorContains(t, warnings[1], "field: unknown arg nope2=x")

	for text, msg := range map[string]string{
		`field(name)`:          `name of name is no literal; quote a name, as in field("name")`,
		`field("a", 1)`:        "operands takes rules, got a literal",
		`field("a", max="x")`:  "max",
		`field("a", name="b")`: "name given twice",
	} {
		_, err := p.Bind(r, call(t, text), &options{})
		assert.ErrorIs(t, err, ErrArgs, text)
		assert.ErrorContains(t, err, msg, text)
	}

	_, err = Params{}.Bind(r, call(t, "zero(1)"), nil)
	assert.ErrorIs(t, err, ErrArgs)
}

func TestParamsCall(t *testing.T) {
	type options struct {
		Name     *string        `json:"name"`
		Operands []expr.Node    `json:"operands"`
		Max      jsontext.Value `json:"max"`
		Strict   bool           `json:"strict"`
	}

	p := Params{
		Positional: []string{"name"},
		Variadic:   "operands",
		Keywords:   []string{"strict", "max"},
		Rules:      []string{"operands"},
	}
	name := func(s string) *string { return &s }
	required := expr.Call{Name: "required"}

	for _, tc := range []struct {
		src  any
		text string
	}{
		{options{}, "f"},
		{&options{Name: name("")}, `f("")`},
		{
			options{Name: name("a"), Operands: []expr.Node{required}, Max: jsontext.Value("3")},
			`f("a", required, max=3)`,
		},
		{options{Max: jsontext.Value("1e3"), Strict: true}, "f(strict=True, max=1e3)"},
	} {
		c, err := p.Call("f", tc.src)
		assert.NilError(t, err)
		assert.Equal(t, expr.Print([]expr.Arg{{Value: c}}), tc.text)
	}

	twoPositional := Params{Positional: []string{"a", "b"}, Variadic: "rest"}

	c, err := twoPositional.Call("f", struct {
		A *string `json:"a"`
		B string  `json:"b"`
	}{B: "x"})
	assert.NilError(t, err)
	assert.Equal(t, c.String(), `f(b="x")`)

	_, err = twoPositional.Call("f", struct {
		Nope int `json:"nope"`
	}{})
	assert.ErrorContains(t, err, "field Nope names no parameter")

	_, err = twoPositional.Call("f", struct {
		A    *string  `json:"a"`
		Rest []string `json:"rest"`
	}{Rest: []string{"x"}})
	assert.ErrorContains(t, err, "rest follows a, which holds nothing")

	_, err = p.Call("f", options{Max: jsontext.Value("1e400")})
	assert.ErrorIs(t, err, expr.ErrArgs)

	_, err = Params{Variadic: "operands", Rules: []string{"operands"}}.Call("f", struct {
		Operands []string `json:"operands"`
	}{Operands: []string{"a"}})
	assert.ErrorContains(t, err, "a parameter of rules takes")
}
