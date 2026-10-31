// Package metadata provides the proc that decodes a metadata file from an
// overlayfs.Fs.
package metadata

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/ngicks/kama/kamalib/pkg/metadata"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
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
	// Formats are tried in order; earlier entries take priority. Empty means
	// yaml, json, toml.
	Formats []metadata.Format `json:"formats"`
}

// Proc decodes the first file of a metadata.PathInput found in the project.
//
//	Param(ProcParam) error
//	In(*overlayfs.Fs)
//	Out()
//	AsInput() *Document
type Proc struct{}

func (Proc) ParamType() reflect.Type { return reflect.TypeFor[ProcParam]() }

func (Proc) InTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*overlayfs.Fs]()}
}

func (Proc) OutTypes() []reflect.Type { return nil }

func (Proc) AsInputType() reflect.Type { return reflect.TypeFor[*Document]() }

func (Proc) Instantiate(context.Context, bool) (workflow.Instance, error) {
	return &instance{}, nil
}

type instance struct {
	input metadata.PathInput
	fs    *overlayfs.Fs
	doc   Document
}

func (i *instance) Param(p ProcParam) error {
	if p.Base == "" {
		return errors.New("metadata: base is required")
	}

	in := metadata.PathInput(p)
	if len(in.Formats) == 0 {
		in.Formats = []metadata.Format{
			metadata.FormatYaml,
			metadata.FormatJson,
			metadata.FormatToml,
		}
	}

	// Candidates reports unknown formats without touching the filesystem.
	if _, err := in.Candidates(); err != nil {
		return fmt.Errorf("metadata: %w", err)
	}

	i.input = in
	return nil
}

func (i *instance) In(fs *overlayfs.Fs) { i.fs = fs }

func (i *instance) Out() {}

func (i *instance) Run(context.Context) error {
	name, err := metadata.Load(i.fs.FS(), i.input, &i.doc.Data)
	if err != nil {
		return err
	}

	i.doc.Path = name
	return nil
}

func (i *instance) AsInput() *Document { return &i.doc }
