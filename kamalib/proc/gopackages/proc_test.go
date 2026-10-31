package gopackages

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestProcParam(t *testing.T) {
	i := &instance{}

	assert.NilError(t, i.Param(ProcParam{}))
	assert.DeepEqual(t, i.param.Patterns, []string{"./..."})

	assert.ErrorContains(t, i.Param(ProcParam{Patterns: []string{""}}), "empty pattern")
}
