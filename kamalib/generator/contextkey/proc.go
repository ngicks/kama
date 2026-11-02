package contextkey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"reflect"
	"slices"

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
// out.
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

func (i *instance) Run(context.Context) error {
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

	for _, name := range slices.Sorted(maps.Keys(files)) {
		if err := i.fs.Write(path.Join(i.dir, name), files[name]); err != nil {
			return fmt.Errorf("contextkey: write %q: %w", name, err)
		}
	}

	return nil
}

// decodeTemplate converts the format-neutral data fileloader decodes into
// a Template.
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
