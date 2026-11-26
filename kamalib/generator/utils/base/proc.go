package base

import (
	"context"
	"fmt"
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
// Partial is the partial variant of the param Param that a step decodes
// into, such as the type partial generates or a [ParamOf]. X is the type
// of the in of a step, and None for a step without one. I is the input of
// the generator. Its instances have the methods below, with In() for X
// None:
//
//	Param(Partial) error
//	In(X)
//	Out(*overlayfs.Fs)
//	Files(context.Context, workflow.Reporter) ([]File, error)
//
// Files returns the files Run would stage, as [Prepare] returns them, and
// stages nothing.
//
// With TakesOptional set, the instances also have
//
//	Optional(map[string]any)
//
// and the run finds the optional inputs of its step in [Env].Optional.
//
// Generator.Generate and Input are required: Instantiate returns an error
// when one is nil.
type Proc[Partial ParamPartial[Partial, Param], Param, X, I any] struct {
	Generator Generator[I]
	// Default returns the values Param takes for the keys a step leaves out.
	// Nil means the zero Partial, which is the default of a ParamOf.
	Default func() Partial
	// Check reports a param the proc cannot run with. Param calls it, in a
	// dry run too, so it must not touch files. Nil accepts every param.
	Check func(param Param) error
	// Input returns the input of a run and the scope of its stale check.
	Input func(ctx context.Context, param Param, in X, out *overlayfs.Fs) (I, Scope, error)
	// Now returns the time of a run. Nil means time.Now.
	Now func() time.Time
	// Registry tells the generators that generated marks name. Nil means
	// DefaultRegistry().
	Registry *genmark.Registry
	// TakesOptional makes the proc take optional inputs of any type, as
	// workflow.OptionalTaker describes.
	TakesOptional bool
}

// None is the in of a Proc whose step takes no in.
type None struct{}

// ParamPartial is the partial variant Partial of a param Param, as partial
// generates it. A proc calls the methods on Partial values, so they have
// value receivers. Merge returns the receiver with the keys r sets taken
// from r: r wins. ToValue returns the param the partial variant
// describes.
type ParamPartial[Partial, Param any] interface {
	Merge(r Partial) Partial
	ToValue() Param
}

func (Proc[Partial, Param, X, I]) ParamType() reflect.Type { return reflect.TypeFor[Partial]() }

func (Proc[Partial, Param, X, I]) InTypes() []reflect.Type {
	if takesNoIn[X]() {
		return nil
	}

	return []reflect.Type{reflect.TypeFor[X]()}
}

func (Proc[Partial, Param, X, I]) OutTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*overlayfs.Fs]()}
}

func (Proc[Partial, Param, X, I]) AsInputType() reflect.Type { return nil }

// OptionalType implements workflow.OptionalTaker. It is nil unless
// TakesOptional is set.
func (p Proc[Partial, Param, X, I]) OptionalType() reflect.Type {
	if !p.TakesOptional {
		return nil
	}

	return reflect.TypeFor[any]()
}

func (p Proc[Partial, Param, X, I]) Instantiate(context.Context, bool) (workflow.Instance, error) {
	switch {
	case p.Generator.Generate == nil:
		return nil, fmt.Errorf("base: proc of %s: Generator.Generate is nil", p.Generator.Id)
	case p.Input == nil:
		return nil, fmt.Errorf("base: proc of %s: Input is nil", p.Generator.Id)
	}

	r := &procRun[Partial, Param, X, I]{proc: p}

	// workflow calls In with exactly the ins InTypes declares, so a step
	// without an in needs an instance whose In takes nothing.
	if takesNoIn[X]() {
		return noInInstance[Partial, Param, X, I]{r}, nil
	}

	return inInstance[Partial, Param, X, I]{r}, nil
}

func (Proc[Partial, Param, X, I]) CleanUp(context.Context) error { return nil }

func takesNoIn[X any]() bool { return reflect.TypeFor[X]() == reflect.TypeFor[None]() }

type procRun[Partial ParamPartial[Partial, Param], Param, X, I any] struct {
	proc  Proc[Partial, Param, X, I]
	param Param
	in    X
	out   *overlayfs.Fs
	// optional is nil unless the proc takes optional inputs.
	optional map[string]any
}

type inInstance[Partial ParamPartial[Partial, Param], Param, X, I any] struct {
	*procRun[Partial, Param, X, I]
}

func (i inInstance[Partial, Param, X, I]) In(in X) { i.in = in }

type noInInstance[Partial ParamPartial[Partial, Param], Param, X, I any] struct {
	*procRun[Partial, Param, X, I]
}

func (noInInstance[Partial, Param, X, I]) In() {}

func (r *procRun[Partial, Param, X, I]) Param(partial Partial) error {
	var def Partial
	if r.proc.Default != nil {
		def = r.proc.Default()
	}

	p := def.Merge(partial).ToValue()

	if r.proc.Check != nil {
		if err := r.proc.Check(p); err != nil {
			return err
		}
	}

	r.param = p

	return nil
}

func (r *procRun[Partial, Param, X, I]) Out(f *overlayfs.Fs) { r.out = f }

func (r *procRun[Partial, Param, X, I]) Optional(in map[string]any) { r.optional = in }

func (r *procRun[Partial, Param, X, I]) Run(ctx context.Context, rep workflow.Reporter) error {
	in, scope, err := r.proc.Input(ctx, r.param, r.in, r.out)
	if err != nil {
		return err
	}

	return Drive(ctx, r.proc.Generator, r.env(rep), in, scope, r.out)
}

func (r *procRun[Partial, Param, X, I]) Files(
	ctx context.Context,
	rep workflow.Reporter,
) ([]File, error) {
	in, scope, err := r.proc.Input(ctx, r.param, r.in, r.out)
	if err != nil {
		return nil, err
	}

	plan, err := Prepare(ctx, r.proc.Generator, r.env(rep), in, scope, r.out)
	if err != nil {
		return nil, err
	}

	return plan.Files, nil
}

func (r *procRun[Partial, Param, X, I]) env(rep workflow.Reporter) Env {
	now, reg := time.Now, defaultRegistry
	if r.proc.Now != nil {
		now = r.proc.Now
	}

	if r.proc.Registry != nil {
		reg = r.proc.Registry
	}

	return Env{
		Reporter: rep,
		Now:      now(),
		Registry: reg,
		Out:      r.out.FS(),
		Optional: r.optional,
	}
}
