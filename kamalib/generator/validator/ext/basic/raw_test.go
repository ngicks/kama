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

	for _, tc := range []struct {
		name, arg, msg string
	}{
		{"cmp", ">=1.5", ""},
		{"cmp", `>"a"`, `"a" is no JSON number`},
		{"multipleof", "0.5", ""},
		{"runes", "<=3", ""},
		{"regexp", "^a", ""},
		{"length", ">=1", ""},
		{"members", "<=1", ""},
		{"unique", "", ""},
		{"const", "1 null `{\"a\":1}`", ""},
		{"const", "`{`", "{ is no JSON value"},
		{"required", "a b", ""},
		{"kind", "integer null", ""},
		{"and", `members="<=1", cmp=">=1"`, ""},
	} {
		r, err := reg.Resolve(tc.name, tc.arg)
		assert.NilError(t, err, tc.name)

		if tc.msg == "" {
			assert.NilError(t, r.Check(value), "%s=%s", tc.name, tc.arg)
		} else {
			assert.ErrorContains(t, r.Check(value), tc.msg)
		}
	}

	r, err := reg.Resolve("kind", "string")
	assert.NilError(t, err)
	assert.ErrorContains(t, r.Check(plain), "is no raw JSON text")

	r, err = reg.Resolve("members", ">=1")
	assert.NilError(t, err)
	assert.ErrorIs(t, r.Check(plain), errMembersStruct)
}

func TestKindParse(t *testing.T) {
	r, err := Kind{}.Parse("integer null", nil)
	assert.NilError(t, err)
	assert.DeepEqual(t, r, ext.Rule(KindOptions{Kinds: []string{"integer", "null"}}))

	for arg, msg := range map[string]string{
		"":      "want one or more JSON kinds",
		"float": `arg "float" is no JSON kind`,
	} {
		_, err := Kind{}.Parse(arg, nil)
		assert.ErrorIs(t, err, ext.ErrArgs)
		assert.ErrorContains(t, err, msg)
	}
}
