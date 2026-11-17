package partial

import (
	"context"
	"fmt"
	"reflect"

	"github.com/ngicks/kama/kamalib/generator/base"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() { workflow.Register("partial.generate", NewProc) }

// ProcParam configures Proc. See Options for the modes.
type ProcParam struct {
	// Merge is "overwrite" or "append". Empty means "overwrite".
	Merge string `json:"merge"`
	// MergeNested is "recursive" or "overwrite". Empty means "recursive".
	MergeNested string `json:"merge_nested"`
	// OptionFile names the option file, its base path relative to the root
	// of the out of Proc and its formats, under the lowercase keys base and
	// formats. Empty formats mean yaml, json, toml. Nil means no option
	// file. A named file that is not present is an error.
	OptionFile *fileloader.PathInput `json:"option_file"`
}

// Proc generates the partial types of the packages its in loaded and
// stages the files in its out. The packages must lie under the root of
// its out. The option file is read from its out, so a file staged by an
// earlier step is read too. A file of its out that would change in the
// generated: line of its header only is not staged. Proc reports the
// warnings of Generate and the number of files written and unchanged.
//
//	Param(ProcParam) error
//	In(*gopackages.Packages)
//	Out(*overlayfs.Fs)
type Proc struct{}

// NewProc returns a new Proc.
func NewProc() workflow.Proc { return Proc{} }

func (Proc) ParamType() reflect.Type { return reflect.TypeFor[ProcParam]() }

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

func (i *instance) Param(p ProcParam) error {
	opts := Options{Merge: MergeMode(p.Merge), MergeNested: MergeNestedMode(p.MergeNested)}
	if err := opts.Validate(); err != nil {
		return err
	}

	i.opts = opts.WithDefaults()

	if p.OptionFile != nil {
		in := *p.OptionFile
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

	for _, w := range warnings {
		r.Warn(w)
	}

	return base.Stage(ctx, i.fs, files, r)
}

// generate runs Generate with the options of i and its option file, and
// returns the files with paths relative to the root of the out of i.
func (i *instance) generate() ([]File, []error, error) {
	opts := i.opts
	if i.optionFile != nil {
		f, err := LoadOptionFile(i.fs.FS(), *i.optionFile)
		if err != nil {
			return nil, nil, err
		}

		opts.OptionFile = f
	}

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
