package validator

import (
	"strings"
	"testing"
)

const genericSrc = `package x

//kama:validate
//kama:validate:hook off
type Box[T any] struct {
	V T
	P *T
	S []T
	M map[string]T
}

//kama:validate
//kama:validate:hook off
type Target struct {
	//kama:validate:required
	N int
}

//kama:validate
//kama:validate:hook off
type Parent[T any] struct {
	I  Box[int]
	B  Box[Target]
	BT Box[T]
	BB Box[Box[T]]
	BP Box[*Target]
	BS Box[[]int]
}

//kama:validate
//kama:validate:hook off
type Pair[_ any, U comparable] struct{ U U }
`

// genericFuncs returns the functions generated from genericSrc keyed as
// funcs keys them.
func genericFuncs(t *testing.T) map[string]string {
	t.Helper()

	return funcs(
		t,
		generateAndBuild(t, map[string]string{"a.go": genericSrc})["a.kama_validate.go"],
	)
}

func TestGenericSignature(t *testing.T) {
	fs := genericFuncs(t)

	assertContains(
		t,
		fs["Box.Validate"],
		"func (v Box[T]) Validate(stopOnFirstError bool, "+
			"validateT func(v T, stopOnFirstError bool) error) error {",
	)
	// A blank type parameter gets a name, for its function too.
	assertContains(
		t,
		fs["Pair.Validate"],
		"func (v Pair[T0, U]) Validate(stopOnFirstError bool, "+
			"validateT0 func(v T0, stopOnFirstError bool) error, "+
			"validateU func(v U, stopOnFirstError bool) error) error {",
		"validateU(v.U, stopOnFirstError)",
	)
}

func TestGenericTypeParamFields(t *testing.T) {
	fn := genericFuncs(t)["Box.Validate"]

	// A nil function leaves the values of the type parameter unchecked.
	assertContains(t, fn,
		"if validateT != nil {",
		check(`validation.Field(validateT(v.V, stopOnFirstError), "V")`),
		"if v.P != nil {",
		check(`validation.Field(validateT((*v.P), stopOnFirstError), "P")`),
		check(`validation.Field(validation.Index(validateT(e, stopOnFirstError), i), "S")`),
		check(`validation.Field(validation.Key(validateT(e, stopOnFirstError), k), "M")`),
	)
}

func TestGenericTypeArgs(t *testing.T) {
	fn := genericFuncs(t)["Parent.Validate"]

	assertContains(t, fn,
		// A type argument holding nothing to validate gets nil.
		check(`validation.Field(v.I.Validate(stopOnFirstError, nil), "I")`),
		check(`validation.Field(v.BS.Validate(stopOnFirstError, nil), "BS")`),
		// A target gets a function calling its Validate.
		"v.B.Validate(stopOnFirstError, func(v Target, stopOnFirstError bool) error {\n"+
			"\t\treturn v.Validate(stopOnFirstError)\n\t})",
		// A type parameter of the receiver gets the function of the receiver.
		check(`validation.Field(v.BT.Validate(stopOnFirstError, validateT), "BT")`),
		"v.BB.Validate(stopOnFirstError, func(v Box[T], stopOnFirstError bool) error {\n"+
			"\t\treturn v.Validate(stopOnFirstError, validateT)\n\t})",
		// A type holding a target gets a function walking to it.
		"v.BP.Validate(stopOnFirstError, func(v *Target, stopOnFirstError bool) error {",
		"if v != nil {",
	)
}

// genericNamesSrc declares a generic target whose type parameter X is
// spelled inside nested function literals, where a local named like X
// would hide it.
const genericNamesSrc = `package x

//kama:validate
//kama:validate:hook off
type Box[X comparable] struct {
	//kama:validate:required
	V  X
	P  *X
	S  []X
	M  map[string]X
	B  Inner[X]
	BB Inner[Inner[X]]
	BS Inner[[]X]
	BD Inner[[]Inner[[]X]]
	BM Inner[map[string]Inner[*X]]
	//kama:validate:cmp >=1 <=3
	N int
}

//kama:validate
//kama:validate:hook off
type Inner[T any] struct{ V T }
`

// TestGenericTypeParamNamedLikeLocal builds a target whose type parameter
// is named like a local of Validate. The local takes a fresh name.
func TestGenericTypeParamNamedLikeLocal(t *testing.T) {
	cases := []struct {
		name string
		want []string
	}{
		{"v", []string{
			"func (v2 Box[v]) Validate(stopOnFirstError bool, " +
				"validatev func(v v, stopOnFirstError bool) error) error {",
			"func(v3 Inner[v], stopOnFirstError bool) error {",
			"func(v4 []v, stopOnFirstError bool) error {",
		}},
		{"stopOnFirstError", []string{
			"func (v Box[stopOnFirstError]) Validate(stopOnFirstError2 bool, ",
			"func(v Inner[stopOnFirstError], stopOnFirstError3 bool) error {",
		}},
		{"errs", []string{"\tvar errs2 []error\n"}},
		{
			"err",
			[]string{"if err2 := validation.Field(requiredComparable(v.V), \"V\"); err2 != nil {"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fn := validateOf(t, map[string]string{
				"a.go": strings.ReplaceAll(genericNamesSrc, "X", tc.name),
			}, "a.kama_validate.go", "Box")

			assertContains(t, fn, tc.want...)
		})
	}
}
