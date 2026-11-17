package contextkey

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"reflect"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/base"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/proc/fileloader"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() { workflow.Register("contextkey.generate", NewProc) }

var ErrNoEntries = errors.New("contextkey: no entries")

// ProcParam configures Proc.
type ProcParam struct {
	// Dir is the output directory, relative to the project root.
	Dir string `json:"dir"`
	// Package is the package clause of generated files.
	Package string `json:"package"`
	// KeyTypeFunc names the function in Package that creates keys. See
	// Options.KeyTypeFunc.
	KeyTypeFunc string `json:"key_type_func"`
}

// Proc renders the Template its in decodes to and stages the files in its
// out. A file of its out that would change in the generated: line of its
// header only is not staged. Proc reports a warning for each stale file of
// its output directory, and the number of files written and unchanged.
//
//	Param(ProcParam) error
//	In(*fileloader.Document)
//	Out(*overlayfs.Fs)
type Proc struct{}

// NewProc returns a new Proc.
func NewProc() workflow.Proc { return Proc{} }

func (Proc) ParamType() reflect.Type { return reflect.TypeFor[ProcParam]() }

func (Proc) InTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*fileloader.Document]()}
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
	dir  string
	opts Options
	doc  *fileloader.Document
	fs   *overlayfs.Fs
}

func (i *instance) Param(p ProcParam) error {
	if p.Dir == "" {
		p.Dir = "."
	}

	if !fs.ValidPath(p.Dir) {
		return fmt.Errorf("contextkey: invalid dir %q", p.Dir)
	}

	opts := Options{Package: p.Package, KeyTypeFunc: p.KeyTypeFunc}
	if err := opts.validate(); err != nil {
		return err
	}

	i.dir, i.opts = p.Dir, opts

	return nil
}

func (i *instance) In(doc *fileloader.Document) { i.doc = doc }

func (i *instance) Out(f *overlayfs.Fs) { i.fs = f }

func (i *instance) Run(ctx context.Context, r workflow.Reporter) error {
	tmpl, err := decodeTemplate(i.doc.Data)
	if err != nil {
		return fmt.Errorf("contextkey: %s: %w", i.doc.Path, err)
	}

	if len(tmpl.Entries) == 0 {
		return fmt.Errorf("%w: %s", ErrNoEntries, i.doc.Path)
	}

	files, err := Render(tmpl.Entries, i.opts)
	if err != nil {
		return fmt.Errorf("%s: %w", i.doc.Path, err)
	}

	inDir := func(name string, src []byte) base.File {
		return base.File{Path: path.Join(i.dir, name), Content: src}
	}

	produced := opiter.WrapSeq2(
		opiter.MapsSorted(files),
	).
		Assemble(inDir).
		Collect()

	opts := i.opts.withDefaults()
	stale := base.StaleCheck{Registry: opts.Registry, Generator: GeneratorName}

	warnings, err := stale.Dir(i.fs.FS(), i.dir, produced)
	if err != nil {
		return fmt.Errorf("contextkey: %w", err)
	}

	for _, w := range warnings {
		r.Warn(w)
	}

	return base.Stage(ctx, i.fs, produced, r)
}

// decodeTemplate converts the format-neutral data fileloader decodes into
// a Template with encoding/json/v2. Keys match the JSON names of the fields
// of Template and Entry case-sensitively. Unknown keys are ignored.
func decodeTemplate(data any) (Template, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return Template{}, err
	}

	var tmpl Template
	if err := json.Unmarshal(b, &tmpl); err != nil {
		return Template{}, err
	}

	return tmpl, nil
}
