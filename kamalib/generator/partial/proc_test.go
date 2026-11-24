package partial

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"

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
	Files(ctx context.Context, r workflow.Reporter) ([]File, error)
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
