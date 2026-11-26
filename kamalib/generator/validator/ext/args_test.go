package ext

import (
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
