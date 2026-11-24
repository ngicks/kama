package rule

import (
	"go/types"
	"testing"

	"gotest.tools/v3/assert"
)

func TestRequiredParse(t *testing.T) {
	o, err := Required{}.Parse("")
	assert.NilError(t, err)
	assert.Equal(t, o, RequiredOptions{})

	_, err = Required{}.Parse("yes")
	assert.ErrorIs(t, err, ErrArgs)
	assert.ErrorContains(t, err, `unexpected arg "yes"; want none`)
}

// TestRegexpVarName names the variable of a pattern after the first 5
// bytes of its SHA-256 in hex.
func TestRegexpVarName(t *testing.T) {
	// The SHA-256 of the empty string starts with e3 b0 c4 42 98.
	assert.Equal(t, regexpVarName(""), "kamaValidateRegexp_e3b0c44298")
	assert.Assert(t, regexpVarName("a") != regexpVarName("b"))
}

func TestZeroParse(t *testing.T) {
	o, err := Zero{}.Parse("")
	assert.NilError(t, err)
	assert.Equal(t, o, ZeroOptions{})

	_, err = Zero{}.Parse("yes")
	assert.ErrorIs(t, err, ErrArgs)
	assert.ErrorContains(t, err, `unexpected arg "yes"; want none`)
}

func TestNilParse(t *testing.T) {
	o, err := Nil{}.Parse("")
	assert.NilError(t, err)
	assert.Equal(t, o, NilOptions{})

	_, err = Nil{}.Parse("x")
	assert.ErrorIs(t, err, ErrArgs)
	assert.ErrorContains(t, err, `unexpected arg "x"; want none`)
}

func TestNullParse(t *testing.T) {
	o, err := Null{}.Parse("")
	assert.NilError(t, err)
	assert.Equal(t, o, NullOptions{})

	_, err = Null{}.Parse("x")
	assert.ErrorIs(t, err, ErrArgs)
	assert.ErrorContains(t, err, `unexpected arg "x"; want none`)
}

func TestRegexpParse(t *testing.T) {
	o, err := Regexp{}.Parse(`^a="b c"$`)
	assert.NilError(t, err)
	assert.Equal(t, o, RegexpOptions{Pattern: `^a="b c"$`})

	for arg, msg := range map[string]string{
		"":   "want a pattern",
		"a(": `pattern "a(" does not compile`,
	} {
		_, err := Regexp{}.Parse(arg)
		assert.ErrorIs(t, err, ErrArgs, arg)
		assert.ErrorContains(t, err, msg, arg)
	}
}

func TestLengthParse(t *testing.T) {
	o, err := Length{}.Parse(">=1 <=+10")
	assert.NilError(t, err)
	assert.DeepEqual(t, o, LengthOptions{Comparisons: []Comparison{
		{Op: ">=", Operand: "1"},
		{Op: "<=", Operand: "+10"},
	}})

	for arg, msg := range map[string]string{
		"":        "want one or more comparisons",
		"=1":      `arg "=1" is no comparison`,
		">=a =1":  `arg "=1" is no comparison`,
		">=-1":    "length -1 is negative",
		">=1.0":   "length 1.0 is not an integer literal",
		`"1`:      "unterminated quoted string",
		">=\"a\"": "is no value of int",
	} {
		_, err := Length{}.Parse(arg)
		assert.ErrorIs(t, err, ErrArgs, arg)
		assert.ErrorContains(t, err, msg, arg)
	}
}

func TestCmpParse(t *testing.T) {
	o, err := Cmp{}.Parse(`>=1 !="a"`)
	assert.NilError(t, err)
	assert.DeepEqual(t, o, CmpOptions{Comparisons: []Comparison{
		{Op: ">=", Operand: "1"},
		{Op: "!=", Operand: `"a"`},
	}})

	for arg, msg := range map[string]string{
		"":    "want one or more comparisons",
		">=":  `arg ">=" is no comparison`,
		"a=1": `arg "a=1" is no comparison`,
	} {
		_, err := Cmp{}.Parse(arg)
		assert.ErrorIs(t, err, ErrArgs, arg)
		assert.ErrorContains(t, err, msg, arg)
	}
}

func TestConstParse(t *testing.T) {
	o, err := Const{}.Parse(`1 "dark blue"`)
	assert.NilError(t, err)
	assert.DeepEqual(t, o, ConstOptions{Values: []ConstValue{
		{Text: "1"},
		{Text: "dark blue", Quoted: true},
	}})
	assert.Equal(t, o.(ConstOptions).Values[1].Literal(), `"dark blue"`)

	for arg, msg := range map[string]string{
		"":    "want one or more values",
		"a=1": `arg "a=1" is a key=value pair`,
		`"a`:  "unterminated quoted string",
	} {
		_, err := Const{}.Parse(arg)
		assert.ErrorIs(t, err, ErrArgs, arg)
		assert.ErrorContains(t, err, msg, arg)
	}
}

func TestFormatParse(t *testing.T) {
	f := Format{Formats: DefaultFormats()}

	o, err := f.Parse("date")
	assert.NilError(t, err)
	assert.Equal(t, o.(FormatOptions).Name, "date")
	assert.Equal(t, o.(FormatOptions).Helper.Name, "kamaValidateFormatDate")

	for arg, msg := range map[string]string{
		"":     "want one format name; known: date, date-time, url",
		"uuid": `unknown format "uuid"; known: date, date-time, url`,
	} {
		_, err := f.Parse(arg)
		assert.ErrorIs(t, err, ErrArgs, arg)
		assert.ErrorContains(t, err, msg, arg)
	}
}

func TestRunesParse(t *testing.T) {
	o, err := Runes{}.Parse(">=1 <=+10")
	assert.NilError(t, err)
	assert.DeepEqual(t, o, RunesOptions{Comparisons: []Comparison{
		{Op: ">=", Operand: "1"},
		{Op: "<=", Operand: "+10"},
	}})

	for arg, msg := range map[string]string{
		"":      "want one or more comparisons",
		"=1":    `arg "=1" is no comparison`,
		">=-1":  "length -1 is negative",
		">=1.0": "length 1.0 is not an integer literal",
		">=n":   "n is not a Go literal",
	} {
		_, err := Runes{}.Parse(arg)
		assert.ErrorIs(t, err, ErrArgs, arg)
		assert.ErrorContains(t, err, msg, arg)
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
	for _, arg := range []string{"2", "+2", "1.5", "0.0001", "1e-8", "0x10", "1_000"} {
		o, err := MultipleOf{}.Parse(arg)
		assert.NilError(t, err, arg)
		assert.Equal(t, o, Rule(MultipleOfOptions{Divisor: arg}))
	}

	for arg, msg := range map[string]string{
		"":       `divisor "" is not a Go literal`,
		"0":      "divisor 0 is not positive",
		"0.0":    "divisor 0.0 is not positive",
		"-1":     "divisor -1 is not an integer or float literal",
		`"1"`:    `divisor "1" is not an integer or float literal`,
		"'a'":    "divisor 'a' is not an integer or float literal",
		"1i":     "divisor 1i is not an integer or float literal",
		"1+1":    "divisor 1+1 is not an integer or float literal",
		"n":      "divisor n is not an integer or float literal",
		"1e9999": "divisor 1e9999 is out of range",
	} {
		_, err := MultipleOf{}.Parse(arg)
		assert.ErrorIs(t, err, ErrArgs, arg)
		assert.ErrorContains(t, err, msg, arg)
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
		{"300", types.Typ[types.Int8], "divisor 300: 300 is no value of int8"},
		{"51.2", types.Typ[types.Uint8], "divisor 51.2: 256 is no value of uint8"},
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
	o, err := Unique{}.Parse("")
	assert.NilError(t, err)
	assert.Equal(t, o, UniqueOptions{})

	_, err = Unique{}.Parse("x")
	assert.ErrorIs(t, err, ErrArgs)
	assert.ErrorContains(t, err, `unexpected arg "x"; want none`)
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
