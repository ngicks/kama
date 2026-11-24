package validator

import (
	"fmt"
	"go/types"
	"strconv"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/generator/validator/rule"
)

func TestTypeValidators(t *testing.T) {
	src := generateAndBuild(t, map[string]string{
		"a.go": `package x

import (
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

//kama:validate cmp=">=3"
type Level int

//kama:validate length=">=1" unique
type Tags []string

//kama:validate length="<=2"
type Inners map[string]Inner

//kama:validate unique
type Pair [2]Inner

// Mode has no fields, so hook=on calls no hook.
//
//kama:validate hook=on const="1 2"
type Mode int

//kama:validate hook=off
type Inner struct {
	//kama:validate required
	N int
}

//kama:validate hook=off
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
	//kama:validate cmp="<=9"
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
			strings.Index(inners, check(`validation.Key(e.Validate(stopOnFirstError), k)`)),
		inners,
	)

	pair := fs["Pair.Validate"]
	assert.Assert(t,
		strings.Index(pair, check(`kamaValidateUnique(kamaValidateCopy(v)[:])`)) <
			strings.Index(pair, check(`validation.Index(e.Validate(stopOnFirstError), i)`)),
		pair,
	)

	assertContains(t, fs["Holder.Validate"],
		check(`validation.Field(v.L.Validate(stopOnFirstError), "L")`),
		"if v.P != nil {",
		check(`validation.Field((*v.P).Validate(stopOnFirstError), "P")`),
		check(`validation.Field(v.O.Value().Validate(stopOnFirstError), "O")`),
		check(`validation.Field(v.U.Value().Validate(stopOnFirstError), "U")`),
		check(`validation.Field(validation.Index(e.Validate(stopOnFirstError), i), "S")`),
		check(`validation.Field(validation.Key(e.Validate(stopOnFirstError), k), "M")`),
		"v.B.Validate(stopOnFirstError, func(v Level, stopOnFirstError bool) error {\n"+
			"\t\treturn v.Validate(stopOnFirstError)",
		check(`validation.Field(kamaValidateCmp(v.F, "<=", 9), "F")`),
		check(`validation.Field(v.F.Validate(stopOnFirstError), "F")`),
		check(`validation.Field(v.Pa.Validate(stopOnFirstError), "Pa")`),
		check(`validation.Field(validation.Index(e.Validate(stopOnFirstError), i), "H")`),
	)

	_, ok := fs["Hand.Validate"]
	assert.Assert(t, !ok, "an implementor keeps its own Validate")
}

// TestTypeValidatorsOfGroup runs the validators of a type group on each
// type of the group before those of the type.
func TestTypeValidatorsOfGroup(t *testing.T) {
	fs := gentest.Funcs(t, generateAndBuild(t, map[string]string{
		"a.go": "package x\n\n//kama:validate length=\"<=3\"\ntype (\n" +
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

// fields is a rule.StructRule of the tests. It passes the members of a
// struct and its embedded fallback to a helper.
type fields struct{}

func (fields) Name() string { return "fields" }

func (fields) Parse(string) (rule.Rule, error) { return fieldsOptions{}, nil }

type fieldsOptions struct{}

func (fieldsOptions) Check(t types.Type) error { return fmt.Errorf("%s is no struct", t) }

func (fieldsOptions) Emit(rule.Emitter, types.Type, rule.Gen) (rule.Gen, error) {
	return nil, fmt.Errorf("want a struct")
}

func (fieldsOptions) Struct(s rule.Struct) (rule.Rule, error) {
	if _, err := s.Members(); err != nil {
		return nil, err
	}

	return fieldsOf{s: s}, nil
}

// fieldsOf is fields applied to a struct, which it reads through s.
type fieldsOf struct{ s rule.Struct }

func (fieldsOf) Check(types.Type) error { return nil }

func (f fieldsOf) Emit(e rule.Emitter, _ types.Type, _ rule.Gen) (rule.Gen, error) {
	e.Declare("kamaValidateFields", func(rule.Importer) rule.Step {
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
		values []rule.Gen
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

func fieldsRegistry() *rule.Registry {
	reg := rule.DefaultRegistry().Clone()
	reg.Register(fields{})

	return reg
}

// TestStructRule applies a rule.StructRule written in the doc comment of a
// struct type to the struct value. The rule sees the members and the
// embedded fallback of the struct.
func TestStructRule(t *testing.T) {
	f, opts := stage(t, map[string]string{
		"a.go": "package x\n\n//kama:validate fields hook=off\ntype S struct {\n" +
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
			src: "//kama:validate fields hook=off\ntype S struct {\n\tIn\n\tA int\n}\n\n" +
				"type In struct{ B int }\n",
			is: ErrUnsupported,
			msgs: []string{
				"//kama:validate fields on type S",
				"field In inlines the members of an embedded struct",
			},
		},
		{
			name: "on a field",
			src: "//kama:validate hook=off\ntype S struct {\n\t//kama:validate fields\n" +
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
				Options{Registry: fieldsRegistry()})
			assert.ErrorIs(t, err, tc.is)

			for _, msg := range tc.msgs {
				assert.ErrorContains(t, err, msg)
			}
		})
	}
}

// TestMembers counts the fields of an optional state that are present, the
// other fields and the entries of the embedded fallback.
func TestMembers(t *testing.T) {
	src := generateAndBuild(t, map[string]string{
		"a.go": "package x\n\nimport \"github.com/ngicks/option/opt\"\n\n" +
			"//kama:validate members=\">=1 <=3\" hook=off\ntype A struct {\n" +
			"\tA opt.Option[int] `json:\"a\"`\n\tB *int\n\tC int\n\tD string `json:\"-\"`\n" +
			"\tE map[string]any `json:\",embed\"`\n}\n\n" +
			"//kama:validate members=\"<=1\" hook=off\ntype B struct{ N int }\n",
	})["a.kama_validate.go"]
	fs := gentest.Funcs(t, src)

	count := `kamaValidateCount(v.A.IsSome(), v.B != nil)+1+len(v.E)`
	assertContains(t, fs["A.Validate"],
		`kamaValidateMembers(`+count+`, ">=", 1)`,
		`kamaValidateMembers(`+count+`, "<=", 3)`,
	)
	assertContains(t, fs["B.Validate"], check(`kamaValidateMembers(1, "<=", 1)`))
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
			src: "//kama:validate members=\">=1\"\ntype A struct {\n\tN int\n" +
				"\tE []int `json:\",embed\"`\n}\n",
			is:   ErrUnsupported,
			msgs: []string{"the embedded fallback field of type []int is no map"},
		},
		{
			name: "inlined struct",
			src: "//kama:validate members=\">=1\"\ntype A struct {\n\tB\n}\n\n" +
				"type B struct{ N int }\n",
			is:   ErrUnsupported,
			msgs: []string{"field B inlines the members of an embedded struct"},
		},
		{
			name: "on a map type",
			src:  "//kama:validate members=\">=1\"\ntype M map[string]int\n",
			is:   ErrDirective,
			msgs: []string{"members on type M validates a struct as a whole"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"a.go": "package x\n\n" + tc.src}, Options{})
			assert.ErrorIs(t, err, tc.is)

			for _, msg := range tc.msgs {
				assert.ErrorContains(t, err, msg)
			}
		})
	}
}
