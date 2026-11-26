package basic

import (
	"go/token"
	"go/types"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

// rawTypes returns jsontext.Value, a type defined over it declaring
// RawJson and one declaring no method.
func rawTypes() (value, marked, plain types.Type) {
	pkg := types.NewPackage("encoding/json/jsontext", "jsontext")
	bytes := types.NewSlice(types.Typ[types.Byte])
	v := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Value", nil), bytes, nil)

	local := types.NewPackage("example.com/p", "p")
	m := types.NewNamed(types.NewTypeName(token.NoPos, local, "M", nil), bytes, nil)
	sig := types.NewSignatureType(
		types.NewVar(token.NoPos, local, "v", m), nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, local, "", v)), false,
	)
	m.AddMethod(types.NewFunc(token.NoPos, local, "RawJson", sig))

	p := types.NewNamed(types.NewTypeName(token.NoPos, local, "P", nil), bytes, nil)

	return v, m, p
}

func TestIsRawJson(t *testing.T) {
	value, marked, plain := rawTypes()

	assert.Assert(t, ext.IsRawJson(value))
	assert.Assert(t, ext.IsRawJson(marked))
	assert.Assert(t, !ext.IsRawJson(plain))
	assert.Assert(t, !ext.IsRawJson(types.NewSlice(types.Typ[types.Byte])))
}

func TestRawCheck(t *testing.T) {
	value, _, plain := rawTypes()
	reg := ext.DefaultRegistry()

	for _, tc := range []struct{ text, msg string }{
		{"cmp(ge=1.5)", ""},
		{`cmp(gt="a")`, `"a" is no JSON number`},
		{"multipleof(0.5)", ""},
		{"runes(le=3)", ""},
		{`regexp("^a")`, ""},
		{"length(ge=1)", ""},
		{"members(le=1)", ""},
		{"unique", ""},
		{`const(1, None, {"a": 1})`, ""},
		{`required("a", "b")`, ""},
		{`kind("integer", "null")`, ""},
		{"members(le=1) and cmp(ge=1)", ""},
	} {
		r, err := resolve(t, tc.text, reg)
		assert.NilError(t, err, tc.text)

		if tc.msg == "" {
			assert.NilError(t, r.Check(value), tc.text)
		} else {
			assert.ErrorContains(t, r.Check(value), tc.msg)
		}
	}

	r, err := resolve(t, `kind("string")`, reg)
	assert.NilError(t, err)
	assert.ErrorContains(t, r.Check(plain), "is no raw JSON text")

	r, err = resolve(t, "members(ge=1)", reg)
	assert.NilError(t, err)
	assert.ErrorIs(t, r.Check(plain), errMembersStruct)
}

func TestKindParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	r, err := resolve(t, `kind("integer", "null")`, reg)
	assert.NilError(t, err)
	assert.DeepEqual(t, r, ext.Rule(KindOptions{Kinds: []string{"integer", "null"}}))

	for text, msg := range map[string]string{
		"kind":          "want one or more JSON kinds",
		`kind("float")`: `"float" is no JSON kind`,
		"kind(null)":    `quote a name, as in kind("null")`,
	} {
		_, err := resolve(t, text, reg)
		assert.ErrorIs(t, err, ext.ErrArgs)
		assert.ErrorContains(t, err, msg)
	}
}
