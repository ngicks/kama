package metadata

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/ngicks/kama/kamalib/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

// Document is a decoded metadata file.
type Document struct {
	// Path is the file that was decoded, relative to the project root.
	Path string
	// Data is the decoded content: maps, slices and scalars.
	Data any
}

// ProcParam configures Proc.
type ProcParam struct {
	// Base is the file path without extension, relative to the project root.
	Base string `json:"base"`
	// Formats are tried in order; earlier entries take priority.
	Formats []Format `json:"formats"`
}

// Proc decodes the first file of a PathInput found in the project.
//
//	In(*overlayfs.Fs)
//	Out(*Document)
type Proc struct{}

func (Proc) InTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*overlayfs.Fs]()}
}

func (Proc) OutTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*Document]()}
}

func (Proc) BuildContext(
	_ context.Context,
	param workflow.Param,
	_ bool,
) (workflow.Instance, error) {
	var p ProcParam
	if err := param.Decode(&p); err != nil {
		return nil, err
	}

	if p.Base == "" {
		return nil, errors.New("metadata: base is required")
	}

	in := PathInput(p)
	if len(in.Formats) == 0 {
		in.Formats = []Format{FormatYaml, FormatJson, FormatToml}
	}

	// Candidates reports unknown formats without touching the filesystem.
	if _, err := in.Candidates(); err != nil {
		return nil, fmt.Errorf("metadata: %w", err)
	}

	return &instance{input: in}, nil
}

type instance struct {
	input PathInput
	fs    *overlayfs.Fs
	out   *Document
}

func (i *instance) In(fs *overlayfs.Fs) { i.fs = fs }

func (i *instance) Out(out *Document) { i.out = out }

func (i *instance) Run(context.Context) error {
	name, err := Load(i.fs.FS(), i.input, &i.out.Data)
	if err != nil {
		return err
	}

	i.out.Path = name
	return nil
}
