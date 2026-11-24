package contextkey

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io/fs"

	"github.com/ngicks/option/und"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
	"github.com/ngicks/kama/kamalib/proc/fileloader"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() { workflow.Register(GeneratorId.ProcKind(), NewProc) }

// ProcParam configures Proc. A spec decodes into ProcParamPartial, which
// Proc merges onto DefaultProcParam.
//
//kama:partial
type ProcParam struct {
	// Dir is the output directory, relative to the project root.
	Dir string `json:"dir"`
	// Package is the package clause of generated files.
	Package string `json:"package"`
	// KeyTypeFunc names the function in Package that creates keys. See
	// Options.KeyTypeFunc.
	KeyTypeFunc string `json:"key_type_func"`
	// Unknown captures the keys of a param that name no field.
	Unknown map[string]any `json:",embed"`
}

// DefaultProcParam returns the values a ProcParam takes for the keys a spec
// leaves out.
func DefaultProcParam() ProcParamPartial {
	return ProcParamPartial{Dir: und.Defined(".")}
}

// UnknownKeys implements workflow.UnknownKeyer.
func (p *ProcParamPartial) UnknownKeys() [][]string { return unknownkey.Paths(p.Unknown) }

// Proc runs Generator on the Template its in decodes to and stages the
// files in its out, as base.Proc does. Its stale check covers the Go files
// of its output directory.
//
//	Param(ProcParamPartial) error
//	In(*fileloader.Document)
//	Out(*overlayfs.Fs)
var Proc = base.Proc[ProcParamPartial, ProcParam, *fileloader.Document, Input]{
	Generator: Generator,
	Default:   DefaultProcParam,
	Check:     checkParam,
	Input:     procInput,
}

// NewProc returns Proc. It is the constructor of the kind
// contextkey.generate in workflow.DefaultRegistry.
func NewProc() workflow.Proc { return Proc }

func checkParam(p ProcParam) error {
	if !fs.ValidPath(p.Dir) {
		return fmt.Errorf("contextkey: invalid dir %q", p.Dir)
	}

	return paramOptions(p).validate()
}

func paramOptions(p ProcParam) Options {
	return Options{Package: p.Package, KeyTypeFunc: p.KeyTypeFunc}
}

func procInput(
	_ context.Context,
	p ProcParam,
	doc *fileloader.Document,
	_ *overlayfs.Fs,
) (Input, base.Scope, error) {
	tmpl, err := decodeTemplate(doc.Data)
	if err != nil {
		return Input{}, base.Scope{}, fmt.Errorf("contextkey: %s: %w", doc.Path, err)
	}

	in := Input{Path: doc.Path, Template: tmpl, Dir: p.Dir, Options: paramOptions(p)}

	return in, base.Scope{Dirs: []string{p.Dir}}, nil
}

// decodeTemplate converts the format-neutral data fileloader decodes into
// a Template with encoding/json/v2. Keys match the JSON names of the fields
// of Template and Entry case-sensitively. Unknown keys are captured in the
// Unknown fields of Template and Entry.
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
