package basic

import (
	"encoding/json/jsontext"
	"errors"
	"go/types"
	"testing"

	"github.com/google/go-cmp/cmp/cmpopts"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

// ignoreRegistry leaves out the registry the options keep, which tests of
// Parse do not look at.
var ignoreRegistry = cmpopts.IgnoreUnexported(
	RequiredOptions{},
	RequiresOptions{},
	MembersOptions{},
)

func TestRequiredParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	o, err := resolve(t, "required", reg)
	assert.NilError(t, err)
	assert.DeepEqual(t, o, ext.Rule(RequiredOptions{}), ignoreRegistry)

	o, err = resolve(t, `required("a", "b c")`, reg)
	assert.NilError(t, err)
	assert.DeepEqual(
		t, o, ext.Rule(RequiredOptions{Names: []string{"a", "b c"}}), ignoreRegistry,
	)
	assert.ErrorContains(t, o.Check(types.Typ[types.Int]),
		"int is no raw JSON text, which the member names of required apply to")

	_, err = resolve(t, "required(1)", reg)
	assert.ErrorIs(t, err, ext.ErrArgs)

	_, err = resolve(t, "required(a)", reg)
	assert.ErrorIs(t, err, ext.ErrArgs)
	assert.ErrorContains(t, err, `quote a name, as in required("a")`)
}

// TestRegexpVarName names the variable of a pattern after the first 5
// bytes of its SHA-256 in hex.
func TestRegexpVarName(t *testing.T) {
	// The SHA-256 of the empty string starts with e3 b0 c4 42 98.
	assert.Equal(t, regexpVarName(""), "kamaValidateRegexp_e3b0c44298")
	assert.Assert(t, regexpVarName("a") != regexpVarName("b"))
}

func TestZeroParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	o, err := resolve(t, "zero", reg)
	assert.NilError(t, err)
	assert.Equal(t, o, ext.Rule(ZeroOptions{reg: reg}))

	_, err = resolve(t, "zero(1)", reg)
	assert.ErrorIs(t, err, ext.ErrArgs)
	assert.ErrorContains(t, err, "zero takes 0 positional args")
}

func TestNilParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	o, err := resolve(t, "nil", reg)
	assert.NilError(t, err)
	assert.Equal(t, o, ext.Rule(NilOptions{}))

	_, err = resolve(t, "nil(1)", reg)
	assert.ErrorIs(t, err, ext.ErrArgs)
	assert.ErrorContains(t, err, "nil takes 0 positional args")
}

func TestNullParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	o, err := resolve(t, "null", reg)
	assert.NilError(t, err)
	assert.Equal(t, o, ext.Rule(NullOptions{reg: reg}))

	_, err = resolve(t, "null(1)", reg)
	assert.ErrorIs(t, err, ext.ErrArgs)
	assert.ErrorContains(t, err, "null takes 0 positional args")
}

func TestRegexpParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	o, err := resolve(t, `regexp('^a="b c"$')`, reg)
	assert.NilError(t, err)
	assert.Equal(t, o, ext.Rule(RegexpOptions{Pattern: `^a="b c"$`}))

	for text, msg := range map[string]string{
		"regexp":         "want a pattern",
		`regexp("a(")`:   `pattern "a(" does not compile`,
		`regexp(1)`:      "pattern",
		`regexp(x)`:      "quote a name",
		`regexp("a", 1)`: "regexp takes 1 positional args",
	} {
		_, err := resolve(t, text, reg)
		assert.ErrorIs(t, err, ext.ErrArgs, text)
		assert.ErrorContains(t, err, msg, text)
	}
}

func TestLengthParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	o, err := resolve(t, "length(ge=1, le=+10, ne=[3, 4])", reg)
	assert.NilError(t, err)
	assert.DeepEqual(t, o, ext.Rule(LengthOptions{Comparisons: []Comparison{
		{Op: ">=", Operand: "1"},
		{Op: "<=", Operand: "10"},
		{Op: "!=", Operand: "3"},
		{Op: "!=", Operand: "4"},
	}}))

	o, err = resolve(t, "length(3)", reg)
	assert.NilError(t, err)
	assert.DeepEqual(t, o, ext.Rule(LengthOptions{Comparisons: []Comparison{
		{Op: "==", Operand: "3"},
	}}))

	for text, msg := range map[string]string{
		"length":          "want one or more comparisons, as in length(ge=1, le=10)",
		"length(ge=-1)":   "length -1 is negative",
		"length(ge=1.0)":  "length 1.0 is not an integer literal",
		`length(ge="a")`:  "is no value of int",
		"length(1, 2)":    "length takes one positional arg",
		"length(ge=None)": "None is no number, string or bool",
		"length(ge=[1])":  "is no number, string or bool",
	} {
		_, err := resolve(t, text, reg)
		assert.ErrorIs(t, err, ext.ErrArgs, text)
		assert.ErrorContains(t, err, msg, text)
	}
}

func TestCmpParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	o, err := resolve(t, `cmp(ge=1, ne="a", eq=True)`, reg)
	assert.NilError(t, err)
	assert.DeepEqual(t, o, ext.Rule(CmpOptions{Comparisons: []Comparison{
		{Op: ">=", Operand: "1"},
		{Op: "!=", Operand: `"a"`},
		{Op: "==", Operand: "true"},
	}}))

	for text, msg := range map[string]string{
		"cmp":       "want one or more comparisons",
		"cmp(ge=x)": "x is no number, string or bool",
		"cmp(1, 2)": "cmp takes one positional arg",
	} {
		_, err := resolve(t, text, reg)
		assert.ErrorIs(t, err, ext.ErrArgs, text)
		assert.ErrorContains(t, err, msg, text)
	}
}

func TestConstParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	o, err := resolve(t, `const(1, "dark blue", True, None, [1], {"a": 0x10})`, reg)
	assert.NilError(t, err)
	assert.DeepEqual(t, o, ext.Rule(ConstOptions{Values: []ConstValue{
		{Text: "1", Json: jsontext.Value("1")},
		{Text: "dark blue", Quoted: true, Json: jsontext.Value(`"dark blue"`)},
		{Text: "true", Json: jsontext.Value("true")},
		{Json: jsontext.Value("null")},
		{Json: jsontext.Value("[1]")},
		{Json: jsontext.Value(`{"a":16}`)},
	}}))
	assert.Equal(t, o.(ConstOptions).Values[1].Literal(), `"dark blue"`)

	for text, msg := range map[string]string{
		"const":          "want one or more values",
		"const(x)":       "const: x is no literal",
		"const(1, zero)": "const: zero is no literal",
	} {
		_, err := resolve(t, text, reg)
		assert.ErrorIs(t, err, ext.ErrArgs, text)
		assert.ErrorContains(t, err, msg, text)
	}
}

func TestFormatParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	o, err := resolve(t, `format("date")`, reg)
	assert.NilError(t, err)
	assert.Equal(t, o.(FormatOptions).Name, "date")
	assert.Equal(t, o.(FormatOptions).Helper.Name, "kamaValidateFormatDate")

	for text, msg := range map[string]string{
		"format": `want one format name, as in format("date");` +
			" known: date, date-time, ipv4, ipv6, url, uuid",
		`format("email")`: `unknown format "email"; known: date, date-time, ipv4, ipv6, url, uuid`,
	} {
		_, err := resolve(t, text, reg)
		assert.ErrorIs(t, err, ext.ErrArgs, text)
		assert.ErrorContains(t, err, msg, text)
	}
}

func TestRunesParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	o, err := resolve(t, "runes(ge=1, le=10)", reg)
	assert.NilError(t, err)
	assert.DeepEqual(t, o, ext.Rule(RunesOptions{Comparisons: []Comparison{
		{Op: ">=", Operand: "1"},
		{Op: "<=", Operand: "10"},
	}}))

	for text, msg := range map[string]string{
		"runes":         "want one or more comparisons",
		"runes(ge=-1)":  "length -1 is negative",
		"runes(ge=1.0)": "length 1.0 is not an integer literal",
	} {
		_, err := resolve(t, text, reg)
		assert.ErrorIs(t, err, ext.ErrArgs, text)
		assert.ErrorContains(t, err, msg, text)
	}
}

func TestRunesCheck(t *testing.T) {
	o := RunesOptions{Comparisons: []Comparison{{Op: ">=", Operand: "1"}}}
	str := types.NewNamed(types.NewTypeName(0, nil, "Str", nil), types.Typ[types.String], nil)

	assert.NilError(t, o.Check(types.Typ[types.String]))
	assert.NilError(t, o.Check(str))

	for _, typ := range []types.Type{
		bytesType,
		types.NewSlice(types.Typ[types.Rune]),
		types.Typ[types.Int],
	} {
		assert.ErrorContains(t, o.Check(typ), typ.String()+" is not a string type")
	}
}

func TestMultipleOfParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	for text, divisor := range map[string]string{
		"2": "2", "+2": "2", "1.5": "1.5", "0.0001": "0.0001", "1e-8": "1e-8", "0x10": "16",
	} {
		o, err := resolve(t, "multipleof("+text+")", reg)
		assert.NilError(t, err, text)
		assert.Equal(t, o, ext.Rule(MultipleOfOptions{Divisor: divisor}))
	}

	for text, msg := range map[string]string{
		"":        "want one number, as in multipleof(0.01)",
		"0":       "divisor 0 is not positive",
		"0.0":     "divisor 0.0 is not positive",
		"-1":      "divisor -1 is not an integer or float literal",
		`"1"`:     "want one number",
		"n":       "quote a name",
		"1e-9999": "divisor 1e-9999 is out of range",
	} {
		_, err := resolve(t, "multipleof("+text+")", reg)
		assert.ErrorIs(t, err, ext.ErrArgs, text)
		assert.ErrorContains(t, err, msg, text)
	}
}

func TestMultipleOfCheck(t *testing.T) {
	for _, tc := range []struct {
		divisor string
		typ     types.Type
		msg     string
	}{
		{"3", types.Typ[types.Int], ""},
		{"1.5", types.Typ[types.Uint8], ""},
		{"1e-8", types.Typ[types.Int64], ""},
		{"0.5", types.Typ[types.Float32], ""},
		{"1e300", types.Typ[types.Float64], ""},
		{"300", types.Typ[types.Int8], ""},
		{"51.2", types.Typ[types.Uint8], ""},
		{"1e400", types.Typ[types.Float64], ""},
		{"1e9999", types.Typ[types.Float32], ""},
		{"2", types.Typ[types.String], "string is not an integer or float type"},
		{"2", types.Typ[types.Complex128], "complex128 is not an integer or float type"},
		{"2", types.NewSlice(types.Typ[types.Int]), "[]int is not an integer or float type"},
	} {
		err := MultipleOfOptions{Divisor: tc.divisor}.Check(tc.typ)
		if tc.msg == "" {
			assert.NilError(t, err, tc.divisor)
		} else {
			assert.ErrorContains(t, err, tc.msg, tc.divisor)
		}
	}
}

func TestUniqueParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	o, err := resolve(t, "unique", reg)
	assert.NilError(t, err)
	assert.Equal(t, o, ext.Rule(UniqueOptions{}))

	_, err = resolve(t, "unique(1)", reg)
	assert.ErrorIs(t, err, ext.ErrArgs)
	assert.ErrorContains(t, err, "unique takes 0 positional args")
}

func TestUniqueCheck(t *testing.T) {
	param := types.NewTypeParam(
		types.NewTypeName(0, nil, "T", nil),
		types.NewInterfaceType(nil, nil),
	)
	anyType := types.Universe.Lookup("any").Type()
	field := func(typ types.Type) *types.Struct {
		return types.NewStruct([]*types.Var{types.NewField(0, nil, "F", typ, false)}, nil)
	}

	for _, tc := range []struct {
		typ    types.Type
		helper string
	}{
		{types.NewSlice(types.Typ[types.Int]), uniqueName},
		{types.NewArray(types.Typ[types.String], 3), uniqueName},

		{types.NewSlice(field(types.Typ[types.Int])), uniqueName},
		{types.NewSlice(types.NewArray(types.Typ[types.Int], 2)), uniqueName},
		{types.NewSlice(anyType), uniqueDeepName},
		{types.NewSlice(field(anyType)), uniqueDeepName},
		{types.NewSlice(types.NewArray(anyType, 2)), uniqueDeepName},
		{types.NewSlice(types.NewSlice(types.Typ[types.Int])), uniqueDeepName},
		{types.NewSlice(param), uniqueDeepName},
		{types.NewSlice(types.NewPointer(types.Typ[types.Int])), uniqueDeepName},
		{types.NewSlice(field(types.NewPointer(types.Typ[types.Int]))), uniqueDeepName},
		{types.NewSlice(types.NewChan(types.SendRecv, types.Typ[types.Int])), uniqueName},
	} {
		assert.NilError(t, UniqueOptions{}.Check(tc.typ), tc.typ.String())

		helper, _ := uniqueHelper(tc.typ)
		assert.Equal(t, helper, tc.helper, tc.typ.String())
	}

	for _, typ := range []types.Type{
		types.Typ[types.String],
		types.NewMap(types.Typ[types.String], types.Typ[types.Int]),
		param,
	} {
		assert.ErrorContains(
			t,
			UniqueOptions{}.Check(typ),
			typ.String()+" is not a slice or array type",
		)
	}
}

// testStruct is a Struct of the tests. Its members are those of types, in
// the order of names.
type testStruct struct {
	names    []string
	types    map[string]types.Type
	fallback types.Type
	err      error
}

func (s testStruct) Members() ([]string, error) { return s.names, s.err }

func (s testStruct) Member(name string) (types.Type, ext.Gen, bool) {
	t, ok := s.types[name]
	return t, lang.Expr("v.%s", name), ok
}

func (testStruct) Omittable(string) bool { return true }

func (s testStruct) Fallback() (types.Type, ext.Gen, bool) {
	return s.fallback, lang.Expr("v.X"), s.fallback != nil
}

func TestRequiresParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	o, err := resolve(t, `requires("b", "c d")`, reg)
	assert.NilError(t, err)
	assert.DeepEqual(t, o, ext.Rule(RequiresOptions{Names: []string{"b", "c d"}}), ignoreRegistry)

	for text, msg := range map[string]string{
		"requires":    "want one or more JSON names of sibling fields",
		"requires(1)": "names",
	} {
		_, err := resolve(t, text, reg)
		assert.ErrorIs(t, err, ext.ErrArgs, text)
		assert.ErrorContains(t, err, msg, text)
	}
}

func TestRequiresSiblings(t *testing.T) {
	s := testStruct{
		names: []string{"a", "b", "c"},
		types: map[string]types.Type{
			"a": types.NewPointer(types.Typ[types.Int]),
			"b": types.Typ[types.String],
			"c": types.NewStruct([]*types.Var{
				types.NewField(0, nil, "L", types.NewSlice(types.Typ[types.Int]), false),
			}, nil),
		},
	}
	o := RequiresOptions{Names: []string{"b"}}

	assert.ErrorContains(t, o.Check(types.Typ[types.Int]), "write it on a field of a struct")

	r, err := o.Siblings(s, "a")
	assert.NilError(t, err)
	assert.NilError(t, r.Check(s.types["a"]))
	assert.ErrorContains(t, r.Check(s.types["c"]), "is not comparable")

	_, err = RequiresOptions{Names: []string{"b", "z"}}.Siblings(s, "a")
	assert.ErrorContains(t, err, `no sibling field encodes as the member "z"`)

	_, err = RequiresOptions{Names: []string{"c"}}.Siblings(s, "a")
	assert.ErrorContains(t, err, `sibling "c" has no presence test`)
}

func TestMembersParse(t *testing.T) {
	reg := ext.DefaultRegistry()

	o, err := resolve(t, "members(ge=1, le=3)", reg)
	assert.NilError(t, err)
	assert.DeepEqual(t, o, ext.Rule(MembersOptions{Comparisons: []Comparison{
		{Op: ">=", Operand: "1"},
		{Op: "<=", Operand: "3"},
	}}), ignoreRegistry)

	for text, msg := range map[string]string{
		"members":        "want one or more comparisons",
		"members(gt=-1)": "length -1 is negative",
	} {
		_, err := resolve(t, text, reg)
		assert.ErrorIs(t, err, ext.ErrArgs, text)
		assert.ErrorContains(t, err, msg, text)
	}
}

func TestMembersStruct(t *testing.T) {
	o := MembersOptions{Comparisons: []Comparison{{Op: ">=", Operand: "1"}}}
	assert.ErrorContains(t, o.Check(types.Typ[types.Int]), "members counts the members of a struct")

	strMap := types.NewMap(types.Typ[types.String], types.Typ[types.Int])

	r, err := o.Struct(testStruct{fallback: strMap})
	assert.NilError(t, err)
	assert.NilError(t, r.Check(types.Typ[types.Int]))

	_, err = o.Struct(testStruct{err: errors.New("inlined")})
	assert.ErrorContains(t, err, "inlined")

	_, err = o.Struct(testStruct{fallback: types.Typ[types.String]})
	assert.ErrorContains(t, err, "the embedded fallback field of type string is no map")
}

func TestOptionalState(t *testing.T) {
	for _, typ := range []types.Type{
		types.NewPointer(types.Typ[types.Int]),
		types.NewSlice(types.Typ[types.Int]),
		types.NewInterfaceType(nil, nil),
	} {
		assert.Assert(t, optionalState(nil, typ), typ.String())
	}

	for _, typ := range []types.Type{
		types.Typ[types.Int],
		types.Typ[types.String],
		types.NewStruct(nil, nil),
	} {
		assert.Assert(t, !optionalState(nil, typ), typ.String())
	}
}
