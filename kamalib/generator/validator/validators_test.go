package validator

import (
	"fmt"
	"go/types"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/generator/validator/ext/basic"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
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
//kama:validate hook=False
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
	//kama:validate regexp("^a+$")
	ReString string
	//kama:validate regexp("^a+$")
	ReStr Str
	//kama:validate regexp("^a+$")
	ReBytes []byte
	//kama:validate regexp("^b+$")
	ReNamedBytes Bytes
	//kama:validate regexp('^a="b c"$')
	RePtr *string
	//kama:validate length(ge=1, le=10)
	LenString string
	//kama:validate length(3)
	LenArray [3]int
	//kama:validate length(ne=0)
	LenMap map[string]int
	//kama:validate length(lt=5)
	LenChan chan int
	//kama:validate length(gt=0)
	LenOpt opt.Option[[]int]
	//kama:validate length(gt=0)
	LenPtr *[]int
	//kama:validate length(ge=1)
	LenPlus string
	//kama:validate runes(ge=1, le=10)
	RunesString string
	//kama:validate runes(3)
	RunesStr Str
	//kama:validate runes(gt=0)
	RunesOpt opt.Option[string]
	//kama:validate cmp(ge=-1, lt=10)
	CmpInt int8
	//kama:validate cmp(gt=0)
	CmpUint uint
	//kama:validate cmp(ge=1.5)
	CmpFloat float32
	//kama:validate cmp(gt="a b")
	CmpString Str
	//kama:validate cmp(True)
	CmpBool Bool
	//kama:validate cmp(ne=97)
	CmpRune rune
	//kama:validate cmp(gt=1)
	CmpUnd und.Und[Int]
	//kama:validate const(1, 2)
	ConstInt Int
	//kama:validate const(0.5, -1)
	ConstFloat float64
	//kama:validate const("a", 'b c')
	ConstString string
	//kama:validate const(False)
	ConstBool bool
	//kama:validate const(1)
	ConstPtr *int
	//kama:validate multipleof(3)
	MulInt int
	//kama:validate multipleof(1.5)
	MulUint8 uint8
	//kama:validate multipleof(2)
	MulNamed Int
	//kama:validate multipleof(0.0001)
	MulFloat float64
	//kama:validate multipleof(0.5)
	MulFloat32 float32
	//kama:validate multipleof(2)
	MulPtr *float64
	//kama:validate unique
	UniqueInts []int
	//kama:validate unique
	UniqueBytes Bytes
	//kama:validate unique
	UniqueArray [3]int
	//kama:validate unique
	UniqueArrayPtr *[3]int
	//kama:validate unique
	UniqueArrayOpt opt.Option[[3]int]
	//kama:validate unique
	UniqueAny []any
	//kama:validate unique
	UniquePtrs []*int
	//kama:validate unique
	UniqueParam []T
	//kama:validate unique
	UniqueDeepArray [2][]int
	//kama:validate format("date")
	FormatDate string
	//kama:validate format("date-time")
	FormatDateTime Str
	//kama:validate format("url")
	FormatUrl opt.Option[string]
}
`,
	})
	fn := gentest.Funcs(t, out["a.kama_validate.go"])["A.Validate"]

	assertContains(
		t,
		fn,
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
		`kamaValidateLength(len(v.LenPlus), ">=", 1)`,
		`kamaValidateAll(stopOnFirstError, `+
			`kamaValidateRunes(utf8.RuneCountInString(v.RunesString), ">=", 1), `+
			`kamaValidateRunes(utf8.RuneCountInString(v.RunesString), "<=", 10))`,
		`kamaValidateRunes(utf8.RuneCountInString(string(v.RunesStr)), "==", 3)`,
		"if v.RunesOpt.IsSome() {",
		`kamaValidateRunes(utf8.RuneCountInString(v.RunesOpt.Value()), ">", 0)`,
		`kamaValidateAll(stopOnFirstError, kamaValidateCmp(v.CmpInt, ">=", -1), `+
			`kamaValidateCmp(v.CmpInt, "<", 10))`,
		`kamaValidateCmp(v.CmpUint, ">", 0)`,
		`kamaValidateCmp(v.CmpFloat, ">=", 1.5)`,
		`kamaValidateCmp(v.CmpString, ">", "a b")`,
		`kamaValidateEqual(v.CmpBool, "==", true)`,
		`kamaValidateCmp(v.CmpRune, "!=", 97)`,
		"if v.CmpUnd.IsDefined() {",
		`kamaValidateCmp(v.CmpUnd.Value(), ">", 1)`,
		`kamaValidateConst(v.ConstInt, 1, 2)`,
		`kamaValidateConst(v.ConstFloat, 0.5, -1)`,
		`kamaValidateConst(v.ConstString, "a", "b c")`,
		`kamaValidateConst(v.ConstBool, false)`,
		`kamaValidateConst((*v.ConstPtr), 1)`,
		`kamaValidateMultipleInt(v.MulInt, 3, "3")`,
		`kamaValidateMultipleInt(v.MulUint8, 3, "1.5")`,
		`kamaValidateMultipleInt(v.MulNamed, 2, "2")`,
		`kamaValidateMultipleFloat(v.MulFloat, 64, "1/10000", "0.0001")`,
		`kamaValidateMultipleFloat(float64(v.MulFloat32), 32, "1/2", "0.5")`,
		"if v.MulPtr != nil {",
		`kamaValidateMultipleFloat((*v.MulPtr), 64, "2/1", "2")`,
		`kamaValidateUnique(v.UniqueInts)`,
		`kamaValidateUnique(v.UniqueBytes)`,
		`kamaValidateUnique(kamaValidateCopy(v.UniqueArray)[:])`,
		"if v.UniqueArrayPtr != nil {",
		`kamaValidateUnique(kamaValidateCopy((*v.UniqueArrayPtr))[:])`,
		"if v.UniqueArrayOpt.IsSome() {",
		`kamaValidateUnique(kamaValidateCopy(v.UniqueArrayOpt.Value())[:])`,
		`kamaValidateUniqueDeep(v.UniqueAny)`,
		`kamaValidateUniqueDeep(v.UniquePtrs)`,
		`kamaValidateUniqueDeep(v.UniqueParam)`,
		`kamaValidateUniqueDeep(kamaValidateCopy(v.UniqueDeepArray)[:])`,
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
		"func kamaValidateRunes(got int, op string, want int) error {",
		"func kamaValidateMultipleInt[T ~int | ~int8 | ~int16 | ~int32 | ~int64 |",
		"func kamaValidateMultipleFloat(got float64, bitSize int, step, want string) error {",
		"func kamaValidateUnique[T comparable](got []T) error {",
		"func kamaValidateUniqueDeep[T any](got []T) error {",
		"func kamaValidateCopy[T any](v T) *T {",
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
		{"zero args", "//kama:validate zero(\"yes\")\n\tN int", ErrUnsupported,
			[]string{"//kama:validate zero on field V", "zero takes 0 positional args"}},
		{"zero IsZero promoted through a pointer",
			"//kama:validate zero\n\tN struct{ *elastic.Elastic[int]; L []int }", ErrUnsupported,
			[]string{"has no method IsZero() bool declared on it or promoted" +
				" without a pointer or an interface"}},
		{"zero not comparable", "//kama:validate zero\n\tN struct{ L []int }", ErrUnsupported,
			[]string{"struct{L []int} is not nillable, has no method IsZero() bool declared on it" +
				" or promoted without a pointer or an interface, and is not comparable"}},
		// nil
		{"nil args", "//kama:validate nil(\"x\")\n\tN *int", ErrUnsupported,
			[]string{"nil takes 0 positional args"}},
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
		{"regexp bad pattern", "//kama:validate regexp(\"a(\")\n\tN string", ErrUnsupported,
			[]string{"//kama:validate regexp on field V", `pattern "a(" does not compile`}},
		{"regexp no pattern", "//kama:validate regexp\n\tN string", ErrUnsupported,
			[]string{"want a pattern"}},
		{"regexp empty pattern", "//kama:validate regexp(\"\")\n\tN string", ErrUnsupported,
			[]string{"want a pattern"}},
		{"regexp int", "//kama:validate regexp(\"a\")\n\tN int", ErrUnsupported,
			[]string{"int is neither a string nor a []byte type"}},
		{"regexp runes", "//kama:validate regexp(\"a\")\n\tN []rune", ErrUnsupported,
			[]string{"[]rune is neither a string nor a []byte type"}},
		{"regexp bad pattern on pointer", "//kama:validate regexp(\"a(\")\n\tN *string",
			ErrUnsupported, []string{`pattern "a(" does not compile`}},
		// length
		{"length no args", "//kama:validate length\n\tN string", ErrUnsupported,
			[]string{"want one or more comparisons"}},
		{"length old spelling", "//kama:validate length=\">=1\"\n\tN string", ErrDirective,
			[]string{`length=">=1" on field V names the validator length as a keyword;` +
				" write it as a call, as in length(ge=1)"}},
		{"length null", "//kama:validate length(ge=None)\n\tN string", ErrUnsupported,
			[]string{"None is no number, string or bool"}},
		{"length two positional", "//kama:validate length(1, 2)\n\tN string", ErrUnsupported,
			[]string{"length takes one positional arg, the value compared with ==, got 2"}},
		{"length list", "//kama:validate length(ge=[1])\n\tN string", ErrUnsupported,
			[]string{"[1] is no number, string or bool"}},
		{"length negative", "//kama:validate length(ge=-1)\n\tN string", ErrUnsupported,
			[]string{"length -1 is negative"}},
		{"length float", "//kama:validate length(ge=1.5)\n\tN string", ErrUnsupported,
			[]string{"1.5 is no value of int", "cannot convert"}},
		{"length not literal", "//kama:validate length(ge=n)\n\tN string", ErrUnsupported,
			[]string{"n is no number, string or bool"}},
		{"length float literal", "//kama:validate length(ge=1.0)\n\tN string", ErrUnsupported,
			[]string{"length 1.0 is not an integer literal"}},
		{"length string", "//kama:validate length(ge=\"a\")\n\tN string", ErrUnsupported,
			[]string{`"a" is no value of int`}},
		{"length plus float literal", "//kama:validate length(ge=+1.0)\n\tN string",
			ErrUnsupported, []string{"length 1.0 is not an integer literal"}},
		{"length int", "//kama:validate length(ge=1)\n\tN int", ErrUnsupported,
			[]string{"//kama:validate length on field V",
				"int is not a string, slice, array, map or channel type"}},
		{"length float on option", "//kama:validate length(ge=1.5)\n\tN opt.Option[string]",
			ErrUnsupported, []string{"1.5 is no value of int"}},
		// runes
		{"runes no args", "//kama:validate runes\n\tN string", ErrUnsupported,
			[]string{"want one or more comparisons"}},
		{"runes negative", "//kama:validate runes(ge=-1)\n\tN string", ErrUnsupported,
			[]string{"//kama:validate runes on field V", "length -1 is negative"}},
		{"runes bytes", "//kama:validate runes(ge=1)\n\tN []byte", ErrUnsupported,
			[]string{"[]byte is not a string type"}},
		{"runes slice", "//kama:validate runes(ge=1)\n\tN []string", ErrUnsupported,
			[]string{"[]string is not a string type"}},
		// multipleof
		{"multipleof no args", "//kama:validate multipleof\n\tN int", ErrUnsupported,
			[]string{"want one number, as in multipleof(0.01)"}},
		{"multipleof zero", "//kama:validate multipleof(0)\n\tN int", ErrUnsupported,
			[]string{"//kama:validate multipleof on field V", "divisor 0 is not positive"}},
		{"multipleof negative", "//kama:validate multipleof(-2)\n\tN *int", ErrUnsupported,
			[]string{"divisor -2 is not an integer or float literal"}},
		{"multipleof string", "//kama:validate multipleof(2)\n\tN string", ErrUnsupported,
			[]string{"string is not an integer or float type"}},
		{"multipleof type parameter", "//kama:validate multipleof(2)\n\tN P", ErrUnsupported,
			[]string{"P is not an integer or float type"}},
		// unique
		{"unique args", "//kama:validate unique(\"x\")\n\tN []int", ErrUnsupported,
			[]string{"unique takes 0 positional args"}},
		{
			"unique map",
			"//kama:validate unique\n\tN map[string]int",
			ErrUnsupported,
			[]string{
				"//kama:validate unique on field V",
				"map[string]int is not a slice or array type",
			},
		},
		{"unique type parameter", "//kama:validate unique\n\tN P", ErrUnsupported,
			[]string{"P is not a slice or array type"}},
		{"unique elastic", "//kama:validate unique\n\tN elastic.Elastic[int]", ErrUnsupported,
			[]string{"elastic.Elastic[int] is not a slice or array type"}},
		// cmp
		{"cmp no args", "//kama:validate cmp\n\tN int", ErrUnsupported,
			[]string{"want one or more comparisons"}},
		{"cmp int rejects string", "//kama:validate cmp(gt=\"a\")\n\tN int", ErrUnsupported,
			[]string{`"a" is no value of int`}},
		{"cmp string rejects number", "//kama:validate cmp(gt=65)\n\tN string", ErrUnsupported,
			[]string{"65 is no value of string; want a quoted string"}},
		{"cmp float rejects string", "//kama:validate cmp(gt=\"1\")\n\tN float64", ErrUnsupported,
			[]string{`"1" is no value of float64`}},
		{"cmp bool order", "//kama:validate cmp(gt=True)\n\tN bool", ErrUnsupported,
			[]string{"bool is a bool type, which takes == and != only; got >"}},
		{"cmp bool rejects number", "//kama:validate cmp(1)\n\tN bool", ErrUnsupported,
			[]string{"1 is no value of bool"}},
		{"cmp bare name", "//kama:validate cmp(gt=unknown)\n\tN int", ErrUnsupported,
			[]string{"unknown is no number, string or bool"}},
		{"cmp struct", "//kama:validate cmp(gt=1)\n\tN struct{}", ErrUnsupported,
			[]string{"struct{} is not an integer, float, string or bool type"}},
		{"cmp complex", "//kama:validate cmp(gt=1)\n\tN complex128", ErrUnsupported,
			[]string{"complex128 is not an integer, float, string or bool type"}},
		{"cmp elastic", "//kama:validate cmp(gt=1)\n\tN elastic.Elastic[int]", ErrUnsupported,
			[]string{"elastic.Elastic[int] is not an integer"}},
		// const
		{"const no args", "//kama:validate const\n\tN int", ErrUnsupported,
			[]string{"want one or more values"}},
		{"const int rejects word", "//kama:validate const(1, a)\n\tN int", ErrUnsupported,
			[]string{"//kama:validate const on field V", "const: a is no literal"}},
		{"const string rejects number", "//kama:validate const(1)\n\tN string",
			ErrUnsupported,
			[]string{`1 is no value of string; want a quoted string, as in "1"`}},
		{"const string rejects list", `//kama:validate const(["a"])` + "\n\tN string",
			ErrUnsupported,
			[]string{`["a"] is no value of string`}},
		{"const int rejects quoted", "//kama:validate const(\"1\")\n\tN int", ErrUnsupported,
			[]string{`"1" is no value of int`}},
		{"const float rejects quoted", "//kama:validate const(\"1.5\")\n\tN float64",
			ErrUnsupported, []string{`"1.5" is no value of float64`}},
		{"const overflow", "//kama:validate const(256)\n\tN uint8", ErrUnsupported,
			[]string{"256 is no value of uint8"}},
		{"const slice", "//kama:validate const(1)\n\tN []int", ErrUnsupported,
			[]string{"[]int is not a bool, numeric or string type"}},
		// format
		{"format unknown", "//kama:validate format(\"email\")\n\tN string", ErrUnsupported,
			[]string{"//kama:validate format on field V",
				`unknown format "email"; known: date, date-time, ipv4, ipv6, url, uuid`}},
		{"format no name", "//kama:validate format\n\tN string", ErrUnsupported,
			[]string{`want one format name, as in format("date");` +
				" known: date, date-time, ipv4, ipv6, url, uuid"}},
		{"format int", "//kama:validate format(\"date\")\n\tN int", ErrUnsupported,
			[]string{"int is neither a string type nor raw JSON text"}},
		{
			"format elastic",
			"//kama:validate format(\"date\")\n\tN elastic.Elastic[string]",
			ErrUnsupported,
			[]string{"elastic.Elastic[string] is neither a string type nor raw JSON text"},
		},
		{"nil elastic", "//kama:validate nil\n\tN elastic.Elastic[*int]", ErrUnsupported,
			[]string{"elastic.Elastic[*int] is not nillable"}},
		{"regexp elastic", "//kama:validate regexp(\"a\")\n\tN elastic.Elastic[string]",
			ErrUnsupported, []string{"elastic.Elastic[string] is neither a string"}},
		{"length struct", "//kama:validate length(gt=0)\n\tN struct{}",
			ErrUnsupported, []string{"struct{} is not a string, slice"}},
		{"const elastic", "//kama:validate const(1)\n\tN elastic.Elastic[int]", ErrUnsupported,
			[]string{"elastic.Elastic[int] is not a bool, numeric or string type"}},
		{"format bytes", "//kama:validate format(\"date\")\n\tN []byte", ErrUnsupported,
			[]string{"[]byte is neither a string type nor raw JSON text"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "package x\n\nimport (\n" +
				"\t\"github.com/ngicks/option/elastic\"\n" +
				"\t\"github.com/ngicks/option/opt\"\n)\n\n" +
				"var (\n\t_ elastic.Elastic[int]\n\t_ opt.Option[int]\n)\n\n" +
				"//kama:validate\n//kama:validate hook=False\ntype V[P any] struct {\n\t" +
				strings.Replace(tc.field, "\n\tN ", "\n\tV ", 1) + "\n}\n"

			_, err := generate(t, map[string]string{"a.go": src}, testRun{})
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
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate cmp(gt=1)\n\tP *struct{}\n}\n",
	}, testRun{})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorContains(t, err,
		"*struct{} is not an integer, float, string or bool type;"+
			" for the value it holds: struct{} is not an integer")
}

// TestArgsErrorOnHeldValue reports an error of the args alone once,
// without applying the validator again to the value a pointer holds.
func TestArgsErrorOnHeldValue(t *testing.T) {
	_, err := generate(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate length(ge=1.5)\n\tP *string\n}\n",
	}, testRun{})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorIs(t, err, ext.ErrArgs)
	assert.Equal(t, strings.Count(err.Error(), "1.5 is no value of int"), 1, err.Error())
	assert.Assert(t, !strings.Contains(err.Error(), "for the value it holds"), err.Error())
}

// lengthOne is a test validator replacing the built-in length: the value
// has length one, and it takes no args.
type lengthOne struct{}

func (lengthOne) Name() string { return "length" }

func (lengthOne) Parse(c expr.Call, _ ext.Resolver) (ext.Rule, error) {
	if len(c.Args) > 0 {
		return nil, fmt.Errorf("%w: unexpected args %s; want none", ext.ErrArgs, expr.Print(c.Args))
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

func (lengthOne) Emit(e ext.Emitter, _ types.Type, v ext.Gen) (ext.Gen, error) {
	e.Declare("kamaValidateLengthOne", func(imp ext.Importer) ext.Step {
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
	reg := ext.DefaultRegistry().Clone()
	reg.Register(lengthOne{})

	files := map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate length\n\tS string\n}\n",
	}

	f, opts := stage(t, files)
	out, err := generateInto(t, f, opts, Options{Registry: reg})
	assert.NilError(t, err)
	assert.NilError(t, gentest.Build(f, opts))

	assertContains(t, out["a.kama_validate.go"], `kamaValidateLengthOne(len(v.S))`)
	assertNotContains(t, out[HelpersFile], "kamaValidateCompare")

	// The default registry keeps the built-in, which wants comparisons.
	_, err = generate(t, files, testRun{})
	assert.ErrorContains(t, err, "want one or more comparisons")
}

// multiple is the validator of the package doc of rule, written as the doc
// writes it. The doc example declares its own lang, of the same type as the
// lang of this package.
type multiple struct{}

func (multiple) Name() string { return "multiple" }

type multipleOptions struct{ N int64 }

func (multiple) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	var args struct {
		N int64 `json:"n"`
	}

	if _, err := (ext.Params{Positional: []string{"n"}}).Bind(r, c, &args); err != nil {
		return nil, err
	}

	if args.N <= 0 {
		return nil, fmt.Errorf("%w: want a positive integer", ext.ErrArgs)
	}

	return multipleOptions{N: args.N}, nil
}

func (multipleOptions) Check(t types.Type) error {
	if b, ok := t.Underlying().(*types.Basic); !ok || b.Info()&types.IsInteger == 0 {
		return fmt.Errorf("%s is not an integer type", t)
	}

	return nil
}

func (o multipleOptions) Emit(e ext.Emitter, _ types.Type, v ext.Gen) (ext.Gen, error) {
	e.Declare("kamaValidateMultiple", func(imp ext.Importer) ext.Step {
		return lang.Func(
			lang.Expr("func kamaValidateMultiple(%s, %s int64) error",
				genhelper.Shadow("got"), genhelper.Shadow("n")),
			lang.If(
				lang.Expr("%s%%%s != 0", genhelper.Ref("got"), genhelper.Ref("n")),
				lang.Stmt(`return %s.New("want a multiple")`, imp.Import("errors")),
				nil,
			),
			lang.Stmt("return nil"),
		)
	})

	return lang.Expr("kamaValidateMultiple(int64(%s), %d)", v, o.N), nil
}

// TestDocExample generates and builds code with the validator the package
// doc of rule writes.
func TestDocExample(t *testing.T) {
	reg := ext.DefaultRegistry().Clone()
	reg.Register(multiple{})

	files := map[string]string{
		"a.go": "package x\n\ntype Count uint8\n\n" +
			"//kama:validate\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate multiple(3)\n\tN int\n" +
			"\t//kama:validate multiple(4)\n\tC Count\n" +
			"\t//kama:validate multiple(5)\n\tP *int64\n}\n",
	}

	f, opts := stage(t, files)
	out, err := generateInto(t, f, opts, Options{Registry: reg})
	assert.NilError(t, err)
	assert.NilError(t, gentest.Build(f, opts))

	assertContains(t, out["a.kama_validate.go"],
		check(`validator.Field(kamaValidateMultiple(int64(v.N), 3), "N")`),
		check(`validator.Field(kamaValidateMultiple(int64(v.C), 4), "C")`),
		check(`validator.Field(kamaValidateMultiple(int64((*v.P)), 5), "P")`),
	)
	assertContains(t, out[HelpersFile],
		"func kamaValidateMultiple(got, n int64) error {",
		"if got%n != 0 {",
	)

	_, err = generate(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate multiple(0)\n\tN int\n}\n",
	}, testRun{Registry: reg})
	assert.ErrorIs(t, err, ext.ErrArgs)

	_, err = generate(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate multiple(3)\n\tS string\n}\n",
	}, testRun{Registry: reg})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorContains(t, err, "string is not an integer type")
}

// TestFormatExtra registers a format validator with a format besides the
// built-in ones.
func TestFormatExtra(t *testing.T) {
	formats := basic.DefaultFormats()
	formats["lower"] = basic.FormatHelper{
		Name: "kamaValidateFormatLower",
		Decl: func(imp ext.Importer) ext.Step {
			v := imp.Import(ext.ValidationPath)

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

	reg := ext.DefaultRegistry().Clone()
	reg.Register(basic.Format{Formats: formats})

	files := map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate format(\"lower\")\n\tS string\n" +
			"\t//kama:validate format(\"date\")\n\tD string\n}\n",
	}

	f, opts := stage(t, files)
	out, err := generateInto(t, f, opts, Options{Registry: reg})
	assert.NilError(t, err)
	assert.NilError(t, gentest.Build(f, opts))

	assertContains(t, out["a.kama_validate.go"],
		`kamaValidateFormatLower(v.S)`, `kamaValidateFormatDate(v.D)`)
	assertContains(t, out[HelpersFile], "func kamaValidateFormatLower(got string) error {")

	_, err = generate(t, files, testRun{})
	assert.ErrorContains(
		t,
		err,
		`unknown format "lower"; known: date, date-time, ipv4, ipv6, url, uuid`,
	)
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
//kama:validate hook=False
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
			"//kama:validate\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate cmp(ge=1, le=3)\n\tN int\n}\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		"func (v *A) Validate(stopOnFirstError2 bool) error {",
		`kamaValidateAll(stopOnFirstError2, kamaValidateCmp(v.N, ">=", 1), `+
			`kamaValidateCmp(v.N, "<=", 3))`,
	)
	assertNotContains(t, fn, "kamaValidateAll(stopOnFirstError,")
}
