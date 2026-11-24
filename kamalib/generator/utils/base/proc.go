package base

import (
	"context"
	"fmt"
	"io/fs"
	"iter"
	"reflect"
	"time"

	"github.com/ngicks/option/opt"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
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

// SourceProc is the workflow proc of a generator that generates from the
// packages its in loaded and stages the files in its out. It is a [Proc]
// whose in is the loaded packages. The packages must lie under the root of
// its out. The option file and the package option files are read from its
// out, so a file staged by an earlier step is read too. The proc reports
// the warnings of Generate and the number of files written and unchanged.
//
// Pp is the partial variant of the param P that a spec decodes into. O is
// the options of Generate, and L is what Loader loads. Generate checks the
// stale files itself: the proc gives its run no stale check scope.
type SourceProc[Pp ParamPartial[Pp, P], P, O, L any] struct {
	// Id is the generator.
	Id Id
	// Default returns the values P takes for the keys a spec leaves out.
	Default func() Pp
	// Options returns the options of a param and the option file it names.
	// An error rejects the param.
	Options func(param P) (O, opt.Option[fileloader.PathInput], error)
	// Loader loads the directives and the option files of the packages.
	Loader OptionLoader[L]
	// LoadError wraps an error of Loader. Nil leaves it as it is.
	LoadError func(err error) error
	// Generate returns the files generated from pkgs, with absolute paths,
	// and the warnings.
	Generate func(pkgs []*packages.Package, opts O, loaded L) ([]File, []error, error)
}

// OptionLoader loads the options of a generator. genopt.Loader implements
// it.
type OptionLoader[L any] interface {
	LoadOptionNamed(
		fsys fs.FS,
		root string,
		pkgs []*packages.Package,
		optionFile *fileloader.PathInput,
	) (L, error)
}

func (p SourceProc[Pp, P, O, L]) ParamType() reflect.Type { return p.proc().ParamType() }

func (p SourceProc[Pp, P, O, L]) InTypes() []reflect.Type { return p.proc().InTypes() }

func (p SourceProc[Pp, P, O, L]) OutTypes() []reflect.Type { return p.proc().OutTypes() }

func (p SourceProc[Pp, P, O, L]) AsInputType() reflect.Type { return p.proc().AsInputType() }

func (p SourceProc[Pp, P, O, L]) Instantiate(
	ctx context.Context,
	dryRun bool,
) (workflow.Instance, error) {
	return p.proc().Instantiate(ctx, dryRun)
}

func (p SourceProc[Pp, P, O, L]) CleanUp(ctx context.Context) error {
	return p.proc().CleanUp(ctx)
}

// sourceInput is the input of the generator of a SourceProc.
type sourceInput[O, L any] struct {
	pkgs   []*packages.Package
	opts   O
	loaded L
	// root is the root of the out, which the absolute paths of the files of
	// Generate are made relative to.
	root string
}

func (p SourceProc[Pp, P, O, L]) proc() Proc[Pp, P, *gopackages.Packages, sourceInput[O, L]] {
	return Proc[Pp, P, *gopackages.Packages, sourceInput[O, L]]{
		Generator: Generator[sourceInput[O, L]]{Id: p.Id, Generate: p.generate},
		Default:   p.Default,
		Check: func(param P) error {
			_, _, err := p.options(param)
			return err
		},
		Input: p.input,
	}
}

// options returns the options of param and its option file, normalized.
func (p SourceProc[Pp, P, O, L]) options(param P) (O, *fileloader.PathInput, error) {
	opts, optionFile, err := p.Options(param)
	if err != nil {
		return opts, nil, err
	}

	if optionFile.IsNone() {
		return opts, nil, nil
	}

	in, err := optionFile.Value().Normalize()
	if err != nil {
		return opts, nil, fmt.Errorf("option_file: %w", err)
	}

	return opts, &in, nil
}

func (p SourceProc[Pp, P, O, L]) input(
	_ context.Context,
	param P,
	pkgs *gopackages.Packages,
	out *overlayfs.Fs,
) (sourceInput[O, L], Scope, error) {
	opts, optionFile, err := p.options(param)
	if err != nil {
		return sourceInput[O, L]{}, Scope{}, err
	}

	loaded, err := p.Loader.LoadOptionNamed(
		out.FS(), out.Root(), SourcePackages(pkgs.Pkgs), optionFile,
	)
	if err != nil {
		if p.LoadError != nil {
			err = p.LoadError(err)
		}

		return sourceInput[O, L]{}, Scope{}, err
	}

	in := sourceInput[O, L]{pkgs: pkgs.Pkgs, opts: opts, loaded: loaded, root: out.Root()}

	return in, Scope{}, nil
}

// generate yields the files of Generate as Wholes and reports its
// warnings.
func (p SourceProc[Pp, P, O, L]) generate(
	_ context.Context,
	env Env,
	in sourceInput[O, L],
) iter.Seq2[Output, error] {
	return func(yield func(Output, error) bool) {
		files, warnings, err := p.Generate(in.pkgs, in.opts, in.loaded)
		if err != nil {
			yield(nil, err)
			return
		}

		for _, w := range warnings {
			env.Reporter.Warn(w)
		}

		files, err = RelFiles(in.root, files)
		if err != nil {
			yield(nil, fmt.Errorf("%s: %w", p.Id, err))
			return
		}

		for _, f := range files {
			if !yield(Whole(f), nil) {
				return
			}
		}
	}
}
