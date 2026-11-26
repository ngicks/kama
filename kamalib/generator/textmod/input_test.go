package textmod

import (
	"context"
	"path"
	"reflect"
	"strings"
	"testing"
	"text/template"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
)

type inputPoint struct{ X, Y int }

func executeInputs(t *testing.T, inputs map[string]any, src string) (string, error) {
	t.Helper()

	funcs, err := Funcs(Run{Inputs: inputs})
	assert.NilError(t, err)

	tmpl, err := ParseMod("t", src, funcs)
	assert.NilError(t, err)

	var b strings.Builder

	err = tmpl.Execute(&b, nil)

	return b.String(), err
}

func TestInput(t *testing.T) {
	inputs := map[string]any{
		"tree":  map[string]string{"config": "Config\n└─ .Addr"},
		"text":  "plain",
		"point": inputPoint{X: 1, Y: 2},
	}

	for _, c := range []struct{ src, want string }{
		{`{{ index (input "tree") "config" }}`, "Config\n└─ .Addr"},
		{`{{ input "text" | upper }}`, "PLAIN"},
		{`{{ (input "point").Y }}`, "2"},
	} {
		got, err := executeInputs(t, inputs, c.src)
		assert.NilError(t, err, c.src)
		assert.Equal(t, got, c.want, c.src)
	}

	_, err := executeInputs(t, inputs, `{{ input "nope" }}`)
	assert.ErrorContains(t, err, `input "nope": the step takes no such optional input; `+
		`it takes ["point" "text" "tree"]`)

	_, err = executeInputs(t, nil, `{{ input "tree" }}`)
	assert.ErrorContains(t, err, "it takes []")
}

// TestInputCollidesWithProvider fails a run whose provider defines input,
// which the built-in functions own.
func TestInputCollidesWithProvider(t *testing.T) {
	const name = "test-input-collision"

	RegisterFuncs(name, func(Run) template.FuncMap {
		return template.FuncMap{"input": func() string { return "" }}
	})

	defer func() {
		providersMu.Lock()
		delete(providers, name)
		providersMu.Unlock()
	}()

	_, err := Funcs(Run{})
	assert.ErrorContains(t, err, `function "input" of funcs "test-input-collision" is defined by `+
		"the built-in functions too")
}

// TestCheckParamInput parses a mod reading an input in a dry run, before
// any input exists.
func TestCheckParamInput(t *testing.T) {
	assert.NilError(t, checkParam(ProcParam{
		Mods: map[string]string{"help": `{{ index (input "tree") "config" }}`},
	}))
	assert.Equal(t, proc.OptionalType(), reflect.TypeFor[any]())
}

// TestGenerateReadsOptional runs Generate with the optional inputs of a
// step in its Env.
func TestGenerateReadsOptional(t *testing.T) {
	const src = "package p\n\n//kama:textmod help\nconst Help = `old`\n"

	f, in := stage(t, map[string]string{"p/a.go": src})
	in.Mods = map[string]string{"help": `{{ index (input "tree") "config" }}`}

	r := &gentest.RecordReporter{}

	var outs []base.Output

	for out, err := range Generate(context.Background(), base.Env{
		Reporter: r,
		Out:      f.FS(),
		Optional: map[string]any{"tree": map[string]string{"config": "new"}},
	}, in) {
		assert.NilError(t, err)

		outs = append(outs, out)
	}

	assert.DeepEqual(t, outs, []base.Output{base.Whole{
		Path:    path.Join(gentest.InlineDir(t), "p/a.go"),
		Content: []byte(strings.Replace(src, "`old`", "`new`", 1)),
	}})
}
