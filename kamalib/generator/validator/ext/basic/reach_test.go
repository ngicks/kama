package basic

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

func TestReachParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	for _, text := range []string{
		`props(field("a b", kind("string")), match("^x"), rest(never))`,
		`items(kind("string"), cmp(ge=1))`,
		`prefix(kind("string"), always, rest(kind("null")))`,
		`contains(kind("string"), ge=2, le=3)`,
		`keys(runes(le=3))`,
		`unevalprops(props(field("a")), never)`,
		`unevalitems(items(kind("string")))`,
		`ref("Tree")`,
		`decode("Pet")`,
	} {
		_, err := resolve(t, text, reg)
		assert.NilError(t, err, text)
	}

	for _, tc := range []struct{ text, msg string }{
		{`props(a(kind("string")))`, "entry a is no call of field, match or rest"},
		{`props(kind("string"))`, "is no call of field, match or rest"},
		{`props(1)`, "entries takes rules, got a literal"},
		{`props(field(kind("string")))`, "is no literal"},
		{`props(field())`, "want a name or a pattern first"},
		{`props(field("a"), field("a"))`, `entry field "a" given twice`},
		{`props(match("("))`, "does not compile"},
		{`contains(ge=-1)`, "is negative"},
		{`unevalprops`, "want the operand scope first"},
		{`ref("a.B")`, "is no type name"},
		{`decode`, "want a type name"},
	} {
		_, err := resolve(t, tc.text, reg)
		assert.ErrorIs(t, err, ext.ErrArgs, tc.text)
		assert.ErrorContains(t, err, tc.msg)
	}
}

func TestReachCheck(t *testing.T) {
	value, marked, plain := rawTypes()
	reg := ext.DefaultRegistry()

	for _, text := range []string{
		`props(field("a", kind("string")))`,
		`items(kind("string"))`,
		`prefix(kind("string"), rest(kind("null")))`,
		`contains(kind("string"))`,
		`keys(regexp("^a"))`,
		`unevalprops(always, kind("null"))`,
		`unevalitems(always, kind("null"))`,
		`ref("Tree")`,
		`decode("Pet")`,
	} {
		r, err := resolve(t, text, reg)
		assert.NilError(t, err)
		assert.NilError(t, r.Check(value), text)
		assert.NilError(t, r.Check(marked), text)
		assert.ErrorContains(t, r.Check(plain), "is no raw JSON text", text)
	}

	// The operands check the elements as jsontext.Value, which compares
	// JSON numbers only.
	r, err := resolve(t, `items(cmp(gt="a"))`, reg)
	assert.NilError(t, err)
	assert.ErrorContains(t, r.Check(marked), "is no JSON number")
}
