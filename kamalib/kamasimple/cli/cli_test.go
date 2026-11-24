package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

	out.Reset()

	pc.OnWarning("", errors.New("unknown key"))
	assert.Equal(t, out.String(), "warning: unknown key\n")
}

func TestError(t *testing.T) {
	var out bytes.Buffer

	Error(&out, "kama-simple", errors.New("boom"))
	assert.Equal(t, out.String(), "kama-simple: boom\n")
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	assert.NilError(t, os.WriteFile(
		filepath.Join(dir, "kama.yaml"),
		[]byte("version: 1\nsteps:\n  root:\n    proc: overlayfs.open\n    typo: 1\n"),
		0o644,
	))
	t.Chdir(t.TempDir())

	var out bytes.Buffer

	assert.NilError(t, Run(t.Context(), "acme-kama", []string{"-C", dir, "-v"}, &out))
	assert.Assert(
		t,
		strings.Contains(out.String(), "warning: kamasimple: kama.yaml: "),
		out.String(),
	)

	out.Reset()
	assert.NilError(t, Run(t.Context(), "acme-kama", []string{"-h"}, &out))
	assert.Assert(t, strings.Contains(out.String(), "Usage of acme-kama"), out.String())

	assert.ErrorContains(
		t,
		Run(t.Context(), "acme-kama", []string{"-spec", "none.yaml"}, &out),
		"none.yaml",
	)
}
