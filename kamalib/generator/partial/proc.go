package partial

import (
	"context"
	"fmt"

	"github.com/ngicks/kama/kamalib/generator/base"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() { workflow.Register("partial.generate", NewProc) }

// ProcParam configures the proc of NewProc. See Options for the modes.
type ProcParam struct {
	// Merge is "overwrite" or "append". Empty means "overwrite".
	Merge string `json:"merge"`
	// MergeNested is "recursive" or "overwrite". Empty means "recursive".
	MergeNested string `json:"merge_nested"`
	// OptionFile names the option file, its base path relative to the root
	// of the out of the proc and its formats, under the lowercase keys base and
	// formats. Empty formats mean yaml, json, toml. Nil means no option
	// file. A named file that is not present is an error.
	OptionFile *fileloader.PathInput `json:"option_file"`
}

// NewProc returns the proc generating the partial types of the packages its
// in loaded and staging the files in its out. The packages must lie under
// the root of its out. The option file is read from its out, so a file
// staged by an earlier step is read too. A file of its out that would
// change in the generated: line of its header only is not staged. The proc
// reports the warnings of Generate and the number of files written and
// unchanged.
//
//	Param(ProcParam) error
//	In(*gopackages.Packages)
//	Out(*overlayfs.Fs)
func NewProc() workflow.Proc { return base.NewProc(newGenerator)() }

// generator runs Generate as a step.
type generator struct {
	opts       Options
	optionFile *fileloader.PathInput
}

func newGenerator() base.Generator[ProcParam, *gopackages.Packages] { return &generator{} }

func (g *generator) Configure(p ProcParam) error {
	opts := Options{Merge: MergeMode(p.Merge), MergeNested: MergeNestedMode(p.MergeNested)}
	if err := opts.Validate(); err != nil {
		return err
	}
	g.opts = opts.WithDefaults()

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
		// Candidates reports unknown formats before Generate opens any file.
		if _, err := in.Candidates(); err != nil {
			return fmt.Errorf("%w: option_file: %w", ErrOption, err)
		}
		g.optionFile = &in
	}
	return nil
}

func (g *generator) Generate(
	_ context.Context,
	pkgs *gopackages.Packages,
	out *overlayfs.Fs,
) ([]File, []error, error) {
	opts := g.opts
	if g.optionFile != nil {
		f, err := LoadOptionFile(out.FS(), *g.optionFile)
		if err != nil {
			return nil, nil, err
		}
		opts.OptionFile = f
	}
	files, warnings, err := Generate(pkgs.Pkgs, opts)
	if err != nil {
		return nil, nil, err
	}
	files, err = base.RelFiles(out.Root(), files)
	if err != nil {
		return nil, nil, fmt.Errorf("partial: %w", err)
	}
	return files, warnings, nil
}
