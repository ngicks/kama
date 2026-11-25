package partial

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	procgopackages "github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

// procInstance is an instance of the proc of NewProc.
type procInstance interface {
	Param(p ProcParamPartial) error
	In(pkgs *procgopackages.Packages)
	Out(f *overlayfs.Fs)
	Run(ctx context.Context, r workflow.Reporter) error
	Files(ctx context.Context, r workflow.Reporter) ([]base.File, error)
}

func newInstance(t *testing.T, p workflow.Proc) procInstance {
	t.Helper()

	inst, err := p.Instantiate(context.Background(), false)
	assert.NilError(t, err)

	i, ok := inst.(procInstance)
	assert.Assert(t, ok, "%T", inst)

	return i
}

// paramOptions returns the options of the param p merged onto
// DefaultProcParam.
func paramOptions(t *testing.T, p ProcParamPartial) Options {
	t.Helper()

	opts, _, err := procOptions(DefaultProcParam().Merge(p).ToValue())
	assert.NilError(t, err)

	return opts
}

// TestProcRunRejectsPackagesWithErrors runs the proc over packages a load
// allowing type errors kept, whose types the generator cannot trust.
func TestProcRunRejectsPackagesWithErrors(t *testing.T) {
	f, load := gentest.Stage(t, map[string]string{
		"a/a.go": "package a\n\n//kama:partial\ntype A struct{ N int }\n",
		"a/b.go": "package a\n\nvar _ = gone()\n",
	})
	load.AllowTypeErrors = true

	pkgs, err := gopackages.Load(context.Background(), f, load)
	assert.NilError(t, err)

	i := newInstance(t, NewProc())
	assert.NilError(t, i.Param(ProcParamPartial{}))
	i.In(&procgopackages.Packages{Pkgs: pkgs})
	i.Out(f)

	err = i.Run(context.Background(), gentest.NopReporter{})
	assert.ErrorContains(t, err, "kama:partial needs packages that type-check")
	assert.ErrorContains(t, err, "/a:\n\t")
	assert.ErrorContains(t, err, "undefined: gone")

	staged, err := f.Overlay()
	assert.NilError(t, err)
	assert.Equal(t, len(staged), 2, "%v", staged)
}
