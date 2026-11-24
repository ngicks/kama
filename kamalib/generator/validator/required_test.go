package validator

import (
	"context"
	"errors"
	"fmt"
	"go/types"
	"slices"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"

	"github.com/ngicks/kama/kamalib/generator/validator/rule"
)

func TestRequired(t *testing.T) {
	out := generateAndBuild(t, map[string]string{
		"a.go": `package x

import (
	"time"

	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

//kama:validate
//kama:validate hook=off
type A[T comparable] struct {
	//kama:validate required
	S string
	//kama:validate required
	N int
	//kama:validate required
	C Comparable
	//kama:validate required
	P *int
	//kama:validate required
	L []int
	//kama:validate required
	M map[string]int
	//kama:validate required
	Ch chan int
	//kama:validate required
	F func()
	//kama:validate required
	I any
	//kama:validate required
	Time time.Time
	//kama:validate required
	O opt.Option[[]int]
	//kama:validate required
	U und.Und[int]
	//kama:validate required
	E elastic.Elastic[int]
	//kama:validate required
	T T
}

type Comparable struct{ X, Y int }
`,
	})
	fn := gentest.Funcs(t, out["a.kama_validate.go"])["A.Validate"]

	assertContains(t, fn,
		`requiredComparable(v.S)`,
		`requiredComparable(v.N)`,
		`requiredComparable(v.C)`,
		`kamaValidateRequired(v.P != nil, "nil")`,
		`kamaValidateRequired(v.L != nil, "nil")`,
		`kamaValidateRequired(v.M != nil, "nil")`,
		`kamaValidateRequired(v.Ch != nil, "nil")`,
		`kamaValidateRequired(v.F != nil, "nil")`,
		`kamaValidateRequired(v.I != nil, "nil")`,
		`kamaValidateRequired(!v.Time.IsZero(), "the zero value")`,
		`kamaValidateRequired(v.O.IsSome(), "none")`,
		`kamaValidateRequired(!v.U.IsUndefined(), "undefined")`,
		`kamaValidateRequired(!v.E.IsUndefined(), "undefined")`,
		`requiredComparable(v.T)`,
	)
	assertContains(t, out[HelpersFile],
		"func requiredComparable[T comparable](t T) error {",
		"func kamaValidateRequired(ok bool, got string) error {",
		"//kama:generated kama:validate",
	)
}

func TestRequiredErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		is   error
		msgs []string
	}{
		{
			name: "not comparable",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct {\n" +
				"\t//kama:validate required\n\tS struct{ L []int }\n}\n",
			is: ErrUnsupported,
			msgs: []string{
				"a.go:6:2", "//kama:validate required on field S",
				"struct{L []int} is not an option type, not nillable",
			},
		},
		{
			name: "IsZero promoted through a pointer",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct {\n" +
				"\t//kama:validate required\n\tS struct{ *Z; L []int }\n}\n\n" +
				"type Z struct{}\n\nfunc (Z) IsZero() bool { return true }\n",
			is: ErrUnsupported,
			msgs: []string{
				"has no method IsZero() bool declared on it or promoted" +
					" without a pointer or an interface, and is not comparable",
			},
		},
		{
			name: "type parameter not comparable",
			src: "//kama:validate\n//kama:validate hook=off\ntype A[T any] struct {\n" +
				"\t//kama:validate required\n\tV T\n}\n",
			is:   ErrUnsupported,
			msgs: []string{"on field V", "T is not an option type"},
		},
		{
			name: "args",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct {\n" +
				"\t//kama:validate required=yes\n\tN int\n}\n",
			is:   ErrUnsupported,
			msgs: []string{"int is no raw JSON text, which the member names of required apply to"},
		},
		{
			name: "unknown validator",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct {\n" +
				"\t//kama:validate requird\n\tN int\n}\n",
			is: ErrDirective,
			msgs: []string{
				"unknown validator //kama:validate requird on field N;" +
					" known: and, closed, cmp, const, contains, decode, format, if, items, json, keys," +
					" kind, length, members, multipleof, nil, not, null, oneof, or, prefix, props," +
					" ref, regexp, required, requires, runes, unevalitems, unevalprops, unique, zero",
			},
		},
		{
			name: "helper name taken",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct {\n" +
				"\t//kama:validate required\n\tN int\n}\n\nfunc requiredComparable() {}\n",
			is:   ErrConflict,
			msgs: []string{"requiredComparable is already declared for a helper"},
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

// TestHelperFileForeignDecl rejects a declaration the generator did not
// write in a helpers file, which the next run would overwrite.
func TestHelperFileForeignDecl(t *testing.T) {
	_, err := generate(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=off\ntype A struct {\n" +
			"\t//kama:validate required\n\tN int\n}\n",
		HelpersFile: "package x\n\nvar mine = 1\n",
	}, testRun{})
	assert.ErrorIs(t, err, ErrConflict)
	assert.ErrorContains(t, err, HelpersFile+" holds a declaration the generator did not write")
}

// TestHelpersOnlyWhenUsed leaves out the helpers file of a package whose
// Validate methods call no helper.
func TestHelpersOnlyWhenUsed(t *testing.T) {
	out := generateAndBuild(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=off\ntype A struct{ I I }\n\n" +
			"//kama:validate\n//kama:validate hook=off\ntype I struct{ N int }\n",
	})
	_, ok := out[HelpersFile]
	assert.Assert(t, !ok)
}

// TestHelpersOfConstrainedFiles declares a helper only a constrained file
// calls in a helpers file of the same constraint.
func TestHelpersOfConstrainedFiles(t *testing.T) {
	out := generateAndBuild(t, map[string]string{
		"a_linux.go": "package x\n\n//kama:validate\n//kama:validate hook=off\ntype A struct {\n" +
			"\t//kama:validate required\n\tN int\n}\n",
		"b.go": "package x\n\n//kama:validate\n//kama:validate hook=off\ntype B struct {\n" +
			"\t//kama:validate required\n\tP *int\n}\n",
	})

	assertContains(t, out["zz_gen.helper.a.kama_validate_linux.go"], "func requiredComparable[")
	assertContains(t, out[HelpersFile], "func kamaValidateRequired(")
	assertNotContains(t, out[HelpersFile], "requiredComparable")
}

// positive is a validator of the test: an integer is greater than zero.
type positive struct{}

func (positive) Name() string { return "positive" }

func (positive) Parse(string, rule.Resolver) (rule.Rule, error) { return positive{}, nil }

func (positive) Check(t types.Type) error {
	if b, ok := t.Underlying().(*types.Basic); ok && b.Info()&types.IsInteger != 0 {
		return nil
	}

	return fmt.Errorf("%s is not an integer", t)
}

// Emit declares a package-level variable besides the helper function, as a
// validator compiling a pattern once does.
func (positive) Emit(e rule.Emitter, _ types.Type, v rule.Gen) (rule.Gen, error) {
	e.Declare("kamaValidateNotPositive", func(imp rule.Importer) rule.Step {
		return lang.Stmt(
			`var kamaValidateNotPositive = %s.New("not positive")`,
			imp.Import("errors"),
		)
	})
	e.Declare("kamaValidatePositive", func(rule.Importer) rule.Step {
		return lang.Func(
			lang.Expr("func kamaValidatePositive(%s int64) error", decl("n")),
			lang.If(
				lang.Expr("%s <= 0", lang.Name("n")),
				lang.Stmt("return kamaValidateNotPositive"),
				nil,
			),
			lang.Stmt("return nil"),
		)
	})

	return lang.Expr("kamaValidatePositive(int64(%s))", v), nil
}

// TestRegistry adds a validator, which applies to the value a pointer or an
// option holds when it cannot apply to the field itself.
func TestRegistry(t *testing.T) {
	reg := rule.DefaultRegistry().Clone()
	reg.Register(positive{})
	assert.Assert(t, slices.Contains(reg.Names(), "positive"))
	assert.Assert(t, !slices.Contains(rule.DefaultRegistry().Names(), "positive"))

	files := map[string]string{
		"a.go": `package x

import (
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

//kama:validate
//kama:validate hook=off
type A struct {
	//kama:validate positive
	N int
	//kama:validate required
	//kama:validate positive
	P *int
	//kama:validate positive
	O opt.Option[int]
	//kama:validate positive
	U und.Und[*int]
}
`,
	}

	f, opts := stage(t, files)
	out, err := generateInto(t, f, opts, Options{Registry: reg})
	assert.NilError(t, err)

	fn := gentest.Funcs(t, out["a.kama_validate.go"])["A.Validate"]
	assertContains(t, fn,
		check(`validation.Field(kamaValidatePositive(int64(v.N)), "N")`),
		check(`validation.Field(kamaValidateRequired(v.P != nil, "nil"), "P")`),
		"if v.P != nil {",
		check(`validation.Field(kamaValidatePositive(int64((*v.P))), "P")`),
		"if v.O.IsSome() {",
		check(`validation.Field(kamaValidatePositive(int64(v.O.Value())), "O")`),
		"if v.U.IsDefined() {",
		"if v.U.Value() != nil {",
	)
	assertContains(
		t,
		out[HelpersFile],
		"\t\"errors\"\n",
		"func kamaValidatePositive(n int64) error {",
		"// kamaValidateNotPositive is a helper of the generated Validate methods.\n//\n"+
			"//kama:generated kama:validate\nvar kamaValidateNotPositive = errors.New(",
	)

	// The marked variable is the generator's own on the next run.
	again, err := generateInto(t, f, opts, Options{Registry: reg})
	assert.NilError(t, err)
	assert.DeepEqual(t, again, out)

	_, err = gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)

	_, err = generate(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=off\ntype A struct {\n" +
			"\t//kama:validate positive\n\tS *string\n}\n",
	}, testRun{Registry: reg})
	assert.ErrorIs(t, err, ErrUnsupported)
	// The error names the type of the field.
	assert.ErrorContains(t, err, "*string is not an integer")
	assert.Assert(t, !errors.Is(err, ErrDirective))
}

// renamed is positive registered under another name.
type renamed struct {
	positive
	name string
}

func (r renamed) Name() string { return r.name }

// TestReservedNames rejects a registry holding a validator named by a word
// of //kama:validate that names no validator.
func TestReservedNames(t *testing.T) {
	for _, name := range []string{"hook", "ignore", "file", "package"} {
		t.Run(name, func(t *testing.T) {
			reg := rule.DefaultRegistry().Clone()
			reg.Register(renamed{name: name})

			_, err := generate(t, map[string]string{
				"a.go": "package x\n\n//kama:validate hook=off\ntype A struct{ N int }\n",
			}, testRun{Registry: reg})
			assert.ErrorIs(t, err, ErrConflict)
			assert.ErrorContains(t, err, "validators registered as "+name+";")
		})
	}
}
