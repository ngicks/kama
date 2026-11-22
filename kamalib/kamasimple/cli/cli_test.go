package cli

import (
	"bytes"
	"errors"
	"testing"

	"gotest.tools/v3/assert"
)

func TestProgress(t *testing.T) {
	var out bytes.Buffer

	pc := Progress(&out, false)
	pc.OnWarning("load", errors.New("skipped x"))
	assert.Assert(t, pc.OnProgress == nil)
	assert.Equal(t, out.String(), "warning: load: skipped x\n")

	out.Reset()

	pc = Progress(&out, true)
	pc.OnProgress("load", "start")
	pc.OnWarning("load", errors.New("skipped x"))
	assert.Equal(t, out.String(), "load: start\nwarning: load: skipped x\n")
}

func TestError(t *testing.T) {
	var out bytes.Buffer

	Error(&out, "kama-simple", errors.New("boom"))
	assert.Equal(t, out.String(), "kama-simple: boom\n")
}
