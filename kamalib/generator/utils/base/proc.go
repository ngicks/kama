package base

import (
	"context"
	"fmt"
	"io/fs"
	"reflect"

	"github.com/ngicks/option/opt"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

// SourceProc is the workflow proc of a generator that generates from the
// packages its in loaded and stages the files in its out. The packages
// must lie under the root of its out. The option file and the package
// option files are read from its out, so a file staged by an earlier step
// is read too. A file of its out that would change in the generated: line
// of its header only is not staged. The proc reports the warnings of
// Generate and the number of files written and unchanged.
//
// Pp is the partial variant of the param P that a spec decodes into. O is
// the options of Generate, and L is what Loader loads. Its instances have
// the methods below. Generate returns the files Run stages, with paths
// relative to the root of the out, and stages nothing.
//
//	Param(Pp) error
//	In(*gopackages.Packages)
//	Out(*overlayfs.Fs)
//	Generate() ([]File, []error, error)
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

// ParamPartial is the partial variant Pp of a param P, as partial
// generates it.
type ParamPartial[Pp, P any] interface {
	Merge(r Pp) Pp
	ToValue() P
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

func (SourceProc[Pp, P, O, L]) ParamType() reflect.Type { return reflect.TypeFor[Pp]() }

func (SourceProc[Pp, P, O, L]) InTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*gopackages.Packages]()}
}

func (SourceProc[Pp, P, O, L]) OutTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*overlayfs.Fs]()}
}

func (SourceProc[Pp, P, O, L]) AsInputType() reflect.Type { return nil }

func (p SourceProc[Pp, P, O, L]) Instantiate(context.Context, bool) (workflow.Instance, error) {
	return &sourceInstance[Pp, P, O, L]{proc: p}, nil
}

func (SourceProc[Pp, P, O, L]) CleanUp(context.Context) error { return nil }

type sourceInstance[Pp ParamPartial[Pp, P], P, O, L any] struct {
	proc       SourceProc[Pp, P, O, L]
	opts       O
	optionFile *fileloader.PathInput
	pkgs       *gopackages.Packages
	fs         *overlayfs.Fs
}

func (i *sourceInstance[Pp, P, O, L]) Param(partial Pp) error {
	opts, optionFile, err := i.proc.Options(i.proc.Default().Merge(partial).ToValue())
	if err != nil {
		return err
	}

	i.opts = opts

	if optionFile.IsSome() {
		in, err := optionFile.Value().Normalize()
		if err != nil {
			return fmt.Errorf("option_file: %w", err)
		}

		i.optionFile = &in
	}

	return nil
}

func (i *sourceInstance[Pp, P, O, L]) In(pkgs *gopackages.Packages) { i.pkgs = pkgs }

func (i *sourceInstance[Pp, P, O, L]) Out(f *overlayfs.Fs) { i.fs = f }

func (i *sourceInstance[Pp, P, O, L]) Run(ctx context.Context, r workflow.Reporter) error {
	files, warnings, err := i.Generate()
	if err != nil {
		return err
	}

	return StageWithWarnings(ctx, i.fs, files, warnings, r)
}

func (i *sourceInstance[Pp, P, O, L]) Generate() ([]File, []error, error) {
	loaded, err := i.proc.Loader.LoadOptionNamed(
		i.fs.FS(), i.fs.Root(), SourcePackages(i.pkgs.Pkgs), i.optionFile,
	)
	if err != nil {
		if i.proc.LoadError != nil {
			err = i.proc.LoadError(err)
		}

		return nil, nil, err
	}

	files, warnings, err := i.proc.Generate(i.pkgs.Pkgs, i.opts, loaded)
	if err != nil {
		return nil, nil, err
	}

	files, err = RelFiles(i.fs.Root(), files)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", i.proc.Id, err)
	}

	return files, warnings, nil
}
