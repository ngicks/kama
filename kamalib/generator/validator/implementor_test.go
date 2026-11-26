package validator

import (
	"context"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
)

func TestImplementorShape(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": `package x

//kama:validate
//kama:validate hook=False
type A struct {
	Good    Good
	NoArg   NoArg
	Extra   Extra
	Generic Generic[int]
	Wrong   Wrong[int]
	Iface   Iface
}

type Good struct{}

func (Good) Validate(bool) error { return nil }

type NoArg struct{}

func (NoArg) Validate() error { return nil }

type Extra struct{}

func (Extra) Validate(bool, int) error { return nil }

type Generic[T any] struct{ V T }

func (g Generic[T]) Validate(stop bool, validateT func(T, bool) error) error { return nil }

// Wrong takes no function for its type parameter.
type Wrong[T any] struct{ V T }

func (Wrong[T]) Validate(bool) error { return nil }

type Iface interface{ Validate(bool) error }
`,
	}, "a.kama_validate.go", "A")

	assertContains(t, fn, "v.Good.Validate(stopOnFirstError)",
		"v.Generic.Validate(stopOnFirstError, nil)", "v.NoArg.Validate()")
	assertNotContains(t, fn, "v.Extra", "v.Wrong", "v.Iface")
}

// TestImplementorPointerReceiver calls a Validate on the pointer receiver
// on the value in place, or on a copy of a value that has no address, and
// passes a function literal calling it for a type parameter.
func TestImplementorPointerReceiver(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": `package x

import "github.com/ngicks/option/opt"

//kama:validate
//kama:validate hook=False
type A struct {
	F P
	S []P
	Q []*P
	M map[string]P
	O opt.Option[P]
	B Bare
	G G[P]
}

type P struct{}

func (*P) Validate(bool) error { return nil }

type Bare struct{}

func (*Bare) Validate(...int) error { return nil }

//kama:validate hook=False
type G[T any] struct{ V T }
`,
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		check(`validator.Field(v.F.Validate(stopOnFirstError), "F")`),
		"for i := range v.S {\n\t\tif err := validator.Field(validator.Index(v.S[i].Validate(",
		"if v.Q[i] != nil {\n\t\t\tif err := validator.Field(validator.Index((*v.Q[i]).Validate(",
		"e := v.M[k]",
		check(`validator.Field(validator.Key(e.Validate(stopOnFirstError), k), "M")`),
		"if v.O.IsSome() {\n\t\tif err := validator.Field(func() error {\n\t\t\tp := v.O.Value()\n"+
			"\t\t\treturn p.Validate(stopOnFirstError)",
		check(`validator.Field(v.B.Validate(), "B")`),
		"v.G.Validate(stopOnFirstError, func(v P, stopOnFirstError bool) error {\n"+
			"\t\treturn v.Validate(stopOnFirstError)",
	)
}

// generateDependent generates the package in the sub directory of files
// first and stages its output. It then generates the package at the top of
// files alone, stages the output and builds every package. It returns the
// files of the second run keyed by base name.
func generateDependent(t *testing.T, files map[string]string) map[string]string {
	t.Helper()

	f, all := stage(t, files)
	top := "./" + gentest.InlineDir(t)

	_, err := generateInto(t, f, gopackages.Options{Patterns: []string{top + "/sub"}}, Options{})
	assert.NilError(t, err)

	out, err := generateInto(t, f, gopackages.Options{Patterns: []string{top}}, Options{})
	assert.NilError(t, err)

	_, err = gopackages.Load(context.Background(), f, all)
	assert.NilError(t, err)

	return out
}

// implementorSub declares targets of a package generated before the
// package depending on it is loaded.
const implementorSub = `package sub

//kama:validate
//kama:validate hook=False
type S struct {
	//kama:validate required
	X int
}

//kama:validate
type L []S

//kama:validate
//kama:validate hook=False
type G[T any] struct{ V T }

type Plain struct{ Y int }
`

// TestImplementorSameAsTarget generates a package alone, with the package
// it depends on generated before, and together with that package. Both
// ways give the same files for it.
func TestImplementorSameAsTarget(t *testing.T) {
	files := map[string]string{
		"a.go": `package x

import "%s/sub"

// A holds the types of sub, which it is a dependant of.
type A struct {
	sub.S
	V     sub.S
	P     *sub.S
	L     sub.L
	G     sub.G[int]
	GS    sub.G[sub.S]
	Items []sub.S
	Plain sub.Plain
}

//kama:validate
type List []sub.S

//kama:validate
//kama:validate hook=False
type R struct {
	//kama:validate required
	N string
}
`,
		"sub/sub.go": implementorSub,
	}
	files["a.go"] = strings.ReplaceAll(files["a.go"], "%s", modulePath(t))

	alone := generateDependent(t, files)
	together := generateAndBuild(t, files)

	for _, file := range []string{"a.kama_validate.go", HelpersFile} {
		assert.Equal(
			t,
			gentest.WithoutGenerated(alone[file]),
			gentest.WithoutGenerated(together[file]),
			file,
		)
	}

	fs := gentest.Funcs(t, alone["a.kama_validate.go"])
	assertContains(t, fs["A.Validate"],
		check(`v.S.Validate(stopOnFirstError)`),
		"v.G.Validate(stopOnFirstError, nil)",
		"v.GS.Validate(stopOnFirstError, func(v sub.S, stopOnFirstError bool) error {",
	)
	assertNotContains(t, fs["A.Validate"], "Plain")
}
