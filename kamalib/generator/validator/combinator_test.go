package validator

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

// TestCombinators compiles each combinator over its operands to one call
// of a helper taking their errors, nested three deep included. Each line
// of a directive holds whole items.
func TestCombinators(t *testing.T) {
	out := generateAndBuild(t, map[string]string{
		"a.go": "package x\n\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate cmp(ge=1) and multipleof(2), const(0) or cmp(ge=10)\n\tN int\n" +
			"\t//kama:validate not regexp(\"^x\"), oneof(length(1), length(3))\n\tS string\n" +
			"\t//kama:validate cmp(le=10) if const(1) else always\n\tM int\n" +
			"\t//kama:validate cmp(ge=1) and not (const(5) or const(7)) or const(-1)\n" +
			"\tD int\n" +
			"\t//kama:validate cmp(ge=1),\n\t//kama:validate   multipleof(3)\n" +
			"\tL int\n}\n",
	})

	fn := gentest.Funcs(t, out["a.kama_validate.go"])["A.Validate"]
	assertContains(t, fn,
		check(`validator.Field(kamaValidateAll(stopOnFirstError,`+
			` kamaValidateCmp(v.N, ">=", 1), kamaValidateMultipleInt(v.N, 2, "2")), "N")`),
		check(`validator.Field(kamaValidateOr(kamaValidateConst(v.N, 0),`+
			` kamaValidateCmp(v.N, ">=", 10)), "N")`),
		check(`validator.Field(kamaValidateNot(kamaValidateRegexpString(`+
			`kamaValidateRegexp_9c5b6217da, v.S), "regexp(\"^x\")"), "S")`),
		check(`validator.Field(kamaValidateOneOf([]string{"length(1)", "length(3)"},`+
			` kamaValidateLength(len(v.S), "==", 1), kamaValidateLength(len(v.S), "==", 3)), "S")`),
		check(`validator.Field(kamaValidateIf(kamaValidateConst(v.M, 1),`+
			` kamaValidateCmp(v.M, "<=", 10), nil), "M")`),
		check(`validator.Field(kamaValidateOr(kamaValidateAll(stopOnFirstError,`+
			` kamaValidateCmp(v.D, ">=", 1), kamaValidateNot(kamaValidateOr(`+
			`kamaValidateConst(v.D, 5), kamaValidateConst(v.D, 7)),`+
			` "const(5) or const(7)")), kamaValidateConst(v.D, -1)), "D")`),
		check(`validator.Field(kamaValidateCmp(v.L, ">=", 1), "L")`),
		check(`validator.Field(kamaValidateMultipleInt(v.L, 3, "3"), "L")`),
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
// sees the same value. Operands taking different levels apply each to its
// own.
func TestCombinatorHeldValue(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\nimport \"github.com/ngicks/option/opt\"\n\n" +
			"//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate cmp(ge=10) or const(0)\n\tP *int\n" +
			"\t//kama:validate not length(0)\n\tO opt.Option[string]\n}\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		"if v.P != nil {\n\t\t"+check(`validator.Field(kamaValidateOr(`+
			`kamaValidateCmp((*v.P), ">=", 10), kamaValidateConst((*v.P), 0)), "P")`),
		"if v.O.IsSome() {\n\t\t"+check(`validator.Field(kamaValidateNot(`+
			`kamaValidateLength(len(v.O.Value()), "==", 0), "length(0)"), "O")`),
	)

	// nil applies to the pointer and cmp to the value only, so each operand
	// applies to the level taking it, and cmp holds for a nil pointer.
	fn = validateOf(t, map[string]string{
		"a.go": "package x\n\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate nil or cmp(ge=1)\n\tQ *int\n}\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn, "kamaValidateOr(kamaValidateNil(v.Q == nil), func() error {\n"+
		"\t\tif v.Q != nil {\n\t\t\treturn kamaValidateCmp((*v.Q), \">=\", 1)")

	// No level takes nil or regexp.
	_, err := generate(t, map[string]string{
		"a.go": "package x\n\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate nil or regexp(\"a\")\n\tQ *int\n}\n",
	}, testRun{})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorContains(t, err, `operand regexp("a"): *int is neither a string`)
	assert.ErrorContains(t, err, "for the value it holds: operand nil: int is not nillable")
}

// TestCombinatorOperandRegistry parses the operands through the registry
// of the run: a user validator and a validator replacing a built-in are
// operands like any other.
func TestCombinatorOperandRegistry(t *testing.T) {
	reg := ext.DefaultRegistry().Clone()
	reg.Register(multiple{})
	reg.Register(lengthOne{})

	files := map[string]string{
		"a.go": "package x\n\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate multiple(3) or not multiple(5)\n\tN int\n" +
			"\t//kama:validate length\n\tS string\n}\n",
	}

	f, opts := stage(t, files)
	out, err := generateInto(t, f, opts, Options{Registry: reg})
	assert.NilError(t, err)
	assert.NilError(t, gentest.Build(f, opts))

	assertContains(t, out["a.kama_validate.go"],
		check(`validator.Field(kamaValidateOr(kamaValidateMultiple(int64(v.N), 3),`+
			` kamaValidateNot(kamaValidateMultiple(int64(v.N), 5), "multiple(5)")), "N")`),
		check(`validator.Field(kamaValidateLengthOne(len(v.S)), "S")`),
	)

	_, err = generate(t, files, testRun{})
	assert.ErrorIs(t, err, ErrDirective)
	assert.ErrorIs(t, err, ext.ErrUnknown)
	assert.ErrorContains(
		t,
		err,
		"//kama:validate multiple(3) or not multiple(5) on field N: operand multiple(3):"+
			" unknown validator multiple; known: always,",
	)
	assert.ErrorContains(t, err, "//kama:validate length on field S: want one or more comparisons")
}

// TestCombinatorSiblings binds a combinator holding requires to the
// siblings of the field. The combinator applies to the field itself. Each
// error is located at the field, except those requires puts at a sibling.
func TestCombinatorSiblings(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate requires(\"b\") if const(\"x\") else requires(\"c\") and length(ge=2)\n" +
			"\tA string `json:\"a\"`\n" +
			"\t//kama:validate not requires(\"a\")\n\tB *int `json:\"b\"`\n" +
			"\tC *int `json:\"c\"`\n}\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		check(`kamaValidateIf(validator.Field(kamaValidateConst(v.A, "x"), "a"),`+
			` kamaValidateRequires(stopOnFirstError, "a", kamaValidateNonZero(v.A),`+
			` []string{"b"}, v.B != nil),`+
			` kamaValidateAll(stopOnFirstError, kamaValidateRequires(stopOnFirstError, "a",`+
			` kamaValidateNonZero(v.A), []string{"c"}, v.C != nil),`+
			` validator.Field(kamaValidateLength(len(v.A), ">=", 2), "a")))`),
		check(`validator.Field(kamaValidateNot(kamaValidateRequires(stopOnFirstError, "b",`+
			` v.B != nil, []string{"a"}, kamaValidateNonZero(v.A)), "requires(\"a\")"), "b")`),
	)
	assertNotContains(t, fn, "if v.B != nil {")
}

// TestCombinatorSiblingsHeld applies an operand other than requires of a
// combinator bound to the siblings to the value a pointer holds, under a
// guard of its own.
func TestCombinatorSiblingsHeld(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate requires(\"b\") if cmp(ge=1) else always\n\tA *int `json:\"a\"`\n" +
			"\tB *int `json:\"b\"`\n}\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		"kamaValidateIf(validator.Field(func() error {",
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
			"//kama:validate hook=False, members(0) or members(ge=2)\ntype A struct {\n" +
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
			src: "//kama:validate hook=False\ntype A struct {\n" +
				"\t//kama:validate not members(ge=1)\n\tA int\n}\n",
			is: []error{ErrDirective},
			msgs: []string{
				"//kama:validate not members(ge=1) on field A validates a struct as a whole;" +
					" write it in the doc comment of a struct type",
			},
		},
		{
			name: "requires on a type",
			src:  "//kama:validate requires(\"a\") or cmp(ge=1)\ntype N int\n",
			is:   []error{ErrDirective},
			msgs: []string{
				`//kama:validate requires("a") or cmp(ge=1) on type N reads the siblings` +
					" of a struct field",
			},
		},
		{
			name: "plain combinator on a struct type",
			src:  "//kama:validate hook=False, required and zero\ntype A struct{ A int }\n",
			is:   []error{ErrDirective},
			msgs: []string{
				"//kama:validate required and zero on struct type A; a struct type takes only" +
					" validators of the struct as a whole",
			},
		},
		{
			name: "requires and members",
			src: "//kama:validate hook=False\ntype A struct {\n" +
				"\t//kama:validate requires(\"b\") and members(ge=1)\n\tA int `json:\"a\"`\n" +
				"\tB int `json:\"b\"`\n}\n",
			is:   []error{ErrUnsupported, ext.ErrArgs},
			msgs: []string{"no place takes both"},
		},
		{
			name: "operand on no held value with requires",
			src: "//kama:validate hook=False\ntype A struct {\n" +
				"\t//kama:validate requires(\"b\") if cmp(ge=1) else always\n\tA []int `json:\"a\"`\n" +
				"\tB int `json:\"b\"`\n}\n",
			is: []error{ErrUnsupported},
			msgs: []string{
				`//kama:validate requires("b") if cmp(ge=1) else always on field A`,
				`operand cmp(ge=1): []int is not an integer, float, string or bool type`,
			},
		},
		{
			name: "literal operand",
			src: "//kama:validate hook=False\ntype A struct {\n" +
				"\t//kama:validate oneof(zero, 1)\n\tA *int\n}\n",
			is: []error{ErrUnsupported, ext.ErrArgs},
			msgs: []string{
				"//kama:validate oneof on field A: validator: invalid args: oneof: operands takes rules",
			},
		},
		{
			name: "grammar",
			src: "//kama:validate hook=False\ntype A struct {\n" +
				"\t//kama:validate zero nil\n\tA *int\n}\n",
			is:   []error{ErrDirective},
			msgs: []string{"got identifier, want ','"},
		},
		{
			name: "combinator twice",
			src: "//kama:validate hook=False\ntype A struct {\n" +
				"\t//kama:validate not zero, not zero\n\tA *int\n}\n",
			is:   []error{ErrDirective},
			msgs: []string{"validator not zero given twice on field"},
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
