package partial

import (
	"context"
	"fmt"
	"reflect"

	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() { workflow.Register("partial.generate", NewProc) }

// ProcParam configures Proc. See Options for the modes. A spec decodes into
// ProcParamPartial, which Proc merges onto DefaultProcParam.
//
//kama:partial
type ProcParam struct {
	// MergeStrategy is "overwrite" or "append". The partial variant has a
	// Merge method, so the field cannot take that name.
	MergeStrategy string `json:"merge"`
	// MergeNestedStrategy is "recursive" or "overwrite".
	MergeNestedStrategy string `json:"merge_nested"`
	// OptionFile names the option file, its base path relative to the root
	// of the out of Proc and its formats, under the lowercase keys base and
	// formats. Empty formats mean yaml, json, toml. None means no option
	// file. A named file that is not present is an error.
	OptionFile opt.Option[fileloader.PathInput] `json:"option_file"`
	// FlagSet tells whether Proc writes the flag set file of the packages it
	// generates into. A package needs the file when one of its targets has
	// a flag option, in any build variant. Turning it off with such a target
	// loaded is an error.
	FlagSet bool `json:"flag_set"`
}

// DefaultProcParam returns the values a ProcParam takes for the keys a spec
// leaves out.
func DefaultProcParam() ProcParamPartial {
	return ProcParamPartial{
		MergeStrategy:       und.Defined(string(MergeOverwrite)),
		MergeNestedStrategy: und.Defined(string(MergeNestedRecursive)),
		FlagSet:             und.Defined(true),
	}
}

// Proc generates the partial types of the packages its in loaded and
// stages the files in its out. The packages must lie under the root of
// its out. The option file and the package option files are read from its
// out, so a file staged by an earlier step is read too. A package option
// file is the file //kama:option-file partial=<path> names, or else
// kama_partial.yaml, .yml, .json or .toml in the package directory when
// present. A file of its out that would change in the
// generated: line of its header only is not staged. Proc reports the
// warnings of Generate and the number of files written and unchanged.
//
//	Param(ProcParamPartial) error
//	In(*gopackages.Packages)
//	Out(*overlayfs.Fs)
type Proc struct{}

// NewProc returns a new Proc.
func NewProc() workflow.Proc { return Proc{} }

func (Proc) ParamType() reflect.Type { return reflect.TypeFor[ProcParamPartial]() }

func (Proc) InTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*gopackages.Packages]()}
}

func (Proc) OutTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*overlayfs.Fs]()}
}

func (Proc) AsInputType() reflect.Type { return nil }

func (Proc) Instantiate(context.Context, bool) (workflow.Instance, error) {
	return &instance{}, nil
}

func (Proc) CleanUp(context.Context) error { return nil }

type instance struct {
	opts       Options
	optionFile *fileloader.PathInput
	pkgs       *gopackages.Packages
	fs         *overlayfs.Fs
}

func (i *instance) Param(partial ProcParamPartial) error {
	p := DefaultProcParam().Merge(partial).ToValue()

	opts := Options{
		Merge:       MergeMode(p.MergeStrategy),
		MergeNested: MergeNestedMode(p.MergeNestedStrategy),
		OmitFlagSet: !p.FlagSet,
	}
	if err := opts.Validate(); err != nil {
		return err
	}

	i.opts = opts.WithDefaults()

	if p.OptionFile.IsSome() {
		in := p.OptionFile.Value()
		if in.Base == "" {
			return fmt.Errorf("%w: option_file: base is required", ErrOption)
		}

		if len(in.Formats) == 0 {
			in.Formats = []fileloader.Format{
				fileloader.FormatYaml,
				fileloader.FormatJson,
				fileloader.FormatToml,
			}
		}
		// Candidates reports unknown formats before Run opens any file.
		if _, err := in.Candidates(); err != nil {
			return fmt.Errorf("%w: option_file: %w", ErrOption, err)
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

// generate runs Generate with the options of i, its option file and the
// package option files, and returns the files with paths relative to the
// root of the out of i.
func (i *instance) generate() ([]File, []error, error) {
	var optionFile fileloader.Input
	if i.optionFile != nil {
		optionFile = *i.optionFile
	}

	loaded, err := NewLoader(optionFile).LoadOption(
		i.fs.FS(), i.fs.Root(), base.SourcePackages(i.pkgs.Pkgs),
	)
	if err != nil {
		return nil, nil, loadError(err)
	}

	if i.optionFile != nil && len(loaded.Files) == 0 {
		return nil, nil, fmt.Errorf(
			"%w: %w: %q", ErrOptionFile, fileloader.ErrNotFound, i.optionFile.Base,
		)
	}

	opts := i.opts
	opts.Loaded = loaded

	files, warnings, err := Generate(i.pkgs.Pkgs, opts)
	if err != nil {
		return nil, nil, err
	}

	files, err = base.RelFiles(i.fs.Root(), files)
	if err != nil {
		return nil, nil, fmt.Errorf("partial: %w", err)
	}

	return files, warnings, nil
}
