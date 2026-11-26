package fieldtree

import (
	"context"
	"encoding/json/v2"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	pkggopackages "github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

func crossFiles(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{
		"p/p.go": `package p

import "` + modulePath + gentest.InlineDir(t) + `/q"

//kama:fieldtree app
type App struct {
	Settings q.Settings ` + "`json:\"settings\"`" + `
}
`,
		"q/q.go": `package q

type Settings struct {
	// Level is the log level.
	Level string ` + "`json:\"level\"`" + `
}
`,
	}
}

// TestCrossPackage walks a struct of another package and reads its docs,
// and names a type without a directive through Types.
func TestCrossPackage(t *testing.T) {
	q := modulePath + gentest.InlineDir(t) + "/q"

	texts, warnings, err := gen(t, crossFiles(t), map[string]Target{
		"settings": {Type: q + ".Settings", Label: "q settings"},
	})
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)
	assert.Equal(t, texts["app"], "App\n"+
		"└─ .Settings  q.Settings  #   (settings)\n"+
		"    └─ .Level  string  # Level is the log level.  (settings.level)\n")
	assert.Equal(t, texts["settings"], "q settings\n"+
		"└─ .Level  string  # Level is the log level.  (level)\n")
}

func TestTargetErrors(t *testing.T) {
	const src = `package p

//kama:fieldtree notStruct
type N int

type (
	//kama:fieldtree inGroup
	A struct{ X int }
	B struct{ Y int }
)

//kama:fieldtree onFunc
func F() {}

//kama:fieldtree app-version
type H struct{}

//kama:fieldtree a, b
type Two struct{}

//kama:fieldtree ok
type Ok struct{ Z int }
`

	texts, _, err := gen(t, map[string]string{"p/p.go": src}, map[string]Target{
		"bad":     {Type: "nodot"},
		"missing": {Type: "example.com/nowhere.T"},
		"noType":  {Type: modulePath + gentest.InlineDir(t) + "/p.Nope"},
	})
	assert.ErrorIs(t, err, ErrTarget)
	assert.ErrorIs(t, err, ErrDirective)

	for _, msg := range []string{
		"N is no struct type",
		"the directive is on no single type spec",
		`quote an id holding a -, as "app-version"`,
		"a second id b",
		`types.bad: "nodot" is no <import path>.<type name>`,
		"no loaded package has the path example.com/nowhere",
		"declares no type Nope",
	} {
		assert.ErrorContains(t, err, msg)
	}

	// A spec of a group carries its own directive.
	assert.Equal(t, texts["inGroup"], "A\n└─ .X  int  #   (X)\n")
	assert.Equal(t, texts["ok"], "Ok\n└─ .Z  int  #   (Z)\n")
}

func TestProcKind(t *testing.T) {
	assert.Equal(t, ProcKind(), "fieldtree.text")
}

func TestProcParam(t *testing.T) {
	var p ProcParam
	assert.NilError(t, json.Unmarshal(
		[]byte(`{"types":{"a":{"type":"x.T","color":1}},"typo":2}`), &p,
	))
	assert.DeepEqual(t, p.UnknownKeys(), [][]string{{"typo"}, {"types", "a", "color"}})

	inst, err := NewProc().Instantiate(context.Background(), true)
	assert.NilError(t, err)
	assert.ErrorContains(
		t,
		inst.(*instance).Param(ProcParam{Types: map[string]Target{"a": {Type: "T"}}}),
		`types.a: "T" is no <import path>.<type name>`,
	)
}

// TestProcRun runs the proc on loaded packages and reads its texts.
func TestProcRun(t *testing.T) {
	f, opts := gentest.Stage(t, crossFiles(t))

	pkgs, err := pkggopackages.Load(t.Context(), f, opts)
	assert.NilError(t, err)

	inst, err := NewProc().Instantiate(context.Background(), false)
	assert.NilError(t, err)

	i := inst.(*instance)
	assert.NilError(t, i.Param(ProcParam{}))
	i.In(&gopackages.Packages{Pkgs: pkgs})
	i.Out()

	var r workflow.Reporter = &gentest.RecordReporter{}
	assert.NilError(t, i.Run(context.Background(), r))
	assert.DeepEqual(t, keys(i.AsInput()), []string{"app"})
}

func keys(texts Texts) []string {
	var out []string
	for k := range texts {
		out = append(out, k)
	}

	return out
}
