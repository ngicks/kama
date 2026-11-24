package validator

import (
	"slices"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
)

// TestGeneratedModels checks the declarations another registered generator
// wrote: a type is a target only when //kama:validate marks it, so a type
// holding a marked one is no dependant, and a Validate the other generator
// wrote counts as written by hand.
func TestGeneratedModels(t *testing.T) {
	reg := genmark.NewRegistry(base.ToolName)
	assert.NilError(t, reg.Register(GeneratorId().String()))
	assert.NilError(t, reg.Register("x:models"))

	out, err := generate(t, map[string]string{"a.go": `package x

// Model is handed on.
//
//kama:validate hook=off
//kama:generated x:models
type Model struct {
	//kama:validate cmp=">0"
	N int
	U Union
}

// Union has a Validate the other generator wrote.
//
//kama:generated x:models
type Union struct{ Kind any }

// Validate is written by the other generator.
//
//kama:generated x:models
func (Union) Validate(bool) error { return nil }

// wire holds Model and is not handed on.
//
//kama:generated x:models
type wire struct{ M Model }
`}, testRun{Markers: reg})
	assert.NilError(t, err)

	files := gentest.ByBase(out)
	fns := gentest.Funcs(t, files["a.kama_validate.go"])

	assertContains(
		t,
		fns["Model.Validate"],
		"kamaValidateCmp(v.N",
		"v.U.Validate(stopOnFirstError)",
	)

	for name := range fns {
		assert.Assert(t, !slices.Contains([]string{"Union.Validate", "wire.Validate"}, name), name)
	}
}
