package validator

import (
	"context"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
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

		for fn := range maps.Keys(gentest.Funcs(t, src)) {
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
			src: "//kama:validate\n//kama:validate hook=off\ntype P struct{ C C }\n\n" +
				"type C struct{ X int }\n",
			want: []string{"P.Validate"},
		},
		{
			// A marked child makes the types holding it dependants.
			name: "marked child",
			src: "type P struct{ C *C }\n\ntype PP struct{ P []P }\n\n" +
				"type M map[string]C\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			want: []string{"C.Validate", "M.Validate", "P.Validate", "PP.Validate"},
		},
		{
			// A map key, a channel and a function hold no value Validate
			// reaches.
			name: "not followed",
			src: "type K map[C]int\n\ntype Ch struct{ C chan C }\n\n" +
				"type F struct{ F func(C) }\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			want: []string{"C.Validate"},
		},
		{
			// A named array or pointer type holding nothing to validate is no
			// error, a cycle of them included. One with a Validate of its own is
			// an implementor.
			name: "named array and pointer",
			src: "type N [2]int\n\ntype Cy [2]Cp\n\ntype Cp *Cy\n\n" +
				"type H [2]C\n\nfunc (H) Validate(bool) error { return nil }\n\n" +
				"//kama:validate\n//kama:validate hook=off\n" +
				"type A struct {\n\tN  N\n\tCy Cy\n\tH  H\n}\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			want: []string{"A.Validate", "C.Validate"},
		},
		{
			name: "ignored dependant",
			src: "//kama:validate ignore\ntype P struct{ C C }\n\ntype PP struct{ P P }\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			want: []string{"C.Validate"},
		},
		{
			// A file mark skips types of other kinds and ignored types.
			name: "file mark",
			src: "//kama:validate file\n\n//kama:validate hook=off\ntype A struct{ N int }\n\n" +
				"type L []int\n\ntype ID int\n\n//kama:validate ignore\ntype I struct{}\n",
			want: []string{"A.Validate", "L.Validate"},
		},
		{
			name: "package mark",
			src: "//kama:validate package\n\n//kama:validate hook=off\ntype A struct{ N int }\n\n" +
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
				"//kama:validate\n//kama:validate hook=off\ntype Inner struct{ X int }\n",
			want: []string{"Inner.Validate", "Outer.Validate"},
		},
		{
			// A type group mark marks every type of the group.
			name: "type group",
			src: "//kama:validate\n//kama:validate hook=off\ntype (\n" +
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
			src:  "type A struct {\n\t//kama:validate required\n\tN int\n}\n",
			is:   ErrDirective,
			msgs: []string{"a.go:4:2", "//kama:validate required on field", "A.N", "mark the type"},
		},
		{
			name: "hook on a type without Validate",
			src:  "//kama:validate ignore hook=on\ntype A struct{ N int }\n",
			is:   ErrDirective,
			msgs: []string{"//kama:validate hook=on on", "gets no Validate"},
		},
		{
			// hook=on alone marks the type, so its hooks are called.
			name: "hook on marks the type",
			src:  "//kama:validate hook=on\ntype A struct{ N int }\n",
			is:   ErrHook,
			msgs: []string{"A has no hook validateN"},
		},
		{
			name: "field hook on a type without Validate",
			src:  "type A struct {\n\t//kama:validate hook=on\n\tN int\n}\n",
			is:   ErrDirective,
			msgs: []string{"//kama:validate hook=on on field", "A.N"},
		},
		{
			// A dependant with a Validate of another shape would get a second
			// one.
			name: "Validate of another shape",
			src: "type P struct{ C C }\n\nfunc (P) Validate() error { return nil }\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is: ErrConflict,
			msgs: []string{
				"P declares Validate on its value receiver with signature func() error;" +
					" want func(bool) error on the value receiver",
			},
		},
		{
			name: "Validate on the pointer receiver",
			src: "type P struct{ C C }\n\nfunc (*P) Validate(bool) error { return nil }\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is:   ErrConflict,
			msgs: []string{"P declares Validate on its pointer receiver"},
		},
		{
			name: "field named Validate",
			src:  "//kama:validate\n//kama:validate hook=off\ntype A struct{ Validate int }\n",
			is:   ErrConflict,
			msgs: []string{"field Validate of"},
		},
		{
			name: "type parameter named like a predeclared identifier",
			src:  "//kama:validate\n//kama:validate hook=off\ntype A[error any] struct{ V error }\n",
			is:   ErrConflict,
			msgs: []string{"type parameter error"},
		},
		{
			name: "type parameter named like a helper",
			src: "//kama:validate\n//kama:validate hook=off\n" +
				"type A[requiredComparable comparable, kamaValidateAll any] struct {\n" +
				"\t//kama:validate required\n\tX requiredComparable\n" +
				"\t//kama:validate cmp=\">=1 <=3\"\n\tN int\n}\n",
			is: ErrConflict,
			msgs: []string{
				"type parameter requiredComparable of", "shadows the helper requiredComparable",
				"type parameter kamaValidateAll of", "shadows the helper kamaValidateAll",
			},
		},
		{
			name: "named array field",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct{ P Pair }\n\n" +
				"type Pair [2]C\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is: ErrUnsupported,
			msgs: []string{
				"a.go:5:16", "field P of", "holds", "Pair, a named array type",
				"only struct, slice and map named types get a Validate",
				"use an unnamed array or pointer type, or a wrapper struct",
			},
		},
		{
			name: "named pointer field",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct{ P []PI }\n\n" +
				"type PI *C\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is:   ErrUnsupported,
			msgs: []string{"field P of", "PI, a named pointer type"},
		},
		{
			// The holder would be a dependant through the named array.
			name: "named array in a dependant",
			src: "type Holder struct{ P Pair }\n\ntype Pair [2]C\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is:   ErrUnsupported,
			msgs: []string{"field P of", "Holder holds", "Pair, a named array type"},
		},
		{
			name: "named pointer in a slice type",
			src: "type L []PP\n\ntype PP *Pair\n\ntype Pair [2]C\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is:   ErrUnsupported,
			msgs: []string{"type ", "L holds", "PP, a named pointer type"},
		},
		{
			name: "named array as a type argument",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct{ B Box[Pair] }\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype Box[T any] struct{ V T }\n\n" +
				"type Pair [2]C\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is: ErrUnsupported,
			msgs: []string{
				"field B:", "Pair is a named array type holding values to validate",
				"only struct, slice and map named types get a Validate",
			},
		},
		{
			name: "generic named pointer of a type parameter",
			src: "//kama:validate\n//kama:validate hook=off\ntype G[T any] struct{ P PT[T] }\n\n" +
				"type PT[T any] *T\n",
			is: ErrUnsupported,
			msgs: []string{
				"a.go:5:23", "field P of", "G holds", "PT[T], a named pointer type",
				"use an unnamed array or pointer type, or a wrapper struct",
			},
		},
		{
			name: "generic named array of a type parameter",
			src: "//kama:validate\n//kama:validate hook=off\ntype G[T any] struct{ A []AT[T] }\n\n" +
				"type AT[T any] [2]T\n",
			is:   ErrUnsupported,
			msgs: []string{"field A of", "AT[T], a named array type"},
		},
		{
			name: "generic named pointer holding a type parameter",
			src: "//kama:validate\n//kama:validate hook=off\ntype G[T any] struct{ P PP[T] }\n\n" +
				"type PP[T any] *PT[T]\n\ntype PT[T any] *[]T\n",
			is:   ErrUnsupported,
			msgs: []string{"field P of", "PP[T], a named pointer type"},
		},
		{
			name: "generic named pointer of a type parameter as a type argument",
			src: "//kama:validate\n//kama:validate hook=off\n" +
				"type G[T any] struct{ B Box[PT[T]] }\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype Box[T any] struct{ V T }\n\n" +
				"type PT[T any] *T\n",
			is:   ErrUnsupported,
			msgs: []string{"field B:", "PT[T] is a named pointer type holding values to validate"},
		},
		{
			name: "generic named pointer of a target",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct{ P PT[C] }\n\n" +
				"type PT[T any] *T\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is:   ErrUnsupported,
			msgs: []string{"field P of", "C], a named pointer type"},
		},
		{
			// PT[int] holds nothing to validate. PT[C] after it does.
			name: "second instance of a generic named pointer",
			src: "//kama:validate\n//kama:validate hook=off\n" +
				"type A struct{ S struct{ I PT[int]; C PT[C] } }\n\n" +
				"type PT[T any] *T\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is:   ErrUnsupported,
			msgs: []string{"field S of", "C], a named pointer type"},
		},
		{
			name: "nested instances of a generic named pointer",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct{ P PT[PT[C]] }\n\n" +
				"type PT[T any] *T\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is:   ErrUnsupported,
			msgs: []string{"field P of", "C]], a named pointer type"},
		},
		{
			// PA and PB hold each other. Either one reaches C.
			name: "cycle of named pointers",
			src: "type H struct {\n\tX PA\n\tY PB\n}\n\n" +
				"type PA *struct {\n\tB PB\n\tC C\n}\n\ntype PB *PA\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is: ErrUnsupported,
			msgs: []string{
				"field X of",
				"PA, a named pointer type",
				"field Y of",
				"PB, a named pointer type",
			},
		},
		{
			name: "unmarked generic slice of a target",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct{ L LS[C] }\n\n" +
				"type LS[T any] []T\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is: ErrUnsupported,
			msgs: []string{
				"a.go:5:16",
				"field L of",
				"C], which holds values to validate through its type arguments",
				"LS with //kama:validate to pass validation on",
			},
		},
		{
			name: "unmarked generic map of a target",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct{ M MS[string, C] }\n\n" +
				"type MS[K comparable, V any] map[K]V\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is:   ErrUnsupported,
			msgs: []string{"field M of", "MS[string, ", "MS with //kama:validate"},
		},
		{
			name: "pointer to an unmarked generic slice of a target",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct{ P *LS[C] }\n\n" +
				"type LS[T any] []T\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is:   ErrUnsupported,
			msgs: []string{"field P of", "LS with //kama:validate"},
		},
		{
			name: "unmarked generic slice of an unmarked generic slice of a target",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct{ L LS[LS[C]] }\n\n" +
				"type LS[T any] []T\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is:   ErrUnsupported,
			msgs: []string{"field L of", "C]], which holds values to validate"},
		},
		{
			name: "unmarked generic types of a type parameter",
			src: "//kama:validate\n//kama:validate hook=off\n" +
				"type Box[T any] struct {\n\tL LS[T]\n\tW W[T]\n}\n\n" +
				"type LS[T any] []T\n\ntype W[T any] struct{ X T }\n",
			is: ErrUnsupported,
			msgs: []string{
				"field L of", "LS[T], which holds values to validate", "LS with //kama:validate",
				"field W of", "W[T], which holds values to validate", "W with //kama:validate",
			},
		},
		{
			// The holder would be a dependant if LS had a Validate.
			name: "unmarked generic slice in a dependant",
			src: "type H struct{ L LS[C] }\n\ntype LS[T any] []T\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is:   ErrUnsupported,
			msgs: []string{"field L of", "H holds", "LS with //kama:validate"},
		},
		{
			name: "unmarked generic slice in a slice type",
			src: "type L []LS[C]\n\ntype LS[T any] []T\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is:   ErrUnsupported,
			msgs: []string{"type ", "L holds", "LS with //kama:validate"},
		},
		{
			name: "unmarked generic slice as a type argument",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct{ B Box[LS[C]] }\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype Box[T any] struct{ V T }\n\n" +
				"type LS[T any] []T\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			is: ErrUnsupported,
			msgs: []string{
				"field B:", "C] holds values to validate through its type arguments",
				"LS with //kama:validate to pass validation on",
			},
		},
		{
			name: "unmarked generic struct of an implementor",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct{ W W[H] }\n\n" +
				"type W[T any] struct{ X T }\n\ntype H struct{}\n\n" +
				"func (H) Validate(bool) error { return nil }\n",
			is:   ErrUnsupported,
			msgs: []string{"field W of", "W with //kama:validate"},
		},
		{
			name: "fields sharing a hook",
			src: "//kama:validate\ntype A struct {\n\tname string\n\tName string\n}\n\n" +
				"func (A) validateName(bool) error { return nil }\n",
			is: ErrConflict,
			msgs: []string{
				"a.go:6:2", "fields name and Name of", "share the hook validateName",
				"//kama:validate hook=off",
			},
		},
		{
			name: "directive on a field",
			src:  "//kama:validate\ntype A struct {\n\t//kama:validate\n\tN int\n}\n",
			is:   ErrDirective,
			msgs: []string{"names no validator", "mark the type in its doc comment instead"},
		},
		{
			name: "validator on a type",
			src:  "//kama:validate required\ntype A struct{ N int }\n",
			is:   ErrDirective,
			msgs: []string{`unknown arg "required"`, "validators apply to struct fields only"},
		},
		{
			name: "validator given twice",
			src: "//kama:validate hook=off\ntype A struct {\n\t//kama:validate required\n" +
				"\t//kama:validate cmp=\">0\" required\n\tN int\n}\n",
			is:   ErrDirective,
			msgs: []string{"a.go:5:2", "validator required given twice on field"},
		},
		{
			name: "hook given twice on a type",
			src:  "//kama:validate hook=off\n//kama:validate hook=on\ntype A struct{ N int }\n",
			is:   ErrDirective,
			msgs: []string{"a.go:3:1", "hook given twice on type"},
		},
		{
			name: "hook given twice on a field",
			src:  "//kama:validate\ntype A struct {\n\t//kama:validate hook=off hook=off\n\tN int\n}\n",
			is:   ErrDirective,
			msgs: []string{"hook given twice on field", "A.N"},
		},
		{
			name: "bad hook args",
			src:  "//kama:validate\n//kama:validate hook=maybe\ntype A struct{ N int }\n",
			is:   ErrDirective,
			msgs: []string{`arg "hook=maybe" of //kama:validate; want hook=on or hook=off`},
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

// TestSelectionAccepted builds inputs close to the errors of
// TestSelectionErrors that hold nothing left unchecked or no conflict.
func TestSelectionAccepted(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// want are the methods the output declares, keyed as funcs keys them.
		want []string
	}{
		{
			name: "named array of a non-validatable type",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct{ Id Id }\n\n" +
				"type Id [16]byte\n",
			want: []string{"A.Validate"},
		},
		{
			name: "named pointer to a non-validatable type",
			src:  "//kama:validate\n//kama:validate hook=off\ntype A struct{ P PI }\n\ntype PI *int\n",
			want: []string{"A.Validate"},
		},
		{
			name: "generic named pointer of a non-validatable type",
			src: "//kama:validate\n//kama:validate hook=off\n" +
				"type G[T any] struct {\n\tP PT[int]\n\tV T\n}\n\ntype PT[T any] *T\n",
			want: []string{"G.Validate"},
		},
		{
			// A type parameter makes no dependant, so H validates nothing.
			name: "generic named pointer of a type parameter in a non-target",
			src: "type H[T any] struct{ P PT[T] }\n\ntype PT[T any] *T\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			want: []string{"C.Validate"},
		},
		{
			name: "unmarked generic types of non-validatable types",
			src: "//kama:validate\n//kama:validate hook=off\n" +
				"type A struct {\n\tL LS[int]\n\tM MS[string, int]\n\tW W[*int]\n}\n\n" +
				"type LS[T any] []T\n\ntype MS[K comparable, V any] map[K]V\n\n" +
				"type W[T any] struct{ X T }\n",
			want: []string{"A.Validate"},
		},
		{
			// An ignored type is left unchecked on purpose.
			name: "ignored generic slice of a target",
			src: "//kama:validate\n//kama:validate hook=off\ntype A struct{ L LS[C] }\n\n" +
				"//kama:validate ignore\ntype LS[T any] []T\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			want: []string{"A.Validate", "C.Validate"},
		},
		{
			// A type parameter makes no dependant, so H validates nothing.
			name: "unmarked generic slice of a type parameter in a non-target",
			src: "type H[T any] struct{ L LS[T] }\n\ntype LS[T any] []T\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			want: []string{"C.Validate"},
		},
		{
			name: "fields sharing a hook name with one hook off",
			src: "//kama:validate\ntype A struct {\n\tname string\n" +
				"\t//kama:validate hook=off\n\tName string\n}\n\n" +
				"func (A) validateName(bool) error { return nil }\n",
			want: []string{"A.Validate"},
		},
		{
			name: "fields sharing a hook name in a dependant",
			src: "type D struct {\n\tname string\n\tName string\n\tC    C\n}\n\n" +
				"//kama:validate\n//kama:validate hook=off\ntype C struct{ X int }\n",
			want: []string{"C.Validate", "D.Validate"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := generateAndBuild(t, map[string]string{"a.go": "package x\n\n" + tc.src})
			assert.DeepEqual(
				t,
				slices.Sorted(maps.Keys(gentest.Funcs(t, out["a.kama_validate.go"]))),
				tc.want,
			)
		})
	}
}

// TestSelectionOptionTypes makes a type holding a target as the type
// argument of an option type a dependant. TestSelectionMarkedGeneric covers
// the type arguments of other generic types.
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

type K map[opt.Option[T]]int

//kama:validate
//kama:validate hook=off
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

// TestSelectionMarkedGeneric passes validation on through marked generic
// struct, slice and map types. Their type arguments make no dependant, so
// the types holding them are targets by their own marks.
func TestSelectionMarkedGeneric(t *testing.T) {
	out := generateAndBuild(t, map[string]string{
		"a.go": `package x

//kama:validate
//kama:validate hook=off
type LS[T any] []T

//kama:validate
//kama:validate hook=off
type MS[K comparable, V any] map[K]V

//kama:validate
//kama:validate hook=off
type W[T any] struct{ X T }

//kama:validate
//kama:validate hook=off
type A struct {
	L LS[C]
	M MS[string, C]
	P *LS[C]
}

//kama:validate
//kama:validate hook=off
type Box[T any] struct {
	L LS[T]
	W W[T]
}

//kama:validate
//kama:validate hook=off
type C struct{ X int }
`,
	})
	fs := gentest.Funcs(t, out["a.kama_validate.go"])

	assertContains(t, fs["A.Validate"],
		"v.L.Validate(stopOnFirstError, func(v C, stopOnFirstError bool) error {",
		"v.M.Validate(stopOnFirstError, nil, func(v C, stopOnFirstError bool) error {",
		"if v.P != nil {",
		"(*v.P).Validate(stopOnFirstError, func(v C, stopOnFirstError bool) error {",
	)
	assertContains(t, fs["Box.Validate"],
		check(`validation.Field(v.L.Validate(stopOnFirstError, validateT), "L")`),
		check(`validation.Field(v.W.Validate(stopOnFirstError, validateT), "W")`),
	)
}

// foreignSrc declares named array and pointer types of a package the tests
// load as a dependency only.
const foreignSrc = `package y

type Impl struct{ A string }

func (Impl) Validate(bool) error { return nil }

type PI *Impl

type AI [2]Impl

type PT[T any] *T

type PN *int

type AN [2]int

type LN *struct {
	Next LN
	N    int
}

type AV [2]Impl

func (AV) Validate(bool) error { return nil }

type L *struct {
	Next L
	I    Impl
}
`

// TestSelectionForeignNamedArrayAndPointer walks through named array and
// pointer types of a package outside the run as through unnamed ones.
func TestSelectionForeignNamedArrayAndPointer(t *testing.T) {
	out, err := generateLoading(t, map[string]string{
		"y/y.go": foreignSrc,
		"x/x.go": `package x

import "` + modulePath(t) + `/y"

//kama:validate
//kama:validate hook=off
type S struct {
	P  y.PI
	A  y.AI
	M  map[string]y.AI
	G  y.PT[Inner]
	N  y.PN
	AN y.AN
	LN y.LN
	V  y.AV
}

type D struct{ A y.AI }

//kama:validate
//kama:validate hook=off
type Inner struct {
	//kama:validate required
	X string
}
`,
	}, "x")
	assert.NilError(t, err)

	fs := gentest.Funcs(t, out["x.kama_validate.go"])

	assert.DeepEqual(t, slices.Sorted(maps.Keys(fs)),
		[]string{"D.Validate", "Inner.Validate", "S.Validate"})
	assertContains(t, fs["S.Validate"],
		"if v.P != nil {",
		check(`validation.Field((*v.P).Validate(stopOnFirstError), "P")`),
		"for i, e := range v.A {",
		check(`validation.Field(validation.Index(e.Validate(stopOnFirstError), i), "A")`),
		check(`validation.Field(validation.Key(validation.Index(`+
			`e2.Validate(stopOnFirstError), i2), k), "M")`),
		"if v.G != nil {",
		check(`validation.Field((*v.G).Validate(stopOnFirstError), "G")`),
		check(`validation.Field(v.V.Validate(stopOnFirstError), "V")`),
	)
	assertNotContains(t, fs["S.Validate"], "v.N", "v.AN", "v.LN")
}

// TestSelectionForeignNamedErrors reports a named pointer type of a package
// outside the run holding itself and something to validate, and keeps the
// error for named array and pointer types of the run.
func TestSelectionForeignNamedErrors(t *testing.T) {
	cases := []struct {
		name  string
		field string
		dirs  []string
		msgs  []string
	}{
		{
			name:  "recursive",
			field: "R y.L",
			dirs:  []string{"x"},
			msgs: []string{
				"field R:", "y.L is a named pointer type of another package holding itself" +
					" and values to validate",
			},
		},
		{
			name:  "pointer of the run",
			field: "P y.PI",
			dirs:  []string{"x", "y"},
			msgs:  []string{"field P of", "PI, a named pointer type"},
		},
		{
			name:  "array of the run",
			field: "A y.AI",
			dirs:  []string{"x", "y"},
			msgs:  []string{"field A of", "AI, a named array type"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generateLoading(t, map[string]string{
				"y/y.go": foreignSrc,
				"x/x.go": "package x\n\nimport \"" + modulePath(t) + "/y\"\n\n" +
					"//kama:validate\n//kama:validate hook=off\ntype S struct{ " + tc.field + " }\n",
			}, tc.dirs...)
			assert.ErrorIs(t, err, ErrUnsupported)

			for _, msg := range tc.msgs {
				assert.ErrorContains(t, err, msg)
			}
		})
	}
}

// chainSrc declares the chain A00 to An in package pkg, each holding the
// next twice, and the last holding nothing to validate. A walk that visits a
// type once per path to it takes 2^n steps.
func chainSrc(pkg string, n int) string {
	name := func(i int) string { return fmt.Sprintf("A%02d", i) }

	var b strings.Builder

	b.WriteString("package " + pkg + "\n\n")

	for i := range n {
		fmt.Fprintf(&b, "type %s [1]struct {\n\tX %s\n\tY %s\n}\n\n", name(i), name(i+1), name(i+1))
	}

	fmt.Fprintf(&b, "type %s [1]int\n", name(n))

	return b.String()
}

// TestSelectionDeepChain generates from a target holding a deep chain of
// named arrays, of the run and of a package outside the run. Generate is
// bounded by a generous 30 s, far below the time an exponential walk takes
// at depth 22. Loading the chain takes seconds in go/types itself.
func TestSelectionDeepChain(t *testing.T) {
	const depth = 22

	target := "//kama:validate\n//kama:validate hook=off\ntype S struct {\n" +
		"\tF A00\n\t//kama:validate required\n\tN string\n}\n"

	cases := map[string]map[string]string{
		"run": {"x/x.go": chainSrc("x", depth) + "\n" + target},
		"foreign": {
			"y/y.go": chainSrc("y", depth),
			// The import path names the subtest, which stage writes to.
			"x/x.go": "package x\n\nimport . \"MODULE/y\"\n\n" + target,
		},
	}

	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			for name, src := range files {
				files[name] = strings.ReplaceAll(src, "MODULE", modulePath(t))
			}

			f, opts := stage(t, files)
			opts.Patterns = []string{"./" + path.Join(gentest.InlineDir(t), "x")}

			pkgs, err := gopackages.Load(context.Background(), f, opts)
			assert.NilError(t, err)

			start := time.Now()
			out, _, err := Generate(pkgs, testOptions(t, Options{}))
			elapsed := time.Since(start)

			assert.NilError(t, err)
			assert.Assert(t, elapsed < 30*time.Second, "Generate took %s", elapsed)
			assertContains(t, gentest.ByBase(out)["x.kama_validate.go"], "func (v S) Validate(")
		})
	}
}
