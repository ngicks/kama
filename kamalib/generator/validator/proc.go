package validator

import (
	"context"
	"fmt"
	"reflect"

	"github.com/ngicks/option/opt"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() { workflow.Register("validate.generate", NewProc) }

// ProcParam configures Proc. A spec decodes into ProcParamPartial, which
// Proc merges onto DefaultProcParam.
//
//kama:partial
type ProcParam struct {
	// OptionFile names the option file, its base path relative to the root
	// of the out of Proc and its formats, under the lowercase keys base and
	// formats. Empty formats mean yaml, json, toml. None means no option
	// file. A named file that is not present is an error.
	OptionFile opt.Option[fileloader.PathInput] `json:"option_file"`
}

// DefaultProcParam returns the values a ProcParam takes for the keys a spec
// leaves out.
func DefaultProcParam() ProcParamPartial {
	return ProcParamPartial{}
}

// Proc generates the Validate methods of the packages its in loaded and
// stages the files in its out. The packages must lie under the root of its
// out. The option file and the package option files are read from its out,
// so a file staged by an earlier step is read too. A package option file
// is the file //kama:option-file validate=<path> names, or else
// kama_validate.yaml, .yml, .json or .toml in the package directory when
// present. A file of its out that would change in the generated: line of
// its header only is not staged. Proc reports the warnings of Generate and
// the number of files written and unchanged.
//
// Registry holds the validators the directives name. It is
// DefaultRegistry() when nil. NewProcWith returns constructors of a Proc
// with a registry for a workflow.Registry.
//
//	Param(ProcParamPartial) error
//	In(*gopackages.Packages)
//	Out(*overlayfs.Fs)
type Proc struct {
	Registry *Registry
}

// NewProc returns a new Proc with the default registry. It is the
// constructor of the kind validate.generate in workflow.DefaultRegistry.
func NewProc() workflow.Proc { return Proc{} }

// NewProcWith returns a constructor of Procs generating with the validators
// of reg, for workflow.Registry.Register. It keeps a clone of reg, so later
// changes to reg do not reach the Procs. The Procs only read the clone, so
// the graphs built with them share nothing they change. A nil reg means
// DefaultRegistry().
func NewProcWith(reg *Registry) func() workflow.Proc {
	if reg != nil {
		reg = reg.Clone()
	}

	return func() workflow.Proc { return Proc{Registry: reg} }
}

func (Proc) ParamType() reflect.Type { return reflect.TypeFor[ProcParamPartial]() }

func (Proc) InTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*gopackages.Packages]()}
}

func (Proc) OutTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*overlayfs.Fs]()}
}

func (Proc) AsInputType() reflect.Type { return nil }

func (p Proc) Instantiate(context.Context, bool) (workflow.Instance, error) {
	return &instance{registry: p.Registry}, nil
}

func (Proc) CleanUp(context.Context) error { return nil }

type instance struct {
	registry   *Registry
	optionFile *fileloader.PathInput
	pkgs       *gopackages.Packages
	fs         *overlayfs.Fs
}

func (i *instance) Param(partial ProcParamPartial) error {
	p := DefaultProcParam().Merge(partial).ToValue()

	if p.OptionFile.IsSome() {
		in, err := genopt.OptionFileParam(p.OptionFile.Value())
		if err != nil {
			return err
		}

		i.optionFile = &in
	}

	return nil
}

func (i *instance) In(pkgs *gopackages.Packages) { i.pkgs = pkgs }

func (i *instance) Out(f *overlayfs.Fs) { i.fs = f }

func (i *instance) Run(ctx context.Context, r workflow.Reporter) error {
	files, warnings, err := i.generate()
	if err != nil {
		return err
	}

	return base.StageWithWarnings(ctx, i.fs, files, warnings, r)
}

// generate runs Generate with the option file of i and the package option
// files, and returns the files with paths relative to the root of the out
// of i.
func (i *instance) generate() ([]File, []error, error) {
	loaded, err := NewLoader(nil).LoadOptionNamed(
		i.fs.FS(), i.fs.Root(), base.SourcePackages(i.pkgs.Pkgs), i.optionFile,
	)
	if err != nil {
		return nil, nil, loadError(err)
	}

	files, warnings, err := Generate(i.pkgs.Pkgs, Options{Registry: i.registry, Loaded: loaded})
	if err != nil {
		return nil, nil, err
	}

	files, err = base.RelFiles(i.fs.Root(), files)
	if err != nil {
		return nil, nil, fmt.Errorf("validator: %w", err)
	}

	return files, warnings, nil
}
