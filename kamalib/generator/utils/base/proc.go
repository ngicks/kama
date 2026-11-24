package base

import (
	"context"
	"reflect"
	"time"

	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

// Proc is the workflow proc of a Generator. Its instances merge the param
// of a step onto Default, build the input of the generator from the param,
// the in and the out of the step, and run the generator with [Drive],
// staging into the out. The out is read too, so a file staged by an
// earlier step is seen.
//
// Pp is the partial variant of the param P that a step decodes into. X is
// the type of the in of a step, and None for a step without one. I is the
// input of the generator. Its instances have the methods below, with In()
// for X None:
//
//	Param(Pp) error
//	In(X)
//	Out(*overlayfs.Fs)
//	Files(context.Context, workflow.Reporter) ([]File, error)
//
// Files returns the files Run would stage, and stages nothing.
type Proc[Pp ParamPartial[Pp, P], P, X, I any] struct {
	Generator Generator[I]
	// Default returns the values P takes for the keys a step leaves out.
	Default func() Pp
	// Check reports a param the proc cannot run with. Param calls it, in a
	// dry run too, so it must not touch files. Nil accepts every param.
	Check func(param P) error
	// Input returns the input of a run and the scope of its stale check.
	Input func(ctx context.Context, param P, in X, out *overlayfs.Fs) (I, Scope, error)
	// Now returns the time of a run. Nil means time.Now.
	Now func() time.Time
	// Registry tells the generators that generated marks name. Nil means
	// DefaultRegistry.
	Registry *genmark.Registry
}

// None is the in of a Proc whose step takes no in.
type None struct{}

// ParamPartial is the partial variant Pp of a param P, as partial
// generates it.
type ParamPartial[Pp, P any] interface {
	Merge(r Pp) Pp
	ToValue() P
}

func (Proc[Pp, P, X, I]) ParamType() reflect.Type { return reflect.TypeFor[Pp]() }

func (Proc[Pp, P, X, I]) InTypes() []reflect.Type {
	if takesNoIn[X]() {
		return nil
	}

	return []reflect.Type{reflect.TypeFor[X]()}
}

func (Proc[Pp, P, X, I]) OutTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*overlayfs.Fs]()}
}

func (Proc[Pp, P, X, I]) AsInputType() reflect.Type { return nil }

func (p Proc[Pp, P, X, I]) Instantiate(context.Context, bool) (workflow.Instance, error) {
	r := &procRun[Pp, P, X, I]{proc: p}

	// workflow calls In with exactly the ins InTypes declares, so a step
	// without an in needs an instance whose In takes nothing.
	if takesNoIn[X]() {
		return noInInstance[Pp, P, X, I]{r}, nil
	}

	return inInstance[Pp, P, X, I]{r}, nil
}

func (Proc[Pp, P, X, I]) CleanUp(context.Context) error { return nil }

func takesNoIn[X any]() bool { return reflect.TypeFor[X]() == reflect.TypeFor[None]() }

type procRun[Pp ParamPartial[Pp, P], P, X, I any] struct {
	proc  Proc[Pp, P, X, I]
	param P
	in    X
	out   *overlayfs.Fs
}

type inInstance[Pp ParamPartial[Pp, P], P, X, I any] struct {
	*procRun[Pp, P, X, I]
}

func (i inInstance[Pp, P, X, I]) In(in X) { i.in = in }

type noInInstance[Pp ParamPartial[Pp, P], P, X, I any] struct {
	*procRun[Pp, P, X, I]
}

func (noInInstance[Pp, P, X, I]) In() {}

func (r *procRun[Pp, P, X, I]) Param(partial Pp) error {
	p := r.proc.Default().Merge(partial).ToValue()

	if r.proc.Check != nil {
		if err := r.proc.Check(p); err != nil {
			return err
		}
	}

	r.param = p

	return nil
}

func (r *procRun[Pp, P, X, I]) Out(f *overlayfs.Fs) { r.out = f }

func (r *procRun[Pp, P, X, I]) Run(ctx context.Context, rep workflow.Reporter) error {
	in, scope, err := r.proc.Input(ctx, r.param, r.in, r.out)
	if err != nil {
		return err
	}

	return Drive(ctx, r.proc.Generator, r.env(rep), in, scope, r.out)
}

func (r *procRun[Pp, P, X, I]) Files(ctx context.Context, rep workflow.Reporter) ([]File, error) {
	in, _, err := r.proc.Input(ctx, r.param, r.in, r.out)
	if err != nil {
		return nil, err
	}

	return Assemble(ctx, r.proc.Generator, r.env(rep), in)
}

func (r *procRun[Pp, P, X, I]) env(rep workflow.Reporter) Env {
	now, reg := time.Now, DefaultRegistry
	if r.proc.Now != nil {
		now = r.proc.Now
	}

	if r.proc.Registry != nil {
		reg = r.proc.Registry
	}

	return Env{Reporter: rep, Now: now(), Registry: reg, Out: r.out.FS()}
}
