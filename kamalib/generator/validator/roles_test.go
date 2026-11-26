package validator

import (
	"testing"

	"gotest.tools/v3/assert"
)

// TestRoleItems applies the rules of role items to the values a field
// holds, each under its guard or in its loop and located at its value.
func TestRoleItems(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": `package x

import (
	"github.com/ngicks/option/either"
	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/tuple"
)

//kama:validate hook=False
type A struct {
	//kama:validate length(ge=1), e=length(le=64)
	S []string
	//kama:validate k=regexp(r"^[a-z]+$"), v=cmp(ge=0)
	M map[string]int
	//kama:validate t=length(le=64)
	O opt.Option[string]
	//kama:validate length(3) or null, t=length(le=64)
	E elastic.Elastic[string]
	//kama:validate l=length(ge=1), r=cmp(ge=0)
	L either.Either[string, int]
	//kama:validate ok=cmp(gt=0), err=not nil
	R either.Result[int]
	//kama:validate v1=length(ge=1)
	T tuple.Tuple2[string, int]
	//kama:validate e=g(length(ge=1), v=cmp(ge=0))
	N []map[string]int
	//kama:validate e=g(nil or not cmp(0))
	P **int
	//kama:validate cmp(ge=1)
	Q *opt.Option[int]
	//kama:validate e=g(e=cmp(ge=1))
	D [2]*int
	//kama:validate k=cmp(gt=0)
	F map[float64]bool
}
`,
	}, "a.kama_validate.go", "A")

	assertContains(
		t,
		fn,
		"for i, e := range v.S {\n\t\t"+
			check(`validator.Field(validator.Index(kamaValidateLength(len(e), "<=", 64), i), "S")`),
		"for _, k := range slices.Sorted(maps.Keys(v.M)) {\n\t\t"+
			check(`validator.Field(validator.Key(`+
				`kamaValidateRegexpString(kamaValidateRegexp_39c46b3647, k), k), "M")`),
		"for _, k := range slices.Sorted(maps.Keys(v.M)) {\n\t\te := v.M[k]\n\t\t"+
			check(`validator.Field(validator.Key(kamaValidateCmp(e, ">=", 0), k), "M")`),
		"if v.O.IsSome() {\n\t\t"+
			check(`validator.Field(kamaValidateLength(len(v.O.Value()), "<=", 64), "O")`),
		check(`validator.Field(kamaValidateOr(kamaValidateLength(len(v.E.Options()), "==", 3),`+
			` kamaValidateNull(v.E.IsNull())), "E")`),
		"for i, e := range v.E.Options() {\n\t\tif e.IsSome() {\n\t\t\t"+
			check(`validator.Field(validator.Index(`+
				`kamaValidateLength(len(e.Value()), "<=", 64), i), "E")`),
		"if v.L.IsLeft() {\n\t\t"+check(`validator.Field(validator.Field(`+
			`kamaValidateLength(len(v.L.UnwrapLeft()), ">=", 1), "left"), "L")`),
		"if v.L.IsRight() {\n\t\t"+check(`validator.Field(validator.Field(`+
			`kamaValidateCmp(v.L.UnwrapRight(), ">=", 0), "right"), "L")`),
		"if v.R.IsOk() {\n\t\t"+check(`validator.Field(validator.Field(`+
			`kamaValidateCmp(v.R.Value(), ">", 0), "ok"), "R")`),
		"if v.R.IsErr() {\n\t\t"+check(`validator.Field(validator.Field(`+
			`kamaValidateNot(kamaValidateNil(v.R.Err() == nil), "nil"), "err"), "R")`),
		check(
			`validator.Field(validator.Field(kamaValidateLength(len(v.T.V1), ">=", 1), "V1"), "T")`,
		),
		"for _, k2 := range slices.Sorted(maps.Keys(e)) {\n\t\t\te2 := e[k2]\n\t\t\t"+
			check(`validator.Field(validator.Index(validator.Key(`+
				`kamaValidateCmp(e2, ">=", 0), k2), i), "N")`),
		"if v.P != nil {\n\t\tif err := validator.Field(kamaValidateOr("+
			"kamaValidateNil((*v.P) == nil), func() error {",
		"if (*v.P) != nil {\n\t\t\t\treturn "+
			`kamaValidateNot(kamaValidateCmp((*(*v.P)), "==", 0), "cmp(0)")`,
		"if v.Q != nil {\n\t\tif (*v.Q).IsSome() {\n\t\t\t"+
			check(`validator.Field(kamaValidateCmp((*v.Q).Value(), ">=", 1), "Q")`),
		"for i, e := range v.D {\n\t\tif e != nil {\n\t\t\t"+
			check(`validator.Field(validator.Index(kamaValidateCmp((*e), ">=", 1), i), "D")`),
		"for k := range v.F {\n\t\t"+
			check(`validator.Field(validator.Key(kamaValidateCmp(k, ">", 0), k), "F")`),
	)
}

// TestRoleItemOnType applies a role item in the doc comment of a type to
// the values the type holds.
func TestRoleItemOnType(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\n//kama:validate e=runes(le=3)\ntype L []string\n",
	}, "a.kama_validate.go", "L")

	assertContains(t, fn, "for i, e := range v {\n\t\t"+
		check(`validator.Index(kamaValidateRunes(utf8.RuneCountInString(e), "<=", 3), i)`))
}

// TestRoleItemGeneric applies a role item to values of a type parameter
// with a rule every type argument takes.
func TestRoleItemGeneric(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\nimport \"github.com/ngicks/option/opt\"\n\n" +
			"//kama:validate hook=False\ntype A[T comparable] struct {\n" +
			"\t//kama:validate e=zero\n\tS []T\n\t//kama:validate t=zero\n\tO opt.Option[T]\n}\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		"for i, e := range v.S {",
		"kamaValidateZeroComparable(e)",
		"if v.O.IsSome() {",
		"kamaValidateZeroComparable(v.O.Value())",
	)
}

func TestRoleItemErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		is   error
		msgs []string
	}{
		{
			name: "role the type does not offer",
			src:  "\t//kama:validate k=cmp(ge=0)\n\tS []int\n",
			is:   ErrDirective,
			msgs: []string{"k=cmp(ge=0) on field S: []int offers no role k; its roles are e"},
		},
		{
			name: "role on a type holding nothing",
			src:  "\t//kama:validate e=cmp(ge=0)\n\tN int\n",
			is:   ErrDirective,
			msgs: []string{"int offers no role e; it holds no value a role names"},
		},
		{
			name: "role of a held value",
			src:  "\t//kama:validate e=g(v=cmp(ge=0))\n\tS []int\n",
			is:   ErrDirective,
			msgs: []string{"on field S role e: int offers no role v"},
		},
		{
			name: "rule the held value rejects",
			src:  "\t//kama:validate e=regexp(\"a\")\n\tS []int\n",
			is:   ErrUnsupported,
			msgs: []string{"on field S role e:", "int is neither a string nor a []byte type"},
		},
		{
			name: "rule in the old spelling",
			src:  "\t//kama:validate length=\">=1\"\n\tS []int\n",
			is:   ErrDirective,
			msgs: []string{"write it as a call, as in length(ge=1)"},
		},
		{
			name: "sibling rule on a held value",
			src:  "\t//kama:validate e=requires(\"b\")\n\tS []*int `json:\"a\"`\n\tB *int `json:\"b\"`\n",
			is:   ErrDirective,
			msgs: []string{`requires("b") applies to a field or the doc comment of a type only`},
		},
		{
			name: "role given twice",
			src:  "\t//kama:validate e=cmp(ge=0)\n\t//kama:validate e=cmp(le=9)\n\tS []int\n",
			is:   ErrDirective,
			msgs: []string{"validator e= given twice on field"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{
				"a.go": "package x\n\n//kama:validate hook=False\ntype A struct {\n" + tc.src + "}\n",
			}, testRun{})
			assert.ErrorIs(t, err, tc.is)

			for _, msg := range tc.msgs {
				assert.ErrorContains(t, err, msg)
			}
		})
	}
}
