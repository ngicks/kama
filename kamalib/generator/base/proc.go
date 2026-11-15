package base

import (
	"context"
	"reflect"

	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

// Generator is what a code generator implements to run as a workflow step.
// P is the param of the step and I what it reads.
type Generator[P, I any] interface {
	// Configure validates p and keeps what Generate needs of it. Graph
	// formation calls it on a dry run, so it must not open files, start
	// processes or otherwise touch state outside the process.
	Configure(p P) error
	// Generate returns the files to stage in out, with paths relative to
	// the root of out, and the warnings to report. It may read out, which
	// holds what earlier steps staged, and leaves the writing to the proc.
	Generate(
		ctx context.Context,
		in I,
		out *overlayfs.Fs,
	) (files []File, warnings []error, err error)
}

// Proc runs a Generator as a workflow step: it reports the warnings of
// Generate and stages its files with Stage.
//
//	Param(P) error
//	In(I)
//	Out(*overlayfs.Fs)
type Proc[P, I any] struct {
	// New returns the generator of one step.
	New func() Generator[P, I]
}

// NewProc returns a constructor of a Proc over newGenerator, as
// workflow.Register takes it.
func NewProc[P, I any](newGenerator func() Generator[P, I]) func() workflow.Proc {
	return func() workflow.Proc { return Proc[P, I]{New: newGenerator} }
}

func (Proc[P, I]) ParamType() reflect.Type { return reflect.TypeFor[P]() }

func (Proc[P, I]) InTypes() []reflect.Type { return []reflect.Type{reflect.TypeFor[I]()} }

func (Proc[P, I]) OutTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*overlayfs.Fs]()}
}

func (Proc[P, I]) AsInputType() reflect.Type { return nil }

func (p Proc[P, I]) Instantiate(context.Context, bool) (workflow.Instance, error) {
	return NewInstance(p.New()), nil
}

func (Proc[P, I]) CleanUp(context.Context) error { return nil }

// Instance is one step of a Proc.
type Instance[P, I any] struct {
	gen Generator[P, I]
	in  I
	out *overlayfs.Fs
}

// NewInstance returns an Instance running gen.
func NewInstance[P, I any](gen Generator[P, I]) *Instance[P, I] {
	return &Instance[P, I]{gen: gen}
}

func (i *Instance[P, I]) Param(p P) error { return i.gen.Configure(p) }

func (i *Instance[P, I]) In(in I) { i.in = in }

func (i *Instance[P, I]) Out(f *overlayfs.Fs) { i.out = f }

func (i *Instance[P, I]) Run(ctx context.Context, r workflow.Reporter) error {
	files, warnings, err := i.gen.Generate(ctx, i.in, i.out)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		r.Warn(w)
	}
	return Stage(ctx, i.out, files, r)
}
