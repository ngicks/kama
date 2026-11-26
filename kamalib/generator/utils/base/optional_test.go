package base_test

import (
	"context"
	"iter"
	"reflect"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	procoverlayfs "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

func optionalProc(
	takes bool,
	got *map[string]any,
) base.Proc[noInParamPartial, noInParam, base.None, string] {
	name := "a.go"

	return base.Proc[noInParamPartial, noInParam, base.None, string]{
		Generator: base.Generator[string]{
			Id: driveId,
			Generate: func(_ context.Context, env base.Env, _ string) iter.Seq2[base.Output, error] {
				*got = env.Optional
				return func(func(base.Output, error) bool) {}
			},
		},
		Default: func() noInParamPartial { return noInParamPartial{Name: &name} },
		Input: func(
			context.Context,
			noInParam,
			base.None,
			*overlayfs.Fs,
		) (string, base.Scope, error) {
			return "", base.Scope{}, nil
		},
		TakesOptional: takes,
	}
}

func optionalSpec(root string) workflow.Spec {
	return workflow.Spec{
		Version: 1,
		Steps: map[string]workflow.Step{
			"root": {Proc: "overlayfs.open", Param: map[string]any{"root": root}},
			"side": {Proc: "overlayfs.open", Param: map[string]any{"root": root}},
			"gen": {
				Proc:     driveId.ProcKind(),
				Out:      workflow.Names{"root"},
				Optional: workflow.Names{"side"},
			},
		},
	}
}

func TestProcTakesOptional(t *testing.T) {
	ctx := context.Background()

	var got map[string]any

	proc := optionalProc(true, &got)
	assert.Equal(t, proc.OptionalType(), reflect.TypeFor[any]())

	reg := workflow.NewRegistry()
	assert.NilError(t, reg.Register("overlayfs.open", procoverlayfs.NewOpenProc))
	assert.NilError(t, reg.Register(driveId.ProcKind(), func() workflow.Proc { return proc }))

	g, err := optionalSpec(t.TempDir()).Build(ctx, reg)
	assert.NilError(t, err)
	assert.NilError(t, g.Run(ctx, workflow.ProgressControl{}))

	_, ok := got["side"].(*overlayfs.Fs)
	assert.Assert(t, ok, "%v", got)
	assert.Equal(t, len(got), 1)
}

func TestProcTakesNoOptional(t *testing.T) {
	var got map[string]any

	proc := optionalProc(false, &got)
	assert.Assert(t, proc.OptionalType() == nil)

	reg := workflow.NewRegistry()
	assert.NilError(t, reg.Register("overlayfs.open", procoverlayfs.NewOpenProc))
	assert.NilError(t, reg.Register(driveId.ProcKind(), func() workflow.Proc { return proc }))

	_, err := optionalSpec(t.TempDir()).Build(context.Background(), reg)
	assert.ErrorIs(t, err, workflow.ErrInvalidStep)
}
