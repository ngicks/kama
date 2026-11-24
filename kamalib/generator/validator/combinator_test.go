package validator

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/generator/validator/rule"
)

// TestCombinators compiles each combinator over its operands to one call
// of a helper taking their errors, nested three deep included. A call may
// span directive lines.
func TestCombinators(t *testing.T) {
	out := generateAndBuild(t, map[string]string{
		"a.go": "package x\n\n//kama:validate hook=off\ntype A struct {\n" +
			"\t//kama:validate and(cmp=\">=1\", multipleof=2) or(const=\"0\", cmp=\">=10\")\n\tN int\n" +
			"\t//kama:validate not(regexp=\"^x\") oneof(length=\"==1\", length=\"==3\")\n\tS string\n" +
			"\t//kama:validate if(const=\"1\", cmp=\"<=10\", and())\n\tM int\n" +
			"\t//kama:validate or(and(cmp=\">=1\", not(or(const=\"5\", const=\"7\"))), const=\"-1\")\n" +
			"\tD int\n" +
			"\t//kama:validate and(\n\t//kama:validate   cmp=\">=1\",\n\t//kama:validate cmp=\"<=9\")\n" +
			"\tL int\n}\n",
	})

	fn := gentest.Funcs(t, out["a.kama_validate.go"])["A.Validate"]
	assertContains(t, fn,
		check(`validation.Field(kamaValidateAll(stopOnFirstError,`+
			` kamaValidateCmp(v.N, ">=", 1), kamaValidateMultipleInt(v.N, 2, "2")), "N")`),
		check(`validation.Field(kamaValidateOr(kamaValidateConst(v.N, 0),`+
			` kamaValidateCmp(v.N, ">=", 10)), "N")`),
		check(`validation.Field(kamaValidateNot(kamaValidateRegexpString(`+
			`kamaValidateRegexp_9c5b6217da, v.S), "regexp=\"^x\""), "S")`),
		check(`validation.Field(kamaValidateOneOf([]string{"length=\"==1\"", "length=\"==3\""},`+
			` kamaValidateLength(len(v.S), "==", 1), kamaValidateLength(len(v.S), "==", 3)), "S")`),
		check(`validation.Field(kamaValidateIf(kamaValidateConst(v.M, 1),`+
			` kamaValidateCmp(v.M, "<=", 10), error(nil)), "M")`),
		check(`validation.Field(kamaValidateOr(kamaValidateAll(stopOnFirstError,`+
			` kamaValidateCmp(v.D, ">=", 1), kamaValidateNot(kamaValidateOr(`+
			`kamaValidateConst(v.D, 5), kamaValidateConst(v.D, 7)),`+
			` "or(const=\"5\", const=\"7\")")), kamaValidateConst(v.D, -1)), "D")`),
		check(`validation.Field(kamaValidateAll(stopOnFirstError,`+
			` kamaValidateCmp(v.L, ">=", 1), kamaValidateCmp(v.L, "<=", 9)), "L")`),
	)
	assertContains(t, out[HelpersFile],
		"func kamaValidateOr(errs ...error) error {",
		"func kamaValidateNot(err error, operand string) error {",
		"func kamaValidateOneOf(operands []string, errs ...error) error {",
		"func kamaValidateIf(cond, then, otherwise error) error {",
	)
}

// TestCombinatorHeldValue applies a combinator none of whose operands takes
// a pointer or an option to the value it holds, as a whole: every operand
// sees the same value.
func TestCombinatorHeldValue(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\nimport \"github.com/ngicks/option/opt\"\n\n" +
			"//kama:validate hook=off\ntype A struct {\n" +
			"\t//kama:validate or(cmp=\">=10\", const=\"0\")\n\tP *int\n" +
			"\t//kama:validate not(length=\"==0\")\n\tO opt.Option[string]\n}\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		"if v.P != nil {\n\t\t"+check(`validation.Field(kamaValidateOr(`+
			`kamaValidateCmp((*v.P), ">=", 10), kamaValidateConst((*v.P), 0)), "P")`),
		"if v.O.IsSome() {\n\t\t"+check(`validation.Field(kamaValidateNot(`+
			`kamaValidateLength(len(v.O.Value()), "==", 0), "length=\"==0\""), "O")`),
	)

	// nil applies to the pointer and cmp to the value only, so the
	// combinator applies to neither.
	_, err := generate(t, map[string]string{
		"a.go": "package x\n\n//kama:validate hook=off\ntype A struct {\n" +
			"\t//kama:validate or(nil, cmp=\">=1\")\n\tQ *int\n}\n",
	}, testRun{})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorContains(t, err, `operand cmp=">=1": *int is not an integer, float, string`)
	assert.ErrorContains(t, err, "for the value it holds: operand nil: int is not nillable")
}

// TestCombinatorOperandRegistry parses the operands through the registry
// of the run: a user validator and a validator replacing a built-in are
// operands like any other.
func TestCombinatorOperandRegistry(t *testing.T) {
	reg := rule.DefaultRegistry().Clone()
	reg.Register(multiple{})
	reg.Register(lengthOne{})

	files := map[string]string{
		"a.go": "package x\n\n//kama:validate hook=off\ntype A struct {\n" +
			"\t//kama:validate or(multiple=3, not(multiple=5))\n\tN int\n" +
			"\t//kama:validate and(length)\n\tS string\n}\n",
	}

	f, opts := stage(t, files)
	out, err := generateInto(t, f, opts, Options{Registry: reg})
	assert.NilError(t, err)
	assert.NilError(t, gentest.Build(f, opts))

	assertContains(t, out["a.kama_validate.go"],
		check(`validation.Field(kamaValidateOr(kamaValidateMultiple(int64(v.N), 3),`+
			` kamaValidateNot(kamaValidateMultiple(int64(v.N), 5), "multiple=\"5\"")), "N")`),
		check(`validation.Field(kamaValidateLengthOne(len(v.S)), "S")`),
	)

	_, err = generate(t, files, testRun{})
	assert.ErrorIs(t, err, ErrDirective)
	assert.ErrorIs(t, err, rule.ErrUnknown)
	assert.ErrorContains(
		t,
		err,
		"//kama:validate or on field N: operand multiple=\"3\":"+
			" unknown validator multiple; known: and,",
	)
	assert.ErrorContains(t, err, "operand length: want one or more comparisons")
}

// TestCombinatorSiblings binds a combinator holding requires to the
// siblings of the field. The combinator applies to the field itself. Each
// error is located at the field, except those requires puts at a sibling.
func TestCombinatorSiblings(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\n//kama:validate hook=off\ntype A struct {\n" +
			"\t//kama:validate if(const=`\"x\"`, requires=b, and(requires=c, length=\">=2\"))\n" +
			"\tA string `json:\"a\"`\n" +
			"\t//kama:validate not(requires=a)\n\tB *int `json:\"b\"`\n" +
			"\tC *int `json:\"c\"`\n}\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		check(`kamaValidateIf(validation.Field(kamaValidateConst(v.A, "x"), "a"),`+
			` kamaValidateRequires(stopOnFirstError, "a", kamaValidateNonZero(v.A),`+
			` []string{"b"}, v.B != nil),`+
			` kamaValidateAll(stopOnFirstError, kamaValidateRequires(stopOnFirstError, "a",`+
			` kamaValidateNonZero(v.A), []string{"c"}, v.C != nil),`+
			` validation.Field(kamaValidateLength(len(v.A), ">=", 2), "a")))`),
		check(`validation.Field(kamaValidateNot(kamaValidateRequires(stopOnFirstError, "b",`+
			` v.B != nil, []string{"a"}, kamaValidateNonZero(v.A)), "requires=\"a\""), "b")`),
	)
	assertNotContains(t, fn, "if v.B != nil {")
}

// TestCombinatorSiblingsHeld applies an operand other than requires of a
// combinator bound to the siblings to the value a pointer holds, under a
// guard of its own.
func TestCombinatorSiblingsHeld(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\n//kama:validate hook=off\ntype A struct {\n" +
			"\t//kama:validate if(cmp=\">=1\", requires=b)\n\tA *int `json:\"a\"`\n" +
			"\tB *int `json:\"b\"`\n}\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		"kamaValidateIf(validation.Field(func() error {",
		"if v.A != nil {",
		`return kamaValidateCmp((*v.A), ">=", 1)`,
		`kamaValidateRequires(stopOnFirstError, "a", v.A != nil, []string{"b"}, v.B != nil)`,
	)
}

// TestCombinatorMembers binds a combinator holding members to the struct
// in the doc comment of a struct type.
func TestCombinatorMembers(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\nimport \"github.com/ngicks/option/opt\"\n\n" +
			"//kama:validate hook=off or(members=\"==0\", members=\">=2\")\ntype A struct {\n" +
			"\tA opt.Option[int] `json:\"a,omitzero\"`\n\tB *int `json:\"b,omitzero\"`\n}\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn, check(`kamaValidateOr(`+
		`kamaValidateMembers(kamaValidateCount(v.A.IsSome(), v.B != nil), "==", 0),`+
		` kamaValidateMembers(kamaValidateCount(v.A.IsSome(), v.B != nil), ">=", 2))`))
}

func TestCombinatorErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		is   []error
		msgs []string
	}{
		{
			name: "members on a field",
			src: "//kama:validate hook=off\ntype A struct {\n" +
				"\t//kama:validate not(members=\">=1\")\n\tA int\n}\n",
			is: []error{ErrDirective},
			msgs: []string{
				"//kama:validate not on field A validates a struct as a whole;" +
					" write it in the doc comment of a struct type",
			},
		},
		{
			name: "requires on a type",
			src:  "//kama:validate or(requires=a, cmp=\">=1\")\ntype N int\n",
			is:   []error{ErrDirective},
			msgs: []string{"//kama:validate or on type N reads the siblings of a struct field"},
		},
		{
			name: "plain combinator on a struct type",
			src:  "//kama:validate hook=off and(required)\ntype A struct{ A int }\n",
			is:   []error{ErrDirective},
			msgs: []string{"//kama:validate and on struct type A; a struct type takes only" +
				" validators of the struct as a whole"},
		},
		{
			name: "requires and members",
			src: "//kama:validate hook=off\ntype A struct {\n" +
				"\t//kama:validate and(requires=b, members=\">=1\")\n\tA int `json:\"a\"`\n" +
				"\tB int `json:\"b\"`\n}\n",
			is:   []error{ErrUnsupported, rule.ErrArgs},
			msgs: []string{"no place takes both"},
		},
		{
			name: "operand on no held value with requires",
			src: "//kama:validate hook=off\ntype A struct {\n" +
				"\t//kama:validate if(cmp=\">=1\", requires=b)\n\tA []int `json:\"a\"`\n" +
				"\tB int `json:\"b\"`\n}\n",
			is: []error{ErrUnsupported},
			msgs: []string{
				"//kama:validate if on field A",
				`operand cmp=">=1": []int is not an integer, float, string or bool type`,
			},
		},
		{
			name: "arity",
			src: "//kama:validate hook=off\ntype A struct {\n" +
				"\t//kama:validate not(zero, nil)\n\tA *int\n}\n",
			is:   []error{ErrUnsupported, rule.ErrArgs},
			msgs: []string{"//kama:validate not on field A: want one operand, got 2"},
		},
		{
			name: "grammar",
			src: "//kama:validate hook=off\ntype A struct {\n" +
				"\t//kama:validate or(zero nil)\n\tA *int\n}\n",
			is:   []error{ErrDirective},
			msgs: []string{`want , or ) after zero, got "nil)"`},
		},
		{
			name: "combinator twice",
			src: "//kama:validate hook=off\ntype A struct {\n" +
				"\t//kama:validate not(zero) not(nil)\n\tA *int\n}\n",
			is:   []error{ErrDirective},
			msgs: []string{"validator not given twice on field"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"a.go": "package x\n\n" + tc.src}, testRun{})
			for _, is := range tc.is {
				assert.ErrorIs(t, err, is)
			}

			for _, msg := range tc.msgs {
				assert.ErrorContains(t, err, msg)
			}
		})
	}
}
