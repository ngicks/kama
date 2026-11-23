// Package fileloader provides the proc that decodes a metadata file from an
// overlayfs.Fs.
package fileloader

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() { workflow.Register("file.load", NewProc) }

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
	Formats []fileloader.Format `json:"formats"`
	// Unknown captures the keys of a param that name no field.
	Unknown map[string]any `json:",embed"`
}

// UnknownKeys implements workflow.UnknownKeyer.
func (p *ProcParam) UnknownKeys() [][]string { return workflow.UnknownKeys(p.Unknown) }

// Proc decodes the first file of a fileloader.PathInput found in the project.
//
//	Param(ProcParam) error
//	In(*overlayfs.Fs)
//	Out()
//	AsInput() *Document
type Proc struct{}

// NewProc returns a new Proc.
func NewProc() workflow.Proc { return Proc{} }

func (Proc) ParamType() reflect.Type { return reflect.TypeFor[ProcParam]() }

func (Proc) InTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*overlayfs.Fs]()}
}

func (Proc) OutTypes() []reflect.Type { return nil }

func (Proc) AsInputType() reflect.Type { return reflect.TypeFor[*Document]() }

func (Proc) Instantiate(context.Context, bool) (workflow.Instance, error) {
	return &instance{}, nil
}

func (Proc) CleanUp(context.Context) error { return nil }

type instance struct {
	input fileloader.PathInput
	fs    *overlayfs.Fs
	doc   Document
}

func (i *instance) Param(p ProcParam) error {
	if p.Base == "" {
		return errors.New("fileloader: base is required")
	}

	in := fileloader.PathInput(p)
	if len(in.Formats) == 0 {
		in.Formats = []fileloader.Format{
			fileloader.FormatYaml,
			fileloader.FormatJson,
			fileloader.FormatToml,
		}
	}

	// Candidates reports unknown formats without touching the filesystem.
	if _, err := in.Candidates(); err != nil {
		return fmt.Errorf("fileloader: %w", err)
	}

	i.input = in

	return nil
}

func (i *instance) In(fs *overlayfs.Fs) { i.fs = fs }

func (i *instance) Out() {}

func (i *instance) Run(context.Context, workflow.Reporter) error {
	name, err := fileloader.Load(i.fs.FS(), i.input, &i.doc.Data)
	if err != nil {
		return err
	}

	i.doc.Path = name

	return nil
}

func (i *instance) AsInput() *Document { return &i.doc }
