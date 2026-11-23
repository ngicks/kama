package rule

import (
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
