package contextkey

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"

	"github.com/ngicks/kama/kamalib/generator/base"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/proc/fileloader"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() { workflow.Register("contextkey.generate", NewProc) }

var ErrNoEntries = errors.New("contextkey: no entries")

// ProcParam configures the proc of NewProc.
type ProcParam struct {
	// Dir is the output directory, relative to the project root.
	Dir string `json:"dir"`
	// Package is the package clause of generated files.
	Package string `json:"package"`
	// KeyTypeFunc names the function in Package that creates keys. See
	// Options.KeyTypeFunc.
	KeyTypeFunc string `json:"key_type_func"`
}

// NewProc returns the proc rendering the Template its in decodes to and
// staging the files in its out. A file of its out that would not change is
// not staged. The proc reports the number of files written and unchanged.
//
//	Param(ProcParam) error
//	In(*fileloader.Document)
//	Out(*overlayfs.Fs)
func NewProc() workflow.Proc { return base.NewProc(newGenerator)() }

// generator runs Render as a step.
type generator struct {
	dir  string
	opts Options
}

func newGenerator() base.Generator[ProcParam, *fileloader.Document] { return &generator{} }

func (g *generator) Configure(p ProcParam) error {
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

	g.dir, g.opts = p.Dir, opts
	return nil
}

func (g *generator) Generate(
	_ context.Context,
	doc *fileloader.Document,
	_ *overlayfs.Fs,
) ([]base.File, []error, error) {
	tmpl, err := decodeTemplate(doc.Data)
	if err != nil {
		return nil, nil, fmt.Errorf("contextkey: %s: %w", doc.Path, err)
	}
	if len(tmpl.Entries) == 0 {
		return nil, nil, fmt.Errorf("%w: %s", ErrNoEntries, doc.Path)
	}

	files, err := Render(tmpl.Entries, g.opts)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", doc.Path, err)
	}

	inDir := func(name string, src []byte) base.File {
		return base.File{Path: path.Join(g.dir, name), Content: src}
	}
	return slices.Collect(opiter.Unify(inDir, opiter.MapsSorted(files))), nil, nil
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
