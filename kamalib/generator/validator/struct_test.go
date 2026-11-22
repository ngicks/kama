package validator

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// check is the statement a generated check holds around the error
// expression err, without its indentation.
func check(err string) string { return "if err := " + err + "; err != nil {" }

func TestStructNesting(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": `package x

import (
	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

//kama:validate
//kama:validate:hook off
type A struct {
	Inner Inner
	P     *Inner
	PP    **Inner
	S     []Inner
	Arr   [2]Inner
	M     map[string]Inner
	SM    []map[int]*Inner
	O     opt.Option[Inner]
	U     und.Und[[]Inner]
	E     elastic.Elastic[Inner]
	N     int
	Ints  []int
	Plain Plain
}

type Plain struct{ X int }

//kama:validate
//kama:validate:hook off
type Inner struct{ X int }
`,
	}, "a.kama_validate.go", "A")

	assertContains(
		t,
		fn,
		check(`validation.Field(v.Inner.Validate(stopOnFirstError), "Inner")`),
		"if v.P != nil {",
		check(`validation.Field((*v.P).Validate(stopOnFirstError), "P")`),
		"if (*v.PP) != nil {",
		check(`validation.Field((*(*v.PP)).Validate(stopOnFirstError), "PP")`),
		"for i, e := range v.S {",
		check(`validation.Field(validation.Index(e.Validate(stopOnFirstError), i), "S")`),
		"for i, e := range v.Arr {",
		"for _, k := range slices.Sorted(maps.Keys(v.M)) {\n\t\te := v.M[k]\n",
		check(`validation.Field(validation.Key(e.Validate(stopOnFirstError), k), "M")`),
		"for i, e := range v.SM {",
		"for _, k2 := range slices.Sorted(maps.Keys(e)) {\n\t\t\te2 := e[k2]\n",
		check(
			`validation.Field(validation.Index(validation.Key(`+
				`(*e2).Validate(stopOnFirstError), k2), i), "SM")`,
		),
		"if v.O.IsSome() {",
		check(`validation.Field(v.O.Value().Validate(stopOnFirstError), "O")`),
		"if v.U.IsDefined() {",
		"for i, e := range v.U.Value() {",
		"for i, e := range v.E.Options() {",
		"if e.IsSome() {",
		check(`validation.Field(validation.Index(e.Value().Validate(stopOnFirstError), i), "E")`),
	)
	// Fields holding nothing to validate get no step.
	assertNotContains(t, fn, "v.N", "v.Ints", "v.Plain")
}

func TestStopOnFirstError(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate:hook off\n" +
			"type A struct{ I Inner }\n\n" +
			"//kama:validate\n//kama:validate:hook off\ntype Inner struct{ X int }\n",
	}, "a.kama_validate.go", "A")

	// Returning the first error and collecting every error both appear.
	assertContains(t, fn,
		"var errs []error",
		"if stopOnFirstError {\n\t\t\treturn err\n\t\t}",
		"errs = append(errs, err)",
		"return validation.Join(errs...)",
	)
}

func TestEmptyBody(t *testing.T) {
	out := generateAndBuild(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate:hook off\n" +
			"type A struct{ N int }\n\n//kama:validate\ntype L []int\n",
	})
	src := out["a.kama_validate.go"]

	assert.Equal(t, strings.Count(src, "\treturn nil\n"), 2, src)
	// A file that collects no error does not import the error package.
	assertNotContains(t, src, "validation", "errs")
}

func TestSliceAndMapTargets(t *testing.T) {
	files := map[string]string{
		"a.go": `package x

//kama:validate
type L []Inner

//kama:validate
type M map[string]*Inner

//kama:validate
type LL [][]Inner

//kama:validate
//kama:validate:hook off
type Inner struct{ X int }
`,
	}
	src := generateAndBuild(t, files)["a.kama_validate.go"]
	fs := funcs(t, src)

	assertContains(t, fs["L.Validate"],
		"for i, e := range v {",
		check(`validation.Index(e.Validate(stopOnFirstError), i)`),
	)
	assertContains(t, fs["M.Validate"],
		"for _, k := range slices.Sorted(maps.Keys(v)) {\n\t\te := v[k]\n",
		"if e != nil {",
		check(`validation.Key((*e).Validate(stopOnFirstError), k)`),
	)
	assertContains(t, fs["LL.Validate"],
		"for i2, e2 := range e {",
		check(`validation.Index(validation.Index(e2.Validate(stopOnFirstError), i2), i)`),
	)
	// A collection target has no fields, so it has no path segment of a field.
	assertNotContains(t, fs["L.Validate"], "validation.Field")
}

func TestPathSegments(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate:hook off\ntype A struct {\n" +
			"\tNamed   Inner `json:\"named,omitempty\"`\n" +
			"\tSkipped Inner `json:\"-\"`\n" +
			"\tOpts    Inner `json:\",omitempty\"`\n" +
			"\tOther   Inner `yaml:\"other\"`\n" +
			"\tEmbedded\n" +
			"\t*PtrEmbedded\n" +
			"\tTagged `json:\"tagged\"`\n" +
			"\tList\n" +
			"}\n\n" +
			"//kama:validate\n//kama:validate:hook off\ntype Inner struct{ X int }\n\n" +
			"//kama:validate\n//kama:validate:hook off\ntype Embedded struct{ X int }\n\n" +
			"//kama:validate\n//kama:validate:hook off\ntype PtrEmbedded struct{ X int }\n\n" +
			"//kama:validate\n//kama:validate:hook off\ntype Tagged struct{ X int }\n\n" +
			"//kama:validate\ntype List []Inner\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		check(`validation.Field(v.Named.Validate(stopOnFirstError), "named")`),
		check(`validation.Field(v.Skipped.Validate(stopOnFirstError), "Skipped")`),
		check(`validation.Field(v.Opts.Validate(stopOnFirstError), "Opts")`),
		check(`validation.Field(v.Other.Validate(stopOnFirstError), "Other")`),
		// Encoders inline an embedded struct without json name.
		check(`v.Embedded.Validate(stopOnFirstError)`),
		check(`(*v.PtrEmbedded).Validate(stopOnFirstError)`),
		check(`validation.Field(v.Tagged.Validate(stopOnFirstError), "tagged")`),
		// An embedded type other than a struct is a field named after it.
		check(`validation.Field(v.List.Validate(stopOnFirstError), "List")`),
	)
}

// TestForeignUnexportedFields skips the fields of a struct of another
// package that the generated code cannot spell.
func TestForeignUnexportedFields(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\nimport \"" + modulePath(t) + "/sub\"\n\n" +
			"//kama:validate\n//kama:validate:hook off\ntype A sub.S\n",
		"sub/sub.go": "package sub\n\n//kama:validate\n//kama:validate:hook off\n" +
			"type S struct {\n\tPublic  Inner\n\tprivate Inner\n}\n\n" +
			"//kama:validate\n//kama:validate:hook off\ntype Inner struct{ X int }\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn, "v.Public.Validate")
	assertNotContains(t, fn, "private")
}

// TestAnonymousStruct walks the fields of struct type literals, which make
// their holders dependants as fields of a named struct do.
func TestAnonymousStruct(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\ntype A struct {\n" +
			"\tS struct {\n\t\tI Inner `json:\"i\"`\n\t\tN int\n\t} `json:\"s\"`\n" +
			"\tL []struct{ I *Inner }\n}\n\n" +
			"//kama:validate\n//kama:validate:hook off\ntype Inner struct{ X int }\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		check(`validation.Field(validation.Field(v.S.I.Validate(stopOnFirstError), "i"), "s")`),
		"for i, e := range v.L {",
		"if e.I != nil {",
		check(`validation.Field(validation.Index(validation.Field(`+
			`(*e.I).Validate(stopOnFirstError), "I"), i), "L")`),
	)
	assertNotContains(t, fn, "v.S.N")
}

// TestMapKeyOrder sorts the keys of an integer or string type and ranges
// over any other keys directly.
func TestMapKeyOrder(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": `package x

type MyInt int

type Key struct{ A, B int }

//kama:validate
//kama:validate:hook off
type A[K comparable] struct {
	ByString map[string]Inner
	ByMyInt  map[MyInt]Inner
	ByFloat  map[float64]Inner
	ByStruct map[struct{ A int }]Inner
	ByPtr    map[*Key]Inner
	ByParam  map[K]Inner
	ByBool   map[bool]Inner
}

//kama:validate
//kama:validate:hook off
type Inner struct{ X int }
`,
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		"for _, k := range slices.Sorted(maps.Keys(v.ByString)) {",
		"for _, k := range slices.Sorted(maps.Keys(v.ByMyInt)) {",
		// A NaN key would miss the lookup after sorting.
		"for k, e := range v.ByFloat {",
		"for k, e := range v.ByStruct {",
		"for k, e := range v.ByPtr {",
		"for k, e := range v.ByParam {",
		"for k, e := range v.ByBool {",
	)
}

// TestElasticElements walks the options of each elastic.Elastic element of a
// slice, a map and an array, as it walks a direct elastic.Elastic field.
func TestElasticElements(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": `package x

import "github.com/ngicks/option/elastic"

//kama:validate
//kama:validate:hook off
type A struct {
	S []elastic.Elastic[Inner]
	M map[string]elastic.Elastic[Inner]
	R [2]elastic.Elastic[Inner]
	E elastic.Elastic[Inner]
}

//kama:validate
//kama:validate:hook off
type Inner struct{ X int }
`,
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		"for i, e := range v.S {\n\t\tfor i2, e2 := range e.Options() {\n\t\t\tif e2.IsSome() {",
		check(`validation.Field(validation.Index(validation.Index(`+
			`e2.Value().Validate(stopOnFirstError), i2), i), "S")`),
		"e := v.M[k]\n\t\tfor i2, e2 := range e.Options() {",
		check(`validation.Field(validation.Key(validation.Index(`+
			`e2.Value().Validate(stopOnFirstError), i2), k), "M")`),
		"for i, e := range v.R {\n\t\tfor i2, e2 := range e.Options() {",
		"for i, e := range v.E.Options() {\n\t\tif e.IsSome() {",
	)
	assertNotContains(t, fn, "if e.IsSome() {\n\t\t\tfor")
}
