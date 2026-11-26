package basic

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

func TestReachParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	for name, arg := range map[string]string{
		"props":       `field("a b", kind=string), match("^x"), rest(or())`,
		"items":       `kind=string, cmp=">=1"`,
		"prefix":      `kind=string, and(), rest(kind=null)`,
		"contains":    `>=2, <=3, kind=string`,
		"keys":        `runes="<=3"`,
		"unevalprops": `props(field("a")), or()`,
		"unevalitems": `items(kind=string)`,
		"ref":         "Tree",
		"decode":      "Pet",
	} {
		_, err := reg.Resolve(name, arg)
		assert.NilError(t, err, "%s(%s)", name, arg)
	}

	for _, tc := range []struct {
		name, arg, msg string
	}{
		{"props", `a(kind=string)`, "entry a is no call of field, match or rest"},
		{"props", `kind=string`, "is no call of field, match or rest"},
		{"props", `field(kind=string)`, "want a name or a pattern first"},
		{"props", `field("a"), field("a")`, `entry field "a" given twice`},
		{"props", `match("(")`, "does not compile"},
		{"contains", `>=-1`, "is negative"},
		{"unevalprops", ``, "want the operand scope first"},
		{"ref", `a.B`, "is no type name"},
		{"decode", ``, "is no type name"},
	} {
		_, err := reg.Resolve(tc.name, tc.arg)
		assert.ErrorIs(t, err, ext.ErrArgs, "%s(%s)", tc.name, tc.arg)
		assert.ErrorContains(t, err, tc.msg)
	}
}

func TestReachCheck(t *testing.T) {
	value, marked, plain := rawTypes()
	reg := ext.DefaultRegistry()

	for _, tc := range []struct{ name, arg string }{
		{"props", `field("a", kind=string)`},
		{"items", `kind=string`},
		{"prefix", `kind=string, rest(kind=null)`},
		{"contains", `kind=string`},
		{"keys", `regexp="^a"`},
		{"unevalprops", `and(), kind=null`},
		{"unevalitems", `and(), kind=null`},
		{"ref", "Tree"},
		{"decode", "Pet"},
	} {
		r, err := reg.Resolve(tc.name, tc.arg)
		assert.NilError(t, err)
		assert.NilError(t, r.Check(value), tc.name)
		assert.NilError(t, r.Check(marked), tc.name)
		assert.ErrorContains(t, r.Check(plain), "is no raw JSON text", tc.name)
	}

	// The operands check the elements as jsontext.Value, which compares
	// JSON numbers only.
	r, err := reg.Resolve("items", `cmp=">a"`)
	assert.NilError(t, err)
	assert.ErrorContains(t, r.Check(marked), "is no JSON number")
}
