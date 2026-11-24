package base

import (
	"context"
	"fmt"
	"io/fs"
	"iter"
	"path/filepath"
	"reflect"
	"time"

	"github.com/ngicks/option/opt"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

// Input is the input of a generator generating from loaded packages, as
// [SourceProc] builds it. O is the options a step gives the generator, and
// P, T and F are its package, type and field options.
type Input[O, P, T, F any] struct {
	// Packages are the packages the run reads, as SourcePackages picks
	// them.
	Packages []*packages.Package
	// Loaded holds the options of the directives of Packages overlaid with
	// those of the option files.
	Loaded  *genopt.Loaded[P, T, F]
	Options O
	// Root is the absolute directory of the root of the output.
	Root string
}

// Rel returns abs, an absolute path under Root, as the path of an output:
// slash-separated and relative to Root. A path outside Root is an error.
func (in Input[O, P, T, F]) Rel(abs string) (string, error) {
	rel, err := filepath.Rel(in.Root, abs)
	if err != nil {
		return "", fmt.Errorf("base: %w", err)
	}

	if !inside(rel) {
		return "", fmt.Errorf("base: %q is outside %q", abs, in.Root)
	}

	return filepath.ToSlash(rel), nil
}

// Scope returns the scope of the stale check of a run on in: the files of
// Packages that lie under Root. Files of a build variant the load left out
// are not among them, since whether such a file is stale cannot be told
// from the run.
func (in Input[O, P, T, F]) Scope() Scope {
	var out []string

	for _, pkg := range in.Packages {
		for _, f := range pkg.Syntax {
			tf := pkg.Fset.File(f.Pos())
			if tf == nil {
				continue
			}

			if rel, err := in.Rel(tf.Name()); err == nil {
				out = append(out, rel)
			}
		}
	}

	return Scope{Files: out}
}

// SourceProc is the workflow proc of a generator that generates from the
// packages its in loaded and stages the files in its out. It is a [Proc]
// whose in is the loaded packages. The packages must lie under the root of
// its out. The option file and the package option files are read from its
// out, so a file staged by an earlier step is read too.
//
// The proc reports the warnings of loading the options before it runs the
// generator. The scope of its stale check is the files of the packages the
// run reads.
//
// Partial is the partial variant of the param Param that a spec decodes
// into. O, P, T and F are those of the [Input] of the generator.
//
// Default, Options and Loader are required: Instantiate returns an error
// when one is nil.
type SourceProc[Partial ParamPartial[Partial, Param], Param, O, P, T, F any] struct {
	Generator Generator[Input[O, P, T, F]]
	// Default returns the values Param takes for the keys a spec leaves out.
	Default func() Partial
	// Options returns the options of a param and the option file it names.
	// An error rejects the param.
	Options func(param Param) (O, opt.Option[fileloader.PathInput], error)
	// Loader loads the directives and the option files of the packages.
	Loader OptionLoader[P, T, F]
	// LoadError wraps an error of Loader. Nil leaves it as it is.
	LoadError func(err error) error
	// Now returns the time of a run. Nil means time.Now.
	Now func() time.Time
	// Registry tells the generators that generated marks name. Nil means
	// DefaultRegistry().
	Registry *genmark.Registry
}

// OptionLoader loads the options of a generator. genopt.Loader implements
// it.
type OptionLoader[P, T, F any] interface {
	LoadOptionNamed(
		fsys fs.FS,
		root string,
		pkgs []*packages.Package,
		optionFile *fileloader.PathInput,
	) (*genopt.Loaded[P, T, F], error)
}

func (p SourceProc[Partial, Param, O, P, T, F]) ParamType() reflect.Type {
	return p.proc().ParamType()
}

func (p SourceProc[Partial, Param, O, P, T, F]) InTypes() []reflect.Type {
	return p.proc().InTypes()
}

func (p SourceProc[Partial, Param, O, P, T, F]) OutTypes() []reflect.Type {
	return p.proc().OutTypes()
}

func (p SourceProc[Partial, Param, O, P, T, F]) AsInputType() reflect.Type {
	return p.proc().AsInputType()
}

func (p SourceProc[Partial, Param, O, P, T, F]) Instantiate(
	ctx context.Context,
	dryRun bool,
) (workflow.Instance, error) {
	switch {
	case p.Options == nil:
		return nil, fmt.Errorf("base: proc of %s: Options is nil", p.Generator.Id)
	case p.Loader == nil:
		return nil, fmt.Errorf("base: proc of %s: Loader is nil", p.Generator.Id)
	}

	return p.proc().Instantiate(ctx, dryRun)
}

func (p SourceProc[Partial, Param, O, P, T, F]) CleanUp(ctx context.Context) error {
	return p.proc().CleanUp(ctx)
}

func (p SourceProc[Partial, Param, O, P, T, F]) proc() Proc[
	Partial, Param, *gopackages.Packages, Input[O, P, T, F],
] {
	return Proc[Partial, Param, *gopackages.Packages, Input[O, P, T, F]]{
		Generator: Generator[Input[O, P, T, F]]{Id: p.Generator.Id, Generate: p.generate},
		Default:   p.Default,
		Check: func(param Param) error {
			_, _, err := p.options(param)
			return err
		},
		Input:    p.input,
		Now:      p.Now,
		Registry: p.Registry,
	}
}

// options returns the options of param and its option file, normalized.
func (p SourceProc[Partial, Param, O, P, T, F]) options(
	param Param,
) (O, *fileloader.PathInput, error) {
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

func (p SourceProc[Partial, Param, O, P, T, F]) input(
	_ context.Context,
	param Param,
	pkgs *gopackages.Packages,
	out *overlayfs.Fs,
) (Input[O, P, T, F], Scope, error) {
	opts, optionFile, err := p.options(param)
	if err != nil {
		return Input[O, P, T, F]{}, Scope{}, err
	}

	srcPkgs := SourcePackages(pkgs.Pkgs)

	loaded, err := p.Loader.LoadOptionNamed(out.FS(), out.Root(), srcPkgs, optionFile)
	if err != nil {
		if p.LoadError != nil {
			err = p.LoadError(err)
		}

		return Input[O, P, T, F]{}, Scope{}, err
	}

	if loaded == nil {
		return Input[O, P, T, F]{}, Scope{}, fmt.Errorf(
			"base: %s: the option loader returned no options and no error", p.Generator.Id,
		)
	}

	in := Input[O, P, T, F]{Packages: srcPkgs, Loaded: loaded, Options: opts, Root: out.Root()}

	return in, in.Scope(), nil
}

// generate reports the warnings of loading the options and runs the
// generator.
func (p SourceProc[Partial, Param, O, P, T, F]) generate(
	ctx context.Context,
	env Env,
	in Input[O, P, T, F],
) iter.Seq2[Output, error] {
	for _, w := range in.Loaded.Warnings {
		env.Reporter.Warn(w)
	}

	return p.Generator.Generate(ctx, env, in)
}
