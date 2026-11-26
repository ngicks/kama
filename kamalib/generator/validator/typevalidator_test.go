package validator

import (
	"fmt"
	"go/types"
	"strconv"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

func TestTypeValidators(t *testing.T) {
	src := generateAndBuild(t, map[string]string{
		"a.go": `package x

import (
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

//kama:validate cmp(ge=3)
type Level int

//kama:validate length(ge=1), unique
type Tags []string

//kama:validate length(le=2)
type Inners map[string]Inner

//kama:validate unique
type Pair [2]Inner

// Mode has no fields, so hook=True calls no hook.
//
//kama:validate hook=True, const(1, 2)
type Mode int

//kama:validate hook=False
type Inner struct {
	//kama:validate required
	N int
}

//kama:validate hook=False
type Box[T any] struct{ V T }

type Hand int

func (Hand) Validate(bool) error { return nil }

type Holder struct {
	L Level
	P *Level
	O opt.Option[Level]
	U und.Und[Level]
	S []Level
	M map[string]Level
	B Box[Level]
	//kama:validate cmp(le=9)
	F  Level
	Pa Pair
	H  []Hand
}
`,
	})["a.kama_validate.go"]
	fs := gentest.Funcs(t, src)

	assertContains(t, fs["Level.Validate"], check(`kamaValidateCmp(v, ">=", 3)`))
	assertContains(t, fs["Tags.Validate"],
		check(`kamaValidateLength(len(v), ">=", 1)`),
		check(`kamaValidateUnique(v)`),
	)
	assertContains(t, fs["Mode.Validate"], check(`kamaValidateConst(v, 1, 2)`))
	assertNotContains(t, fs["Mode.Validate"], "validateMode")

	// The validators of a slice, array or map type run before the walk of
	// its elements.
	inners := fs["Inners.Validate"]
	assert.Assert(t,
		strings.Index(inners, check(`kamaValidateLength(len(v), "<=", 2)`)) <
			strings.Index(inners, check(`validator.Key(e.Validate(stopOnFirstError), k)`)),
		inners,
	)

	pair := fs["Pair.Validate"]
	assert.Assert(t,
		strings.Index(pair, check(`kamaValidateUnique(kamaValidateCopy(v)[:])`)) <
			strings.Index(pair, check(`validator.Index(e.Validate(stopOnFirstError), i)`)),
		pair,
	)

	assertContains(t, fs["Holder.Validate"],
		check(`validator.Field(v.L.Validate(stopOnFirstError), "L")`),
		"if v.P != nil {",
		check(`validator.Field((*v.P).Validate(stopOnFirstError), "P")`),
		check(`validator.Field(v.O.Value().Validate(stopOnFirstError), "O")`),
		check(`validator.Field(v.U.Value().Validate(stopOnFirstError), "U")`),
		check(`validator.Field(validator.Index(e.Validate(stopOnFirstError), i), "S")`),
		check(`validator.Field(validator.Key(e.Validate(stopOnFirstError), k), "M")`),
		"v.B.Validate(stopOnFirstError, func(v Level, stopOnFirstError bool) error {\n"+
			"\t\treturn v.Validate(stopOnFirstError)",
		check(`validator.Field(kamaValidateCmp(v.F, "<=", 9), "F")`),
		check(`validator.Field(v.F.Validate(stopOnFirstError), "F")`),
		check(`validator.Field(v.Pa.Validate(stopOnFirstError), "Pa")`),
		check(`validator.Field(validator.Index(e.Validate(stopOnFirstError), i), "H")`),
	)

	_, ok := fs["Hand.Validate"]
	assert.Assert(t, !ok, "an implementor keeps its own Validate")
}

// TestTypeValidatorsOfGroup runs the validators of a type group on each
// type of the group before those of the type.
func TestTypeValidatorsOfGroup(t *testing.T) {
	fs := gentest.Funcs(t, generateAndBuild(t, map[string]string{
		"a.go": "package x\n\n//kama:validate length(le=3)\ntype (\n" +
			"\t//kama:validate unique\n\tA []int\n\tB string\n)\n",
	})["a.kama_validate.go"])

	a := fs["A.Validate"]
	assert.Assert(t,
		strings.Index(a, check(`kamaValidateLength(len(v), "<=", 3)`)) <
			strings.Index(a, check(`kamaValidateUnique(v)`)),
		a,
	)
	assertContains(t, fs["B.Validate"], check(`kamaValidateLength(len(v), "<=", 3)`))
}

// fields is an ext.StructRule of the tests. It passes the members of a
// struct and its embedded fallback to a helper.
type fields struct{}

func (fields) Name() string { return "fields" }

func (fields) Parse(expr.Call, ext.Resolver) (ext.Rule, error) { return fieldsOptions{}, nil }

type fieldsOptions struct{}

func (fieldsOptions) Check(t types.Type) error { return fmt.Errorf("%s is no struct", t) }

func (fieldsOptions) Emit(ext.Emitter, types.Type, ext.Gen) (ext.Gen, error) {
	return nil, fmt.Errorf("want a struct")
}

func (fieldsOptions) Struct(s ext.Struct) (ext.Rule, error) {
	if _, err := s.Members(); err != nil {
		return nil, err
	}

	return fieldsOf{s: s}, nil
}

// fieldsOf is fields applied to a struct, which it reads through s.
type fieldsOf struct{ s ext.Struct }

func (fieldsOf) Check(types.Type) error { return nil }

func (f fieldsOf) Emit(e ext.Emitter, _ types.Type, _ ext.Gen) (ext.Gen, error) {
	e.Declare("kamaValidateFields", func(ext.Importer) ext.Step {
		return lang.Func(
			lang.Expr("func kamaValidateFields(%s []string, %s ...any) error",
				decl("names"), decl("values")),
			lang.Stmt("return nil"),
		)
	})

	names, err := f.s.Members()
	if err != nil {
		return nil, err
	}

	var (
		quoted []string
		values []ext.Gen
	)

	for _, name := range names {
		_, v, _ := f.s.Member(name)
		quoted = append(quoted, strconv.Quote(name))
		values = append(values, v)
	}

	if _, v, ok := f.s.Fallback(); ok {
		values = append(values, v)
	}

	return lang.Expr("kamaValidateFields([]string{%s}, %s)",
		strings.Join(quoted, ", "), lang.Join(", ", values...)), nil
}

func fieldsRegistry() *ext.Registry {
	reg := ext.DefaultRegistry().Clone()
	reg.Register(fields{})

	return reg
}

// TestStructRule applies an ext.StructRule written in the doc comment of a
// struct type to the struct value. The rule sees the members and the
// embedded fallback of the struct.
func TestStructRule(t *testing.T) {
	f, opts := stage(t, map[string]string{
		"a.go": "package x\n\n//kama:validate fields, hook=False\ntype S struct {\n" +
			"\tA int `json:\"a,omitempty\"`\n\tB *int\n\tc int\n\tD int `json:\"-\"`\n" +
			"\tE map[string]any `json:\",embed\"`\n\t//kama:validate required\n\tF string\n}\n",
	})
	out, err := generateInto(t, f, opts, Options{Registry: fieldsRegistry()})
	assert.NilError(t, err)
	assert.NilError(t, gentest.Build(f, opts))

	fn := gentest.Funcs(t, out["a.kama_validate.go"])["S.Validate"]
	assertContains(t, fn,
		check(`kamaValidateFields([]string{"a", "B", "F"}, v.A, v.B, v.F, v.E)`),
	)
	// The rule of the struct runs before those of its fields.
	assert.Assert(t,
		strings.Index(fn, "kamaValidateFields") < strings.Index(fn, "requiredComparable"), fn,
	)
}

func TestStructRuleErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		is   error
		msgs []string
	}{
		{
			name: "inlined struct",
			src: "//kama:validate fields, hook=False\ntype S struct {\n\tIn\n\tA int\n}\n\n" +
				"type In struct{ B int }\n",
			is: ErrUnsupported,
			msgs: []string{
				"//kama:validate fields on type S",
				"field In inlines the members of an embedded struct",
			},
		},
		{
			name: "on a field",
			src: "//kama:validate hook=False\ntype S struct {\n\t//kama:validate fields\n" +
				"\tA struct{}\n}\n",
			is: ErrDirective,
			msgs: []string{
				"//kama:validate fields on field A validates a struct as a whole;" +
					" write it in the doc comment of a struct type",
			},
		},
		{
			name: "on a type of another kind",
			src:  "//kama:validate fields\ntype N []int\n",
			is:   ErrDirective,
			msgs: []string{"//kama:validate fields on type N validates a struct as a whole"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"a.go": "package x\n\n" + tc.src},
				testRun{Registry: fieldsRegistry()})
			assert.ErrorIs(t, err, tc.is)

			for _, msg := range tc.msgs {
				assert.ErrorContains(t, err, msg)
			}
		})
	}
}

// TestMembers counts the fields of an optional state that encoders may
// leave out and that are present, the other fields and the entries of the
// embedded fallback.
func TestMembers(t *testing.T) {
	src := generateAndBuild(t, map[string]string{
		"a.go": "package x\n\nimport \"github.com/ngicks/option/opt\"\n\n" +
			"//kama:validate members(ge=1, le=3), hook=False\ntype A struct {\n" +
			"\tA opt.Option[int] `json:\"a,omitzero\"`\n\tB *int `json:\",omitempty\"`\n" +
			"\tC int\n\tD string `json:\"-\"`\n\tF *int\n" +
			"\tE map[string]any `json:\",embed\"`\n}\n\n" +
			"//kama:validate members(le=1), hook=False\ntype B struct{ N int }\n",
	})["a.kama_validate.go"]
	fs := gentest.Funcs(t, src)

	// F counts always, as encoders write null for nil.
	count := `kamaValidateCount(v.A.IsSome(), v.B != nil)+2+len(v.E)`
	assertContains(t, fs["A.Validate"],
		`kamaValidateMembers(`+count+`, ">=", 1)`,
		`kamaValidateMembers(`+count+`, "<=", 3)`,
	)
	assertContains(t, fs["B.Validate"], check(`kamaValidateMembers(1, "<=", 1)`))
}

// TestClosed passes the fallback map to the helper, and a struct without
// one always passes.
func TestClosed(t *testing.T) {
	src := generateAndBuild(t, map[string]string{
		"a.go": "package x\n\n" +
			"//kama:validate closed, hook=False\ntype A struct {\n" +
			"\tN int\n\tE map[string]int `json:\",embed\"`\n}\n\n" +
			"//kama:validate closed, hook=False\ntype B struct{ N int }\n",
	})["a.kama_validate.go"]
	fs := gentest.Funcs(t, src)

	assertContains(t, fs["A.Validate"], check(`kamaValidateClosed(v.E, stopOnFirstError)`))
	assertNotContains(t, fs["B.Validate"], "kamaValidateClosed")
}

// TestCmpIntegerBounds rounds a number an integer type cannot represent to
// the bound of the values satisfying the comparison. A comparison every
// value satisfies emits nothing, and one no value satisfies always fails.
func TestCmpIntegerBounds(t *testing.T) {
	src := generateAndBuild(t, map[string]string{
		"a.go": "package x\n\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate cmp(gt=1.5, lt=2.5)\n\tA int\n" +
			"\t//kama:validate cmp(ge=-1e30, le=1e30, ne=0.5)\n\tB int8\n" +
			"\t//kama:validate cmp(gt=300)\n\tC uint8\n}\n",
	})["a.kama_validate.go"]
	fn := gentest.Funcs(t, src)["A.Validate"]

	assertContains(
		t,
		fn,
		`kamaValidateAll(stopOnFirstError, kamaValidateCmp(v.A, ">=", 2), `+
			`kamaValidateCmp(v.A, "<=", 2))`,
		check(`validator.Field(error(nil), "B")`),
		`kamaValidateNever(v.C, ">", "300")`,
	)
}

// TestBoundsBeyondType folds a bound no value of the type reaches: a
// float comparison becomes one with the greatest finite value, a length
// above every int holds always or never, and a divisor above every value
// leaves 0 the only multiple.
func TestBoundsBeyondType(t *testing.T) {
	src := generateAndBuild(t, map[string]string{
		"a.go": "package x\n\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate cmp(le=1e39, gt=-1e39)\n\tA float32\n" +
			"\t//kama:validate cmp(ge=1e39)\n\tB float32\n" +
			"\t//kama:validate length(le=18446744073709551616)\n\tC string\n" +
			"\t//kama:validate runes(ge=18446744073709551616)\n\tD string\n" +
			"\t//kama:validate multipleof(1e30)\n\tE int64\n" +
			"\t//kama:validate multipleof(1" + strings.Repeat("0", 400) + ")\n\tF float64\n}\n",
	})["a.kama_validate.go"]
	fn := gentest.Funcs(t, src)["A.Validate"]

	assertContains(
		t,
		fn,
		`kamaValidateCmp(v.A, "<=", 3.4028234663852886e+38), `+
			`kamaValidateCmp(v.A, ">=", -3.4028234663852886e+38)`,
		`kamaValidateCmp(v.B, ">", 3.4028234663852886e+38)`,
		check(`validator.Field(error(nil), "C")`),
		`kamaValidateRunes(utf8.RuneCountInString(v.D), ">", 9223372036854775807)`,
		`kamaValidateMultipleZero(v.E, "1e30")`,
		`kamaValidateMultipleZero(v.F, "1`+strings.Repeat("0", 400)+`")`,
	)
}

func TestClosedErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		is   error
		msg  string
	}{
		{
			name: "fallback of another type",
			src: "//kama:validate closed\ntype A struct {\n\tN int\n" +
				"\tE []int `json:\",embed\"`\n}\n",
			is:  ErrUnsupported,
			msg: "the embedded fallback field of type []int is no map keyed by strings",
		},
		{
			name: "arg",
			src:  "//kama:validate closed(\"x\"), hook=False\ntype A struct{ N int }\n",
			is:   ErrUnsupported,
			msg:  "closed takes 0 positional args",
		},
		{
			name: "on a field",
			src:  "//kama:validate hook=False\ntype A struct {\n\t//kama:validate closed\n\tN int\n}\n",
			is:   ErrDirective,
			msg:  "closed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"a.go": "package x\n\n" + tc.src}, testRun{})
			assert.ErrorIs(t, err, tc.is)
			assert.ErrorContains(t, err, tc.msg)
		})
	}
}

func TestMembersErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		is   error
		msgs []string
	}{
		{
			name: "fallback of another type",
			src: "//kama:validate members(ge=1)\ntype A struct {\n\tN int\n" +
				"\tE []int `json:\",embed\"`\n}\n",
			is:   ErrUnsupported,
			msgs: []string{"the embedded fallback field of type []int is no map"},
		},
		{
			name: "inlined struct",
			src: "//kama:validate members(ge=1)\ntype A struct {\n\tB\n}\n\n" +
				"type B struct{ N int }\n",
			is:   ErrUnsupported,
			msgs: []string{"field B inlines the members of an embedded struct"},
		},
		{
			name: "on a map type",
			src:  "//kama:validate members(ge=1)\ntype M map[string]int\n",
			is:   ErrDirective,
			msgs: []string{"members on type M validates a struct as a whole"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"a.go": "package x\n\n" + tc.src}, testRun{})
			assert.ErrorIs(t, err, tc.is)

			for _, msg := range tc.msgs {
				assert.ErrorContains(t, err, msg)
			}
		})
	}
}
