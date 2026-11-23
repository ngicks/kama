package validator

import (
	"fmt"
	"go/types"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/rule"
)

// TestBuiltinValidators applies every built-in validator to the types it
// accepts, pointers and option types included, and builds the output.
func TestBuiltinValidators(t *testing.T) {
	out := generateAndBuild(t, map[string]string{
		"a.go": `package x

import (
	"time"

	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

type (
	Str   string
	Bytes []byte
	Int   int
	Bool  bool
)

//kama:validate
//kama:validate hook=off
type A[T comparable] struct {
	//kama:validate zero
	Zero int
	//kama:validate zero
	ZeroStruct struct{ X int }
	//kama:validate zero
	ZeroPtr *int
	//kama:validate zero
	ZeroTime time.Time
	//kama:validate zero
	ZeroOpt opt.Option[int]
	//kama:validate zero
	ZeroUnd und.Und[[]int]
	//kama:validate zero
	ZeroElastic elastic.Elastic[int]
	//kama:validate zero
	ZeroParam T
	//kama:validate nil
	NilPtr *int
	//kama:validate nil
	NilSlice []int
	//kama:validate nil
	NilAny any
	//kama:validate nil
	NilOpt opt.Option[*int]
	//kama:validate null
	NullUnd und.Und[int]
	//kama:validate null
	NullPtr *und.Und[int]
	//kama:validate null
	NullElastic elastic.Elastic[int]
	//kama:validate regexp="^a+$"
	ReString string
	//kama:validate regexp="^a+$"
	ReStr Str
	//kama:validate regexp="^a+$"
	ReBytes []byte
	//kama:validate regexp="^b+$"
	ReNamedBytes Bytes
	//kama:validate regexp=` + "`^a=\"b c\"$`" + `
	RePtr *string
	//kama:validate length=">=1 <=10"
	LenString string
	//kama:validate length="==3"
	LenArray [3]int
	//kama:validate length="!=0"
	LenMap map[string]int
	//kama:validate length="<5"
	LenChan chan int
	//kama:validate length=">0"
	LenOpt opt.Option[[]int]
	//kama:validate length=">0"
	LenPtr *[]int
	//kama:validate length=">=+1"
	LenPlus string
	//kama:validate cmp=">=-1 <10"
	CmpInt int8
	//kama:validate cmp=">0"
	CmpUint uint
	//kama:validate cmp=">=1.5"
	CmpFloat float32
	//kama:validate cmp=">\"a b\""
	CmpString Str
	//kama:validate cmp="==true"
	CmpBool Bool
	//kama:validate cmp="!='a'"
	CmpRune rune
	//kama:validate cmp=">1"
	CmpUnd und.Und[Int]
	//kama:validate const="1 2"
	ConstInt Int
	//kama:validate const="0.5 -1"
	ConstFloat float64
	//kama:validate const="\"a\" ` + "`b c`" + `"
	ConstString string
	//kama:validate const=false
	ConstBool bool
	//kama:validate const=1
	ConstPtr *int
	//kama:validate format=date
	FormatDate string
	//kama:validate format=date-time
	FormatDateTime Str
	//kama:validate format=url
	FormatUrl opt.Option[string]
}
`,
	})
	fn := funcs(t, out["a.kama_validate.go"])["A.Validate"]

	assertContains(t, fn,
		`kamaValidateZeroComparable(v.Zero)`,
		`kamaValidateZeroComparable(v.ZeroStruct)`,
		`kamaValidateZero(v.ZeroPtr == nil, "a non-nil value")`,
		`kamaValidateZero(v.ZeroTime.IsZero(), "a non-zero value")`,
		`kamaValidateZero(v.ZeroOpt.IsZero(), "a non-zero value")`,
		`kamaValidateZero(v.ZeroUnd.IsZero(), "a non-zero value")`,
		`kamaValidateZero(v.ZeroElastic.IsZero(), "a non-zero value")`,
		`kamaValidateZeroComparable(v.ZeroParam)`,
		`kamaValidateNil(v.NilPtr == nil)`,
		`kamaValidateNil(v.NilSlice == nil)`,
		`kamaValidateNil(v.NilAny == nil)`,
		"if v.NilOpt.IsSome() {",
		`kamaValidateNil(v.NilOpt.Value() == nil)`,
		`kamaValidateNull(v.NullUnd.IsNull())`,
		"if v.NullPtr != nil {",
		`kamaValidateNull((*v.NullPtr).IsNull())`,
		`kamaValidateNull(v.NullElastic.IsNull())`,
		`kamaValidateRegexpString(kamaValidateRegexp_376d544f32, v.ReString)`,
		`kamaValidateRegexpString(kamaValidateRegexp_376d544f32, string(v.ReStr))`,
		`kamaValidateRegexpBytes(kamaValidateRegexp_376d544f32, v.ReBytes)`,
		`kamaValidateRegexpBytes(kamaValidateRegexp_ad7b89858f, []byte(v.ReNamedBytes))`,
		"if v.RePtr != nil {",
		`kamaValidateRegexpString(kamaValidateRegexp_0ed6109140, (*v.RePtr))`,
		`kamaValidateAll(stopOnFirstError, kamaValidateLength(len(v.LenString), ">=", 1), `+
			`kamaValidateLength(len(v.LenString), "<=", 10))`,
		`kamaValidateLength(len(v.LenArray), "==", 3)`,
		`kamaValidateLength(len(v.LenMap), "!=", 0)`,
		`kamaValidateLength(len(v.LenChan), "<", 5)`,
		"if v.LenOpt.IsSome() {",
		`kamaValidateLength(len(v.LenOpt.Value()), ">", 0)`,
		`kamaValidateLength(len((*v.LenPtr)), ">", 0)`,
		`kamaValidateLength(len(v.LenPlus), ">=", +1)`,
		`kamaValidateAll(stopOnFirstError, kamaValidateCmp(v.CmpInt, ">=", -1), `+
			`kamaValidateCmp(v.CmpInt, "<", 10))`,
		`kamaValidateCmp(v.CmpUint, ">", 0)`,
		`kamaValidateCmp(v.CmpFloat, ">=", 1.5)`,
		`kamaValidateCmp(v.CmpString, ">", "a b")`,
		`kamaValidateEqual(v.CmpBool, "==", true)`,
		`kamaValidateCmp(v.CmpRune, "!=", 'a')`,
		"if v.CmpUnd.IsDefined() {",
		`kamaValidateCmp(v.CmpUnd.Value(), ">", 1)`,
		`kamaValidateConst(v.ConstInt, 1, 2)`,
		`kamaValidateConst(v.ConstFloat, 0.5, -1)`,
		`kamaValidateConst(v.ConstString, "a", "b c")`,
		`kamaValidateConst(v.ConstBool, false)`,
		`kamaValidateConst((*v.ConstPtr), 1)`,
		`kamaValidateFormatDate(v.FormatDate)`,
		`kamaValidateFormatDateTime(string(v.FormatDateTime))`,
		"if v.FormatUrl.IsSome() {",
		`kamaValidateFormatUrl(v.FormatUrl.Value())`,
	)

	helpers := out[HelpersFile]
	// One pattern compiles into one variable, however many fields use it.
	assert.Equal(t, strings.Count(helpers, "var kamaValidateRegexp_376d544f32 = "), 1, helpers)
	assertContains(t, helpers,
		"var kamaValidateRegexp_", `regexp.MustCompile("^a=\"b c\"$")`,
		"func kamaValidateCompare[T cmp.Ordered](got T, op string, want T) bool {",
		"func kamaValidateSpell(v any) string {",
		"func kamaValidateAll(stopOnFirstError bool, errs ...error) error {",
	)
}

func TestBuiltinValidatorErrors(t *testing.T) {
	cases := []struct {
		name  string
		field string
		is    error
		msgs  []string
	}{
		// zero
		{"zero args", "//kama:validate zero=yes\n\tN int", ErrUnsupported,
			[]string{"//kama:validate zero on field V", `unexpected arg "yes"; want none`}},
		{"zero IsZero promoted through a pointer",
			"//kama:validate zero\n\tN struct{ *elastic.Elastic[int]; L []int }", ErrUnsupported,
			[]string{"has no method IsZero() bool declared on it or promoted" +
				" without a pointer or an interface"}},
		{"zero not comparable", "//kama:validate zero\n\tN struct{ L []int }", ErrUnsupported,
			[]string{"struct{L []int} is not nillable, has no method IsZero() bool declared on it" +
				" or promoted without a pointer or an interface, and is not comparable"}},
		// nil
		{"nil args", "//kama:validate nil=x\n\tN *int", ErrUnsupported,
			[]string{`unexpected arg "x"`}},
		{"nil not nillable", "//kama:validate nil\n\tN int", ErrUnsupported,
			[]string{"//kama:validate nil on field V", "int is not nillable"}},
		{"nil type parameter", "//kama:validate nil\n\tN P", ErrUnsupported,
			[]string{"P is not nillable"}},
		// null
		{"null without IsNull", "//kama:validate null\n\tN int", ErrUnsupported,
			[]string{"//kama:validate null on field V", "int has no method IsNull() bool"}},
		{"null on option", "//kama:validate null\n\tN opt.Option[int]", ErrUnsupported,
			[]string{"opt.Option[int] has no method IsNull() bool"}},
		{"null on interface", "//kama:validate null\n\tN interface{ IsNull() bool }",
			ErrUnsupported, []string{"is nillable, and nil cannot tell IsNull"}},
		{
			"null promoted through a pointer",
			"//kama:validate null\n\tN struct{ *elastic.Elastic[int] }",
			ErrUnsupported,
			[]string{"has no method IsNull() bool"},
		},
		// regexp
		{"regexp bad pattern", "//kama:validate regexp=\"a(\"\n\tN string", ErrUnsupported,
			[]string{"//kama:validate regexp on field V", `pattern "a(" does not compile`}},
		{"regexp no pattern", "//kama:validate regexp\n\tN string", ErrUnsupported,
			[]string{"want a pattern"}},
		{"regexp empty pattern", "//kama:validate regexp=\"\"\n\tN string", ErrUnsupported,
			[]string{"want a pattern"}},
		{"regexp int", "//kama:validate regexp=a\n\tN int", ErrUnsupported,
			[]string{"int is neither a string nor a []byte type"}},
		{"regexp runes", "//kama:validate regexp=a\n\tN []rune", ErrUnsupported,
			[]string{"[]rune is neither a string nor a []byte type"}},
		{"regexp bad pattern on pointer", "//kama:validate regexp=\"a(\"\n\tN *string",
			ErrUnsupported, []string{`pattern "a(" does not compile`}},
		// length
		{"length no args", "//kama:validate length\n\tN string", ErrUnsupported,
			[]string{"want one or more comparisons"}},
		{"length bad op", "//kama:validate length=\"=1\"\n\tN string", ErrUnsupported,
			[]string{`arg "=1" is no comparison`}},
		{"length no operand", "//kama:validate length=\">=\"\n\tN string", ErrUnsupported,
			[]string{`arg ">=" is no comparison`}},
		{"length pair", "//kama:validate length=\"a=1\"\n\tN string", ErrUnsupported,
			[]string{`arg "a=1" is no comparison`}},
		{"length bad quoting", "//kama:validate length=`\"1`\n\tN string", ErrUnsupported,
			[]string{"unterminated quoted string"}},
		{"length negative", "//kama:validate length=\">=-1\"\n\tN string", ErrUnsupported,
			[]string{"length -1 is negative"}},
		{"length float", "//kama:validate length=\">=1.5\"\n\tN string", ErrUnsupported,
			[]string{"1.5 is no value of int", "cannot convert"}},
		{"length not literal", "//kama:validate length=\">=n\"\n\tN string", ErrUnsupported,
			[]string{"n is not a Go literal"}},
		{"length float literal", "//kama:validate length=\">=1.0\"\n\tN string", ErrUnsupported,
			[]string{"length 1.0 is not an integer literal"}},
		{"length rune literal", "//kama:validate length=\">='a'\"\n\tN string", ErrUnsupported,
			[]string{"length 'a' is not an integer literal"}},
		{"length plus float literal", "//kama:validate length=\">=+1.0\"\n\tN string",
			ErrUnsupported, []string{"length +1.0 is not an integer literal"}},
		{"length plus rune literal", "//kama:validate length=\">=+'a'\"\n\tN string",
			ErrUnsupported, []string{"length +'a' is not an integer literal"}},
		{"length expression", "//kama:validate length=\">=1+1\"\n\tN string", ErrUnsupported,
			[]string{"1+1 is not a Go literal"}},
		{"length plus negative", "//kama:validate length=\">=+-1\"\n\tN string", ErrUnsupported,
			[]string{"+-1 is not a Go literal"}},
		{"length int", "//kama:validate length=\">=1\"\n\tN int", ErrUnsupported,
			[]string{"//kama:validate length on field V",
				"int is not a string, slice, array, map or channel type"}},
		{"length float on option", "//kama:validate length=\">=1.5\"\n\tN opt.Option[string]",
			ErrUnsupported, []string{"1.5 is no value of int"}},
		// cmp
		{"cmp no args", "//kama:validate cmp\n\tN int", ErrUnsupported,
			[]string{"want one or more comparisons"}},
		{"cmp int rejects float", "//kama:validate cmp=\">1.5\"\n\tN int", ErrUnsupported,
			[]string{"//kama:validate cmp on field V", "1.5 is no value of int", "cannot convert"}},
		{"cmp int rejects string", "//kama:validate cmp=`>\"a\"`\n\tN int", ErrUnsupported,
			[]string{`"a" is no value of int`}},
		{"cmp uint rejects negative", "//kama:validate cmp=\">-1\"\n\tN uint", ErrUnsupported,
			[]string{"-1 is no value of uint"}},
		{"cmp overflow", "//kama:validate cmp=\"<300\"\n\tN int8", ErrUnsupported,
			[]string{"300 is no value of int8"}},
		{"cmp float overflow", "//kama:validate cmp=\"<1e100\"\n\tN float32", ErrUnsupported,
			[]string{"1e100 is no value of float32"}},
		{"cmp string rejects number", "//kama:validate cmp=\">65\"\n\tN string", ErrUnsupported,
			[]string{"65 is no value of string; want a quoted string"}},
		{"cmp float rejects string", "//kama:validate cmp=`>\"1\"`\n\tN float64", ErrUnsupported,
			[]string{`"1" is no value of float64`}},
		{"cmp bool order", "//kama:validate cmp=\">true\"\n\tN bool", ErrUnsupported,
			[]string{"bool is a bool type, which takes == and != only; got >"}},
		{"cmp bool rejects number", "//kama:validate cmp=\"==1\"\n\tN bool", ErrUnsupported,
			[]string{"1 is no value of bool"}},
		{"cmp expression", "//kama:validate cmp=\">1+1\"\n\tN int", ErrUnsupported,
			[]string{"1+1 is not a Go literal"}},
		{"cmp struct", "//kama:validate cmp=\">1\"\n\tN struct{}", ErrUnsupported,
			[]string{"struct{} is not an integer, float, string or bool type"}},
		{"cmp complex", "//kama:validate cmp=\">1\"\n\tN complex128", ErrUnsupported,
			[]string{"complex128 is not an integer, float, string or bool type"}},
		{"cmp elastic", "//kama:validate cmp=\">1\"\n\tN elastic.Elastic[int]", ErrUnsupported,
			[]string{"elastic.Elastic[int] is not an integer"}},
		// const
		{"const no args", "//kama:validate const\n\tN int", ErrUnsupported,
			[]string{"want one or more values"}},
		{"const pair", "//kama:validate const=\"a=b\"\n\tN string", ErrUnsupported,
			[]string{`arg "a=b" is a key=value pair; quote a value holding =`}},
		{"const int rejects word", "//kama:validate const=\"1 a\"\n\tN int", ErrUnsupported,
			[]string{"//kama:validate const on field V", "a is not a Go literal"}},
		{"const string rejects bare word", "//kama:validate const=red\n\tN string",
			ErrUnsupported,
			[]string{`red is no value of string; want a quoted string, as in "red"`}},
		{"const int rejects quoted", "//kama:validate const=`\"1\"`\n\tN int", ErrUnsupported,
			[]string{`"1" is no value of int`}},
		{"const float rejects quoted", "//kama:validate const=\"`1.5`\"\n\tN float64",
			ErrUnsupported, []string{`"1.5" is no value of float64`}},
		{"const overflow", "//kama:validate const=256\n\tN uint8", ErrUnsupported,
			[]string{"256 is no value of uint8"}},
		{"const slice", "//kama:validate const=1\n\tN []int", ErrUnsupported,
			[]string{"[]int is not a bool, numeric or string type"}},
		// format
		{"format unknown", "//kama:validate format=email\n\tN string", ErrUnsupported,
			[]string{"//kama:validate format on field V",
				`unknown format "email"; known: date, date-time, url`}},
		{"format no name", "//kama:validate format\n\tN string", ErrUnsupported,
			[]string{"want one format name; known: date, date-time, url"}},
		{"format int", "//kama:validate format=date\n\tN int", ErrUnsupported,
			[]string{"int is not a string type"}},
		{"format elastic", "//kama:validate format=date\n\tN elastic.Elastic[string]",
			ErrUnsupported, []string{"elastic.Elastic[string] is not a string type"}},
		{"nil elastic", "//kama:validate nil\n\tN elastic.Elastic[*int]", ErrUnsupported,
			[]string{"elastic.Elastic[*int] is not nillable"}},
		{"regexp elastic", "//kama:validate regexp=a\n\tN elastic.Elastic[string]",
			ErrUnsupported, []string{"elastic.Elastic[string] is neither a string"}},
		{"length elastic", "//kama:validate length=\">0\"\n\tN elastic.Elastic[string]",
			ErrUnsupported, []string{"elastic.Elastic[string] is not a string, slice"}},
		{"const elastic", "//kama:validate const=1\n\tN elastic.Elastic[int]", ErrUnsupported,
			[]string{"elastic.Elastic[int] is not a bool, numeric or string type"}},
		{"format bytes", "//kama:validate format=date\n\tN []byte", ErrUnsupported,
			[]string{"[]byte is not a string type"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "package x\n\nimport (\n" +
				"\t\"github.com/ngicks/option/elastic\"\n" +
				"\t\"github.com/ngicks/option/opt\"\n)\n\n" +
				"var (\n\t_ elastic.Elastic[int]\n\t_ opt.Option[int]\n)\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype V[P any] struct {\n\t" +
				strings.Replace(tc.field, "\n\tN ", "\n\tV ", 1) + "\n}\n"

			_, err := generate(t, map[string]string{"a.go": src}, Options{})
			assert.ErrorIs(t, err, tc.is)

			for _, msg := range tc.msgs {
				assert.ErrorContains(t, err, msg)
			}
		})
	}
}

// TestHeldValueError reports why a validator applies neither to a pointer
// nor to the value it points to.
func TestHeldValueError(t *testing.T) {
	_, err := generate(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=off\ntype A struct {\n" +
			"\t//kama:validate cmp=\">1\"\n\tP *struct{}\n}\n",
	}, Options{})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorContains(t, err,
		"*struct{} is not an integer, float, string or bool type;"+
			" for the value it holds: struct{} is not an integer")
}

// TestArgsErrorOnHeldValue reports an error of the args alone once,
// without applying the validator again to the value a pointer holds.
func TestArgsErrorOnHeldValue(t *testing.T) {
	_, err := generate(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=off\ntype A struct {\n" +
			"\t//kama:validate length=\">=1.5\"\n\tP *string\n}\n",
	}, Options{})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorIs(t, err, rule.ErrArgs)
	assert.Equal(t, strings.Count(err.Error(), "1.5 is no value of int"), 1, err.Error())
	assert.Assert(t, !strings.Contains(err.Error(), "for the value it holds"), err.Error())
}

// lengthOne is a test validator replacing the built-in length: the value
// has length one, and it takes no args.
type lengthOne struct{}

func (lengthOne) Name() string { return "length" }

func (lengthOne) Parse(arg string) (rule.Rule, error) {
	if arg != "" {
		return nil, fmt.Errorf("%w: unexpected arg %q; want none", rule.ErrArgs, arg)
	}

	return lengthOne{}, nil
}

func (lengthOne) Check(t types.Type) error {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		if u.Info()&types.IsString != 0 {
			return nil
		}
	case *types.Slice, *types.Array, *types.Map, *types.Chan:
		return nil
	}

	return fmt.Errorf("%s has no length", t)
}

func (lengthOne) Emit(e rule.Emitter, _ types.Type, v rule.Gen) (rule.Gen, error) {
	e.Declare("kamaValidateLengthOne", func(imp rule.Importer) rule.Step {
		return lang.Func(
			lang.Expr("func kamaValidateLengthOne(%s int) error", decl("n")),
			lang.If(
				lang.Expr("%s != 1", lang.Name("n")),
				lang.Stmt(`return %s.New("want length 1")`, imp.Import("errors")),
				nil,
			),
			lang.Stmt("return nil"),
		)
	})

	return lang.Expr("kamaValidateLengthOne(len(%s))", v), nil
}

// TestReplaceBuiltin registers a validator under the name of a built-in,
// which replaces both its args and its code.
func TestReplaceBuiltin(t *testing.T) {
	reg := rule.DefaultRegistry().Clone()
	reg.Register(lengthOne{})

	files := map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=off\ntype A struct {\n" +
			"\t//kama:validate length\n\tS string\n}\n",
	}

	f, opts := stage(t, files)
	out, err := generateInto(t, f, opts, Options{Registry: reg})
	assert.NilError(t, err)
	assert.NilError(t, build(f, opts))

	assertContains(t, out["a.kama_validate.go"], `kamaValidateLengthOne(len(v.S))`)
	assertNotContains(t, out[HelpersFile], "kamaValidateCompare")

	// The default registry keeps the built-in, which wants comparisons.
	_, err = generate(t, files, Options{})
	assert.ErrorContains(t, err, "want one or more comparisons")
}

// TestFormatExtra registers a format validator with a format besides the
// built-in ones.
func TestFormatExtra(t *testing.T) {
	formats := rule.DefaultFormats()
	formats["lower"] = rule.FormatHelper{
		Name: "kamaValidateFormatLower",
		Decl: func(imp rule.Importer) rule.Step {
			v := imp.Import(rule.ValidationPath)

			return lang.Func(
				lang.Expr("func kamaValidateFormatLower(%s string) error", decl("got")),
				lang.If(
					lang.Expr("%s.ToLower(%s) != %s",
						imp.Import("strings"), lang.Name("got"), lang.Name("got")),
					lang.Stmt(`return %s.New(%s.ErrFormat, "want format lower, got %%q", %s)`,
						v, v, lang.Name("got")),
					nil,
				),
				lang.Stmt("return nil"),
			)
		},
	}

	reg := rule.DefaultRegistry().Clone()
	reg.Register(rule.Format{Formats: formats})

	files := map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=off\ntype A struct {\n" +
			"\t//kama:validate format=lower\n\tS string\n" +
			"\t//kama:validate format=date\n\tD string\n}\n",
	}

	f, opts := stage(t, files)
	out, err := generateInto(t, f, opts, Options{Registry: reg})
	assert.NilError(t, err)
	assert.NilError(t, build(f, opts))

	assertContains(t, out["a.kama_validate.go"],
		`kamaValidateFormatLower(v.S)`, `kamaValidateFormatDate(v.D)`)
	assertContains(t, out[HelpersFile], "func kamaValidateFormatLower(got string) error {")

	_, err = generate(t, files, Options{})
	assert.ErrorContains(t, err, `unknown format "lower"; known: date, date-time, url`)
}

// TestPromotedIsZero calls IsZero only when the zero value can call it. A
// method promoted through an embedded pointer or interface would panic on
// nil, so such a type is compared with its zero value.
func TestPromotedIsZero(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": `package x

import "time"

type Stamp struct{ *time.Time }

type Plain struct{ time.Time }

type Deep struct{ Plain }

type Zeroer interface{ IsZero() bool }

type Iface struct{ Zeroer }

//kama:validate
//kama:validate hook=off
type A struct {
	//kama:validate required
	S Stamp
	//kama:validate required
	P Plain
	//kama:validate required
	D Deep
	//kama:validate required
	I Iface
	//kama:validate zero
	ZS Stamp
	//kama:validate zero
	ZP Plain
}
`,
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		"requiredComparable(v.S)",
		`kamaValidateRequired(!v.P.IsZero(), "the zero value")`,
		`kamaValidateRequired(!v.D.IsZero(), "the zero value")`,
		"requiredComparable(v.I)",
		"kamaValidateZeroComparable(v.ZS)",
		`kamaValidateZero(v.ZP.IsZero(), "a non-zero value")`,
	)
}

// TestStopFlagRenamed passes the stop flag to a helper by the name the
// method spells its parameter by, which differs when the package declares
// stopOnFirstError.
func TestStopFlagRenamed(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\nvar stopOnFirstError = true\n\n" +
			"//kama:validate\n//kama:validate hook=off\ntype A struct {\n" +
			"\t//kama:validate cmp=\">=1 <=3\"\n\tN int\n}\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		"func (v A) Validate(stopOnFirstError2 bool) error {",
		`kamaValidateAll(stopOnFirstError2, kamaValidateCmp(v.N, ">=", 1), `+
			`kamaValidateCmp(v.N, "<=", 3))`,
	)
	assertNotContains(t, fn, "kamaValidateAll(stopOnFirstError,")
}
