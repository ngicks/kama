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
//kama:validate hook=off
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
		"v.Generic.Validate(stopOnFirstError, nil)")
	assertNotContains(t, fn, "v.NoArg", "v.Extra", "v.Wrong", "v.Iface")
}

func TestImplementorPointerReceiver(t *testing.T) {
	_, err := generate(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=off\n" +
			"type A struct{ P []P }\n\ntype P struct{}\n\n" +
			"func (*P) Validate(bool) error { return nil }\n",
	}, testRun{})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorContains(t, err, "declares Validate on its pointer receiver")
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
//kama:validate hook=off
type S struct {
	//kama:validate required
	X int
}

//kama:validate
type L []S

//kama:validate
//kama:validate hook=off
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
//kama:validate hook=off
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
