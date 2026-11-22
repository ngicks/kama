package validator

import (
	"maps"
	"slices"
	"testing"

	"gotest.tools/v3/assert"
)

// validated returns the types the files generated from files declare
// Validate for, sorted, failing unless the output builds.
func validated(t *testing.T, files map[string]string) []string {
	t.Helper()

	var names []string

	for name, src := range generateAndBuild(t, files) {
		if name == HelpersFile {
			continue
		}

		for fn := range maps.Keys(funcs(t, src)) {
			names = append(names, fn)
		}
	}

	slices.Sort(names)

	return names
}

func TestSelection(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			// A marked parent does not make its field types targets.
			name: "marked parent",
			src: "//kama:validate\n//kama:validate:hook off\ntype P struct{ C C }\n\n" +
				"type C struct{ X int }\n",
			want: []string{"P.Validate"},
		},
		{
			// A marked child makes the types holding it dependants.
			name: "marked child",
			src: "type P struct{ C *C }\n\ntype PP struct{ P []P }\n\n" +
				"type M map[string]C\n\n" +
				"//kama:validate\n//kama:validate:hook off\ntype C struct{ X int }\n",
			want: []string{"C.Validate", "M.Validate", "P.Validate", "PP.Validate"},
		},
		{
			// A map key, a channel, a function and an array type hold no value
			// Validate reaches.
			name: "not followed",
			src: "type K map[C]int\n\ntype Ch struct{ C chan C }\n\n" +
				"type F struct{ F func(C) }\n\ntype A [2]C\n\ntype AA struct{ A A }\n\n" +
				"//kama:validate\n//kama:validate:hook off\ntype C struct{ X int }\n",
			want: []string{"C.Validate"},
		},
		{
			name: "ignored dependant",
			src: "//kama:validate ignore\ntype P struct{ C C }\n\ntype PP struct{ P P }\n\n" +
				"//kama:validate\n//kama:validate:hook off\ntype C struct{ X int }\n",
			want: []string{"C.Validate"},
		},
		{
			// A file mark skips types of other kinds and ignored types.
			name: "file mark",
			src: "//kama:validate file\n\n//kama:validate:hook off\ntype A struct{ N int }\n\n" +
				"type L []int\n\ntype ID int\n\n//kama:validate ignore\ntype I struct{}\n",
			want: []string{"A.Validate", "L.Validate"},
		},
		{
			name: "package mark",
			src: "//kama:validate package\n\n//kama:validate:hook off\ntype A struct{ N int }\n\n" +
				"type M map[string]int\n\ntype F func()\n",
			want: []string{"A.Validate", "M.Validate"},
		},
		{
			// A hand-written implementor gets no Validate but makes its holders
			// dependants, which call it.
			name: "implementor",
			src: "type P struct{ H H }\n\ntype H struct{}\n\n" +
				"func (H) Validate(bool) error { return nil }\n",
			want: []string{"P.Validate"},
		},
		{
			// A Validate promoted from an embedded target is no implementor.
			name: "promoted",
			src: "type Outer struct{ Inner }\n\n" +
				"//kama:validate\n//kama:validate:hook off\ntype Inner struct{ X int }\n",
			want: []string{"Inner.Validate", "Outer.Validate"},
		},
		{
			// A type group mark marks every type of the group.
			name: "type group",
			src: "//kama:validate\n//kama:validate:hook off\ntype (\n" +
				"\tA struct{ N int }\n\tB struct{ N int }\n)\n",
			want: []string{"A.Validate", "B.Validate"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.DeepEqual(
				t,
				validated(t, map[string]string{"a.go": "package x\n\n" + tc.src}),
				tc.want,
			)
		})
	}
}

func TestSelectionImplementorCalled(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\ntype P struct{ H H }\n\ntype H struct{}\n\n" +
			"func (H) Validate(bool) error { return nil }\n",
	}, "a.kama_validate.go", "P")

	assertContains(t, fn, check(`validation.Field(v.H.Validate(stopOnFirstError), "H")`))
}

func TestSelectionErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		is   error
		msgs []string
	}{
		{
			name: "marked basic type",
			src:  "//kama:validate\ntype ID int\n",
			is:   ErrUnsupported,
			msgs: []string{"a.go:3:1", "ID has underlying type int; want a struct, slice or map"},
		},
		{
			name: "marked alias",
			src:  "//kama:validate\ntype A = B\n\ntype B struct{}\n",
			is:   ErrUnsupported,
			msgs: []string{"A is an alias"},
		},
		{
			name: "marked implementor",
			src:  "//kama:validate\ntype H struct{}\n\nfunc (H) Validate(bool) error { return nil }\n",
			is:   ErrConflict,
			msgs: []string{"H declares Validate itself"},
		},
		{
			name: "marked and ignored",
			src: "//kama:validate\ntype (\n\t//kama:validate ignore\n\tA struct{}\n" +
				"\tB struct{}\n)\n",
			is:   ErrDirective,
			msgs: []string{"A is both marked and ignored"},
		},
		{
			name: "validator on a type without Validate",
			src:  "type A struct {\n\t//kama:validate:required\n\tN int\n}\n",
			is:   ErrDirective,
			msgs: []string{"a.go:4:2", "//kama:validate:required on field", "A.N", "mark the type"},
		},
		{
			name: "hook on a type without Validate",
			src:  "//kama:validate:hook on\ntype A struct{ N int }\n",
			is:   ErrDirective,
			msgs: []string{"//kama:validate:hook on", "gets no Validate"},
		},
		{
			name: "field hook on a type without Validate",
			src:  "type A struct {\n\t//kama:validate:hook on\n\tN int\n}\n",
			is:   ErrDirective,
			msgs: []string{"//kama:validate:hook on field", "A.N"},
		},
		{
			// A dependant with a Validate of another shape would get a second
			// one.
			name: "Validate of another shape",
			src: "type P struct{ C C }\n\nfunc (P) Validate() error { return nil }\n\n" +
				"//kama:validate\n//kama:validate:hook off\ntype C struct{ X int }\n",
			is: ErrConflict,
			msgs: []string{
				"P declares Validate on its value receiver with signature func() error;" +
					" want func(bool) error on the value receiver",
			},
		},
		{
			name: "Validate on the pointer receiver",
			src: "type P struct{ C C }\n\nfunc (*P) Validate(bool) error { return nil }\n\n" +
				"//kama:validate\n//kama:validate:hook off\ntype C struct{ X int }\n",
			is:   ErrConflict,
			msgs: []string{"P declares Validate on its pointer receiver"},
		},
		{
			name: "field named Validate",
			src:  "//kama:validate\n//kama:validate:hook off\ntype A struct{ Validate int }\n",
			is:   ErrConflict,
			msgs: []string{"field Validate of"},
		},
		{
			name: "type parameter named like a predeclared identifier",
			src:  "//kama:validate\n//kama:validate:hook off\ntype A[error any] struct{ V error }\n",
			is:   ErrConflict,
			msgs: []string{"type parameter error"},
		},
		{
			name: "directive on a field",
			src:  "//kama:validate\ntype A struct {\n\t//kama:validate\n\tN int\n}\n",
			is:   ErrDirective,
			msgs: []string{"mark the type instead"},
		},
		{
			name: "validator on a type",
			src:  "//kama:validate:required\ntype A struct{ N int }\n",
			is:   ErrDirective,
			msgs: []string{"validators apply to struct fields only"},
		},
		{
			name: "bad hook args",
			src:  "//kama:validate\n//kama:validate:hook maybe\ntype A struct{ N int }\n",
			is:   ErrDirective,
			msgs: []string{"want on or off"},
		},
		{
			name: "bad free-floating args",
			src:  "//kama:validate everything\n\ntype A struct{ N int }\n",
			is:   ErrDirective,
			msgs: []string{"want file or package"},
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

// TestSelectionOptionTypes makes a type holding a target as the type
// argument of an option type a dependant. The type argument of any other
// generic type, such as Box, does not: Box[T] gets validated only when Box
// is a target itself, and its Validate then takes the function for T.
func TestSelectionOptionTypes(t *testing.T) {
	got := validated(t, map[string]string{
		"a.go": `package x

import (
	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

type U struct{ T und.Und[T] }

type O struct{ T opt.Option[*T] }

type S struct{ T []und.Und[T] }

type E struct{ T elastic.Elastic[T] }

type M map[string]opt.Option[T]

type Nested struct{ T opt.Option[und.Und[[]T]] }

type ThroughDependant struct{ U opt.Option[U] }

type B struct{ T Box[T] }

type Box[X any] struct{ X X }

type K map[opt.Option[T]]int

//kama:validate
//kama:validate:hook off
type T struct{ X int }
`,
	})

	assert.DeepEqual(t, got, []string{
		"E.Validate", "M.Validate", "Nested.Validate", "O.Validate", "S.Validate",
		"T.Validate", "ThroughDependant.Validate", "U.Validate",
	})
}

// TestSelectionOptionForeignImplementor makes a type holding an
// implementor of another package in an option type a dependant.
func TestSelectionOptionForeignImplementor(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\nimport (\n\t\"github.com/ngicks/option/opt\"\n\n\t\"" +
			modulePath(t) + "/sub\"\n)\n\ntype A struct{ H opt.Option[sub.H] }\n",
		"sub/sub.go": "package sub\n\ntype H struct{}\n\n" +
			"func (H) Validate(bool) error { return nil }\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn, "if v.H.IsSome() {",
		check(`validation.Field(v.H.Value().Validate(stopOnFirstError), "H")`))
}
