package partial

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"

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
	// of the out of Proc and its formats, as base and formats. Empty
	// formats mean yaml, json, toml. Nil means no option file. A named file
	// that is not present is an error.
	OptionFile *fileloader.PathInput `json:"option_file"`
}

// Proc generates the partial types of the packages its in loaded and
// stages the files in its out. The packages must lie under the root of
// its out. The option file is read from its out, so a file staged by an
// earlier step is read too. A file of its out that would change in the
// generated: line of its header only is staged unchanged. Proc reports the
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
	opts := i.opts
	if i.optionFile != nil {
		f, err := LoadOptionFile(i.fs.FS(), *i.optionFile)
		if err != nil {
			return err
		}
		opts.OptionFile = f
	}

	files, warnings, err := Generate(i.pkgs.Pkgs, opts)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		r.Warn(w)
	}

	var written, kept int
	for _, f := range files {
		rel, err := filepath.Rel(i.fs.Root(), f.Path)
		if err != nil {
			return fmt.Errorf("partial: %w", err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("partial: %q is outside %q", f.Path, i.fs.Root())
		}
		changed, err := i.write(ctx, filepath.ToSlash(rel), f.Content)
		if err != nil {
			return err
		}
		if changed {
			written++
		} else {
			kept++
		}
	}
	r.Progress(fmt.Sprintf("%d files written, %d unchanged", written, kept))
	return nil
}

// write stages content at name and reports whether it changed the file.
// The existing file is staged again in place of content when the two
// differ in the generated: line only.
//
// Generate returns unformatted content, while the file in fs is formatted
// by fs.Write. Comparing the staged result, rather than content, keeps the
// comparison right for any formatter fs runs.
func (i *instance) write(ctx context.Context, name string, content []byte) (bool, error) {
	existing, err := fs.ReadFile(i.fs.FS(), name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("partial: read %q: %w", name, err)
	}
	if err := i.fs.Write(ctx, name, content); err != nil {
		return false, fmt.Errorf("partial: write %q: %w", name, err)
	}
	if existing == nil {
		return true, nil
	}
	staged, err := fs.ReadFile(i.fs.FS(), name)
	if err != nil {
		return false, fmt.Errorf("partial: read %q: %w", name, err)
	}
	if !SameExceptGenerated(existing, staged) {
		return true, nil
	}
	if !bytes.Equal(existing, staged) {
		if err := i.fs.Write(ctx, name, existing); err != nil {
			return false, fmt.Errorf("partial: write %q: %w", name, err)
		}
	}
	return false, nil
}
