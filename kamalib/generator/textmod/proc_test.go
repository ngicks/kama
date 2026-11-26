package textmod

import (
	"encoding/json/v2"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
)

func TestProcKind(t *testing.T) {
	assert.Equal(t, ProcKind(), "textmod.replace")
}

// TestProcParamUnknownKeys decodes a param with a key naming no field, which
// is captured and reported, not rejected.
func TestProcParamUnknownKeys(t *testing.T) {
	var p base.ParamOf[ProcParam]
	assert.NilError(t, json.Unmarshal([]byte(`{"mods":{"a":"1"},"var":{"x":1}}`), &p))
	assert.DeepEqual(t, p.ToValue().Mods, map[string]string{"a": "1"})
	assert.DeepEqual(t, p.UnknownKeys(), [][]string{{"var"}})
}

// TestCheckParam reports a template that does not parse without touching
// files.
func TestCheckParam(t *testing.T) {
	assert.NilError(t, checkParam(ProcParam{Mods: map[string]string{"a": "{{ .Value }}"}}))
	assert.ErrorContains(
		t,
		checkParam(ProcParam{Mods: map[string]string{"a": "{{ nope }}"}}),
		`"a"`,
	)
}
